package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// A real restic backup waits for stdin while holding a live lock. Plain unlock
// must refuse it, then remove that same lock after its owner is killed. This
// checks restic's stale-lock contract without manufacturing encrypted lock data.
func TestNativeUnlockPreservesActiveLockAndRemovesKilledOwnerLock(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s, _, _ := newTestService(t)
	s.runner = nativeRunner{}
	d := addDestination(t, s, "Native lock recovery")
	if _, err := s.restic(ctx, d, "", "init"); err != nil {
		t.Fatal(err)
	}
	priorAt := time.Now().UTC().Add(-time.Hour)
	d.DataCheck = CheckEvidence{Status: "failed", Scope: "all", CheckedAt: &priorAt, Error: "previous corruption (integrity_failed)"}
	normalizeCheckEvidence(&d)
	s.state.Destinations[d.ID] = d

	cmd := exec.CommandContext(ctx, "restic", "--repo", d.Repository, "--json", "backup", "--stdin", "--stdin-filename", "pending-input")
	cmd.Env = append(cleanProcessEnvironment(), "RESTIC_PASSWORD_FILE="+filepath.Join(s.secretDirectory(d.ID), "password"), "RESTIC_CACHE_DIR="+filepath.Join(s.directory, "cache", d.ID))
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	var diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = io.Discard, &diagnostic
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	locks := func() []string {
		t.Helper()
		entries, err := os.ReadDir(filepath.Join(d.Repository, "locks"))
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, entry := range entries {
			// Ignore restic's temporary file until atomic lock publication.
			if len(entry.Name()) == 64 && snapshotID.MatchString(entry.Name()) {
				ids = append(ids, entry.Name())
			}
		}
		return ids
	}
	deadline := time.Now().Add(10 * time.Second)
	var activeLocks []string
	for time.Now().Before(deadline) {
		if activeLocks = locks(); len(activeLocks) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(activeLocks) != 1 {
		t.Fatal("restic did not acquire exactly one live lock before fixture deadline")
	}
	unlock := func(active bool) {
		t.Helper()
		job, err := s.StartRepositoryJob(ctx, d.ID, "backup_unlock", "test")
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{})
		var commandErr *repositoryCommandError
		if active {
			if !errors.As(err, &commandErr) || commandErr.Code != "unlock_refused" {
				t.Fatalf("live owner was not refused: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.state.Destinations[d.ID].DataCheck, d.DataCheck) {
			t.Fatal("stale-lock removal changed existing integrity evidence")
		}
	}
	unlock(true)
	if current := locks(); !reflect.DeepEqual(current, activeLocks) {
		t.Fatalf("unlock removed active external lock: before=%v after=%v", activeLocks, current)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	waited = true
	if current := locks(); !reflect.DeepEqual(current, activeLocks) {
		t.Fatal("fixture did not leave its stale lock after SIGKILL")
	}
	unlock(true) // Even an actually killed owner must satisfy the minimum age.
	// Simulate elapsed age only after proving refusal at the real wall clock.
	// Restic still sees the original native lock and verifies PID absence itself.
	s.now = func() time.Time { return time.Now().UTC().Add(minimumUnlockAge + time.Minute) }
	unlock(false)
	if current := locks(); len(current) != 0 {
		t.Fatalf("unlock did not remove killed owner's stale lock: %v", current)
	}
	if err := s.check(ctx, d, "metadata"); err != nil {
		t.Fatalf("repository did not resume ordinary operations after unlock: %v", err)
	}
}
