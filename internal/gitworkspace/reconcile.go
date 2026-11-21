package gitworkspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"stacklab/internal/atomicfile"
	"stacklab/internal/limitedio"
)

var ErrReconcileBlocked = errors.New("reconciliation requires clean diverged commits")
var ErrDraftExists = errors.New("a reconciliation draft already exists")
var ErrDraftStale = errors.New("reconciliation draft changed or became stale")
var ErrResolutionInvalid = errors.New("invalid conflict resolution")

const reconcileStateLimit = 8 * 1024 * 1024

type ReconcileRequest struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}
type ReconcileFileRequest struct {
	ReconcileRequest
	Path string `json:"path"`
}
type ResolveRequest struct {
	ReconcileFileRequest
	Choice  string `json:"choice"`
	Content string `json:"content"`
}
type ReconcileConflict struct {
	Path     string `json:"path"`
	Resolved bool   `json:"resolved"`
}
type ReconcileResponse struct {
	ID            string              `json:"id"`
	Revision      int                 `json:"revision"`
	ExpectedState string              `json:"expected_state"`
	Branch        string              `json:"branch"`
	LocalCommit   string              `json:"local_commit"`
	RemoteCommit  string              `json:"remote_commit"`
	CreatedAt     time.Time           `json:"created_at"`
	Files         []IncomingFile      `json:"files"`
	Conflicts     []ReconcileConflict `json:"conflicts"`
	Ready         bool                `json:"ready"`
	Applied       bool                `json:"applied"`
}
type ReconcileEnvelope struct {
	Draft *ReconcileResponse `json:"draft"`
}
type ReconcileVersion struct {
	Exists bool    `json:"exists"`
	Mode   string  `json:"mode"`
	Size   int64   `json:"size"`
	Text   *string `json:"text"`
}
type ReconcileFileResponse struct {
	Path      string           `json:"path"`
	Base      ReconcileVersion `json:"base"`
	Local     ReconcileVersion `json:"local"`
	Remote    ReconcileVersion `json:"remote"`
	Editable  bool             `json:"editable"`
	Choice    string           `json:"choice"`
	Content   string           `json:"content"`
	Diff      string           `json:"diff"`
	Truncated bool             `json:"truncated"`
}
type mergeEntry struct{ Mode, OID string }
type mergeResolution struct{ Choice, Content string }
type reconcileState struct {
	ID           string
	Revision     int
	Review       IncomingResponse
	CreatedAt    time.Time
	Conflicts    map[string][3]mergeEntry
	Resolutions  map[string]mergeResolution
	ResultCommit string
}

func (s *Service) reconcileRoot() string {
	return filepath.Join(filepath.Dir(s.syncStatePath), "git-reconcile")
}
func (s *Service) draftDir(d *reconcileState) string  { return filepath.Join(s.reconcileRoot(), d.ID) }
func (s *Service) draftRepo(d *reconcileState) string { return filepath.Join(s.draftDir(d), "repo") }

// The scratch repository deliberately inherits neither host/repository merge
// drivers nor filters, hooks, signing, rerere or credentials. It never writes to
// the managed checkout. Blob contents travel on stdin, never in command arguments.
func (s *Service) draftGit(ctx context.Context, dir, index string, input []byte, limit int64, args ...string) ([]byte, bool, error) {
	base := []string{"-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "core.attributesFile=/dev/null", "-c", "core.autocrlf=false", "-c", "user.name=Stacklab", "-c", "user.email=stacklab@localhost", "-c", "commit.gpgSign=false", "-c", "submodule.recurse=false", "--literal-pathspecs"}
	cmd := exec.CommandContext(ctx, s.gitBinary, append(base, args...)...)
	cmd.WaitDelay = 5 * time.Second
	var environment []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			environment = append(environment, e)
		}
	}
	cmd.Env = append(gitCommandEnvironment(environment, index), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_ATTR_NOSYSTEM=1")
	cmd.Stdin = bytes.NewReader(input)
	out, stderr := limitedio.NewBuffer(limit), limitedio.NewBuffer(gitOutputLimit)
	cmd.Stdout, cmd.Stderr = out, stderr
	err := cmd.Run()
	if stderr.Err() != nil {
		return nil, false, ErrContentTooLarge
	}
	return out.Bytes(), out.Err() != nil, err
}
func (s *Service) draftCommand(ctx context.Context, dir, index string, input []byte, args ...string) ([]byte, error) {
	out, truncated, err := s.draftGit(ctx, dir, index, input, gitOutputLimit, args...)
	if truncated {
		return nil, ErrContentTooLarge
	}
	if err != nil {
		return nil, safeSyncError(nil, err)
	}
	return out, nil
}
func (s *Service) loadDraft() (*reconcileState, error) {
	data, err := limitedio.ReadFile(filepath.Join(s.reconcileRoot(), "current.json"), reconcileStateLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrRemoteFailed
	}
	var d reconcileState
	if json.Unmarshal(data, &d) != nil || len(d.ID) != 32 || d.Revision < 1 {
		return nil, ErrRemoteFailed
	}
	if _, err = hex.DecodeString(d.ID); err != nil {
		return nil, ErrRemoteFailed
	}
	return &d, nil
}
func (s *Service) saveDraft(d *reconcileState) error {
	data, err := json.Marshal(d)
	if err != nil {
		return ErrRemoteFailed
	}
	if len(data) > reconcileStateLimit {
		return ErrContentTooLarge
	}
	if err = atomicfile.WriteBytesMode(filepath.Join(s.reconcileRoot(), "current.json"), data, ".draft-*", 0600); err != nil {
		return ErrRemoteFailed
	}
	return nil
}
func (s *Service) requireDraft(r ReconcileRequest) (*reconcileState, error) {
	d, err := s.loadDraft()
	if err != nil {
		return nil, err
	}
	if d == nil || d.ID != r.ID || d.Revision != r.Revision {
		return nil, ErrDraftStale
	}
	return d, nil
}

