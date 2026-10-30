package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDiagnosticPhaseAndObjectProvenance(t *testing.T) {
	for _, tc := range []struct{ name, text, code string }{
		{"later snapshot cannot explain missing pack", "List(data) returned error, retrying after 1s: EOF\npack aaaaaaaa: does not exist\nerror: failed to load snapshot bbbbbbbb: EOF", "integrity_failed"},
		{"later raw snapshot cannot explain missing pack", "List(data) returned error, retrying after 1s: provider failure\npack aaaaaaaa: does not exist\nLoad(<snapshot/bbbbbbbb>, 0, 0) returned error, retrying after 1s: provider failure\nprovider failure", "integrity_failed"},
		{"listing connection changes", "List(data) returned error, retrying after 1s: read tcp 1.2.3.4:3001: connection reset by peer\nread tcp 1.2.3.4:3002: connection reset by peer\npack aaaaaaaa: does not exist", "unavailable"},
		{"unknown changed listing cause stays uncertain", "List(data) returned error, retrying after 1s: provider error A\nprovider error B\npack aaaaaaaa: does not exist", "check_inconclusive"},
		{"listing cause changes", "List(data) returned error, retrying after 1s: connection reset by peer\nEOF\npack aaaaaaaa: does not exist", "unavailable"},
		{"legacy snapshot wrapper", "Load(<snapshot/aaaaaaaa>, 0, 0) returned error, retrying after 1s: provider failure\nerror: failed to load snapshot aaaaaaaa: provider failure\nFatal: repository contains errors", "unavailable"},
		{"recovered index read", "Load(<index/aaaaaaaa>, 0, 0) returned error, retrying after 1s: EOF\nLoad(<index/aaaaaaaa>, 0, 0) operation successful after 1 retries\nid aaaaaaaa not found in repository", "integrity_failed"},
		{"short load", "Load(<data/aaaaaaaa>, 123, 0) failed: file is too short: readFull: unexpected EOF", "integrity_failed"},
		{"short tree read", "ReadFull(<data/aaaaaaaa>): file is too short: readFull: unexpected EOF", "integrity_failed"},
		{"unknown bare pack error survives transport", "pack header parse failed: unsupported format\nLoad(<data/bbbbbbbb>, 0, 0) failed: EOF\nFatal: repository contains errors", "check_inconclusive"},
		{"lock saving is housekeeping", "Save(<lock/aaaaaaaa>) failed: EOF\nLoad(<data/bbbbbbbb>, 0, 0) failed: EOF", "unavailable"},
		{"legacy lock details", "repository is already locked exclusively by PID 321 on fixture by fixture (UID 1000, GID 1000)\nlock was created at 2026-10-30 10:00:00 (2m1.3s ago)\nstorage ID aaaaaaaa\nthe `unlock` command can be used to remove stale locks", "repository_locked"},
		{"unknown beside lock", "repository is already locked exclusively by PID 321\nunsupported lock contents", "check_inconclusive"},
		{"vanished lock", "Load(<lock/aaaaaaaa>, 0, 0) failed: open /repo/locks/aaaaaaaa: no such file or directory", "unlock_refused"},
		{"vanished data is uncertain", "Load(<data/aaaaaaaa>, 0, 0) failed: open /repo/data/aaaaaaaa: no such file or directory", "check_inconclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) { assertDiagnosticCode(t, tc.text, tc.code) })
	}
}

func TestLegacyRetryCorrelationDoesNotBecomeEvidenceLoss(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			var transcript strings.Builder
			for i := 0; i < 512; i++ {
				fmt.Fprintf(&transcript, "Load(<data/%064x>, 0, 0) returned error, retrying after 1s: EOF\n", i)
			}
			if unknown {
				transcript.WriteString("unsupported repository structure\n")
			}
			dir := t.TempDir()
			program := filepath.Join(dir, "restic")
			if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s' "+shellWord(transcript.String())+" >&2\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			_, err := (nativeRunner{}).Run(context.Background(), program, []string{"check"}, cleanProcessEnvironment(), dir)
			if unknown {
				var classified *repositoryCommandError
				if !errors.As(err, &classified) || classified.Code != "check_inconclusive" {
					t.Fatalf("unknown diagnostic was lost: %v", err)
				}
			} else if err != nil {
				t.Fatalf("recovered legacy retries created hold: %v", err)
			}
		})
	}
}

// Signal the child without cancelling its parent's context, as an external
// service manager can do. Cancellation residue must not diagnose missing packs.
func TestExternalResticSignalBoundary(t *testing.T) {
	for _, before := range []string{"", "pack aaaaaaaa: does not exist", "ciphertext verification failed"} {
		t.Run(before, func(t *testing.T) {
			dir := t.TempDir()
			program := filepath.Join(dir, "restic")
			pidFile := filepath.Join(dir, "pid")
			script := "#!/bin/sh\ntrap " + shellWord("printf '\\033[2Ksignal terminated received, cleaning up\\ncontext canceled\\npack bbbbbbbb: does not exist\\n' >&2; exit 130") + " TERM\nprintf '%s\\n' " + shellWord(before) + " >&2\nprintf '%s' $$ > " + shellWord(pidFile) + "\nwhile :; do sleep 0.05; done\n"
			if err := os.WriteFile(program, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := (nativeRunner{}).Run(ctx, program, []string{"check"}, cleanProcessEnvironment(), dir)
				done <- err
			}()
			deadline := time.Now().Add(3 * time.Second)
			for {
				bytes, err := os.ReadFile(pidFile)
				pid, _ := strconv.Atoi(string(bytes))
				if err == nil && pid > 0 {
					if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
						t.Fatal(err)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("child did not start")
				}
				time.Sleep(10 * time.Millisecond)
			}
			err := <-done
			want := "cancelled"
			if before != "" {
				want = "integrity_failed"
			}
			var classified *repositoryCommandError
			if !errors.As(err, &classified) || classified.Code != want {
				t.Fatalf("got %v, want %s", err, want)
			}
		})
	}
}
