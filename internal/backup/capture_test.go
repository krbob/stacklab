package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"stacklab/internal/config"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

type captureReaderFixture struct {
	compose string
	root    string
}

func (f captureReaderFixture) Get(context.Context, string) (stacks.StackDetailResponse, error) {
	return stacks.StackDetailResponse{Stack: stacks.StackDetail{RootPath: filepath.Join(f.root, "stacks", "demo"), ConfigPath: filepath.Join(f.root, "config", "demo")}}, nil
}
func (f captureReaderFixture) ResolvedConfigCurrent(context.Context, string, string) (stacks.ResolvedConfigResponse, error) {
	return stacks.ResolvedConfigResponse{Valid: true, Content: f.compose}, nil
}

func captureFixture(t *testing.T) (*NativeCapture, string, *[]string) {
	t.Helper()
	root := t.TempDir()
	state := t.TempDir()
	data := filepath.Join(root, "data", "shared")
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	compose := fmt.Sprintf("services:\n  app:\n    image: example:latest\n    volumes:\n      - type: bind\n        source: %s\n        target: /data\n  sidecar:\n    volumes:\n      - type: bind\n        source: %s\n        target: /data\n", data, data)
	makeContainer := func(id, stack, service string, running, writable bool) captureContainer {
		var container captureContainer
		container.ID = id
		container.State.Running = running
		container.Config.Labels = map[string]string{"com.docker.compose.project": stack, "com.docker.compose.service": service}
		container.Mounts = append(container.Mounts, struct {
			Type, Source, Destination, Name string
			RW                              bool
		}{Type: "bind", Source: data, Destination: "/data", RW: writable})
		return container
	}
	containers := []captureContainer{makeContainer("running-app", "demo", "app", true, true), makeContainer("stopped-sidecar", "demo", "sidecar", false, true), makeContainer("unmanaged-writer", "", "", true, true), makeContainer("reader", "other", "read", true, false)}
	encoded, _ := json.Marshal(containers)
	calls := []string{}
	capture := &NativeCapture{cfg: config.Config{RootDir: root, DataDir: state, BackupHelperPath: "/usr/bin/false"}, stacks: captureReaderFixture{compose: compose, root: root}}
	capture.command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "ps":
			return []byte("running-app\nstopped-sidecar\nunmanaged-writer\nreader\n"), nil
		case "inspect":
			return encoded, nil
		case "stop", "start", "check-target":
			return nil, nil
		}
		return nil, errors.New("unexpected command")
	}
	return capture, data, &calls
}

func TestCaptureDeduplicatesDataAndTracksEveryWriter(t *testing.T) {
	capture, data, _ := captureFixture(t)
	plan := Plan{Sources: []Source{{ID: "one", Kind: "service_mount", StackID: "demo", Service: "app", Mount: "/data", Path: data}, {ID: "two", Kind: "service_mount", StackID: "demo", Service: "sidecar", Mount: "/data", Path: data}}}
	sources, err := capture.Preview(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || len(sources[0].Aliases) != 1 || len(sources[0].Writers) != 3 {
		t.Fatalf("shared source relationships lost: %+v", sources)
	}
	if len(sources[0].Consumers) != 4 {
		t.Fatalf("read-only consumer relationship lost: %+v", sources[0].Consumers)
	}
	writers := uniqueRunningWriters(sources)
	if len(writers) != 2 || writers[0].ContainerID != "running-app" || writers[1].ContainerID != "unmanaged-writer" {
		t.Fatalf("running writers = %+v", writers)
	}
}

func TestCaptureRejectsChangedSelectionAndRuntimeMount(t *testing.T) {
	capture, data, _ := captureFixture(t)
	plan := Plan{Sources: []Source{{ID: "source", Kind: "service_mount", StackID: "demo", Service: "app", Mount: "/data", Path: data + "-changed"}}}
	if _, err := capture.Preview(context.Background(), plan); err == nil {
		t.Fatal("changed bind path accepted")
	}
	plan.Sources[0].Path = data
	base := capture.command
	capture.command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		output, err := base(ctx, name, args...)
		if args[0] == "inspect" {
			output = bytes.ReplaceAll(output, []byte(data), []byte(data+"-drift"))
		}
		return output, err
	}
	if _, err := capture.Preview(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "runtime mount") {
		t.Fatalf("runtime drift = %v", err)
	}
}

