package configworkspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeleteFileRemovesOnlySelectedRegularFile(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{"text": "PORT=8080\n", "binary": "\x00\xff", "oversized": strings.Repeat("x", int(MaxFileContentBytes)+1)} {
		t.Run(name, func(t *testing.T) {
			service, root := newTestService(t)
			mustMkdirAll(t, filepath.Join(root, "demo"))
			target := filepath.Join(root, "demo", "delete.conf")
			mustWriteFile(t, target, content)
			mustWriteFile(t, filepath.Join(root, "demo", "keep.conf"), "keep\n")
			info, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			// Deletion depends on the parent directory, not text editability.
			if err := os.Chmod(target, 0o400); err != nil {
				t.Fatal(err)
			}
			response, err := service.DeleteFile(t.Context(), DeleteFileRequest{Path: "demo/delete.conf", ExpectedModifiedAt: info.ModTime()})
			if err != nil || !response.Deleted || response.Path != "demo/delete.conf" || response.AuditAction != "delete_config_file" {
				t.Fatalf("DeleteFile = %#v, %v", response, err)
			}
			if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("deleted file still exists: %v", err)
			}
			if content, err := os.ReadFile(filepath.Join(root, "demo", "keep.conf")); err != nil || string(content) != "keep\n" {
				t.Fatalf("sibling changed: %q, %v", content, err)
			}
		})
	}
}

func TestDeleteFileRejectsUnsafeTargetsAndStaleRequests(t *testing.T) {
	t.Parallel()
	service, root := newTestService(t)
	mustMkdirAll(t, filepath.Join(root, "demo"))
	target := filepath.Join(root, "demo", "keep.conf")
	mustWriteFile(t, target, "keep\n")
	outside := filepath.Join(t.TempDir(), "outside.conf")
	mustWriteFile(t, outside, "outside\n")
	for name, destination := range map[string]string{
		"internal-link": target,
		"external-link": outside,
		"parent-link":   filepath.Dir(outside),
	} {
		if err := os.Symlink(destination, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		path string
		time time.Time
		want error
	}{
		{"missing timestamp", "demo/keep.conf", time.Time{}, ErrValidation},
		{"stale timestamp", "demo/keep.conf", info.ModTime().Add(-time.Second), ErrConflict},
		{"directory", "demo", info.ModTime(), ErrPathNotFile},
		{"root", "", info.ModTime(), ErrPathNotFile},
		{"traversal", "../outside.conf", info.ModTime(), ErrPathOutsideWorkspace},
		{"absolute", outside, info.ModTime(), ErrPathOutsideWorkspace},
		{"internal symlink", "internal-link", info.ModTime(), ErrPathNotFile},
		{"external symlink", "external-link", info.ModTime(), ErrPathOutsideWorkspace},
		{"parent symlink escape", "parent-link/outside.conf", info.ModTime(), ErrPathOutsideWorkspace},
		{"missing", "demo/missing.conf", info.ModTime(), ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.DeleteFile(t.Context(), DeleteFileRequest{Path: tc.path, ExpectedModifiedAt: tc.time})
			if !errors.Is(err, tc.want) {
				t.Fatalf("DeleteFile(%q) error = %v, want %v", tc.path, err, tc.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.DeleteFile(ctx, DeleteFileRequest{Path: "demo/keep.conf", ExpectedModifiedAt: info.ModTime()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled deletion error = %v", err)
	}
	for _, file := range []string{target, outside} {
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("protected file disappeared: %s: %v", file, err)
		}
	}
}

func TestDeleteFileAllowsParentSymlinkWithinWorkspace(t *testing.T) {
	t.Parallel()
	service, root := newTestService(t)
	parent := filepath.Join(root, "demo")
	mustMkdirAll(t, parent)
	if err := os.Symlink(parent, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "delete.conf")
	mustWriteFile(t, target, "temporary\n")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.DeleteFile(t.Context(), DeleteFileRequest{Path: "alias/delete.conf", ExpectedModifiedAt: info.ModTime()})
	if err != nil || !response.Deleted || response.Path != "alias/delete.conf" {
		t.Fatalf("DeleteFile via parent symlink = %#v, %v", response, err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still exists: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "alias")); err != nil {
		t.Fatalf("parent symlink changed: %v", err)
	}
}

func TestDeleteFileRequiresParentDirectoryAccess(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("requires a non-root user")
	}
	service, root := newTestService(t)
	parent := filepath.Join(root, "demo")
	mustMkdirAll(t, parent)
	target := filepath.Join(parent, "keep.conf")
	mustWriteFile(t, target, "keep\n")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	if _, err := service.DeleteFile(t.Context(), DeleteFileRequest{Path: "demo/keep.conf", ExpectedModifiedAt: info.ModTime()}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("DeleteFile error = %v, want %v", err, ErrPermissionDenied)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("file changed on rejected deletion: %v", err)
	}
}
