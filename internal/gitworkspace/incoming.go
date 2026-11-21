package gitworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stacklab/internal/atomicfile"
	"stacklab/internal/limitedio"
)

var ErrPullBlocked = errors.New("Git download is blocked")
var ErrRemoteFailed = errors.New("Git remote operation failed")

type IncomingFile struct {
	Path    string  `json:"path"`
	Status  string  `json:"status"`
	StackID *string `json:"stack_id"`
	Managed bool    `json:"managed"`
}
type PullReceipt struct {
	Branch       string         `json:"branch"`
	BaseCommit   string         `json:"base_commit"`
	HeadCommit   string         `json:"head_commit"`
	DownloadedAt time.Time      `json:"downloaded_at"`
	Files        []IncomingFile `json:"files"`
}
type IncomingResponse struct {
	Available     bool           `json:"available"`
	Reason        string         `json:"reason,omitempty"`
	Branch        string         `json:"branch"`
	UpstreamName  string         `json:"upstream_name"`
	HeadCommit    string         `json:"head_commit"`
	TargetCommit  string         `json:"target_commit"`
	AheadCount    int            `json:"ahead_count"`
	BehindCount   int            `json:"behind_count"`
	CheckedAt     time.Time      `json:"checked_at"`
	CanPull       bool           `json:"can_pull"`
	BlockedReason string         `json:"blocked_reason,omitempty"`
	ExpectedState string         `json:"expected_state"`
	Files         []IncomingFile `json:"files"`
	LastPull      *PullReceipt   `json:"last_pull,omitempty"`
}
type IncomingDiffRequest struct {
	Path          string `json:"path"`
	ExpectedState string `json:"expected_state"`
}
type IncomingDiffResponse struct {
	Path      string `json:"path"`
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}
type PullRequest struct {
	ExpectedState string `json:"expected_state"`
}
type PullResponse struct {
	Updated bool        `json:"updated"`
	Receipt PullReceipt `json:"receipt"`
}
type syncJournal struct {
	Last    *PullReceipt `json:"last,omitempty"`
	Pending *PullReceipt `json:"pending,omitempty"`
}

// Remote diagnostics can contain credential-bearing URLs. Keep them out of
// errors, API responses and server logs, including failures during local preview.
func safeSyncError(stderr []byte, err error) error {
	for _, known := range []error{ErrAuthFailed, ErrHostKeyFailed, ErrPermissionDenied, ErrContentTooLarge, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return known
		}
	}
	classified := classifyGitPushError(nil, stderr, err)
	for _, known := range []error{ErrAuthFailed, ErrHostKeyFailed, ErrPermissionDenied} {
		if errors.Is(classified, known) {
			return known
		}
	}
	return ErrRemoteFailed
}
func (s *Service) syncGit(ctx context.Context, args ...string) ([]byte, error) {
	args = append([]string{"-c", "core.hooksPath=/dev/null", "-c", "submodule.recurse=false"}, args...)
	out, stderr, err := s.runGit(ctx, args...)
	if err != nil {
		return nil, safeSyncError(stderr, err)
	}
	return out, nil
}