func TestCaptureFailureRestoresRunningWritersAndClearsJournal(t *testing.T) {
	capture, data, calls := captureFixture(t)
	plan := Plan{ID: "plan", Consistency: "stop", StagingMaxBytes: 1024 * 1024, StopTimeoutSeconds: 60, Sources: []Source{{ID: "source", Kind: "service_mount", StackID: "demo", Service: "app", Mount: "/data", Path: data}}}
	_, err := capture.Prepare(context.Background(), plan, "run", t.TempDir())
	if err == nil {
		t.Fatal("expected failing archive helper")
	}
	joined := strings.Join(*calls, "\n")
	for _, expected := range []string{"stop --time 30 running-app", "stop --time 30 unmanaged-writer", "start running-app", "start unmanaged-writer"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %s in %s", expected, joined)
		}
	}
	if strings.Contains(joined, "start stopped-sidecar") || strings.Contains(joined, "start reader") {
		t.Fatal("changed previously stopped or readonly service")
	}
	if _, err := os.Stat(capture.journalPath("run")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal remains: %v", err)
	}
}

func TestRecoveryRetainsFailedJournalAndCanRetry(t *testing.T) {
	capture, _, calls := captureFixture(t)
	if err := capture.saveJournal("interrupted", []Writer{{ContainerID: "was-running", Running: true}, {ContainerID: "was-stopped", Running: false}}); err != nil {
		t.Fatal(err)
	}
	capture.command = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("Docker unavailable") }
	if err := capture.Recover(context.Background()); err == nil {
		t.Fatal("failed restart was hidden")
	}
	if _, err := os.Stat(capture.journalPath("interrupted")); err != nil {
		t.Fatal("failed restart lost journal")
	}
	capture.command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(args, " "))
		return nil, nil
	}
	if err := capture.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*calls, []string{"stop --time 30 was-running", "start was-running"}) {
		t.Fatalf("restart calls = %v", *calls)
	}
}

