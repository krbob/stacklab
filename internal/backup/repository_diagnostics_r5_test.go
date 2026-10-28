package backup

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/restic-check/*.stderr
var nativeDiagnosticCorpus embed.FS

func TestRepositoryCheckNativeDiagnosticCorpus(t *testing.T) {
	files, err := fs.Glob(nativeDiagnosticCorpus, "testdata/restic-check/*.stderr")
	if err != nil || len(files) < 4 {
		t.Fatalf("native diagnostic corpus unavailable: %v", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			diagnostic, err := nativeDiagnosticCorpus.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			expected := "integrity_failed"
			if strings.Contains(file, "-outage") {
				expected = "unavailable"
			}
			assertDiagnosticCode(t, string(diagnostic), expected)
		})
	}
}

func assertDiagnosticCode(t *testing.T, diagnostic, expected string) {
	t.Helper()
	err := classifyCommandError("restic", errors.New("exit"), diagnostic)
	var commandErr *repositoryCommandError
	if !errors.As(err, &commandErr) || commandErr.Code != expected {
		t.Fatalf("diagnostic classification %v, want %s", err, expected)
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "fixture.invalid") {
		t.Fatal("raw diagnostics reached public error")
	}
}

func TestRepositoryCheckMixedDiagnosticsAndFutureErrors(t *testing.T) {
	pack := strings.Repeat("a", 64)
	for _, test := range []struct{ name, diagnostic, expected string }{
		{"SFTP truncation and EOF", "pack " + pack + ": unexpected file size: got 500, expected 1000\nLoad(<data/aaaaaaaaaa>, 1000, 0) failed: file is too short: readFull: unexpected EOF\nFatal: repository contains errors", "integrity_failed"},
		{"SFTP short pack alone", "download error: file is too short: readFull: unexpected EOF", "integrity_failed"},
		{"size mismatch with failed listing", "List(data) failed: EOF\npack " + pack + ": unexpected file size: got 500, expected 1000", "integrity_failed"},
		{"invalid node with transport", "node \"fixture\" with invalid type \"unknown\"\ndownload error: EOF", "integrity_failed"},
		{"missing index with retry warning", "List(index) returned error, retrying after 720ms: connection reset by peer\nerror for tree aaaaaaaa:\n  id " + pack + " not found in repository\nFatal: repository contains errors", "check_inconclusive"},
		{"missing pack after retry", "List(data) returned error, retrying after 720ms: connection reset by peer\npack " + pack + ": does not exist\nFatal: repository contains errors", "check_inconclusive"},
		{"missing pack with unrelated download", "Load(<data/bbbbbbbbbb>, 1000, 0) failed: EOF\npack " + pack + ": does not exist\nFatal: repository contains errors", "integrity_failed"},
		{"failed listing after retry", "List(data) returned error, retrying after 720ms: connection reset by peer\nList(data) failed: EOF\npack " + pack + ": does not exist\nFatal: repository contains errors", "unavailable"},
		{"unknown listing failure", "List(data) failed: an unknown backend error\npack " + pack + ": does not exist\nFatal: repository contains errors", "unavailable"},
		{"retry warning is not terminal", "List(data) returned error, retrying after 720ms: connection reset by peer\nFatal: repository contains errors", "check_inconclusive"},
		{"generic damaged summary", "The repository contains damaged pack files.\nFatal: repository contains errors", "check_inconclusive"},
		{"damaged summary from interrupted read", "pack " + pack + " contains 1 errors: [partial download error: EOF]\nThe repository contains damaged pack files.\nFatal: repository contains errors", "unavailable"},
		{"future pack diagnostic", "pack " + pack + " contains 1 errors: [unsupported repository structure]\nFatal: repository contains errors", "check_inconclusive"},
		{"future pack diagnostic with transport", "pack " + pack + " contains 1 errors: [unsupported repository structure]\ndownload error: EOF\nFatal: repository contains errors", "check_inconclusive"},
		{"future tree diagnostic with transport", "error for tree aaaaaaaa:\n  unsupported repository structure\ndownload error: EOF\nFatal: repository contains errors", "check_inconclusive"},
		{"tree transport only", "error for tree aaaaaaaa:\n  download error: EOF\nThe repository is damaged and must be repaired.\nFatal: repository contains errors", "unavailable"},
		{"missing tree after failed inventory", "List(index) failed: EOF\nerror for tree aaaaaaaa:\n  id " + pack + " not found in repository\nFatal: repository contains errors", "unavailable"},
		{"successful second read remains suspect", "check successful on second attempt, original error ciphertext verification failed", "integrity_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticCode(t, test.diagnostic+"\nhttps://user:SECRET@fixture.invalid", test.expected)
			var records []string
			for _, line := range strings.Split(test.diagnostic, "\n") {
				encoded, err := json.Marshal(map[string]string{"message_type": "error", "message": line})
				if err != nil {
					t.Fatal(err)
				}
				records = append(records, string(encoded))
			}
			assertDiagnosticCode(t, strings.Join(records, "\n"), test.expected)
		})
	}
}

func TestNativeRunnerPreservesCorruptionBeforeCancellation(t *testing.T) {
	for _, test := range []struct{ name, diagnostic, expected string }{
		{"corruption", "ciphertext verification failed", "integrity_failed"},
		{"inconclusive", "Fatal: repository contains errors", "cancelled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			marker := filepath.Join(directory, "started")
			program := filepath.Join(directory, "restic")
			body := "#!/bin/sh\nprintf '%s\\n' " + shellWord(test.diagnostic) + " >&2\n: > " + shellWord(marker) + "\nexec sleep 30\n"
			if err := os.WriteFile(program, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := (nativeRunner{}).Run(ctx, program, nil, cleanProcessEnvironment(), directory)
				done <- err
			}()
			for {
				if _, err := os.Stat(marker); err == nil {
					break
				}
				if ctx.Err() != nil {
					t.Fatal("fixture process did not start")
				}
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
			err := <-done
			var commandErr *repositoryCommandError
			if !errors.As(err, &commandErr) || commandErr.Code != test.expected {
				t.Fatalf("cancellation lost established outcome: %v", err)
			}
		})
	}
}
