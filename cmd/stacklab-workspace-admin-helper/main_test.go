package main

import (
	"path/filepath"
	"testing"
)

func TestLoadStacklabRootUsesEnvironmentOverride(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("STACKLAB_ROOT", tempDir)

	got, err := loadStacklabRoot()
	if err != nil {
		t.Fatalf("loadStacklabRoot() error = %v", err)
	}

	want, err := filepath.Abs(tempDir)
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error = %v", tempDir, err)
	}
	if got != want {
		t.Fatalf("loadStacklabRoot() = %q, want %q", got, want)
	}
}
