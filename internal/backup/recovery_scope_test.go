package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExcludedSourceCannotDeleteUnarchivedLiveData(t *testing.T) {
	capture, target, calls := captureFixture(t)
	if err := os.WriteFile(filepath.Join(target, "excluded-live-data"), []byte("only copy"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1, Sources: []ManifestSource{{Source: Source{ID: "source", Kind: "managed_config", Excludes: []string{"excluded*"}}, Archive: true}}}
	for _, mode := range []string{"", "replace"} {
		_, err := capture.PreviewRestore(context.Background(), manifest, []RestoreMapping{{SourceID: "source", Target: target, Mode: mode}})
		if err == nil || !strings.Contains(err.Error(), "exclusions") {
			t.Fatalf("filtered replacement accepted: %v", err)
		}
	}
	for _, call := range *calls {
		if strings.HasPrefix(call, "stop ") {
			t.Fatalf("refusal stopped service: %s", call)
		}
	}
	data, err := os.ReadFile(filepath.Join(target, "excluded-live-data"))
	if err != nil || string(data) != "only copy" {
		t.Fatalf("live data lost: %s %v", data, err)
	}
	preview, err := capture.PreviewRestore(context.Background(), manifest, []RestoreMapping{{SourceID: "source", Target: target, Mode: "merge"}})
	if err != nil || preview.Mappings[0].Mode != "merge" || preview.Mappings[0].sourceScope != "filtered" {
		t.Fatalf("explicit merge: %+v %v", preview, err)
	}
}

func TestRestoreRejectsManagedAndConfiguredBoundaryTargets(t *testing.T) {
	capture, target, _ := captureFixture(t)
	capture.cfg.BackupAllowedPaths = []string{t.TempDir()}
	manifest := Manifest{Version: 1, Sources: []ManifestSource{{Source: Source{ID: "source", Kind: "managed_config"}, Archive: true}}}
	for _, root := range append([]string{filepath.Join(capture.cfg.RootDir, "data"), filepath.Join(capture.cfg.RootDir, "stacks"), filepath.Join(capture.cfg.RootDir, "config")}, capture.cfg.BackupAllowedPaths...) {
		if _, err := capture.PreviewRestore(context.Background(), manifest, []RestoreMapping{{SourceID: "source", Target: root}}); err == nil || !strings.Contains(err.Error(), "root") {
			t.Fatalf("boundary accepted: %s: %v", root, err)
		}
	}
	if _, err := capture.PreviewRestore(context.Background(), manifest, []RestoreMapping{{SourceID: "source", Target: target}}); err != nil {
		t.Fatalf("individual source rejected: %v", err)
	}
}

func TestHelperReadinessChecksEveryPrivilegedRecoveryAction(t *testing.T) {
	capture, _, _ := captureFixture(t)
	for _, failing := range []string{"prepare-restore", "check-restores", "apply-restore", "release-restore", ""} {
		calls := 0
		capture.command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
			calls++
			if len(args) != 2 || args[1] != "--probe" {
				t.Fatalf("probe mutated target: %v", args)
			}
			if args[0] == failing {
				return nil, errors.New("sudo denied private diagnostic")
			}
			return json.Marshal(map[string]any{"protocol": RecoveryHelperProtocol, "action": args[0]})
		}
		err := capture.CheckRecoveryHelper(context.Background())
		if failing == "" {
			if err != nil || calls != 6 {
				t.Fatalf("readiness=%v calls=%d", err, calls)
			}
		} else if err == nil || !strings.Contains(err.Error(), "sudoers") || strings.Contains(err.Error(), "private diagnostic") {
			t.Fatalf("unsafe diagnostic: %v", err)
		}
	}
}

func TestDeleteManagedRestoreReadOnlyDirectoriesKeepsOutsideLinks(t *testing.T) {
	s, _, _ := newTestService(t)
	target := filepath.Join(s.directory, "restores", "restore_readonly")
	readonly := filepath.Join(target, "readonly")
	if err := os.MkdirAll(readonly, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(readonly, "data"), []byte("delete"), 0400); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "data"), []byte("retain"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(readonly, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readonly, 0555); err != nil {
		t.Fatal(err)
	}
	result := RestoreResult{ID: "restore_readonly", Target: target}
	if err := s.registerRestoreResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRestore(context.Background(), result.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed data remains: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outside, "data"))
	if err != nil || string(data) != "retain" {
		t.Fatalf("outside data modified: %s %v", data, err)
	}
	info, err := os.Stat(filepath.Join(outside, "data"))
	if err != nil || info.Mode().Perm() != 0400 {
		t.Fatalf("outside permissions changed: %v %v", info, err)
	}
}