func (s *Service) Incoming(ctx context.Context) (IncomingResponse, error) {
	if !s.mutationMu.TryLock() {
		return IncomingResponse{}, ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return s.incoming(ctx, true)
}

func (s *Service) incoming(ctx context.Context, fetch bool) (IncomingResponse, error) {
	result := IncomingResponse{Files: []IncomingFile{}, CheckedAt: time.Now().UTC()}
	_, available, reason, err := s.repoRoot(ctx)
	if err != nil {
		return result, safeSyncError(nil, err)
	}
	result.Available, result.Reason = available, reason
	if !available {
		return result, nil
	}
	branch, err := s.syncGit(ctx, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		result.Reason = "detached_head"
		return result, nil
	}
	branchRef := strings.TrimSpace(string(branch))
	result.Branch = strings.TrimPrefix(branchRef, "refs/heads/")
	journal, err := s.loadSyncJournal()
	if err != nil {
		return result, err
	}
	result.LastPull = journal.Last
	if journal.Pending != nil && journal.Pending.Branch == result.Branch {
		if _, err := s.syncGit(ctx, "merge-base", "--is-ancestor", journal.Pending.HeadCommit, "HEAD"); err == nil {
			result.LastPull = journal.Pending
		}
	}
	refs, err := s.syncGit(ctx, "for-each-ref", "--format=%(upstream)%00%(upstream:remotename)%00%(upstream:remoteref)", branchRef)
	if err != nil {
		return result, err
	}
	parts := strings.Split(strings.TrimSuffix(string(refs), "\n"), "\x00")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "refs/remotes/") || parts[1] == "" || parts[1] == "." || !strings.HasPrefix(parts[2], "refs/heads/") {
		result.Reason = "upstream_not_configured"
		return result, nil
	}
	result.UpstreamName = strings.TrimPrefix(parts[0], "refs/remotes/")
	if fetch {
		if s.hasOperationInProgress(ctx) {
			return result, ErrOperationInProgress
		}
		_, err = s.syncGit(ctx, "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--no-auto-maintenance", "--refmap=", "--", parts[1], "+"+parts[2]+":"+parts[0])
		if err != nil {
			return result, err
		}
		result.CheckedAt = time.Now().UTC()
	}
	for _, target := range []struct {
		ref   string
		value *string
	}{{"HEAD", &result.HeadCommit}, {parts[0], &result.TargetCommit}} {
		out, err := s.syncGit(ctx, "rev-parse", "--verify", target.ref+"^{commit}")
		if err != nil {
			return result, err
		}
		*target.value = strings.TrimSpace(string(out))
	}
	counts, err := s.syncGit(ctx, "rev-list", "--left-right", "--count", result.HeadCommit+"..."+result.TargetCommit)
	if err != nil {
		return result, err
	}
	if _, err = fmt.Sscanf(string(counts), "%d %d", &result.AheadCount, &result.BehindCount); err != nil {
		return result, ErrRemoteFailed
	}
	digest := sha256.Sum256([]byte(branchRef + "\x00" + result.UpstreamName + "\x00" + result.HeadCommit + "\x00" + result.TargetCommit))
	result.ExpectedState = hex.EncodeToString(digest[:])
	if result.BehindCount == 0 {
		result.BlockedReason = "up_to_date"
		return result, nil
	}
	if result.AheadCount > 0 {
		result.BlockedReason = "diverged"
		return result, nil
	}
	changes, err := s.syncGit(ctx, "diff", "--name-status", "-z", "--no-renames", result.HeadCommit, result.TargetCommit, "--")
	if err != nil {
		return result, err
	}
	records := strings.Split(strings.TrimSuffix(string(changes), "\x00"), "\x00")
	if len(changes) > 0 && len(records)%2 != 0 {
		return result, ErrRemoteFailed
	}
	for i := 0; i+1 < len(records); i += 2 {
		path := records[i+1]
		_, stackID, normalized, managed := managedPathContext(path)
		managed = managed && normalized == path
		if !managed {
			stackID = nil
		}
		result.Files = append(result.Files, IncomingFile{Path: path, Status: records[i], StackID: stackID, Managed: managed})
	}
	if s.hasOperationInProgress(ctx) {
		result.BlockedReason = "operation_in_progress"
		return result, nil
	}
	// Check the whole checkout, including untracked files outside managed roots.
	dirty, err := s.syncGit(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return result, err
	}
	if len(dirty) > 0 {
		result.BlockedReason = "local_changes"
		return result, nil
	}
	indexEntries, err := s.syncGit(ctx, "ls-files", "-v", "-z")
	if err != nil {
		return result, err
	}
	for _, entry := range strings.Split(string(indexEntries), "\x00") {
		if len(entry) > 0 && (entry[0] == 'S' || entry[0] >= 'a' && entry[0] <= 'z') {
			result.BlockedReason = "index_flags"
			return result, nil
		}
	}
	// Submodules need a separate lifecycle; do not silently leave them at an old revision.
	tree, err := s.syncGit(ctx, "ls-tree", "-r", "-z", result.TargetCommit)
	if err != nil {
		return result, err
	}
	for _, entry := range strings.Split(string(tree), "\x00") {
		if strings.HasPrefix(entry, "160000 ") {
			result.BlockedReason = "submodules"
			return result, nil
		}
	}
	result.CanPull = true
	return result, nil
}

