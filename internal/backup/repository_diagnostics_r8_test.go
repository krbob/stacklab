package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticOperationLifecycle(t *testing.T) {
	for _, tc := range []struct{ name, transcript, code string }{
		{"legacy S3 listing", "List(data) returned error, retrying after 1s: We encountered an internal error. Please try again.\nWe encountered an internal error. Please try again.\npack aaaaaaaa: does not exist\nFatal: repository contains errors", "unavailable"},
		{"legacy SFTP listing", "List(data) returned error, retrying after 1s: Walk /repo/data: connection lost\nWalk /repo/data: connection lost\npack aaaaaaaa: does not exist", "unavailable"},
		{"legacy unresolved listing", "List(data) returned error, retrying after 1s: EOF\npack aaaaaaaa: does not exist", "check_inconclusive"},
		{"recovered listing still finds missing pack", "List(data) returned error, retrying after 1s: EOF\nList(data) operation successful after 1 retries\npack aaaaaaaa: does not exist", "integrity_failed"},
		{"legacy index", "List(index) returned error, retrying after 1s: provider failure\nprovider failure\nid aaaaaaaa not found in repository", "unavailable"},
		{"unrelated retry", "Load(<data/bbbbbbbb>, 0, 0) returned error, retrying after 1s: EOF\npack aaaaaaaa: does not exist", "integrity_failed"},
		{"legacy download", "pack aaaaaaaa failed to download: connection reset by peer\nFatal: repository contains errors", "unavailable"},
		{"legacy provider download", "pack aaaaaaaa failed to download: We encountered an internal error. Please try again.", "unavailable"},
		{"legacy truncated download", "pack aaaaaaaa failed to download: file is too short: readFull: unexpected EOF", "integrity_failed"},
		{"legacy denied download", "pack aaaaaaaa failed to download: Access Denied", "permission_denied"},
		{"provider Load", "Load(<data/aaaaaaaa>, 0, 0) failed: We encountered an internal error. Please try again.\npack aaaaaaaa contains 1 errors: [download error: We encountered an internal error. Please try again.]", "unavailable"},
		{"cause before Load", "pack aaaaaaaa contains 1 errors: [download error: provider failure]\nLoad(<data/aaaaaaaa>, 0, 0) failed: provider failure", "unavailable"},
		{"provider Stat", "Stat(<data/aaaaaaaa>) failed: provider failure\nerror: provider failure", "unavailable"},
		{"legacy repeated Load cause", "Load(<data/aaaaaaaa>, 0, 0) returned error, retrying after 1s: provider failure\ndownload error: provider failure", "unavailable"},
		{"legacy tree ReadFull", "Load(<data/aaaaaaaa>, 0, 0) returned error, retrying after 1s: provider failure\nerror for tree aaaaaaaa:\n  ReadFull(<data/aaaaaaaa>): provider failure\npack aaaaaaaa failed to download: StreamPack: provider failure", "unavailable"},
		{"unknown independent of Load", "Load(<data/aaaaaaaa>, 0, 0) failed: provider failure\npack bbbbbbbb contains 1 errors: [unsupported structure]", "check_inconclusive"},
		{"unknown decoding despite Load cause", "Load(<data/aaaaaaaa>, 0, 0) failed: EOF\nerror for tree aaaaaaaa:\n  node \"timeout\": decode failed: EOF", "check_inconclusive"},
		{"retry corruption", "Load(<data/aaaaaaaa>, 0, 0) returned error, retrying after 1s: overlapping blobs in pack\ncontext canceled", "integrity_failed"},
		{"retry invalid length", "Load(<data/aaaaaaaa>, 0, 0) returned error, retrying after 1s: invalid blob length\ncontext canceled", "integrity_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertDiagnosticCode(t, tc.transcript, tc.code)
			var records []string
			for _, line := range strings.Split(tc.transcript, "\n") {
				encoded, _ := json.Marshal(map[string]string{"message_type": "error", "message": line + "\n"})
				records = append(records, string(encoded))
			}
			assertDiagnosticCode(t, strings.Join(records, "\n"), tc.code)
		})
	}
}

