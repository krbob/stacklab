package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteManagedRestoreInaccessibleDirectories(t *testing.T) {
	s, _, _ := newTestService(t)
	target := filepath.Join(s.directory, "restores", "restore_private")
	private := filepath.Join(target, "private")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "data"), []byte("delete"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0700); _ = os.Chmod(private, 0700) })
	result := RestoreResult{ID: "restore_private", Target: target}
	if err := s.registerRestoreResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRestore(context.Background(), result.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed data remains: %v", err)
	}
}
