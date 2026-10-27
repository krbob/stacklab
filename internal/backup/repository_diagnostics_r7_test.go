package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Synthetic combinations, including the complete advice sequence from restic
// 0.18 cmd_check.go. Native backend transcripts have their own provenance.
func TestRepositoryDiagnosticOperationAndCause(t *testing.T) {
	pack := strings.Repeat("a", 64)
	hints := "\nThe repository contains damaged pack files. These damaged files must be removed to repair the repository. This can be done using the following commands. Please read the troubleshooting guide at https://restic.readthedocs.io/en/stable/077_troubleshooting.html first.\nrestic repair packs " + pack + "\nrestic repair snapshots --forget\nDamaged pack files can be caused by backend problems, hardware problems or bugs in restic. Please open an issue at https://github.com/restic/restic/issues/new/choose for further troubleshooting!\nFatal: repository contains errors"
	for _, tc := range []struct{ name, diagnostic, code string }{
		{"full advice with partial download", "pack " + pack + " contains 1 errors: [partial download error: readFull: unexpected EOF]" + hints, "unavailable"},
		{"mixed corruption and transport", "pack " + pack + " contains 2 errors: [wrong data returned, hash is bbbbbbbb partial download error: EOF]" + hints, "integrity_failed"},
		{"mixed unknown and transport", "pack " + pack + " contains 2 errors: [unsupported format partial download error: EOF]" + hints, "check_inconclusive"},
		{"unknown before transport in one pack error", "pack " + pack + " contains 1 errors: [unsupported structure with download error: EOF]" + hints, "check_inconclusive"},
		{"decode and transport in same object", "error for tree aaaaaaaa:\n  node \"timeout.conf\": decode failed: EOF\nFatal: repository contains errors", "check_inconclusive"},
		{"filename cannot explain unknown", "error for tree aaaaaaaa:\n  node \"timeout.conf\": unsupported structure\ndownload error: EOF", "check_inconclusive"},
		{"filename cannot prove damage", "error for tree aaaaaaaa:\n  node \"ciphertext verification failed\": unsupported structure\ndownload error: EOF", "check_inconclusive"},
		{"retry text in unknown message", "error for tree aaaaaaaa:\n  node returned error, retrying after: unsupported structure\ndownload error: EOF", "check_inconclusive"},
		{"s3 listing", "List(data) failed: We encountered an internal error, please try again.\nWe encountered an internal error, please try again.\npack " + pack + ": does not exist\nFatal: repository contains errors", "unavailable"},
		{"cause before listing", "error: provider changed its wording\nList(data) failed: provider changed its wording\npack " + pack + ": does not exist\nFatal: repository contains errors", "unavailable"},
		{"unknown independent of listing", "error: unsupported repository structure\nList(data) failed: provider changed its wording\nFatal: repository contains errors", "check_inconclusive"},
		{"listing access denied", "List(data) failed: Access Denied\nAccess Denied\npack " + pack + ": does not exist\nFatal: repository contains errors", "permission_denied"},
		{"decompression and EOF", "pack " + pack + " contains 2 errors: [decompressing blob failed partial download error: EOF]" + hints, "integrity_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertDiagnosticCode(t, tc.diagnostic, tc.code)
			var records []string
			for _, line := range strings.Split(tc.diagnostic, "\n") {
				encoded, _ := json.Marshal(map[string]string{"message_type": "error", "message": line + "\n"})
				records = append(records, string(encoded))
			}
			assertDiagnosticCode(t, strings.Join(records, "\n"), tc.code)
		})
	}
}