func (s *Service) IncomingDiff(ctx context.Context, request IncomingDiffRequest) (IncomingDiffResponse, error) {
	if request.Path == "" || len(request.ExpectedState) != 64 {
		return IncomingDiffResponse{}, ErrValidation
	}
	if !s.mutationMu.TryLock() {
		return IncomingDiffResponse{}, ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	review, err := s.incoming(ctx, false)
	if err != nil {
		return IncomingDiffResponse{}, err
	}
	if review.ExpectedState != request.ExpectedState {
		return IncomingDiffResponse{}, ErrConflict
	}
	found := false
	for _, file := range review.Files {
		if file.Path == request.Path {
			found = true
			break
		}
	}
	if !found {
		return IncomingDiffResponse{}, ErrNotFound
	}
	out, stderr, truncated, err := s.runGitWithLimits(ctx, diffSizeLimit, gitOutputLimit, "-c", "core.hooksPath=/dev/null", "--literal-pathspecs", "diff", "--no-ext-diff", "--no-textconv", "--no-renames", review.HeadCommit, review.TargetCommit, "--", request.Path)
	if err != nil {
		return IncomingDiffResponse{}, safeSyncError(stderr, err)
	}
	return IncomingDiffResponse{Path: request.Path, Diff: string(out), Truncated: truncated}, nil
}

func (s *Service) Pull(ctx context.Context, request PullRequest) (PullResponse, error) {
	if len(request.ExpectedState) != 64 {
		return PullResponse{}, ErrValidation
	}
	if !s.mutationMu.TryLock() {
		return PullResponse{}, ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return PullResponse{}, err
	}
	// Finish a checkout and its receipt even if the browser disconnects.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	review, err := s.incoming(ctx, false)
	if err != nil {
		return PullResponse{}, err
	}
	if review.ExpectedState != request.ExpectedState {
		return PullResponse{}, ErrConflict
	}
	if !review.CanPull {
		return PullResponse{}, ErrPullBlocked
	}
	receipt := PullReceipt{Branch: review.Branch, BaseCommit: review.HeadCommit, HeadCommit: review.TargetCommit, DownloadedAt: time.Now().UTC(), Files: review.Files}
	journal := syncJournal{Last: review.LastPull, Pending: &receipt}
	if err := s.saveSyncJournal(journal); err != nil {
		return PullResponse{}, err
	}
	// Native Git owns index/ref locks and protects tracked, untracked and ignored
	// files. Never force, stash, recurse into submodules, or run local hooks.
	_, err = s.syncGit(ctx, "merge", "--ff-only", "--no-autostash", "--no-overwrite-ignore", "--no-edit", "--no-stat", review.TargetCommit)
	if err != nil {
		return PullResponse{}, err
	}
	head, err := s.headCommit(ctx)
	if err != nil || head != review.TargetCommit {
		return PullResponse{}, ErrConflict
	}
	// A pending receipt remains recoverable against HEAD if final persistence fails.
	journal.Last, journal.Pending = &receipt, nil
	// Completion is already proven by HEAD and the durable prepared receipt.
	// Failure to compact that receipt must not turn a successful checkout into
	// a failed operation; the next review recovers it from Pending instead.
	_ = s.saveSyncJournal(journal)
	return PullResponse{Updated: true, Receipt: receipt}, nil
}

func (s *Service) loadSyncJournal() (syncJournal, error) {
	var journal syncJournal
	data, err := limitedio.ReadFile(s.syncStatePath, gitOutputLimit)
	if errors.Is(err, os.ErrNotExist) {
		return journal, nil
	}
	if err != nil {
		return journal, errors.New("cannot read Git download history")
	}
	if err := json.Unmarshal(data, &journal); err != nil {
		return journal, errors.New("invalid Git download history")
	}
	return journal, nil
}
func (s *Service) saveSyncJournal(journal syncJournal) error {
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.syncStatePath), 0700); err == nil {
		err = atomicfile.WriteBytesMode(s.syncStatePath, data, ".git-sync-*", 0600)
	}
	if err != nil {
		return errors.New("cannot save Git download history")
	}
	return nil
}