func (s *Service) PrepareReconcile(ctx context.Context, r PullRequest) (ReconcileResponse, error) {
	if !s.mutationMu.TryLock() {
		return ReconcileResponse{}, ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	old, err := s.loadDraft()
	if err != nil {
		return ReconcileResponse{}, err
	}
	if old != nil {
		return ReconcileResponse{}, ErrDraftExists
	}
	review, err := s.incoming(ctx, false)
	if err != nil {
		return ReconcileResponse{}, err
	}
	if review.ExpectedState == "" || review.ExpectedState != r.ExpectedState {
		return ReconcileResponse{}, ErrDraftStale
	}
	if !review.CanReconcile {
		return ReconcileResponse{}, ErrReconcileBlocked
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return ReconcileResponse{}, ErrRemoteFailed
	}
	d := &reconcileState{ID: hex.EncodeToString(random[:]), Revision: 1, Review: review, CreatedAt: time.Now().UTC(), Conflicts: map[string][3]mergeEntry{}, Resolutions: map[string]mergeResolution{}}
	if err = os.MkdirAll(s.reconcileRoot(), 0700); err != nil {
		return ReconcileResponse{}, ErrRemoteFailed
	}
	if err = os.Mkdir(s.draftDir(d), 0700); err != nil {
		return ReconcileResponse{}, ErrRemoteFailed
	}
	saved := false
	defer func() {
		if !saved {
			_ = os.RemoveAll(s.draftDir(d))
		}
	}()
	repo := s.draftRepo(d)
	if _, err = s.draftCommand(ctx, s.draftDir(d), "", nil, "init", "--quiet", "--template=", repo); err != nil {
		return ReconcileResponse{}, err
	}
	if _, err = s.draftCommand(ctx, repo, "", nil, "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--no-auto-maintenance", "--", s.workspaceRoot, review.HeadCommit+":refs/heads/local", review.TargetCommit+":refs/heads/remote"); err != nil {
		return ReconcileResponse{}, err
	}
	if _, err = s.draftCommand(ctx, repo, "", nil, "checkout", "--quiet", "--detach", review.HeadCommit); err != nil {
		return ReconcileResponse{}, err
	}
	_, truncated, mergeErr := s.draftGit(ctx, repo, "", nil, gitOutputLimit, "merge", "--no-commit", "--no-ff", "--no-edit", "--no-autostash", review.TargetCommit)
	if truncated {
		return ReconcileResponse{}, ErrContentTooLarge
	}
	if mergeErr != nil {
		var exit *exec.ExitError
		if !errors.As(mergeErr, &exit) || exit.ExitCode() != 1 {
			return ReconcileResponse{}, ErrRemoteFailed
		}
	}
	unmerged, err := s.draftCommand(ctx, repo, "", nil, "ls-files", "--unmerged", "-z")
	if err != nil {
		return ReconcileResponse{}, err
	}
	for _, record := range strings.Split(string(unmerged), "\x00") {
		if record == "" {
			continue
		}
		metadata, path, ok := strings.Cut(record, "\t")
		parts := strings.Fields(metadata)
		if !ok || len(parts) != 3 {
			return ReconcileResponse{}, ErrRemoteFailed
		}
		stage, e := strconv.Atoi(parts[2])
		if e != nil || stage < 1 || stage > 3 || (parts[0] != "100644" && parts[0] != "100755" && parts[0] != "120000") {
			return ReconcileResponse{}, ErrReconcileBlocked
		}
		entries := d.Conflicts[path]
		entries[stage-1] = mergeEntry{Mode: parts[0], OID: parts[1]}
		d.Conflicts[path] = entries
	}
	if len(d.Conflicts) > 100 {
		return ReconcileResponse{}, ErrContentTooLarge
	}
	if mergeErr != nil && len(d.Conflicts) == 0 {
		return ReconcileResponse{}, ErrRemoteFailed
	}
	if err = copyIndex(filepath.Join(repo, ".git", "index"), filepath.Join(s.draftDir(d), "initial-index")); err != nil {
		return ReconcileResponse{}, ErrRemoteFailed
	}
	response, err := s.describeDraft(ctx, d)
	if err != nil {
		return ReconcileResponse{}, err
	}
	if err = s.saveDraft(d); err != nil {
		return ReconcileResponse{}, err
	}
	saved = true
	return response, nil
}

// Rebuild an index from immutable merge stages and the durable resolutions. A
// failed resolution/save cannot leave a partially modified draft behind.
func (s *Service) candidateIndex(ctx context.Context, d *reconcileState) (string, func(), error) {
	f, err := os.CreateTemp(s.draftDir(d), ".candidate-*")
	if err != nil {
		return "", nil, ErrRemoteFailed
	}
	index := f.Name()
	_ = f.Close()
	_ = os.Remove(index)
	cleanup := func() { _ = os.Remove(index); _ = os.Remove(index + ".lock") }
	if err = copyIndex(filepath.Join(s.draftDir(d), "initial-index"), index); err != nil {
		cleanup()
		return "", nil, ErrRemoteFailed
	}
	paths := make([]string, 0, len(d.Resolutions))
	for path := range d.Resolutions {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		r := d.Resolutions[path]
		entries := d.Conflicts[path]
		var entry mergeEntry
		switch r.Choice {
		case "local":
			entry = entries[1]
		case "remote":
			entry = entries[2]
		case "delete":
		case "edit":
			entry = entries[1]
			if entry.OID == "" {
				entry = entries[2]
			}
			var out []byte
			out, err = s.draftCommand(ctx, s.draftRepo(d), index, []byte(r.Content), "hash-object", "-w", "--stdin")
			entry.OID = strings.TrimSpace(string(out))
		default:
			err = ErrResolutionInvalid
		}
		if err == nil {
			if entry.OID == "" {
				_, err = s.draftCommand(ctx, s.draftRepo(d), index, nil, "update-index", "--force-remove", "--", path)
			} else {
				_, err = s.draftCommand(ctx, s.draftRepo(d), index, nil, "update-index", "--add", "--cacheinfo", entry.Mode, entry.OID, path)
			}
		}
		if err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return index, cleanup, nil
}
func (s *Service) describeDraft(ctx context.Context, d *reconcileState) (ReconcileResponse, error) {
	r := ReconcileResponse{ID: d.ID, Revision: d.Revision, ExpectedState: d.Review.ExpectedState, Branch: d.Review.Branch, LocalCommit: d.Review.HeadCommit, RemoteCommit: d.Review.TargetCommit, CreatedAt: d.CreatedAt, Files: []IncomingFile{}, Conflicts: []ReconcileConflict{}, Ready: true}
	for path := range d.Conflicts {
		_, resolved := d.Resolutions[path]
		r.Conflicts = append(r.Conflicts, ReconcileConflict{Path: path, Resolved: resolved})
		if !resolved {
			r.Ready = false
		}
	}
	sort.Slice(r.Conflicts, func(i, j int) bool { return r.Conflicts[i].Path < r.Conflicts[j].Path })
	index, cleanup, err := s.candidateIndex(ctx, d)
	if err != nil {
		return r, err
	}
	defer cleanup()
	files, err := s.draftCommand(ctx, s.draftRepo(d), index, nil, "diff", "--cached", "--name-status", "--no-renames", "-z", d.Review.HeadCommit, "--")
	if err != nil {
		return r, err
	}
	parts := strings.Split(strings.TrimSuffix(string(files), "\x00"), "\x00")
	if len(parts) > 4000 {
		return r, ErrContentTooLarge
	}
	for i := 0; i+1 < len(parts); i += 2 {
		path := parts[i+1]
		_, stackID, normalized, managed := managedPathContext(path)
		managed = managed && normalized == path
		if !managed {
			stackID = nil
		}
		r.Files = append(r.Files, IncomingFile{Path: path, Status: parts[i], StackID: stackID, Managed: managed})
	}
	if d.ResultCommit != "" {
		_, err := s.syncGit(ctx, "merge-base", "--is-ancestor", d.ResultCommit, "HEAD")
		r.Applied = err == nil
	}
	return r, nil
}
func (s *Service) Reconcile(ctx context.Context) (ReconcileEnvelope, error) {
	if !s.mutationMu.TryLock() {
		return ReconcileEnvelope{}, ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d, err := s.loadDraft()
	if err != nil || d == nil {
		return ReconcileEnvelope{}, err
	}
	r, err := s.describeDraft(ctx, d)
	return ReconcileEnvelope{Draft: &r}, err
}
func (s *Service) draftVersion(ctx context.Context, d *reconcileState, entry mergeEntry) (ReconcileVersion, error) {
	v := ReconcileVersion{Exists: entry.OID != "", Mode: entry.Mode}
	if !v.Exists {
		return v, nil
	}
	out, err := s.draftCommand(ctx, s.draftRepo(d), "", nil, "cat-file", "-s", entry.OID)
	if err != nil {
		return v, err
	}
	v.Size, err = strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return v, ErrRemoteFailed
	}
	if v.Size > diffSizeLimit || (entry.Mode != "100644" && entry.Mode != "100755") {
		return v, nil
	}
	out, err = s.draftCommand(ctx, s.draftRepo(d), "", nil, "cat-file", "blob", entry.OID)
	if err != nil {
		return v, err
	}
	if utf8.Valid(out) && !bytes.ContainsRune(out, 0) {
		text := string(out)
		v.Text = &text
	}
	return v, nil
}
func (s *Service) ReconcileFile(ctx context.Context, r ReconcileFileRequest) (ReconcileFileResponse, error) {
	if !s.mutationMu.TryLock() {
		return ReconcileFileResponse{}, ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d, err := s.requireDraft(r.ReconcileRequest)
	if err != nil {
		return ReconcileFileResponse{}, err
	}
	return s.reconcileFile(ctx, d, r.Path)
}
func (s *Service) reconcileFile(ctx context.Context, d *reconcileState, path string) (ReconcileFileResponse, error) {
	r := ReconcileFileResponse{Path: path}
	description, err := s.describeDraft(ctx, d)
	if err != nil {
		return r, err
	}
	_, isConflict := d.Conflicts[path]
	found := isConflict
	for _, file := range description.Files {
		if file.Path == path {
			found = true
		}
	}
	if !found {
		return r, ErrNotFound
	}
	if isConflict {
		entries := d.Conflicts[path]
		for i, value := range []*ReconcileVersion{&r.Base, &r.Local, &r.Remote} {
			*value, err = s.draftVersion(ctx, d, entries[i])
			if err != nil {
				return r, err
			}
		}
		_, _, normalized, managed := managedPathContext(path)
		r.Editable = managed && normalized == path
		for _, value := range []ReconcileVersion{r.Base, r.Local, r.Remote} {
			if value.Exists && value.Text == nil {
				r.Editable = false
			}
		}
		resolution := d.Resolutions[path]
		r.Choice, r.Content = resolution.Choice, resolution.Content
	}
	index, cleanup, err := s.candidateIndex(ctx, d)
	if err != nil {
		return r, err
	}
	defer cleanup()
	out, truncated, err := s.draftGit(ctx, s.draftRepo(d), index, nil, diffSizeLimit, "diff", "--cached", "--no-ext-diff", "--no-textconv", "--no-renames", d.Review.HeadCommit, "--", path)
	if err != nil {
		return r, safeSyncError(nil, err)
	}
	r.Diff, r.Truncated = string(out), truncated
	return r, nil
}
func (s *Service) ResolveReconcile(ctx context.Context, r ResolveRequest) (ReconcileResponse, error) {
	if !s.mutationMu.TryLock() {
		return ReconcileResponse{}, ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d, err := s.requireDraft(r.ReconcileRequest)
	if err != nil {
		return ReconcileResponse{}, err
	}
	if d.ResultCommit != "" {
		current, err := s.describeDraft(ctx, d)
		if err != nil {
			return ReconcileResponse{}, err
		}
		if current.Applied {
			return ReconcileResponse{}, ErrDraftStale
		}
		// A native checkout may have been refused (for example by index.lock).
		// Editing that still-private result invalidates its prepared commit.
		d.ResultCommit = ""
	}
	if _, ok := d.Conflicts[r.Path]; !ok {
		return ReconcileResponse{}, ErrNotFound
	}
	switch r.Choice {
	case "local", "remote", "delete":
		r.Content = ""
	case "edit":
		file, err := s.reconcileFile(ctx, d, r.Path)
		if err != nil {
			return ReconcileResponse{}, err
		}
		if !file.Editable || !utf8.ValidString(r.Content) || strings.ContainsRune(r.Content, 0) {
			return ReconcileResponse{}, ErrResolutionInvalid
		}
		if int64(len(r.Content)) > diffSizeLimit {
			return ReconcileResponse{}, ErrContentTooLarge
		}
	default:
		return ReconcileResponse{}, ErrResolutionInvalid
	}
	d.Resolutions[r.Path] = mergeResolution{Choice: r.Choice, Content: r.Content}
	d.Revision++
	response, err := s.describeDraft(ctx, d)
	if err != nil {
		return response, err
	}
	if err = s.saveDraft(d); err != nil {
		return ReconcileResponse{}, err
	}
	return response, nil
}
func (s *Service) DiscardReconcile(ctx context.Context, r ReconcileRequest) error {
	if !s.mutationMu.TryLock() {
		return ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	d, err := s.requireDraft(r)
	if err != nil {
		return err
	}
	// Only this private draft is removed; never run clean/reset against the checkout.
	if err = os.RemoveAll(s.draftDir(d)); err != nil {
		return ErrRemoteFailed
	}
	if err = os.Remove(filepath.Join(s.reconcileRoot(), "current.json")); err != nil {
		return ErrRemoteFailed
	}
	return nil
}
func (s *Service) ApplyReconcile(ctx context.Context, r ReconcileRequest) (PullResponse, error) {
	if !s.mutationMu.TryLock() {
		return PullResponse{}, ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return PullResponse{}, err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	d, err := s.requireDraft(r)
	if err != nil {
		return PullResponse{}, err
	}
	description, err := s.describeDraft(ctx, d)
	if err != nil {
		return PullResponse{}, err
	}
	if !description.Ready || description.Applied {
		return PullResponse{}, ErrDraftStale
	}
	review, err := s.incoming(ctx, false)
	if err != nil {
		return PullResponse{}, err
	}
	if review.ExpectedState != d.Review.ExpectedState || !review.CanReconcile {
		return PullResponse{}, ErrDraftStale
	}
	index, cleanup, err := s.candidateIndex(ctx, d)
	if err != nil {
		return PullResponse{}, err
	}
	defer cleanup()
	if d.ResultCommit == "" {
		tree, err := s.draftCommand(ctx, s.draftRepo(d), index, nil, "write-tree")
		if err != nil {
			return PullResponse{}, err
		}
		commit, err := s.draftCommand(ctx, s.draftRepo(d), index, nil, "commit-tree", strings.TrimSpace(string(tree)), "-p", review.HeadCommit, "-p", review.TargetCommit, "-m", "Reconcile configuration in Stacklab")
		if err != nil {
			return PullResponse{}, err
		}
		d.ResultCommit = strings.TrimSpace(string(commit))
		if _, err = s.draftCommand(ctx, s.draftRepo(d), "", nil, "update-ref", "refs/heads/result", d.ResultCommit); err != nil {
			return PullResponse{}, err
		}
		if err = s.saveDraft(d); err != nil {
			return PullResponse{}, err
		}
	}
	if _, err = s.syncGit(ctx, "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--no-auto-maintenance", "--refmap=", "--", s.draftRepo(d), d.ResultCommit); err != nil {
		return PullResponse{}, err
	}
	receipt := PullReceipt{Branch: review.Branch, BaseCommit: review.HeadCommit, HeadCommit: d.ResultCommit, DownloadedAt: time.Now().UTC(), Files: description.Files}
	journal := syncJournal{Last: review.LastPull, Pending: &receipt}
	if err = s.saveSyncJournal(journal); err != nil {
		return PullResponse{}, err
	}
	if _, err = s.syncGit(ctx, "merge", "--ff-only", "--no-autostash", "--no-overwrite-ignore", "--no-edit", "--no-stat", d.ResultCommit); err != nil {
		return PullResponse{}, err
	}
	head, err := s.headCommit(ctx)
	if err != nil || head != d.ResultCommit {
		return PullResponse{}, ErrDraftStale
	}
	journal.Last, journal.Pending = &receipt, nil
	_ = s.saveSyncJournal(journal)
	return PullResponse{Updated: true, Receipt: receipt}, nil
}