func TestRepositoryDiagnosticStreamKeepsEvidenceBeyondOutputLimit(t *testing.T) {
	stream := diagnosticStream{}
	_, _ = stream.Write([]byte("List(data) failed: EOF\n"))
	padding := []byte(strings.Repeat("x", 8191) + "\n")
	for range 2100 {
		_, _ = stream.Write(padding)
	}
	_, _ = stream.Write([]byte("pack aaaaaaaa contains 1 errors: [ciphertext verification "))
	_, _ = stream.Write([]byte("failed]\n"))
	// Evidence in the middle must survive a long suffix too, not just head/tail.
	for range 2100 {
		_, _ = stream.Write(padding)
	}
	stream.finish()
	assertEvidenceCode(t, &stream.evidence, "integrity_failed")
	if len(stream.pending) != 0 {
		t.Fatal("stream retained completed output")
	}
}

func TestRepositoryDiagnosticStreamBoundsUnknownRecords(t *testing.T) {
	stream := diagnosticStream{}
	_, _ = stream.Write([]byte("List(data) failed: EOF\n"))
	_, _ = stream.Write([]byte(strings.Repeat("x", maxDiagnosticRecordBytes+1)))
	_, _ = stream.Write([]byte("\nFatal: repository contains errors"))
	stream.finish()
	assertEvidenceCode(t, &stream.evidence, "check_inconclusive")
	_, _ = stream.Write([]byte("\nciphertext verification failed\n"))
	assertEvidenceCode(t, &stream.evidence, "integrity_failed")
}

func assertEvidenceCode(t *testing.T, evidence *diagnosticEvidence, want string) {
	t.Helper()
	err := classifyDiagnosticEvidence("restic", errors.New("exit"), evidence)
	var diagnostic *repositoryCommandError
	if !errors.As(err, &diagnostic) || diagnostic.Code != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}

func TestCheckFailureBudgetTracksRecoveryOfEachOperation(t *testing.T) {
	w := checkFailureTracker{budget: 10 * time.Minute}
	start := time.Now()
	observe := func(at time.Time, message string) {
		t.Helper()
		if w.observe(at, resticDiagnosticRecord{message: message}) {
			t.Fatal("retry unexpectedly terminal")
		}
	}
	observe(start, "checking data")
	if !w.deadline().IsZero() {
		t.Fatal("quiet healthy check has a failure deadline")
	}
	observe(start, "list(data) returned error, retrying after 1s: EOF")
	observe(start.Add(time.Minute), "list(data) returned error, retrying after 1s: EOF")
	observe(start.Add(time.Minute), "load(<data/aaaaaaaa>, 10, 0) returned error, retrying after 1s: EOF")
	if !w.deadline().Equal(start.Add(10 * time.Minute)) {
		t.Fatal("retry extended failure budget")
	}
	observe(start.Add(2*time.Minute), "list(data) operation successful after 2 retries")
	if !w.deadline().Equal(start.Add(11 * time.Minute)) {
		t.Fatal("unrelated recovery hid another failure")
	}
	observe(start.Add(3*time.Minute), "load(<data/aaaaaaaa>, 10, 0) operation successful after 1 retries")
	if !w.deadline().IsZero() {
		t.Fatal("recovered backend retained deadline")
	}
}

