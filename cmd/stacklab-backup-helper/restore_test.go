package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func helperArchive(t *testing.T, directory string, entries map[string]string) string {
	t.Helper()
	file, err := os.Create(filepath.Join(directory, "source.tar"))
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	if err = writer.WriteHeader(&tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0750, Uid: os.Getuid(), Gid: os.Getgid(), ModTime: time.Unix(1700000000, 0)}); err != nil {
		t.Fatal(err)
	}
	for name, content := range entries {
		if err = writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Uid: os.Getuid(), Gid: os.Getgid(), Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err = writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err = errors.Join(writer.Close(), file.Close()); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}

func requireHelperTar(t *testing.T) {
	t.Helper()
	name := "tar"
	if runtime.GOOS != "linux" {
		name = "gtar"
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skip("GNU tar required")
	}
}

func canonicalTemp(t *testing.T) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestExactReplacementRemovesNewerFilesAndPreservesMountpoint(t *testing.T) {
	requireHelperTar(t)
	directory := canonicalTemp(t)
	target := canonicalTemp(t)
	archive := helperArchive(t, directory, map[string]string{"database": "snapshot"})
	if err := os.WriteFile(filepath.Join(target, "newer-relfilenode"), []byte("must disappear"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	state := preparedRestore{Target: target, Mode: "replace", Phase: "prepared"}
	if err = validatePreparedTarget(context.Background(), archive, target, "replace"); err != nil {
		t.Fatal(err)
	}
	if err = applyPreparedRestore(context.Background(), directory, archive, &state); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("replacement exchanged target mountpoint inode")
	}
	if after.Mode().Perm() != 0750 || after.ModTime().Unix() != 1700000000 {
		t.Fatalf("root metadata changed: %v %v", after.Mode(), after.ModTime())
	}
	if _, err = os.Stat(filepath.Join(target, "newer-relfilenode")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("newer database file survived: %v", err)
	}
	if data, readErr := os.ReadFile(filepath.Join(target, "database")); readErr != nil || string(data) != "snapshot" {
		t.Fatalf("restore=%s err=%v", data, readErr)
	}
	if state.Phase != "completed" {
		t.Fatalf("phase=%s", state.Phase)
	}
}

func TestMergePreflightRejectsTargetConflicts(t *testing.T) {
	directory := canonicalTemp(t)
	target := canonicalTemp(t)
	archive := helperArchive(t, directory, map[string]string{"entry": "file"})
	if err := os.Mkdir(filepath.Join(target, "entry"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := validatePreparedTarget(context.Background(), archive, target, "merge"); err == nil {
		t.Fatal("merge target type conflict accepted")
	}
	if err := validatePreparedTarget(context.Background(), archive, target, "replace"); err != nil {
		t.Fatalf("exact replacement rejected removable old directory: %v", err)
	}
}

func TestReplacementFailureRestoresPreviousContents(t *testing.T) {
	requireHelperTar(t)
	directory := canonicalTemp(t)
	target := canonicalTemp(t)
	if err := os.WriteFile(filepath.Join(target, "original"), []byte("live data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0751); err != nil {
		t.Fatal(err)
	}
	originalMtime := time.Unix(1600000000, 0)
	if err := os.Chtimes(target, originalMtime, originalMtime); err != nil {
		t.Fatal(err)
	}
	// Simulate an extraction failure after the service committed its write intent.
	archive := filepath.Join(directory, "bad.tar")
	if err := os.WriteFile(archive, []byte("not a tar"), 0600); err != nil {
		t.Fatal(err)
	}
	state := preparedRestore{Target: target, Mode: "replace", Phase: "prepared"}
	if err := applyPreparedRestore(context.Background(), directory, archive, &state); err == nil {
		t.Fatal("invalid archive succeeded")
	}
	if data, err := os.ReadFile(filepath.Join(target, "original")); err != nil || string(data) != "live data" {
		t.Fatalf("original data lost: %s %v", data, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0751 || !info.ModTime().Equal(originalMtime) {
		t.Fatalf("rollback lost root metadata: mode=%v mtime=%v", info.Mode(), info.ModTime())
	}
	if state.Phase != "rolled_back" {
		t.Fatalf("phase=%s", state.Phase)
	}
}

func TestRootSpoolRejectsServiceWritableAncestor(t *testing.T) {
	old := restoreSpoolRoot
	defer func() { restoreSpoolRoot = old }()
	parent := canonicalTemp(t)
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	restoreSpoolRoot = filepath.Join(parent, "spool")
	if err := privateRestoreSpool(); err == nil {
		t.Fatal("writable spool ancestor accepted")
	}
}

func TestPreparedArchiveAcceptsCallerBytesAndIsImmutable(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("root Linux helper integration")
	}
	old := restoreSpoolRoot
	defer func() { restoreSpoolRoot = old }()
	directory, err := os.MkdirTemp("/var/lib", "stacklab-helper-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	restoreSpoolRoot = filepath.Join(directory, "spool")
	source := canonicalTemp(t)
	archive := helperArchive(t, source, map[string]string{"data": "sealed"})
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "target")
	token, err := prepareRestore(context.Background(), bytes.NewReader(data), int64(len(data)), target, "replace")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(archive, []byte("changed caller input"), 0600); err != nil {
		t.Fatal(err)
	}
	path, _ := preparedPath(token)
	sealed, err := os.ReadFile(filepath.Join(path, "source.tar"))
	if err != nil || !bytes.Equal(sealed, data) {
		t.Fatal("prepared archive was mutable through caller path")
	}
	stateData, err := os.ReadFile(filepath.Join(path, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state preparedRestore
	if err = json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	if state.Target != target || state.Mode != "replace" || state.Phase != "prepared" {
		t.Fatalf("unbound prepared state=%+v", state)
	}
	if _, err = prepareRestore(context.Background(), strings.NewReader("short"), int64(len(data)), target, "replace"); err == nil {
		t.Fatal("truncated input accepted")
	}
}

func TestPinnedTargetDoesNotFollowReplacementSymlink(t *testing.T) {
	parent := canonicalTemp(t)
	target := filepath.Join(parent, "target")
	outside := canonicalTemp(t)
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "data"), []byte("selected"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "data"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	root, pinned, err := pinRestoreTarget(target, parent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	defer pinned.Close()
	renamed := filepath.Join(parent, "original")
	if err = os.Rename(target, renamed); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if err = root.RemoveAll("data"); err != nil {
		t.Fatal(err)
	}
	if content, readErr := os.ReadFile(filepath.Join(outside, "data")); readErr != nil || string(content) != "outside" {
		t.Fatalf("outside file changed: %s %v", content, readErr)
	}
	if _, err = os.Stat(filepath.Join(renamed, "data")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("operation left pinned directory: %v", err)
	}
}

func TestPrivateRecoveryTargetsAreRejected(t *testing.T) {
	for _, target := range []string{"/var/lib", "/var/lib/stacklab", "/var/lib/stacklab/backup", "/var/lib/stacklab-backup-helper", "/var/lib/stacklab-backup-helper/prepared"} {
		if err := rejectRecoveryPrivateTarget(target, "/var/lib/stacklab"); err == nil {
			t.Fatalf("private target accepted: %s", target)
		}
	}
}

func TestBatchPreflightAccountsForAllTargetsOnOneFilesystem(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("root Linux helper integration")
	}
	directory, err := os.MkdirTemp("/var/lib", "stacklab-helper-batch-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	oldSpool, oldConfig := restoreSpoolRoot, configurationPath
	defer func() { restoreSpoolRoot, configurationPath = oldSpool, oldConfig }()
	restoreSpoolRoot = filepath.Join(directory, "spool")
	configurationPath = filepath.Join(directory, "stacklab.env")
	if err = os.WriteFile(configurationPath, []byte("STACKLAB_ROOT="+directory+"\nSTACKLAB_DATA_DIR="+filepath.Join(directory, "private")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(directory, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	var tokens []string
	for _, name := range []string{"first", "second"} {
		source := canonicalTemp(t)
		archive := helperArchive(t, source, map[string]string{"data": "snapshot"})
		data, readErr := os.ReadFile(archive)
		if readErr != nil {
			t.Fatal(readErr)
		}
		token, prepareErr := prepareRestore(context.Background(), bytes.NewReader(data), int64(len(data)), filepath.Join(directory, "data", name), "replace")
		if prepareErr != nil {
			t.Fatal(prepareErr)
		}
		tokens = append(tokens, token)
	}
	// Sparse extensions retain the valid small tar stream while allowing a real
	// filesystem admission check without consuming the fixture's disk capacity.
	var space unix.Statfs_t
	if err = unix.Statfs(directory, &space); err != nil {
		t.Fatal(err)
	}
	available := uint64(space.Bavail) * uint64(space.Bsize)
	if available < 1<<30 {
		t.Skip("capacity fixture requires 1 GiB free")
	}
	size := int64(available * 6 / 10)
	for _, token := range tokens {
		path, _ := preparedPath(token)
		if err = os.Truncate(filepath.Join(path, "source.tar"), size); err != nil {
			t.Fatal(err)
		}
	}
	err = checkPreparedRestores(context.Background(), []string{"--token", tokens[0], "--token", tokens[1]})
	if err == nil || !strings.Contains(err.Error(), "shared filesystem capacity") {
		t.Fatalf("combined admission failure not detected: %v", err)
	}
}

func TestRootSpoolExpiresAbandonedPreparationButRetainsInterruptedWrites(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("root Linux helper integration")
	}
	directory, err := os.MkdirTemp("/var/lib", "stacklab-helper-gc-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	old := restoreSpoolRoot
	defer func() { restoreSpoolRoot = old }()
	restoreSpoolRoot = directory
	partial, _ := preparedPath(strings.Repeat("a", 48))
	writing, _ := preparedPath(strings.Repeat("b", 48))
	for _, path := range []string{partial, writing} {
		if err = os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err = savePrepared(writing, preparedRestore{Phase: "writing"}); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-25 * time.Hour)
	for _, path := range []string{partial, writing} {
		if err = os.Chtimes(path, before, before); err != nil {
			t.Fatal(err)
		}
	}
	if err = privateRestoreSpool(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abandoned preparation retained: %v", err)
	}
	if _, err = os.Stat(writing); err != nil {
		t.Fatalf("interrupted write evidence removed: %v", err)
	}
}
