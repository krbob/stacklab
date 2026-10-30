package backup

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/restic-check/r10/*.stderr
var r10NativeDiagnostics embed.FS

func TestNativeRoundTenDiagnosticCorpus(t *testing.T) {
	files, err := fs.Glob(r10NativeDiagnostics, "testdata/restic-check/r10/*.stderr")
	if err != nil || len(files) != 10 {
		t.Fatalf("native corpus: %v, %v", files, err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := r10NativeDiagnostics.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			args, exit, code := []string{"check"}, "0", ""
			if strings.Contains(file, "missing-lock") {
				args, exit, code = []string{"cat", "lock"}, "1", "unlock_refused"
			}
			err, _ = runR10Diagnostic(t, args, string(data), exit)
			assertR10Outcome(t, err, code)
		})
	}
}

func TestLockDiagnosticsRespectCommandAndEvidence(t *testing.T) {
	for _, cause := range []string{
		"open /repo/locks/aaaaaaaa: no such file or directory",
		"The specified key does not exist.",
		"Open /srv/repo/locks/aaaaaaaa: file does not exist",
		"<lock/aaaaaaaa> does not exist",
	} {
		t.Run(cause, func(t *testing.T) {
			lock := "Load(<lock/aaaaaaaa>, 0, 0) failed: " + cause + "\n"
			for _, tc := range []struct{ command, extra, code string }{
				{"check", "unsupported repository structure", "check_inconclusive"},
				{"check", "ciphertext verification failed", "integrity_failed"},
				{"check", "", ""},
				{"backup", "Load(<data/bbbbbbbb>, 0, 0) failed: EOF", "unavailable"},
				{"cat", "", "unlock_refused"},
			} {
				t.Run(tc.command+tc.code, func(t *testing.T) {
					args := []string{tc.command}
					if tc.command == "cat" {
						args = append(args, "lock", strings.Repeat("a", 64), "--no-lock")
					}
					exit := "0"
					if tc.command != "check" {
						exit = "1"
					}
					err, _ := runR10Diagnostic(t, args, lock+tc.extra, exit)
					assertR10Outcome(t, err, tc.code)
					if tc.command == "check" && tc.extra != "" {
						d := Destination{}
						recordCheckAttempt(&d, "all", time.Now(), err)
						if !repositoryMaintenanceBlocked(d) {
							t.Fatal("diagnostic did not block automatic maintenance")
						}
					}
				})
			}
		})
	}
}

func TestLegacyIndexRecoveryAtDataListing(t *testing.T) {
	index := "Load(<index/aaaaaaaa>, 0, 0) returned error, retrying after 1s: provider failure\n"
	for _, phase := range []string{"returned error, retrying after 1s: provider failure\nprovider failure", "failed: provider failure", "operation successful after 1 retries"} {
		assertDiagnosticCode(t, index+"List(data) "+phase+"\nerror for tree bbbbbbbb:\nid cccccccc not found in repository", "integrity_failed")
	}
	assertDiagnosticCode(t, index+"unknown index diagnostic\nList(data) failed: provider failure\nid cccccccc not found in repository", "check_inconclusive")
	assertDiagnosticCode(t, "Load(<index/aaaaaaaa>, 0, 0) failed: provider failure\nList(data) failed: provider failure\nid cccccccc not found in repository", "unavailable")
	for _, cause := range []string{"Open /srv/repo/data/aa/aaaaaaaa: file does not exist", "<data/aaaaaaaa> does not exist"} {
		assertDiagnosticCode(t, "Load(<data/aaaaaaaa>, 0, 0) failed: "+cause, "check_inconclusive")
	}
}

func TestHousekeepingPreservesIndependentEvidence(t *testing.T) {
	for _, message := range []string{
		"unable to create cache directory /SECRET, disabling cache: not a directory",
		"unable to open cache: SECRET",
		"unable to find old cache directories: SECRET",
		"error while unlocking: context canceled",
		"error while unlocking: SECRET provider response",
		"rclone: 2026/10/30 10:00:00 NOTICE: snapshots/zz-link: Can't follow symlink without -L/--copy-links",
		`rclone: 2026/10/30 10:00:00 NOTICE: Config file "/SECRET" not found - using defaults`,
	} {
		t.Run(message, func(t *testing.T) {
			for _, tail := range []struct{ text, code string }{
				{"", ""},
				{"\nunsupported repository structure", "check_inconclusive"},
				{`{"message_type":"error","message":"unsupported repository structure"}`, "check_inconclusive"},
				{`{"message_type":"error","message":"ciphertext verification failed"}`, "integrity_failed"},
			} {
				err, warnings := runR10Diagnostic(t, []string{"check"}, message+tail.text, "0")
				assertR10Outcome(t, err, tail.code)
				if len(warnings) != 1 || strings.Contains(strings.Join(warnings, " "), "SECRET") {
					t.Fatalf("unsafe/missing warning: %v", warnings)
				}
			}
		})
	}
	for _, message := range []string{
		"rclone: 2026/10/30 10:00:00 NOTICE: snapshots/" + strings.Repeat("a", 64) + ": Can't follow symlink without -L/--copy-links",
		"rclone: 2026/10/30 10:00:00 NOTICE: config: Can't follow symlink without -L/--copy-links",
		"rclone: 2026/10/30 10:00:00 NOTICE: unknown storage warning",
		"rclone: 2026/10/30 10:00:00 INFO: unknown storage warning",
		`unable to open cache: SECRET{"message_type":"error","message":`,
	} {
		err, _ := runR10Diagnostic(t, []string{"check"}, message, "0")
		assertR10Outcome(t, err, "check_inconclusive")
	}
}

func TestConcatenatedDiagnosticsAcrossWrites(t *testing.T) {
	text := `unable to find old cache directories: SECRET{"message_type":"error","message":"unsupported repository structure"}`
	for split := 0; split <= len(text); split++ {
		stream := diagnosticStream{}
		_, _ = stream.Write([]byte(text[:split]))
		_, _ = stream.Write([]byte(text[split:]))
		stream.finish()
		if !stream.evidence.cacheWarning || !stream.evidence.unexplainedFailure() {
			t.Fatalf("record lost at split %d", split)
		}
	}
}

func TestResticInvocationDoesNotTreatOptionValuesAsCommands(t *testing.T) {
	err, _ := runR10Diagnostic(t, []string{"--repo", "check", "--json", "cat", "lock", strings.Repeat("a", 64)}, "Load(<lock/aaaaaaaa>, 0, 0) failed: <lock/aaaaaaaa> does not exist", "1")
	assertR10Outcome(t, err, "unlock_refused")
	assertDiagnosticCode(t, "Save(<data/aaaaaaaa>) failed: no space left on device", "storage_full")
}

func runR10Diagnostic(t *testing.T, args []string, text, exit string) (error, []string) {
	t.Helper()
	dir := t.TempDir()
	program := filepath.Join(dir, "restic")
	script := "#!/bin/sh\nprintf '%s' " + shellWord(text) + " >&2\nprintf '%s\\n' 'no errors were found'\nexit " + exit + "\n"
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	ctx := context.WithValue(context.Background(), repositoryWarningKey{}, repositoryWarning(func(message string) { warnings = append(warnings, message) }))
	_, err := (nativeRunner{}).Run(ctx, program, args, cleanProcessEnvironment(), dir)
	return err, warnings
}

func assertR10Outcome(t *testing.T, err error, code string) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var diagnostic *repositoryCommandError
	if !errors.As(err, &diagnostic) || diagnostic.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}
