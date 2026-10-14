package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
	"stacklab/internal/config"
)

// Package-created workspace parents have default ACLs. Their inherited ACLs
// remain extended even after chmod 0444, unlike a plain read-only file fixture.
func TestNativeCaptureRestoreReadOnlyInheritedACL(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux POSIX ACL integration")
	}
	for _, tool := range []string{"setfacl", "getfacl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("requires %s", tool)
		}
	}
	command := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %s: %v", name, out, err)
		}
		return string(out)
	}
	root := t.TempDir()
	source := filepath.Join(root, "config", "app")
	if err := os.MkdirAll(source, 0750); err != nil {
		t.Fatal(err)
	}
	command("setfacl", "-m", fmt.Sprintf("u:%d:rwx,d:u:%d:rwx", os.Getuid(), os.Getuid()), source)
	nested := filepath.Join(source, "readonly-dir")
	if err := os.Mkdir(nested, 0750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(nested, "readonly-file")
	if err := os.WriteFile(file, []byte("ACL protected payload"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(file, "user.stacklab-acl-test", []byte("preserved"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file, filepath.Join(nested, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("readonly-file", filepath.Join(nested, "symlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nested, 0555); err != nil {
		t.Fatal(err)
	}
	// Restore writable modes only for test cleanup, after all assertions.
	t.Cleanup(func() { _ = os.Chmod(nested, 0750) })
	capture := NewNativeCapture(config.Config{RootDir: root, DataDir: t.TempDir()}, nil, nil)
	stage := t.TempDir()
	manifest, err := capture.Prepare(context.Background(), Plan{ID: "acl", Consistency: "files", StagingMaxBytes: 1 << 20, Sources: []Source{{ID: "config", Kind: "managed_config", Path: source}}}, "acl-run", stage)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := ManifestArchivePath(stage, manifest.Sources[0])
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(target, "readonly-dir"), 0750) })
	if err := capture.RestoreArchive(context.Background(), archive, target, false); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(target, "readonly-dir", "readonly-file")
	data, err := os.ReadFile(restored)
	if err != nil || string(data) != "ACL protected payload" {
		t.Fatalf("payload=%q err=%v", data, err)
	}
	for _, relative := range []string{".", "readonly-dir", "readonly-dir/readonly-file"} {
		before, after := filepath.Join(source, relative), filepath.Join(target, relative)
		if a, b := command("getfacl", "-cn", before), command("getfacl", "-cn", after); a != b {
			t.Fatalf("ACL changed for %s: before=%s after=%s", relative, a, b)
		}
		a, err := os.Stat(before)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.Stat(after)
		if err != nil {
			t.Fatal(err)
		}
		if a.Mode().Perm() != b.Mode().Perm() {
			t.Fatalf("mode changed for %s: %v -> %v", relative, a.Mode(), b.Mode())
		}
	}
	first, err := os.Stat(restored)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(filepath.Join(target, "readonly-dir", "hardlink"))
	if err != nil || !os.SameFile(first, second) {
		t.Fatalf("hardlink lost: %v", err)
	}
	if link, err := os.Readlink(filepath.Join(target, "readonly-dir", "symlink")); err != nil || link != "readonly-file" {
		t.Fatalf("symlink=%q err=%v", link, err)
	}
	value := make([]byte, 64)
	size, err := unix.Getxattr(restored, "user.stacklab-acl-test", value)
	if err != nil || string(value[:size]) != "preserved" {
		t.Fatalf("xattr lost: %v", err)
	}
}
