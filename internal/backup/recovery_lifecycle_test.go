package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreDeletionKeepsOperatorOwnedAndSymlinkTargets(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	custom := registerCatalogFixture(t, s, []catalogFixtureEntry{{name: "data", content: "retain"}}, nil)
	if err := s.DeleteRestore(ctx, custom.ID); err != nil {
		t.Fatalf("custom deletion: %v", err)
	}
	if _, err := s.ResolveRestore(ctx, custom.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("custom registration remains: %v", err)
	}
	if _, err := os.Stat(custom.Target); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(s.directory, "restores")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "restore_owned")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	result := RestoreResult{ID: "restore_owned", Target: target}
	if err := s.registerRestoreResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(custom.Target, target); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRestore(ctx, result.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink deletion: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "data"), []byte("remove"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRestore(ctx, result.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("managed directory remains: %v", err)
	}
	if _, err := s.ResolveRestore(ctx, result.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("registration remains: %v", err)
	}
	if _, err := os.Stat(custom.Target); err != nil {
		t.Fatal("outside target removed", err)
	}
}