func TestNativeCheckFailureStopPreservesEvidenceAndCleanup(t *testing.T) {
	for _, tc := range []struct{ name, messages, code string }{
		{"terminal inventory", "List(data) failed: provider changed its wording", "unavailable"},
		{"retry budget", "List(data) returned error, retrying after 1s: EOF", "unavailable"},
		{"cancelled read", "error for tree aaaaaaaa:\n  ReadFull(<data/aaaaaaaa>): context canceled\nList(data) failed: EOF", "unavailable"},
		{"prior corruption", "ciphertext verification failed\nList(data) failed: EOF", "integrity_failed"},
		{"prior unknown", "error for tree aaaaaaaa:\n  unsupported structure\nList(data) failed: EOF\ncontext canceled", "check_inconclusive"},
		{"access denied", "List(data) failed: Access Denied", "permission_denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			program, marker := filepath.Join(dir, "restic"), filepath.Join(dir, "cleaned")
			body := "#!/bin/sh\ntrap 'printf cleaned > \"$1\"; exit 130' INT\nprintf '%s\\n' " + shellWord(tc.messages) + " >&2\nwhile :; do sleep 0.05; printf '%s\\n' " + shellWord(tc.messages) + " >&2; done\n"
			if err := os.WriteFile(program, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := (nativeRunner{checkFailureBudget: 100 * time.Millisecond}).Run(ctx, program, []string{marker, "check"}, cleanProcessEnvironment(), dir)
			var diagnostic *repositoryCommandError
			if !errors.As(err, &diagnostic) || diagnostic.Code != tc.code {
				t.Fatalf("got %v, want %s", err, tc.code)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "cleaned" {
				t.Fatalf("SIGINT cleanup missing: %q %v", data, err)
			}
		})
	}
}

func TestNativeCheckSuccessfulRetryAllowsQuietProgress(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "restic")
	body := "#!/bin/sh\nprintf '%s\\n' 'List(data) returned error, retrying after 1s: EOF' 'List(data) operation successful after 1 retries' >&2\nsleep 0.3\n"
	if err := os.WriteFile(program, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := (nativeRunner{checkFailureBudget: 100 * time.Millisecond}).Run(ctx, program, []string{"check"}, cleanProcessEnvironment(), dir); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCheckLegacyRecoveredRetryHasNoSilenceDeadline(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "restic")
	body := "#!/bin/sh\nprintf '%s\\n' 'List(data) returned error, retrying after 1s: EOF' >&2\nsleep 0.3\n"
	if err := os.WriteFile(program, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := (nativeRunner{checkFailureBudget: 100 * time.Millisecond}).Run(ctx, program, []string{"check"}, cleanProcessEnvironment(), dir); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFailureBudgetRequiresContinuingFailures(t *testing.T) {
	start := time.Now()
	w := checkFailureTracker{budget: checkBackendFailureTimeout}
	record := resticDiagnosticRecord{message: "list(data) returned error, retrying after 1s: EOF"}
	for _, elapsed := range []time.Duration{0, 4 * time.Minute, 8 * time.Minute} {
		if w.observe(start.Add(elapsed), record) {
			t.Fatal("failure budget ended early")
		}
	}
	if !w.observe(start.Add(10*time.Minute), record) {
		t.Fatal("continuing failures exceeded budget")
	}
	w = checkFailureTracker{budget: checkBackendFailureTimeout}
	w.observe(start, record)
	if w.observe(start.Add(time.Hour), record) {
		t.Fatal("legacy recovered retry imposed a deadline after quiet progress")
	}
	if !w.deadline().Equal(start.Add(time.Hour + checkBackendFailureTimeout)) {
		t.Fatal("new failure window not started")
	}
}

func TestNativeCheckFailureEscalatesWhenSIGINTIsIgnored(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "restic")
	body := "#!/bin/sh\ntrap '' INT\nprintf '%s\\n' 'List(data) failed: EOF' >&2\nwhile :; do sleep 1; done\n"
	if err := os.WriteFile(program, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	started := time.Now()
	_, err := (nativeRunner{}).Run(ctx, program, []string{"check"}, cleanProcessEnvironment(), dir)
	var diagnostic *repositoryCommandError
	if !errors.As(err, &diagnostic) || diagnostic.Code != "unavailable" || time.Since(started) >= 10*time.Second {
		t.Fatalf("unbounded escalation or wrong outcome: %v", err)
	}
}

func TestRepositoryDiagnosticTerminalJSONPreservesConcreteCause(t *testing.T) {
	for _, tc := range []struct{ message, code string }{
		{"ciphertext verification failed", "integrity_failed"},
		{"Fatal: permission denied", "permission_denied"},
		{"Fatal: connection refused", "unavailable"},
		{"repository contains errors", "check_inconclusive"},
	} {
		encoded, _ := json.Marshal(map[string]any{"message_type": "exit_error", "code": 1, "message": tc.message})
		assertDiagnosticCode(t, string(encoded), tc.code)
	}
}
