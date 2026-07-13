//go:build linux

package atomicfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWriteStringPreservesPOSIXAccessACL(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "acl.conf")
	if err := os.WriteFile(path, []byte("before\n"), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	acl := linuxPOSIXACL(uint32(os.Getuid() + 1))
	if err := unix.Setxattr(path, "system.posix_acl_access", acl, 0); err != nil {
		if xattrUnavailable(err) || errors.Is(err, unix.EINVAL) {
			t.Skipf("POSIX ACL unavailable: %v", err)
		}
		t.Fatalf("Setxattr(POSIX ACL) error = %v", err)
	}
	wantACL := getXattr(t, path, "system.posix_acl_access")

	if err := WriteString(path, "after\n", ".stacklab-acl-test-*"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	gotACL := getXattr(t, path, "system.posix_acl_access")
	if !bytes.Equal(gotACL, wantACL) {
		t.Fatalf("POSIX ACL changed across atomic write: got %x, want %x", gotACL, wantACL)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(ACL file) error = %v", err)
	}
	if err := WriteStringMode(path, "explicit mode\n", ".stacklab-acl-test-*", info.Mode().Perm()); err != nil {
		t.Fatalf("WriteStringMode() error = %v", err)
	}
	gotACL = getXattr(t, path, "system.posix_acl_access")
	if !bytes.Equal(gotACL, wantACL) {
		t.Fatalf("POSIX ACL changed across explicit-mode atomic write: got %x, want %x", gotACL, wantACL)
	}
}

func TestOwnershipAdoptionKeepsACLWhenNamedUserBecomesOwner(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "adopted-acl-*")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	acl := linuxPOSIXACL(uint32(os.Getuid()))
	metadata := platformMetadata{
		uid: os.Getuid() + 1,
		gid: os.Getgid(),
		attributes: []extendedAttribute{{
			name:  "system.posix_acl_access",
			value: acl,
		}},
	}
	permissionDenied := func(int, int) error { return unix.EPERM }
	if err := applyPlatformMetadataWithChown(file, file.Name(), metadata, allowOwnerGroupAdoption, permissionDenied); err != nil {
		if xattrUnavailable(err) || errors.Is(err, unix.EINVAL) {
			t.Skipf("POSIX ACL unavailable: %v", err)
		}
		t.Fatalf("applyPlatformMetadataWithChown() error = %v", err)
	}
	if got := getXattr(t, file.Name(), "system.posix_acl_access"); !bytes.Equal(got, acl) {
		t.Fatalf("adopted ACL = %x, want %x", got, acl)
	}
	if err := file.Chmod(0o600); err != nil {
		t.Fatalf("Chmod(0600) after ACL adoption error = %v", err)
	}
}

func linuxPOSIXACL(namedUserID uint32) []byte {
	const (
		version     = 2
		undefinedID = ^uint32(0)
		userObj     = 0x01
		user        = 0x02
		groupObj    = 0x04
		mask        = 0x10
		other       = 0x20
	)
	entries := []struct {
		tag  uint16
		perm uint16
		id   uint32
	}{
		{tag: userObj, perm: 0x07, id: undefinedID},
		{tag: user, perm: 0x04, id: namedUserID},
		{tag: groupObj, perm: 0x00, id: undefinedID},
		{tag: mask, perm: 0x04, id: undefinedID},
		{tag: other, perm: 0x00, id: undefinedID},
	}
	result := make([]byte, 4+len(entries)*8)
	binary.LittleEndian.PutUint32(result[:4], version)
	for index, entry := range entries {
		offset := 4 + index*8
		binary.LittleEndian.PutUint16(result[offset:offset+2], entry.tag)
		binary.LittleEndian.PutUint16(result[offset+2:offset+4], entry.perm)
		binary.LittleEndian.PutUint32(result[offset+4:offset+8], entry.id)
	}
	return result
}
