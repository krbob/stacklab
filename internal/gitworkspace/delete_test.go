package gitworkspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func deletionService(t *testing.T) (*Service, string) {
	t.Helper()
	s, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	mustWriteFile(t, filepath.Join(root, "stacks/samba/compose.yaml"), "services: {}\n")
	mustWriteFile(t, filepath.Join(root, "config/samba/app.conf"), "original\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "Initial configuration")
	return s, root
}

func TestDeleteChangedFileRefreshesGitWithoutStaging(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ path, content string }{
		{"stacks/samba/compose.yaml.bak-20260806T1108", "old compose\n"},
		{"stacks/samba/backup.bin", "\x00\xff"},
		{"config/samba/app.conf", "changed\n"},
		{"config/samba/large.conf", strings.Repeat("x", int(diffSizeLimit)+1)},
	} {
		t.Run(tc.path, func(t *testing.T) {
			s, root := deletionService(t)
			target := filepath.Join(root, tc.path)
			mustWriteFile(t, target, tc.content)
			diff, err := s.Diff(t.Context(), tc.path)
			if err != nil || !diff.DeleteAllowed || diff.ModifiedAt == nil {
				t.Fatalf("diff deletion metadata: %#v, %v", diff, err)
			}
			before := gitOutput(t, root, "rev-parse", "HEAD")
			result, err := s.DeleteFile(t.Context(), DeleteFileRequest{Path: tc.path, ExpectedModifiedAt: *diff.ModifiedAt})
			if err != nil || !result.Deleted || result.Path != tc.path || result.StackID == nil || *result.StackID != "samba" {
				t.Fatalf("delete: %#v, %v", result, err)
			}
			if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("deleted file still exists: %v", err)
			}
			if got := gitOutput(t, root, "diff", "--cached", "--name-only"); got != "" {
				t.Fatalf("deletion staged files: %s", got)
			}
			if got := gitOutput(t, root, "rev-parse", "HEAD"); got != before {
				t.Fatal("deletion created a commit")
			}
			status, err := s.Status(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if tc.path == "config/samba/app.conf" {
				if len(status.Items) != 1 || status.Items[0].Status != FileStatusDeleted {
					t.Fatalf("tracked deletion missing: %#v", status.Items)
				}
			} else if len(status.Items) != 0 {
				t.Fatalf("untracked file still in Changes: %#v", status.Items)
			}
			if data, err := os.ReadFile(filepath.Join(root, "stacks/samba/compose.yaml")); err != nil || string(data) != "services: {}\n" {
				t.Fatal("active Compose changed")
			}
		})
	}
}

func TestDeleteChangedFileRejectsProtectedPathsAndStaleReview(t *testing.T) {
	t.Parallel()
	s, root := deletionService(t)
	target := filepath.Join(root, "stacks/samba/old.bak")
	mustWriteFile(t, target, "keep\n")
	mustWriteFile(t, filepath.Join(root, "stacks/samba/.env"), "KEY=value\n")
	mustWriteFile(t, filepath.Join(root, "stacks/samba/compose.yaml"), "services: {changed: {}}\n")
	outside := filepath.Join(t.TempDir(), "outside.conf")
	mustWriteFile(t, outside, "outside\n")
	for name, to := range map[string]string{"link": outside, "internal-link": target, "directory-link": filepath.Dir(outside)} {
		if err := os.Symlink(to, filepath.Join(root, "config", name)); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path  string
		stamp time.Time
		want  error
	}{
		{"stacks/samba/old.bak", time.Time{}, ErrValidation},
		{"stacks/samba/old.bak", info.ModTime().Add(-time.Second), ErrConflict},
		{"stacks/samba/compose.yaml", info.ModTime(), ErrReservedPath},
		{"stacks/samba/.env", info.ModTime(), ErrReservedPath},
		{"config/samba", info.ModTime(), ErrPathNotFile},
		{"stacks/samba", info.ModTime(), ErrInvalidManagedPath},
		{"../outside.conf", info.ModTime(), ErrPathOutsideWorkspace},
		{outside, info.ModTime(), ErrPathOutsideWorkspace},
		{"notes.txt", info.ModTime(), ErrInvalidManagedPath},
		{"config/.git/index", info.ModTime(), ErrInvalidManagedPath},
		{"config/link", info.ModTime(), ErrPathNotFile},
		{"config/internal-link", info.ModTime(), ErrPathNotFile},
		{"config/directory-link/outside.conf", info.ModTime(), ErrPathOutsideWorkspace},
		{"config/missing/file", info.ModTime(), ErrNotFound},
		{"config/samba/missing", info.ModTime(), ErrNotFound},
	} {
		t.Run(tc.path+tc.want.Error(), func(t *testing.T) {
			if _, err := s.DeleteFile(t.Context(), DeleteFileRequest{Path: tc.path, ExpectedModifiedAt: tc.stamp}); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
		})
	}
	for _, path := range []string{"stacks/samba/compose.yaml", "stacks/samba/.env", "config/link"} {
		diff, err := s.Diff(t.Context(), path)
		if err != nil || diff.DeleteAllowed || diff.ModifiedAt != nil {
			t.Fatalf("protected deletion metadata: %#v, %v", diff, err)
		}
	}
	for _, path := range []string{target, outside} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("protected file disappeared", err)
		}
	}
}

func TestDeleteChangedFileRejectsCleanFilesAndConcurrentGitOperations(t *testing.T) {
	t.Parallel()
	s, root := deletionService(t)
	path := "config/samba/app.conf"
	info, err := os.Stat(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	request := DeleteFileRequest{Path: path, ExpectedModifiedAt: info.ModTime()}
	if _, err := s.DeleteFile(t.Context(), request); !errors.Is(err, ErrNotFound) {
		t.Fatalf("clean deletion: %v", err)
	}
	s.mutationMu.Lock()
	_, err = s.DeleteFile(t.Context(), request)
	s.mutationMu.Unlock()
	if !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("concurrent deletion: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.DeleteFile(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled deletion: %v", err)
	}
	s.gitBinary = "stacklab-missing-test-git"
	if _, err := s.DeleteFile(t.Context(), request); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unavailable Git: %v", err)
	}
}

func TestDeleteChangedFileRequiresParentDirectoryWriteAccess(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("requires a non-root user")
	}
	s, root := deletionService(t)
	parent := filepath.Join(root, "config/samba")
	target := filepath.Join(parent, "old.bak")
	mustWriteFile(t, target, "keep\n")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	if _, err := s.DeleteFile(t.Context(), DeleteFileRequest{Path: "config/samba/old.bak", ExpectedModifiedAt: info.ModTime()}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("permission error: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("file disappeared", err)
	}
}
