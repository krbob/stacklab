package gitworkspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"stacklab/internal/stacks"
)

var ErrRestoreUnsupported = errors.New("only modified or deleted regular files present in HEAD can be restored")

type RestoreFileRequest struct {
	Path          string `json:"path"`
	ExpectedState string `json:"expected_state"`
}

type RestoreFileResponse struct {
	Restored    bool    `json:"restored"`
	Path        string  `json:"path"`
	Scope       Scope   `json:"scope"`
	StackID     *string `json:"stack_id"`
	HeadCommit  string  `json:"head_commit"`
	AuditAction string  `json:"audit_action"`
}

type restoreReview struct {
	token string
	head  string
}

func canRestore(item StatusItem) bool {
	return item.OldPath == nil && item.BlockedReason == nil &&
		(item.Status == FileStatusModified || item.Status == FileStatusDeleted)
}

// Bind the review to HEAD, this path's index entry and its on-disk bytes and
// metadata. A timestamp alone misses staged changes and deleted-file races.
func (s *Service) restoreReview(ctx context.Context, path string) (restoreReview, error) {
	state, _, err := s.restoreFileState(path)
	if err != nil {
		return restoreReview{}, err
	}
	head, err := s.headCommit(ctx)
	if err != nil {
		return restoreReview{}, err
	}
	literal := ":(literal)" + path
	entry, stderr, err := s.runGit(ctx, "ls-tree", "-z", head, "--", literal)
	if err != nil {
		return restoreReview{}, gitCommandError(stderr, err)
	}
	metadata, entryPath, ok := strings.Cut(strings.TrimSuffix(string(entry), "\x00"), "\t")
	fields := strings.Fields(metadata)
	if !ok || entryPath != path || len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
		return restoreReview{}, ErrRestoreUnsupported
	}
	index, stderr, err := s.runGit(ctx, "ls-files", "--stage", "-z", "--", literal)
	if err != nil {
		return restoreReview{}, gitCommandError(stderr, err)
	}
	for _, record := range strings.Split(string(index), "\x00") {
		if record == "" {
			continue
		}
		metadata, _, _ := strings.Cut(record, "\t")
		fields := strings.Fields(metadata)
		if len(fields) != 3 || fields[2] != "0" {
			return restoreReview{}, ErrRestoreUnsupported
		}
	}
	sum := sha256.Sum256([]byte(path + "\x00" + head + "\x00" + string(index) + "\x00" + state))
	return restoreReview{token: fmt.Sprintf("%x", sum), head: head}, nil
}

// Use directory handles to confine writes even if a parent is moved while an
// operation is running. Never traverse symlinks or write Git metadata.
func (s *Service) restoreParent(path string, create bool) (*os.Root, string, error) {
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if strings.EqualFold(part, ".git") {
			return nil, "", ErrInvalidManagedPath
		}
	}
	if parts[0] == string(ScopeStacks) && (len(parts) < 3 || !stacks.IsValidStackID(parts[1])) {
		return nil, "", ErrInvalidManagedPath
	}
	parent, err := os.OpenRoot(s.workspaceRoot)
	if err != nil {
		return nil, "", err
	}
	for _, part := range parts[:len(parts)-1] {
		info, err := parent.Lstat(part)
		if errors.Is(err, os.ErrNotExist) && create {
			if err = parent.Mkdir(part, 0o755); err == nil || errors.Is(err, os.ErrExist) {
				info, err = parent.Lstat(part)
			}
		}
		if err != nil {
			parent.Close()
			return nil, "", err
		}
		if !info.IsDir() {
			parent.Close()
			return nil, "", ErrPathOutsideWorkspace
		}
		next, err := parent.OpenRoot(part)
		parent.Close()
		if err != nil {
			return nil, "", err
		}
		parent = next
	}
	return parent, parts[len(parts)-1], nil
}

func (s *Service) restoreFileState(path string) (string, os.FileInfo, error) {
	parent, name, err := s.restoreParent(path, false)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	defer parent.Close()
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, ErrPathNotFile
	}
	file, err := parent.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return "", nil, ErrConflict
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return "", nil, err
	}
	if !info.ModTime().Equal(after.ModTime()) || info.Size() != after.Size() || info.Mode() != after.Mode() {
		return "", nil, ErrConflict
	}
	return fmt.Sprintf("%x:%d:%d:%d", hash.Sum(nil), info.Mode(), info.Size(), info.ModTime().UnixNano()), info, nil
}

