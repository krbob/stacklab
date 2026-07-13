//go:build linux || darwin

package atomicfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWriteStringPreservesOwnerGroupAndExtendedAttributes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "managed.conf")
	if err := os.WriteFile(path, []byte("before\n"), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	before := statOwnership(t, path)
	for _, groupID := range supplementaryGroups(t) {
		if groupID == int(before.Gid) {
			continue
		}
		if err := os.Chown(path, -1, groupID); err == nil {
			before = statOwnership(t, path)
			break
		}
	}

	attributeName := "user.stacklab.metadata-test"
	attributeValue := []byte("preserve-me")
	if err := unix.Setxattr(path, attributeName, attributeValue, 0); err != nil {
		if xattrUnavailable(err) {
			t.Skipf("extended attributes unavailable: %v", err)
		}
		t.Fatalf("Setxattr() error = %v", err)
	}

	if err := WriteString(path, "after\n", ".stacklab-metadata-test-*"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	after := statOwnership(t, path)
	if after.Uid != before.Uid || after.Gid != before.Gid {
		t.Fatalf("owner/group after write = %d:%d, want %d:%d", after.Uid, after.Gid, before.Uid, before.Gid)
	}
	value := getXattr(t, path, attributeName)
	if !bytes.Equal(value, attributeValue) {
		t.Fatalf("xattr after write = %q, want %q", value, attributeValue)
	}
	assertFile(t, path, "after\n", 0o640)

	if err := WriteStringMode(path, "explicit mode\n", ".stacklab-metadata-test-*", 0o600); err != nil {
		t.Fatalf("WriteStringMode() error = %v", err)
	}
	afterExplicitMode := statOwnership(t, path)
	if afterExplicitMode.Uid != before.Uid || afterExplicitMode.Gid != before.Gid {
		t.Fatalf("owner/group after explicit-mode write = %d:%d, want %d:%d", afterExplicitMode.Uid, afterExplicitMode.Gid, before.Uid, before.Gid)
	}
	value = getXattr(t, path, attributeName)
	if !bytes.Equal(value, attributeValue) {
		t.Fatalf("xattr after explicit-mode write = %q, want %q", value, attributeValue)
	}
	assertFile(t, path, "explicit mode\n", 0o600)
}

func TestWriteStringPreservesSpecialModeBits(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "special-mode.conf")
	if err := os.WriteFile(path, []byte("before\n"), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(path, 0o640|os.ModeSetgid); err != nil {
		t.Skipf("setgid mode unavailable: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(before) error = %v", err)
	}
	if before.Mode()&os.ModeSetgid == 0 {
		t.Skip("filesystem did not retain setgid mode")
	}
	if err := WriteString(path, "after\n", ".stacklab-special-mode-test-*"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(after) error = %v", err)
	}
	if after.Mode()&os.ModeSetgid == 0 || after.Mode().Perm() != 0o640 {
		t.Fatalf("mode after write = %v, want setgid and 0640", after.Mode())
	}
	if err := WriteStringMode(path, "explicit\n", ".stacklab-special-mode-test-*", after.Mode()); err != nil {
		t.Fatalf("WriteStringMode() error = %v", err)
	}
	afterExplicit, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(after explicit mode) error = %v", err)
	}
	if afterExplicit.Mode()&os.ModeSetgid == 0 || afterExplicit.Mode().Perm() != 0o640 {
		t.Fatalf("mode after explicit write = %v, want setgid and 0640", afterExplicit.Mode())
	}
}

func TestApplyPlatformMetadataAdoptsOwnershipOnlyForPermissionErrors(t *testing.T) {
	t.Parallel()

	attributeName := "user.stacklab.ownership-adoption-test"
	attributeValue := []byte("preserve-me")
	metadata := platformMetadata{
		uid: os.Getuid() + 1,
		gid: os.Getgid(),
		attributes: []extendedAttribute{{
			name:  attributeName,
			value: attributeValue,
		}},
	}
	permissionDenied := func(int, int) error {
		return &os.PathError{Op: "chown", Path: "replacement", Err: syscall.EPERM}
	}

	strictFile, err := os.CreateTemp(t.TempDir(), "strict-*")
	if err != nil {
		t.Fatalf("CreateTemp(strict) error = %v", err)
	}
	t.Cleanup(func() { _ = strictFile.Close() })
	if err := applyPlatformMetadataWithChown(strictFile, strictFile.Name(), metadata, requireOwnerGroupPreservation, permissionDenied); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("strict metadata error = %v, want permission denied", err)
	}

	adoptedFile, err := os.CreateTemp(t.TempDir(), "adopted-*")
	if err != nil {
		t.Fatalf("CreateTemp(adopted) error = %v", err)
	}
	t.Cleanup(func() { _ = adoptedFile.Close() })
	if err := applyPlatformMetadataWithChown(adoptedFile, adoptedFile.Name(), metadata, allowOwnerGroupAdoption, permissionDenied); err != nil {
		if xattrUnavailable(err) {
			t.Skipf("extended attributes unavailable: %v", err)
		}
		t.Fatalf("adopting metadata error = %v", err)
	}
	if value := getXattr(t, adoptedFile.Name(), attributeName); !bytes.Equal(value, attributeValue) {
		t.Fatalf("adopted xattr = %q, want %q", value, attributeValue)
	}

	nonPermissionFile, err := os.CreateTemp(t.TempDir(), "non-permission-*")
	if err != nil {
		t.Fatalf("CreateTemp(non-permission) error = %v", err)
	}
	t.Cleanup(func() { _ = nonPermissionFile.Close() })
	unexpectedFailure := func(int, int) error { return syscall.EIO }
	if err := applyPlatformMetadataWithChown(nonPermissionFile, nonPermissionFile.Name(), metadata, allowOwnerGroupAdoption, unexpectedFailure); !errors.Is(err, syscall.EIO) {
		t.Fatalf("non-permission metadata error = %v, want EIO", err)
	}
}

func statOwnership(t *testing.T, path string) *syscall.Stat_t {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("Stat(%s).Sys() type = %T", path, info.Sys())
	}
	copy := *stat
	return &copy
}

func supplementaryGroups(t *testing.T) []int {
	t.Helper()
	groups, err := os.Getgroups()
	if err != nil {
		t.Logf("Getgroups() error: %v", err)
		return nil
	}
	return groups
}

func getXattr(t *testing.T, path, name string) []byte {
	t.Helper()
	size, err := unix.Getxattr(path, name, nil)
	if err != nil {
		t.Fatalf("Getxattr(%s) size error = %v", name, err)
	}
	value := make([]byte, size)
	size, err = unix.Getxattr(path, name, value)
	if err != nil {
		t.Fatalf("Getxattr(%s) error = %v", name, err)
	}
	return value[:size]
}

func xattrUnavailable(err error) bool {
	return errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM)
}