func TestStateCaptureExcludesTransientDataAndEnforcesBudget(t *testing.T) {
	root := t.TempDir()
	state := t.TempDir()
	stage := filepath.Join(state, "backup", "staging", "run")
	for name, content := range map[string]string{"stacklab.db": "live-db", "stacklab.db-wal": "wal", "backup/credentials/destination.json": "secret", "backup/cache/x": "cache", "backup/restores/old/x": "restore", "backup/staging/old/x": "previous", "settings": "settings"} {
		path := filepath.Join(state, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(stage, 0700); err != nil {
		t.Fatal(err)
	}
	capture := &NativeCapture{cfg: config.Config{RootDir: root, DataDir: state, DatabasePath: filepath.Join(state, "stacklab.db")}, serviceConfigPath: filepath.Join(root, "missing.env"), sqliteBackup: func(_ context.Context, target string) error {
		return os.WriteFile(target, []byte("consistent-db"), 0600)
	}}
	path, cleanup, _, err := capture.prepareState(context.Background(), stage, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if contents, err := os.ReadFile(filepath.Join(path, "stacklab.db")); err != nil || string(contents) != "consistent-db" {
		t.Fatalf("snapshot = %s %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(path, "private-state", "backup", "credentials", "destination.json")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"stacklab.db", "stacklab.db-wal", "backup/staging", "backup/restores", "backup/cache"} {
		if _, err := os.Stat(filepath.Join(path, "private-state", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("transient %s retained: %v", name, err)
		}
	}
	if _, _, _, err = capture.prepareState(context.Background(), stage, 4); err == nil {
		t.Fatal("small state budget accepted")
	}
}

func TestArchiveValidationRejectsEscapesAndPreservesSafeLinks(t *testing.T) {
	tests := []struct {
		name    string
		headers []*tar.Header
		valid   bool
	}{
		{"traversal", []*tar.Header{{Name: "../escape", Typeflag: tar.TypeReg}}, false},
		{"absolute", []*tar.Header{{Name: "/etc/passwd", Typeflag: tar.TypeReg}}, false},
		{"symlink escape", []*tar.Header{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../etc"}}, false},
		{"write through link", []*tar.Header{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "dir"}, {Name: "link/file", Typeflag: tar.TypeReg}}, false},
		{"hardlink escape", []*tar.Header{{Name: "link", Typeflag: tar.TypeLink, Linkname: "../outside"}}, false},
		{"device", []*tar.Header{{Name: "device", Typeflag: tar.TypeChar}}, false},
		{"setuid", []*tar.Header{{Name: "binary", Typeflag: tar.TypeReg, Mode: 04755}}, false},
		{"setgid file", []*tar.Header{{Name: "binary", Typeflag: tar.TypeReg, Mode: 02755}}, false},
		{"setgid directory", []*tar.Header{{Name: "directory", Typeflag: tar.TypeDir, Mode: 02775}}, true},
		{"file capability", []*tar.Header{{Name: "binary", Typeflag: tar.TypeReg, Mode: 0755, Xattrs: map[string]string{"security.capability": "untrusted"}}}, false},
		{"PAX file capability", []*tar.Header{{Name: "binary", Typeflag: tar.TypeReg, Mode: 0755, PAXRecords: map[string]string{"SCHILY.xattr.security.capability": "untrusted"}}}, false},
		{"ordinary metadata", []*tar.Header{{Name: "file", Typeflag: tar.TypeReg, Mode: 0640, Uid: 1000, Gid: 1000, Xattrs: map[string]string{"user.backup": "preserved"}}}, true},
		{"safe links", []*tar.Header{{Name: "file", Typeflag: tar.TypeReg}, {Name: "hardlink", Typeflag: tar.TypeLink, Linkname: "file"}, {Name: "symlink", Typeflag: tar.TypeSymlink, Linkname: "file"}}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buffer bytes.Buffer
			writer := tar.NewWriter(&buffer)
			for _, header := range test.headers {
				if err := writer.WriteHeader(header); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			err := ValidateArchive(&buffer, "")
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
	target := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(target, "link")); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	_ = writer.WriteHeader(&tar.Header{Name: "link/file", Typeflag: tar.TypeReg})
	_ = writer.Close()
	if err := ValidateArchive(&buffer, target); err == nil {
		t.Fatal("existing destination symlink accepted")
	}
	buffer.Reset()
	writer = tar.NewWriter(&buffer)
	_ = writer.WriteHeader(&tar.Header{Name: "link/child", Typeflag: tar.TypeSymlink, Linkname: "safe-relative-target"})
	_ = writer.Close()
	if err := ValidateArchive(&buffer, target); err == nil {
		t.Fatal("safe final symlink bypassed unsafe existing ancestor")
	}
	buffer.Reset()
	writer = tar.NewWriter(&buffer)
	_ = writer.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "safe-relative-target"})
	_ = writer.Close()
	if err := ValidateArchive(&buffer, target); err != nil {
		t.Fatalf("safe replacement of final symlink rejected: %v", err)
	}
}

func TestRestoreRecoveryHoldDoesNotStartPartiallyRestoredWriters(t *testing.T) {
	capture, _, calls := captureFixture(t)
	if err := capture.saveStopJournal(stopJournal{Version: 1, RunID: "restore", Writers: []Writer{{ContainerID: "running-app", Running: true}}, HoldStopped: true}); err != nil {
		t.Fatal(err)
	}
	if err := capture.Recover(context.Background()); err == nil || !strings.Contains(err.Error(), "requires inspection") {
		t.Fatalf("held restore was not surfaced: %v", err)
	}
	if _, err := os.Stat(capture.journalPath("restore")); err != nil {
		t.Fatalf("held journal lost: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("partially restored application restarted: %v", *calls)
	}
}

func TestManifestArchivePathRejectsSymlinkAndTraversal(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "source.tar")
	if err := os.WriteFile(target, []byte("archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.tar")); err != nil {
		t.Fatal(err)
	}
	for _, saved := range []string{"../escape", ".", "link.tar", "/etc/passwd"} {
		if _, err := ManifestArchivePath(root, ManifestSource{Archive: true, SavedPath: saved}); err == nil {
			t.Fatalf("unsafe saved path accepted: %s", saved)
		}
	}
	if _, err := ManifestArchivePath(root, ManifestSource{Archive: true, SavedPath: "source.tar"}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCaptureRestorePreservesFileMetadata(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GNU tar production metadata integration runs on Linux")
	}
	root := t.TempDir()
	state := t.TempDir()
	source := filepath.Join(root, "config", "app")
	if err := os.MkdirAll(source, 0750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(source, "data")
	if err := os.WriteFile(file, []byte("recover me"), 0640); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1700000000, 0)
	if err := os.Chtimes(file, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file, filepath.Join(source, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("data", filepath.Join(source, "symlink")); err != nil {
		t.Fatal(err)
	}
	xattr := unix.Setxattr(file, "user.stacklab-test", []byte("preserved"), 0) == nil
	capture := NewNativeCapture(config.Config{RootDir: root, DataDir: state}, nil, nil)
	plan := Plan{ID: "plan", Consistency: "files", StagingMaxBytes: 1024 * 1024, Sources: []Source{{ID: "config", Kind: "managed_config", Path: source}}}
	stage := t.TempDir()
	manifest, err := capture.Prepare(context.Background(), plan, "run", stage)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Sources) != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
	archive, err := ManifestArchivePath(stage, manifest.Sources[0])
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	if err := capture.RestoreArchive(context.Background(), archive, target, false); err != nil {
		t.Fatal(err)
	}
	// An ordinary second in-place extraction must safely replace the existing
	// relative symlink instead of rejecting its presence as an ancestor escape.
	if err := capture.RestoreArchive(context.Background(), archive, target, false); err != nil {
		t.Fatalf("repeat recovery of existing symlink failed: %v", err)
	}
	restored := filepath.Join(target, "data")
	data, err := os.ReadFile(restored)
	if err != nil || string(data) != "recover me" {
		t.Fatalf("restored data = %q, %v", data, err)
	}
	info, err := os.Stat(restored)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 || !info.ModTime().Equal(mtime) {
		t.Fatalf("metadata = %v %v", info.Mode(), info.ModTime())
	}
	linked, err := os.Stat(filepath.Join(target, "hardlink"))
	if err != nil || !os.SameFile(info, linked) {
		t.Fatalf("hardlink identity lost: %v", err)
	}
	if link, err := os.Readlink(filepath.Join(target, "symlink")); err != nil || link != "data" {
		t.Fatalf("symlink=%q %v", link, err)
	}
	if xattr {
		value := make([]byte, 64)
		size, err := unix.Getxattr(restored, "user.stacklab-test", value)
		if err != nil || string(value[:size]) != "preserved" {
			t.Fatalf("xattr = %q, %v", value, err)
		}
	}
	plan.StagingMaxBytes = 100
	if _, err := capture.Prepare(context.Background(), plan, "limited", t.TempDir()); err == nil {
		t.Fatal("archive exceeded staging byte budget")
	}
}

func TestNativeProtectedServiceConfigurationRecovery(t *testing.T) {
	helper := os.Getenv("STACKLAB_TEST_BACKUP_HELPER")
	if runtime.GOOS != "linux" || helper == "" {
		t.Skip("set STACKLAB_TEST_BACKUP_HELPER on a Linux helper-enabled test host")
	}
	if os.Geteuid() == 0 {
		t.Fatal("this test must run as the non-root service identity")
	}
	file, err := os.Open("/etc/stacklab/stacklab.env")
	if err == nil {
		file.Close()
		t.Fatal("test requires a root-owned configuration unreadable by the service")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
	state := t.TempDir()
	database := filepath.Join(state, "stacklab.db")
	appStore, err := store.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer appStore.Close()
	if err = appStore.SetAppSetting(context.Background(), "capture-test", `{"committed":true}`, time.Now()); err != nil {
		t.Fatal(err)
	}
	capture := NewNativeCapture(config.Config{RootDir: t.TempDir(), DataDir: state, DatabasePath: database, BackupHelperPath: helper, BackupUseSudo: true}, nil, appStore.Snapshot)
	stage := t.TempDir()
	manifest, err := capture.Prepare(context.Background(), Plan{ID: "state", Consistency: "files", StagingMaxBytes: 16 * 1024 * 1024, Sources: []Source{{ID: "state", Kind: "stacklab"}}}, "state-run", stage)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := ManifestArchivePath(stage, manifest.Sources[0])
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	if err = capture.RestoreArchive(context.Background(), archive, target, false); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(target, "stacklab.env")); err != nil || info.Size() == 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("protected service configuration was not privately recovered: %v", err)
	}
	restored, err := store.Open(filepath.Join(target, "stacklab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	value, found, err := restored.AppSetting(context.Background(), "capture-test")
	if err != nil || !found || value != `{"committed":true}` {
		t.Fatal("consistent SQLite state was not recovered")
	}
}

func TestCaptureCancellationTerminatesDescendantPipes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & wait")
	configureCaptureProcess(cmd)
	var output bytes.Buffer
	cmd.Stdout = &output
	started := time.Now()
	if err := cmd.Run(); err == nil {
		t.Fatal("cancelled process succeeded")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("descendant retained archive pipe after cancellation")
	}
}

func TestRestorePreviewRejectsUnapprovedAndPrivateAliasedTargets(t *testing.T) {
	capture, data, _ := captureFixture(t)
	manifest := Manifest{Version: 1, Sources: []ManifestSource{{Source: Source{ID: "source", Kind: "managed_config"}}}}
	for _, target := range []string{t.TempDir(), capture.cfg.DataDir} {
		if _, err := capture.PreviewRestore(context.Background(), manifest, []RestoreMapping{{SourceID: "source", Target: target}}); err == nil {
			t.Fatalf("unsafe restore target accepted: %s", target)
		}
	}
	alias := filepath.Join(filepath.Dir(data), "private-alias")
	if err := os.Symlink(capture.cfg.DataDir, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := capture.PreviewRestore(context.Background(), manifest, []RestoreMapping{{SourceID: "source", Target: filepath.Join(alias, "new")}}); err == nil {
		t.Fatal("private state alias accepted")
	}
}

func TestRestoreFailureAfterWriteNeverRestartsWriters(t *testing.T) {
	capture, data, calls := captureFixture(t)
	stage := t.TempDir()
	archive := filepath.Join(stage, "source.tar")
	writeReviewTar(t, archive, "recovery")
	helper := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ncat >/dev/null\nprintf '0123456789abcdef0123456789abcdef0123456789abcdef\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	capture.cfg.BackupHelperPath = helper
	base := capture.command
	capture.command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if args[0] == "check-restores" {
			return nil, nil
		}
		if args[0] == "apply-restore" {
			return nil, errors.New("partial write")
		}
		return base(ctx, name, args...)
	}
	manifest := Manifest{Version: 1, Sources: []ManifestSource{{Source: Source{ID: "source", Kind: "managed_config"}, SavedPath: "source.tar", Archive: true}}}
	err := capture.ApplyRestore(context.Background(), manifest, []RestoreMapping{{SourceID: "source", Target: data}}, stage, "failed-restore", true)
	if err == nil || !strings.Contains(err.Error(), "remain stopped") {
		t.Fatalf("partial restore outcome = %v", err)
	}
	for _, call := range *calls {
		if strings.HasPrefix(call, "start ") {
			t.Fatalf("partially restored writer restarted: %s", call)
		}
	}
	dataBytes, err := os.ReadFile(capture.journalPath("failed-restore"))
	if err != nil {
		t.Fatalf("recovery journal lost: %v", err)
	}
	var journal stopJournal
	if err = json.Unmarshal(dataBytes, &journal); err != nil || !journal.HoldStopped || len(journal.PreparedTokens) != 1 {
		t.Fatalf("incomplete hold journal: %s, %v", dataBytes, err)
	}
}

func TestCaptureFindsAliasWriterAfterReadOnlyMount(t *testing.T) {
	capture, data, _ := captureFixture(t)
	alias := filepath.Join(filepath.Dir(data), "alias")
	if err := os.Symlink(data, alias); err != nil {
		t.Fatal(err)
	}
	base := capture.command
	capture.command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		output, err := base(ctx, name, args...)
		if args[0] != "inspect" {
			return output, err
		}
		var containers []captureContainer
		if err := json.Unmarshal(output, &containers); err != nil {
			return nil, err
		}
		reader := &containers[len(containers)-1]
		reader.Mounts[0].Source = alias
		extra := reader.Mounts[0]
		extra.RW = true
		extra.Destination = "/second-mount"
		reader.Mounts = append(reader.Mounts, extra)
		return json.Marshal(containers)
	}
	sources, err := capture.Preview(context.Background(), Plan{Sources: []Source{{ID: "data", Kind: "service_mount", StackID: "demo", Service: "app", Mount: "/data", Path: data}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources[0].Writers) != 4 || len(sources[0].Consumers) != 4 {
		t.Fatalf("alias writer missing/duplicated: %+v", sources[0])
	}
	for _, consumer := range sources[0].Consumers {
		if consumer.ContainerID == "reader" && consumer.ReadOnly {
			t.Fatal("writable second mount was misclassified as read-only")
		}
	}
}

func TestRecoveryRejectsMalformedJournalBeforeDocker(t *testing.T) {
	capture, _, calls := captureFixture(t)
	if err := capture.saveJournal("bad", []Writer{{ContainerID: "--attach", Running: true}}); err != nil {
		t.Fatal(err)
	}
	if err := capture.Recover(context.Background()); err == nil {
		t.Fatal("malformed journal accepted")
	}
	if len(*calls) != 0 {
		t.Fatalf("Docker executed for untrusted journal: %v", *calls)
	}
}

func TestCaptureCoordinatesNamedVolumeAndBindWritersOnSameData(t *testing.T) {
	for _, selectedKind := range []string{"bind", "volume"} {
		t.Run(selectedKind, func(t *testing.T) {
			capture, data, _ := captureFixture(t)
			nested := filepath.Join(data, "nested")
			if err := os.MkdirAll(nested, 0700); err != nil {
				t.Fatal(err)
			}
			source := Source{ID: "selected", Kind: "service_mount", StackID: "demo", Service: "app", Mount: "/data", Path: data}
			if selectedKind == "volume" {
				capture.stacks = captureReaderFixture{root: capture.cfg.RootDir, compose: "services:\n  app:\n    volumes:\n      - type: volume\n        source: data\n        target: /data\nvolumes:\n  data:\n    name: shared_data\n"}
				source.Path = "shared_data"
			}
			base := capture.command
			capture.command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if args[0] == "volume" {
					return json.Marshal([]captureVolume{{Name: "shared_data", Driver: "local", Mountpoint: data}})
				}
				output, err := base(ctx, name, args...)
				if args[0] != "inspect" {
					return output, err
				}
				var containers []captureContainer
				if err := json.Unmarshal(output, &containers); err != nil {
					return nil, err
				}
				if selectedKind == "volume" {
					containers[0].Mounts[0].Type = "volume"
					containers[0].Mounts[0].Name = "shared_data"
					// This writer uses a bind of a child directory inside the named
					// volume, rather than the same Docker volume identity.
					containers[2].Mounts[0].Source = nested
				} else {
					// Selecting a bind must also find a named-volume consumer whose
					// mountpoint overlaps the selected physical source.
					containers[2].Mounts[0].Type = "volume"
					containers[2].Mounts[0].Name = "shared_data"
				}
				return json.Marshal(containers)
			}
			sources, err := capture.Preview(context.Background(), Plan{Sources: []Source{source}})
			if err != nil {
				t.Fatal(err)
			}
			writers := uniqueRunningWriters(sources)
			if len(writers) != 2 || writers[0].ContainerID != "running-app" || writers[1].ContainerID != "unmanaged-writer" {
				t.Fatalf("cross-type writer was omitted: %+v", writers)
			}
			if len(sources[0].Writers) != 3 || len(sources[0].Consumers) != 4 {
				t.Fatalf("cross-type relationships were omitted: %+v", sources[0])
			}
		})
	}
}
