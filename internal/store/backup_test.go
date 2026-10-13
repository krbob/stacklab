package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotIncludesCommittedWALAndDoesNotReplaceExistingFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SetAppSetting(ctx, "backup-test", `{"committed":true}`, time.Now()); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(dir, "snapshot.db")
	if err = s.Snapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(snapshot); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private snapshot: %v %v", info, err)
	}
	restored, err := Open(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	value, found, err := restored.AppSetting(ctx, "backup-test")
	if err != nil || !found || value != `{"committed":true}` {
		t.Fatalf("restored WAL value = %q %v %v", value, found, err)
	}
	if err = s.Snapshot(ctx, snapshot); err == nil {
		t.Fatal("snapshot overwrote an existing database")
	}
}
