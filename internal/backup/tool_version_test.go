package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResticVersionProbeCachesAndRefreshesBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "restic")
	calls := filepath.Join(dir, "calls")
	t.Setenv("PATH", dir)
	write := func(version string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho call >> "+shellWord(calls)+"\nprintf '%s\\n' "+shellWord(version)+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write("restic 0.14.0 compiled with go1.19 on linux/arm64")
	var cache toolVersionCache
	for range 2 {
		available, version := cache.restic(context.Background())
		if !available || version != "0.14.0" {
			t.Fatalf("available=%v version=%q", available, version)
		}
	}
	b, _ := os.ReadFile(calls)
	if strings.Count(string(b), "call") != 1 {
		t.Fatal("polling spawned redundant probe")
	}
	write("restic 0.18.0 compiled with a newer toolchain")
	if _, version := cache.restic(context.Background()); version != "0.18.0" {
		t.Fatalf("binary replacement retained stale version: %s", version)
	}
	write("unexpected SECRET diagnostic")
	if _, version := cache.restic(context.Background()); version != "" {
		t.Fatalf("unrecognized output exposed: %s", version)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if available, _ := cache.restic(context.Background()); available {
		t.Fatal("removed tool available")
	}
}
