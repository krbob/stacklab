package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCaptureKeepsRestrictedMetadataWithExplicitOfflineRequirement(t *testing.T) {
	var data bytes.Buffer
	archive := tar.NewWriter(&data)
	for _, header := range []*tar.Header{
		{Name: ".", Typeflag: tar.TypeDir, Mode: 0755},
		{Name: "pipe", Typeflag: tar.TypeFifo, Mode: 0600},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/outside"},
		{Name: "privileged", Typeflag: tar.TypeReg, Mode: 04755},
	} {
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	restrictions, err := ValidateCapturedArchive(bytes.NewReader(data.Bytes()))
	if err != nil || len(restrictions) != 3 {
		t.Fatalf("capture rejected faithful archive or omitted restrictions: %v %v", restrictions, err)
	}
	if err = ValidateArchive(bytes.NewReader(data.Bytes()), ""); err == nil {
		t.Fatal("restricted archive became eligible for privileged extraction")
	}
}

func TestCaptureValidationRejectsTruncationAndCancellation(t *testing.T) {
	var data bytes.Buffer
	archive := tar.NewWriter(&data)
	if err := archive.WriteHeader(&tar.Header{Name: "large", Typeflag: tar.TypeReg, Size: 2048, Mode: 0600}); err != nil {
		t.Fatal(err)
	}
	_, _ = archive.Write(make([]byte, 512))
	if _, err := ValidateCapturedArchive(bytes.NewReader(data.Bytes())); err == nil {
		t.Fatal("truncated archive accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ValidateCapturedArchive(captureContextReader{ctx: ctx, reader: bytes.NewReader(data.Bytes())}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}

func TestRecoveryFencesDaemonStopBeforeRestart(t *testing.T) {
	capture, _, _ := captureFixture(t)
	journal := newStopJournal("daemon-stop", []Writer{{ContainerID: "app", Running: true}, {ContainerID: "untouched", Running: true}})
	journal.Phases["app"] = "stop_requested"
	if err := capture.saveStopJournal(journal); err != nil {
		t.Fatal(err)
	}
	pendingStop, running := true, true
	calls := []string{}
	capture.command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "stop":
			pendingStop = false
			running = false
		case "start":
			if pendingStop {
				t.Fatal("docker start raced outstanding daemon stop")
			}
			running = true
		default:
			t.Fatalf("unexpected command %v", args)
		}
		return nil, nil
	}
	if err := capture.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pendingStop || !running || !reflect.DeepEqual(calls, []string{"stop --time 30 app", "start app"}) {
		t.Fatalf("unsafe recovery: pending=%v running=%v calls=%v", pendingStop, running, calls)
	}
}

func TestRecoveryKeepsMissingWriterAndInvalidJournalVisible(t *testing.T) {
	capture, _, _ := captureFixture(t)
	if err := capture.saveJournal("replaced", []Writer{{ContainerID: "old-id", Running: true}}); err != nil {
		t.Fatal(err)
	}
	capture.command = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("No such container") }
	if err := os.WriteFile(filepath.Join(filepath.Dir(capture.journalPath("replaced")), "broken.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := capture.Recover(context.Background()); err == nil {
		t.Fatal("unresolved recovery hidden")
	}
	status, err := capture.RecoveryJournals(context.Background())
	if err != nil || len(status) != 2 {
		t.Fatalf("diagnostics=%+v err=%v", status, err)
	}
	if _, err = os.Stat(capture.journalPath("replaced")); err != nil {
		t.Fatal("missing container journal discarded")
	}
}

func TestStopWriterRespectsConfiguredGracePeriod(t *testing.T) {
	capture, _, _ := captureFixture(t)
	timeout := 95
	capture.command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if !reflect.DeepEqual(args, []string{"stop", "--time", "95", "app"}) {
			t.Fatalf("grace period ignored: %v", args)
		}
		return nil, nil
	}
	if err := capture.stopWriter(context.Background(), Writer{ContainerID: "app", StopTimeoutSeconds: &timeout}); err != nil {
		t.Fatal(err)
	}
	timeout = -1
	if err := capture.stopWriter(context.Background(), Writer{ContainerID: "app", StopTimeoutSeconds: &timeout}); err == nil {
		t.Fatal("unbounded stop accepted")
	}
}

func TestRestorePreflightFailureDoesNotStopAnyWriter(t *testing.T) {
	capture, data, calls := captureFixture(t)
	stage := t.TempDir()
	writeReviewTar(t, filepath.Join(stage, "source.tar"), "payload")
	manifest := Manifest{Version: 1, Sources: []ManifestSource{{Source: Source{ID: "source", Kind: "managed_config"}, SavedPath: "source.tar", Archive: true}}}
	err := capture.ApplyRestore(context.Background(), manifest, []RestoreMapping{{SourceID: "source", Target: data}}, stage, "preflight", true)
	if err == nil || !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("expected helper preflight failure, got %v", err)
	}
	for _, call := range *calls {
		if strings.HasPrefix(call, "stop ") || strings.HasPrefix(call, "start ") {
			t.Fatalf("preflight caused downtime: %s", call)
		}
	}
}

func TestAcknowledgeHeldRecoveryNeverStartsServices(t *testing.T) {
	capture, _, calls := captureFixture(t)
	journal := newStopJournal("held", []Writer{{ContainerID: "app", Running: true}})
	journal.HoldStopped = true
	if err := capture.saveStopJournal(journal); err != nil {
		t.Fatal(err)
	}
	if err := capture.ResolveRecovery(context.Background(), "held", "retry"); err == nil {
		t.Fatal("automatic restart of partially restored service accepted")
	}
	if err := capture.ResolveRecovery(context.Background(), "held", "acknowledge_hold"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("acknowledgement changed containers: %v", *calls)
	}
	if _, err := os.Stat(capture.journalPath("held")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("acknowledgement left journal: %v", err)
	}
}

func TestSingleFileRestoreCannotReplaceItsParentDirectory(t *testing.T) {
	capture, data, _ := captureFixture(t)
	manifest := Manifest{Version: 1, Sources: []ManifestSource{{Source: Source{ID: "file", Kind: "managed_config"}, FileName: "settings.yml"}}}
	preview, err := capture.PreviewRestore(context.Background(), manifest, []RestoreMapping{{SourceID: "file", Target: data, Mode: "replace"}})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Mappings[0].Mode != "merge" {
		t.Fatalf("single-file mapping can delete neighboring files: %+v", preview.Mappings[0])
	}
}
