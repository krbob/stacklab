package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteStringCreatesAndReplacesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.conf")

	if err := WriteString(path, "initial\n", ".stacklab-test-*"); err != nil {
		t.Fatalf("WriteString(new) error = %v", err)
	}
	assertFile(t, path, "initial\n", 0o644)

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod(app.conf) error = %v", err)
	}
	if err := WriteString(path, "updated\n", ".stacklab-test-*"); err != nil {
		t.Fatalf("WriteString(replace) error = %v", err)
	}
	assertFile(t, path, "updated\n", 0o600)

	matches, err := filepath.Glob(filepath.Join(dir, ".stacklab-test-*"))
	if err != nil {
		t.Fatalf("Glob(temp files) error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %#v", matches)
	}
}

func TestWriteStringAdoptingOwnershipCreatesAndPreservesMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "managed.conf")

	if err := WriteStringAdoptingOwnership(path, "initial\n", ".stacklab-adopting-test-*"); err != nil {
		t.Fatalf("WriteStringAdoptingOwnership(new) error = %v", err)
	}
	assertFile(t, path, "initial\n", 0o644)

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod(managed.conf) error = %v", err)
	}
	if err := WriteStringAdoptingOwnership(path, "updated\n", ".stacklab-adopting-test-*"); err != nil {
		t.Fatalf("WriteStringAdoptingOwnership(replace) error = %v", err)
	}
	assertFile(t, path, "updated\n", 0o600)

	matches, err := filepath.Glob(filepath.Join(dir, ".stacklab-adopting-test-*"))
	if err != nil {
		t.Fatalf("Glob(temp files) error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %#v", matches)
	}
}

func TestWriteStringModeEnforcesModeForNewAndExistingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	if err := WriteStringMode(path, "SECRET=initial\n", ".stacklab-test-*", 0o600); err != nil {
		t.Fatalf("WriteStringMode(new) error = %v", err)
	}
	assertFile(t, path, "SECRET=initial\n", 0o600)

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod(.env) error = %v", err)
	}
	if err := WriteStringMode(path, "SECRET=updated\n", ".stacklab-test-*", 0o600); err != nil {
		t.Fatalf("WriteStringMode(existing) error = %v", err)
	}
	assertFile(t, path, "SECRET=updated\n", 0o600)
}

func assertFile(t *testing.T, path, wantContent string, wantMode os.FileMode) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if string(content) != wantContent {
		t.Fatalf("ReadFile(%s) = %q, want %q", path, string(content), wantContent)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if info.Mode().Perm() != wantMode {
		t.Fatalf("mode(%s) = %04o, want %04o", path, info.Mode().Perm(), wantMode)
	}
}
