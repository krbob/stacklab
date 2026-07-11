package fsmeta

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspectReportsModeOwnershipAndAccess(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(path, []byte("services: {}\n"), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	permissions := Inspect(path, info)
	if permissions.Mode != "0640" {
		t.Fatalf("Inspect().Mode = %q, want 0640", permissions.Mode)
	}
	if !permissions.Readable || !permissions.Writable {
		t.Fatalf("Inspect() access = readable %t writable %t, want true/true", permissions.Readable, permissions.Writable)
	}
	if permissions.OwnerUID == nil || *permissions.OwnerUID != os.Getuid() {
		t.Fatalf("Inspect().OwnerUID = %#v, want %d", permissions.OwnerUID, os.Getuid())
	}
	if permissions.GroupGID == nil || *permissions.GroupGID != os.Getgid() {
		t.Fatalf("Inspect().GroupGID = %#v, want %d", permissions.GroupGID, os.Getgid())
	}
	if permissions.OwnerName == nil || *permissions.OwnerName == "" {
		t.Fatalf("Inspect().OwnerName = %#v, want resolved name", permissions.OwnerName)
	}
	if permissions.GroupName == nil || *permissions.GroupName == "" {
		t.Fatalf("Inspect().GroupName = %#v, want resolved name", permissions.GroupName)
	}
}

func TestInspectSeparatesMetadataFromPathAccess(t *testing.T) {
	t.Parallel()

	info := syntheticFileInfo{mode: 0o604}
	permissions := Inspect(filepath.Join(t.TempDir(), "missing"), info)
	if permissions.Mode != "0604" {
		t.Fatalf("Inspect().Mode = %q, want 0604", permissions.Mode)
	}
	if permissions.Readable || permissions.Writable {
		t.Fatalf("Inspect() missing path access = readable %t writable %t, want false/false", permissions.Readable, permissions.Writable)
	}
	if permissions.OwnerUID != nil || permissions.OwnerName != nil || permissions.GroupGID != nil || permissions.GroupName != nil {
		t.Fatalf("Inspect() synthetic ownership = %#v, want unset", permissions)
	}
}

func TestIdentityLookupsRejectUnknownIDs(t *testing.T) {
	t.Parallel()

	const unknownID = 1<<31 - 1
	if name, ok := lookupUser(unknownID); ok || name != "" {
		t.Fatalf("lookupUser(%d) = %q, %t; want empty/false", unknownID, name, ok)
	}
	if name, ok := lookupGroup(unknownID); ok || name != "" {
		t.Fatalf("lookupGroup(%d) = %q, %t; want empty/false", unknownID, name, ok)
	}
}

type syntheticFileInfo struct {
	mode os.FileMode
}

func (info syntheticFileInfo) Name() string       { return "synthetic" }
func (info syntheticFileInfo) Size() int64        { return 0 }
func (info syntheticFileInfo) Mode() os.FileMode  { return info.mode }
func (info syntheticFileInfo) ModTime() time.Time { return time.Time{} }
func (info syntheticFileInfo) IsDir() bool        { return false }
func (info syntheticFileInfo) Sys() any           { return nil }