func (s *Service) RestoreFile(ctx context.Context, request RestoreFileRequest) (response RestoreFileResponse, resultErr error) {
	if err := ctx.Err(); err != nil {
		return response, err
	}
	if len(request.ExpectedState) != sha256.Size*2 {
		return response, ErrValidation
	}
	path, err := normalizeManagedPath(request.Path)
	if err != nil {
		return response, err
	}
	if !s.mutationMu.TryLock() {
		return response, ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	defer func() {
		if errors.Is(resultErr, os.ErrPermission) {
			resultErr = ErrPermissionDenied
		}
	}()
	status, err := s.Status(ctx)
	if err != nil {
		return response, err
	}
	if !status.Available {
		return response, ErrUnavailable
	}
	var selected *StatusItem
	for i := range status.Items {
		if status.Items[i].Path == path {
			selected = &status.Items[i]
			break
		}
	}
	if selected == nil {
		return response, ErrNotFound
	}
	if !canRestore(*selected) {
		return response, ErrRestoreUnsupported
	}
	if s.hasOperationInProgress(ctx) {
		return response, ErrOperationInProgress
	}
	lock, err := s.acquireIndexLock(ctx)
	if err != nil {
		return response, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.release()) }()
	review, err := s.restoreReview(ctx, path)
	if err != nil {
		return response, err
	}
	if review.token != request.ExpectedState {
		return response, ErrConflict
	}

	// Prepare both the new index and Git's checkout (including filters) away
	// from the live workspace. Unrelated staged entries remain untouched.
	temp, err := os.MkdirTemp(filepath.Dir(lock.path), ".stacklab-restore-")
	if err != nil {
		return response, err
	}
	defer os.RemoveAll(temp)
	index := filepath.Join(temp, "index")
	if err := copyIndex(lock.indexPath, index); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return response, err
		}
		if _, stderr, err := s.runGitWithIndex(ctx, index, "read-tree", review.head); err != nil {
			return response, classifyGitMutationError(stderr, err)
		}
	}
	tree := filepath.Join(temp, "tree")
	if err := os.Mkdir(tree, 0o700); err != nil {
		return response, err
	}
	if _, stderr, err := s.runGitWithIndex(ctx, index, "--work-tree="+tree, "restore", "--source="+review.head, "--staged", "--worktree", "--", ":(literal)"+path); err != nil {
		return response, classifyGitMutationError(stderr, err)
	}
	source, err := os.Open(filepath.Join(tree, filepath.FromSlash(path)))
	if err != nil {
		return response, err
	}
	defer source.Close()
	sourceInfo, err := source.Stat()
	if err != nil {
		return response, err
	}
	parent, name, err := s.restoreParent(path, true)
	if err != nil {
		return response, err
	}
	defer parent.Close()
	tempName := ".stacklab-restore-" + rand.Text()
	destination, err := parent.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return response, err
	}
	defer parent.Remove(tempName)
	defer destination.Close()
	if _, err := io.Copy(destination, source); err != nil {
		return response, err
	}
	_, oldInfo, err := s.restoreFileState(path)
	if err != nil {
		return response, err
	}
	mode := sourceInfo.Mode().Perm()
	if oldInfo != nil {
		// Preserve private read/write permissions while restoring Git's execute bit.
		mode = oldInfo.Mode().Perm()&^0o111 | mode&0o111
		if stat, ok := oldInfo.Sys().(*syscall.Stat_t); ok {
			if err := destination.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
				return response, err
			}
		}
	}
	if err := destination.Chmod(mode); err != nil {
		return response, err
	}
	if err := destination.Sync(); err != nil {
		return response, err
	}
	if err := destination.Close(); err != nil {
		return response, err
	}
	current, err := s.restoreReview(ctx, path)
	if err != nil {
		return response, err
	}
	if current != review {
		return response, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return response, err
	}
	if err := lock.ensureOwned(); err != nil {
		return response, err
	}
	if err := parent.Rename(tempName, name); err != nil {
		return response, err
	}
	// Complete the index transaction even if the HTTP request is canceled.
	if err := lock.install(index); err != nil {
		return response, fmt.Errorf("file restored but Git index finalization failed: %w", err)
	}
	scope, stackID, _, _ := managedPathContext(path)
	return RestoreFileResponse{Restored: true, Path: path, Scope: scope, StackID: stackID, HeadCommit: review.head, AuditAction: "git_restore_file"}, nil
}
