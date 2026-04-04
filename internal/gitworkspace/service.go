package gitworkspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"stacklab/internal/config"
	"stacklab/internal/stacks"
)

var (
	ErrUnavailable          = errors.New("git workspace unavailable")
	ErrNotFound             = errors.New("git workspace path not found")
	ErrPathOutsideWorkspace = errors.New("path outside git workspace")
	ErrInvalidManagedPath   = errors.New("path is outside managed roots")
)

const diffSizeLimit = 256 * 1024

type Service struct {
	workspaceRoot string
	gitBinary     string
}

func NewService(cfg config.Config) *Service {
	root := cfg.RootDir
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	return &Service{
		workspaceRoot: root,
		gitBinary:     "git",
	}
}

func (s *Service) Status(ctx context.Context) (StatusResponse, error) {
	base := StatusResponse{
		RepoRoot:     s.workspaceRoot,
		ManagedRoots: []string{string(ScopeStacks), string(ScopeConfig)},
	}

	repoRoot, available, reason, err := s.repoRoot(ctx)
	if err != nil {
		return StatusResponse{}, err
	}
	if !available {
		base.Available = false
		base.Reason = reason
		return base, nil
	}

	base.Available = true
	base.RepoRoot = repoRoot

	if branch, err := s.branch(ctx); err == nil {
		base.Branch = branch
	}
	if headCommit, err := s.headCommit(ctx); err == nil {
		base.HeadCommit = headCommit
	}
	if upstreamName, hasUpstream, err := s.upstream(ctx); err == nil {
		base.HasUpstream = hasUpstream
		if hasUpstream {
			base.UpstreamName = upstreamName
			if ahead, behind, err := s.aheadBehind(ctx); err == nil {
				base.AheadCount = ahead
				base.BehindCount = behind
			}
		}
	}

	items, err := s.statusItems(ctx)
	if err != nil {
		return StatusResponse{}, err
	}
	base.Items = items
	base.Clean = len(items) == 0

	return base, nil
}

func (s *Service) Diff(ctx context.Context, requestedPath string) (DiffResponse, error) {
	normalizedPath, err := normalizeManagedPath(requestedPath)
	if err != nil {
		return DiffResponse{}, err
	}

	status, err := s.Status(ctx)
	if err != nil {
		return DiffResponse{}, err
	}
	if !status.Available {
		return DiffResponse{}, ErrUnavailable
	}

	var item *StatusItem
	for i := range status.Items {
		if status.Items[i].Path == normalizedPath {
			item = &status.Items[i]
			break
		}
	}
	if item == nil {
		return DiffResponse{}, ErrNotFound
	}

	response := DiffResponse{
		Available: true,
		Path:      item.Path,
		Scope:     item.Scope,
		StackID:   item.StackID,
		Status:    item.Status,
		OldPath:   item.OldPath,
	}

	isBinary, err := s.isBinaryDiff(ctx, *item)
	if err != nil {
		return DiffResponse{}, err
	}
	response.IsBinary = isBinary
	if isBinary {
		return response, nil
	}

	diffText, err := s.diffText(ctx, *item)
	if err != nil {
		return DiffResponse{}, err
	}
	if len(diffText) > diffSizeLimit {
		diffText = diffText[:diffSizeLimit]
		response.Truncated = true
	}
	response.Diff = &diffText

	return response, nil
}