func TestCheckCancellationEvidenceBoundary(t *testing.T) {
	for _, manual := range []bool{false, true} {
		for _, tc := range []struct{ name, before, after, autoCode, manualCode string }{
			{"partial read", "", "pack aaaaaaaa contains 1 errors: [partial download error: readFull: context canceled]", "unavailable", "cancelled"},
			{"HTTP cancellation", "", "download error: Get \"https://fixture.invalid/SECRET\": context canceled", "unavailable", "cancelled"},
			{"snapshot cancellation", "", "error: failed to load snapshot aaaaaaaa: context canceled", "unavailable", "cancelled"},
			{"incomplete listing", "", "context canceled\npack aaaaaaaa: does not exist", "unavailable", "cancelled"},
			{"prior damage", "ciphertext verification failed", "pack aaaaaaaa: does not exist", "integrity_failed", "integrity_failed"},
			{"buffered damage", "", "pack aaaaaaaa contains 1 errors: [ciphertext verification failed]", "integrity_failed", "integrity_failed"},
			{"prior unknown", "error for tree aaaaaaaa:\n unsupported structure", "context canceled", "check_inconclusive", "cancelled"},
			{"retry damage", "Load(<data/aaaaaaaa>, 0, 0) returned error, retrying after 1s: overlapping blobs in pack", "context canceled", "integrity_failed", "integrity_failed"},
		} {
			t.Run(fmt.Sprintf("%s/manual=%v", tc.name, manual), func(t *testing.T) {
				dir := t.TempDir()
				program := filepath.Join(dir, "restic")
				trap := "printf '%s\\n' " + shellWord(tc.after) + " >&2; exit 130"
				script := "#!/bin/sh\ntrap " + shellWord(trap) + " INT\nprintf '%s\\n' " + shellWord(tc.before) + " >&2\nwhile :; do printf '%s\\n' 'List(data) returned error, retrying after 1s: EOF' >&2; sleep 0.05; done\n"
				if err := os.WriteFile(program, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				runner := nativeRunner{checkFailureBudget: 150 * time.Millisecond}
				want := tc.autoCode
				if manual {
					want = tc.manualCode
					runner.checkFailureBudget = time.Hour
					// Trigger only once output confirms the trap has been installed.
					runner.diagnosticObserver = func(data []byte) {
						if strings.Contains(string(data), "retrying after") {
							time.AfterFunc(50*time.Millisecond, cancel)
						}
					}
				}
				_, err := runner.Run(ctx, program, []string{"check"}, cleanProcessEnvironment(), dir)
				var diagnostic *repositoryCommandError
				if !errors.As(err, &diagnostic) || diagnostic.Code != want {
					t.Fatalf("got %v, want %s", err, want)
				}
			})
		}
	}
}

func TestCheckFailureBudgetExpiresIndividualOperations(t *testing.T) {
	w := checkFailureTracker{budget: checkBackendFailureTimeout}
	start := time.Now()
	for i := range 20 {
		if w.observe(start.Add(time.Duration(i)*5*time.Minute), resticDiagnosticRecord{message: fmt.Sprintf("load(<data/%08x>, 0, 0) returned error, retrying after 1s: EOF", i)}) {
			t.Fatal("recovered legacy operation imposed deadline on unrelated progress")
		}
	}
	if len(w.pending) > 2 {
		t.Fatal("inactive operation state retained")
	}
	if w.observe(start.Add(101*time.Minute), resticDiagnosticRecord{message: "load(<data/aaaaaaaa>, 0, 0) returned error, retrying after 1s: invalid blob length"}) {
		t.Fatal("corruption retry counted as backend outage")
	}
}

func TestCheckCacheWarningsAndUnknownSuccess(t *testing.T) {
	for _, tc := range []struct {
		message string
		hold    bool
	}{
		{"error removing temporary cache directory: directory not empty", false},
		{"unable to create temporary directory for cache during check, disabling cache: read-only file system", false},
		{"unsupported repository structure", true},
	} {
		t.Run(tc.message, func(t *testing.T) {
			dir := t.TempDir()
			program := filepath.Join(dir, "restic")
			encoded, _ := json.Marshal(map[string]string{"message_type": "error", "message": tc.message})
			if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s\\n' "+shellWord(string(encoded))+" >&2\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			_, err := (nativeRunner{}).Run(context.Background(), program, []string{"check"}, cleanProcessEnvironment(), dir)
			if (err != nil) != tc.hold {
				t.Fatalf("unexpected result %v", err)
			}
		})
	}
}
