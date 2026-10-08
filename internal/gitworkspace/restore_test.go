package gitworkspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func restoreRequest(t *testing.T, s *Service, path string) RestoreFileRequest {
	t.Helper()
	diff, err := s.Diff(t.Context(), path)
	if err != nil || !diff.RestoreAllowed || diff.RestoreState == "" || diff.RestoreCommit == "" {
		t.Fatalf("restore review: %#v, %v", diff, err)
	}
	return RestoreFileRequest{Path: path, ExpectedState: diff.RestoreState}
}

func TestRestoreFileRestoresOnlySelectedFileAndIndexEntry(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"modified", "staged", "mixed", "deleted", "staged deletion", "removed from index", "missing directory", "binary", "literal path", "compose", "private permissions"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			s, root := deletionService(t)
			path, original := "config/samba/app.conf", "original\n"
			switch kind {
			case "binary":
				original = "\x00\xfforiginal"
			case "literal path":
				path = "config/samba/[a]*.conf"
			case "compose":
				path, original = "stacks/samba/compose.yaml", "services: {}\n"
			}
			mustWriteFile(t, filepath.Join(root, path), original)
			runGit(t, root, "add", ".")
			runGit(t, root, "commit", "--allow-empty", "-m", "Baseline")
			head := strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD"))
			// Unrelated staged and unstaged content must survive byte for byte.
			other := "stacks/samba/other.conf"
			mustWriteFile(t, filepath.Join(root, other), "staged other\n")
			runGit(t, root, "add", other)
			mustWriteFile(t, filepath.Join(root, other), "working other\n")
			otherIndex := gitOutput(t, root, "ls-files", "--stage", "--", other)
			mustWriteFile(t, filepath.Join(root, path), "changed\n")
			if kind == "staged" || kind == "mixed" {
				runGit(t, root, "add", "--", ":(literal)"+path)
			}
			if kind == "mixed" {
				mustWriteFile(t, filepath.Join(root, path), "working changes\n")
			}
			if kind == "removed from index" {
				runGit(t, root, "rm", "--cached", path)
			}
			if kind == "deleted" || kind == "staged deletion" || kind == "missing directory" {
				if err := os.Remove(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
				if kind == "staged deletion" {
					runGit(t, root, "add", "-u", "--", ":(literal)"+path)
					otherIndex = gitOutput(t, root, "ls-files", "--stage", "--", other)
				}
				if kind == "missing directory" {
					if err := os.Remove(filepath.Dir(filepath.Join(root, path))); err != nil {
						t.Fatal(err)
					}
				}
			}
			if kind == "private permissions" {
				if err := os.Chmod(filepath.Join(root, path), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			request := restoreRequest(t, s, path)
			result, err := s.RestoreFile(t.Context(), request)
			if err != nil || !result.Restored || result.HeadCommit != head || result.Path != path {
				t.Fatalf("restore: %#v, %v", result, err)
			}
			if data, err := os.ReadFile(filepath.Join(root, path)); err != nil || string(data) != original {
				t.Fatalf("restored file = %q, %v", data, err)
			}
			if got := gitOutput(t, root, "status", "--porcelain", "--", ":(literal)"+path); got != "" {
				t.Fatalf("restored path still dirty: %s", got)
			}
			if got := gitOutput(t, root, "ls-files", "--stage", "--", other); got != otherIndex {
				t.Fatalf("unrelated index entry changed: %s", got)
			}
			if data, err := os.ReadFile(filepath.Join(root, other)); err != nil || string(data) != "working other\n" {
				t.Fatal("unrelated working copy changed", err)
			}
			if got := strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD")); got != head {
				t.Fatal("restore moved HEAD")
			}
			if kind == "private permissions" {
				info, err := os.Stat(filepath.Join(root, path))
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatal("private permissions lost", err)
				}
			}
		})
	}
}