func (s *Service) statusItems(ctx context.Context) ([]StatusItem, error) {
	output, _, err := s.runGit(ctx, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--", string(ScopeStacks), string(ScopeConfig))
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}

	items := make([]StatusItem, 0, 16)
	records := bytes.Split(output, []byte{0})
	for i := 0; i < len(records); i++ {
		record := string(records[i])
		if record == "" {
			continue
		}

		switch {
		case strings.HasPrefix(record, "1 "):
			item, parseErr := parseOrdinaryStatusRecord(record)
			if parseErr != nil {
				return nil, parseErr
			}
			if item != nil {
				items = append(items, *item)
			}
		case strings.HasPrefix(record, "2 "):
			if i+1 >= len(records) {
				return nil, errors.New("malformed git rename status record")
			}
			item, parseErr := parseRenameStatusRecord(record, string(records[i+1]))
			if parseErr != nil {
				return nil, parseErr
			}
			i++
			if item != nil {
				items = append(items, *item)
			}
		case strings.HasPrefix(record, "u "):
			item, parseErr := parseUnmergedStatusRecord(record)
			if parseErr != nil {
				return nil, parseErr
			}
			if item != nil {
				items = append(items, *item)
			}
		case strings.HasPrefix(record, "? "):
			item, parseErr := parseUntrackedStatusRecord(record)
			if parseErr != nil {
				return nil, parseErr
			}
			if item != nil {
				items = append(items, *item)
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Scope != items[j].Scope {
			return items[i].Scope < items[j].Scope
		}
		leftGroup := groupingKey(items[i])
		rightGroup := groupingKey(items[j])
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		return items[i].Path < items[j].Path
	})

	return items, nil
}

func (s *Service) repoRoot(ctx context.Context) (string, bool, string, error) {
	if err := os.MkdirAll(s.workspaceRoot, 0o755); err != nil {
		return "", false, "", fmt.Errorf("create git workspace root: %w", err)
	}
	if _, err := exec.LookPath(s.gitBinary); err != nil {
		return s.workspaceRoot, false, "git_not_installed", nil
	}

	output, stderr, err := s.runGit(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		if isNotGitRepository(stderr) {
			return s.workspaceRoot, false, "not_a_git_repository", nil
		}
		return "", false, "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}

	repoRoot := strings.TrimSpace(string(output))
	if resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot); err == nil {
		repoRoot = resolvedRepoRoot
	}
	workspaceRoot, absErr := filepath.Abs(s.workspaceRoot)
	if absErr == nil {
		if resolvedWorkspaceRoot, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
			workspaceRoot = resolvedWorkspaceRoot
		}
		s.workspaceRoot = workspaceRoot
	}
	if repoRoot != s.workspaceRoot {
		return s.workspaceRoot, false, "not_a_git_repository", nil
	}

	return repoRoot, true, "", nil
}

func (s *Service) branch(ctx context.Context) (string, error) {
	output, _, err := s.runGit(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}
	return "(detached)", nil
}

