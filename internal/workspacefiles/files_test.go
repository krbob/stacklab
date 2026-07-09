package workspacefiles

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidTextSampleAllowsTruncatedUTF8Rune(t *testing.T) {
	t.Parallel()

	sample := []byte("hello ")
	sample = append(sample, []byte{0xe2, 0x82}...)
	if !ValidTextSample(sample, true) {
		t.Fatal("ValidTextSample(truncated UTF-8) = false, want true")
	}
	if ValidTextSample(sample, false) {
		t.Fatal("ValidTextSample(non-truncated invalid UTF-8) = true, want false")
	}
}

func TestEnsureExpectedModifiedAtUsesDomainConflictError(t *testing.T) {
	t.Parallel()

	conflictErr := errors.New("domain conflict")
	err := EnsureExpectedModifiedAt(filepath.Join(t.TempDir(), "missing.txt"), ptr(time.Now()), conflictErr, "workspace file")
	if !errors.Is(err, conflictErr) {
		t.Fatalf("EnsureExpectedModifiedAt(missing) error = %v, want domain conflict", err)
	}
}

func TestEnsureExpectedModifiedAtAcceptsMatchingMTime(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	modifiedAt := info.ModTime().UTC()
	if err := EnsureExpectedModifiedAt(path, &modifiedAt, errors.New("domain conflict"), "workspace file"); err != nil {
		t.Fatalf("EnsureExpectedModifiedAt(match) error = %v", err)
	}
}

func ptr[T any](value T) *T {
	return &value
}