func TestRestoreRejectsStaleReviewWithoutChangingWorktreeOrIndex(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"content with same timestamp", "index", "head", "recreated deletion"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			s, root := deletionService(t)
			path := "config/samba/app.conf"
			target := filepath.Join(root, path)
			mustWriteFile(t, target, "changed1\n")
			if kind == "recreated deletion" {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			}
			request := restoreRequest(t, s, path)
			switch kind {
			case "content with same timestamp":
				info, _ := os.Stat(target)
				mustWriteFile(t, target, "changed2\n")
				if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "index":
				runGit(t, root, "add", path)
			case "head":
				runGit(t, root, "commit", "--allow-empty", "-m", "Advanced HEAD")
			case "recreated deletion":
				mustWriteFile(t, target, "new file\n")
			}
			beforeFile, _ := os.ReadFile(target)
			beforeIndex, _ := os.ReadFile(filepath.Join(root, ".git/index"))
			if _, err := s.RestoreFile(t.Context(), request); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale review error: %v", err)
			}
			afterFile, _ := os.ReadFile(target)
			afterIndex, _ := os.ReadFile(filepath.Join(root, ".git/index"))
			if string(afterFile) != string(beforeFile) || string(afterIndex) != string(beforeIndex) {
				t.Fatal("stale review changed worktree or index")
			}
		})
	}
}

func TestRestoreRejectsUnsupportedFilesAndOperations(t *testing.T) {
	t.Parallel()
	s, root := deletionService(t)
	path := "config/samba/app.conf"
	mustWriteFile(t, filepath.Join(root, path), "changed\n")
	request := restoreRequest(t, s, path)
	for _, marker := range []string{"index.lock", "MERGE_HEAD", "CHERRY_PICK_HEAD", "rebase-merge"} {
		markerPath := filepath.Join(root, ".git", marker)
		mustWriteFile(t, markerPath, "owned by someone else\n")
		if _, err := s.RestoreFile(t.Context(), request); !errors.Is(err, ErrOperationInProgress) {
			t.Fatalf("%s: %v", marker, err)
		}
		if data, _ := os.ReadFile(markerPath); string(data) != "owned by someone else\n" {
			t.Fatal("changed foreign Git marker")
		}
		if err := os.Remove(markerPath); err != nil {
			t.Fatal(err)
		}
	}
	s.mutationMu.Lock()
	_, err := s.RestoreFile(t.Context(), request)
	s.mutationMu.Unlock()
	if !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("mutation lock: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.RestoreFile(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	for _, newPath := range []string{"config/samba/new.conf", "config/samba/staged.conf"} {
		mustWriteFile(t, filepath.Join(root, newPath), "new\n")
		if strings.Contains(newPath, "staged") {
			runGit(t, root, "add", newPath)
		}
		diff, err := s.Diff(t.Context(), newPath)
		if err != nil || diff.RestoreAllowed || diff.RestoreState != "" {
			t.Fatalf("new file offered restore: %#v, %v", diff, err)
		}
		if _, err := s.RestoreFile(t.Context(), RestoreFileRequest{Path: newPath, ExpectedState: request.ExpectedState}); !errors.Is(err, ErrRestoreUnsupported) {
			t.Fatalf("new file restore: %v", err)
		}
	}
	if _, err := s.RestoreFile(t.Context(), RestoreFileRequest{Path: "../outside", ExpectedState: request.ExpectedState}); !errors.Is(err, ErrPathOutsideWorkspace) {
		t.Fatalf("path escape: %v", err)
	}
	if _, err := s.RestoreFile(t.Context(), RestoreFileRequest{Path: path}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing review: %v", err)
	}
}

func TestRestoreRejectsSymlinkReplacements(t *testing.T) {
	t.Parallel()
	for _, parentLink := range []bool{false, true} {
		t.Run(map[bool]string{true: "parent", false: "file"}[parentLink], func(t *testing.T) {
			s, root := deletionService(t)
			path := "config/samba/app.conf"
			mustWriteFile(t, filepath.Join(root, path), "changed\n")
			request := restoreRequest(t, s, path)
			outside := t.TempDir()
			mustWriteFile(t, filepath.Join(outside, "app.conf"), "outside\n")
			target, link := filepath.Join(root, path), filepath.Join(outside, "app.conf")
			if parentLink {
				target, link = filepath.Dir(target), outside
			}
			if err := os.RemoveAll(target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(link, target); err != nil {
				t.Fatal(err)
			}
			if _, err := s.RestoreFile(t.Context(), request); err == nil {
				t.Fatal("restored through a symbolic link")
			}
			if data, _ := os.ReadFile(filepath.Join(outside, "app.conf")); string(data) != "outside\n" {
				t.Fatal("outside file changed")
			}
		})
	}
}