func (s *Service) headCommit(ctx context.Context) (string, error) {
	output, _, err := s.runGit(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *Service) upstream(ctx context.Context) (string, bool, error) {
	output, stderr, err := s.runGit(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		if strings.Contains(string(stderr), "no upstream configured") || strings.Contains(string(stderr), "no upstream") {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(output)), true, nil
}

func (s *Service) aheadBehind(ctx context.Context) (int, int, error) {
	output, _, err := s.runGit(ctx, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return 0, 0, err
	}

	parts := strings.Fields(strings.TrimSpace(string(output)))
	if len(parts) != 2 {
		return 0, 0, errors.New("unexpected ahead/behind output")
	}
	ahead, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	behind, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return ahead, behind, nil
}

func (s *Service) isBinaryDiff(ctx context.Context, item StatusItem) (bool, error) {
	args := make([]string, 0, 8)
	if item.Status == FileStatusUntracked {
		absolutePath := filepath.Join(s.workspaceRoot, filepath.FromSlash(item.Path))
		args = []string{"diff", "--no-index", "--numstat", "--", "/dev/null", absolutePath}
	} else {
		args = []string{"diff", "--numstat", "HEAD", "--"}
		if item.OldPath != nil {
			args = append(args, *item.OldPath)
		}
		args = append(args, item.Path)
	}

	output, _, err := s.runGitAllowDiff(ctx, args...)
	if err != nil {
		return false, err
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[0] == "-" && fields[1] == "-" {
			return true, nil
		}
	}

	return false, nil
}

func (s *Service) diffText(ctx context.Context, item StatusItem) (string, error) {
	var (
		output []byte
		err    error
	)

	if item.Status == FileStatusUntracked {
		absolutePath := filepath.Join(s.workspaceRoot, filepath.FromSlash(item.Path))
		output, _, err = s.runGitAllowDiff(ctx, "diff", "--no-index", "--binary", "--", "/dev/null", absolutePath)
		if err != nil {
			return "", err
		}
		diff := strings.ReplaceAll(string(output), absolutePath, item.Path)
		return diff, nil
	}

	args := []string{"diff", "--binary", "--find-renames", "HEAD", "--"}
	if item.OldPath != nil {
		args = append(args, *item.OldPath)
	}
	args = append(args, item.Path)
	output, _, err = s.runGitAllowDiff(ctx, args...)
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func (s *Service) runGit(ctx context.Context, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, s.gitBinary, append([]string{"-C", s.workspaceRoot}, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_PAGER=cat", "TERM=dumb")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func (s *Service) runGitAllowDiff(ctx context.Context, args ...string) ([]byte, []byte, error) {
	stdout, stderr, err := s.runGit(ctx, args...)
	if err == nil {
		return stdout, stderr, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return stdout, stderr, nil
	}
	return nil, nil, err
}

func parseOrdinaryStatusRecord(record string) (*StatusItem, error) {
	fields := strings.SplitN(record, " ", 9)
	if len(fields) != 9 {
		return nil, fmt.Errorf("malformed git ordinary status record: %q", record)
	}
	return buildStatusItem(fields[8], nil, fields[1]), nil
}

func parseRenameStatusRecord(record, oldPath string) (*StatusItem, error) {
	fields := strings.SplitN(record, " ", 10)
	if len(fields) != 10 {
		return nil, fmt.Errorf("malformed git rename status record: %q", record)
	}
	oldPath = strings.TrimSpace(oldPath)
	item := buildStatusItem(fields[9], &oldPath, fields[1])
	if item != nil {
		item.Status = FileStatusRenamed
	}
	return item, nil
}

func parseUnmergedStatusRecord(record string) (*StatusItem, error) {
	fields := strings.SplitN(record, " ", 11)
	if len(fields) != 11 {
		return nil, fmt.Errorf("malformed git unmerged status record: %q", record)
	}
	item := buildStatusItem(fields[10], nil, "UU")
	if item != nil {
		item.Status = FileStatusConflicted
	}
	return item, nil
}

func parseUntrackedStatusRecord(record string) (*StatusItem, error) {
	path := strings.TrimSpace(strings.TrimPrefix(record, "? "))
	item := buildStatusItem(path, nil, "??")
	if item != nil {
		item.Status = FileStatusUntracked
	}
	return item, nil
}

func buildStatusItem(path string, oldPath *string, xy string) *StatusItem {
	scope, stackID, normalizedPath, ok := managedPathContext(path)
	if !ok {
		return nil
	}
	var normalizedOldPath *string
	if oldPath != nil {
		if _, _, oldNormalized, oldOK := managedPathContext(*oldPath); oldOK {
			value := oldNormalized
			normalizedOldPath = &value
		}
	}
	return &StatusItem{
		Path:    normalizedPath,
		Scope:   scope,
		StackID: stackID,
		Status:  mapStatus(xy),
		OldPath: normalizedOldPath,
	}
}

func managedPathContext(path string) (Scope, *string, string, bool) {
	normalized, err := normalizeManagedPath(path)
	if err != nil {
		return "", nil, "", false
	}
	parts := strings.Split(normalized, "/")
	if len(parts) < 2 {
		return "", nil, "", false
	}
	scope := Scope(parts[0])
	var stackID *string
	if len(parts) >= 3 && stacks.IsValidStackID(parts[1]) {
		value := parts[1]
		stackID = &value
	}
	return scope, stackID, normalized, true
}

func normalizeManagedPath(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if trimmed == "" {
		return "", ErrInvalidManagedPath
	}
	if strings.HasPrefix(trimmed, "/") {
		return "", ErrPathOutsideWorkspace
	}
	parts := strings.Split(trimmed, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", ErrPathOutsideWorkspace
		default:
			clean = append(clean, part)
		}
	}
	if len(clean) < 2 {
		return "", ErrInvalidManagedPath
	}
	if clean[0] != string(ScopeStacks) && clean[0] != string(ScopeConfig) {
		return "", ErrInvalidManagedPath
	}
	return strings.Join(clean, "/"), nil
}

func mapStatus(xy string) FileStatus {
	switch {
	case xy == "??":
		return FileStatusUntracked
	case strings.ContainsRune(xy, 'U'):
		return FileStatusConflicted
	case strings.ContainsRune(xy, 'R'):
		return FileStatusRenamed
	case strings.ContainsRune(xy, 'D'):
		return FileStatusDeleted
	case strings.ContainsRune(xy, 'A'):
		return FileStatusAdded
	default:
		return FileStatusModified
	}
}

func groupingKey(item StatusItem) string {
	if item.StackID != nil {
		return *item.StackID
	}
	return "~other"
}

func isNotGitRepository(stderr []byte) bool {
	text := string(stderr)
	return strings.Contains(text, "not a git repository")
}
