package backup

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Native programs produce the warnings; neither exit status nor stderr is
// fabricated. Every repository and cache belongs to this fixture.
func TestNativeCheckHousekeeping(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic unavailable")
	}
	for _, kind := range []string{"local", "rclone"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "rclone" {
				if _, err := exec.LookPath("rclone"); err != nil {
					t.Skip("rclone unavailable")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			s, _, _ := newTestService(t)
			var stderr bytes.Buffer
			s.runner = nativeRunner{diagnosticObserver: func(data []byte) { _, _ = stderr.Write(data) }}
			d := addDestination(t, s, "Native cache warning")
			repo := d.Repository
			if kind == "rclone" {
				repo = filepath.Join(t.TempDir(), "repository")
				var err error
				d, err = s.SaveDestination(ctx, "", DestinationInput{Name: "Native rclone notice", Kind: "rclone", Repository: "rclone:fixture:" + repo, Secrets: &Secrets{Password: "fixture-password", RcloneConfig: "[fixture]\ntype = local\n"}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.restic(ctx, d, "", "init"); err != nil {
				t.Fatal(err)
			}
			payload := filepath.Join(t.TempDir(), "payload")
			if err := os.WriteFile(payload, []byte("independent native check payload"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.restic(ctx, d, "", "backup", payload); err != nil {
				t.Fatal(err)
			}
			if kind == "local" {
				cache := filepath.Join(s.directory, "cache", d.ID)
				if err := os.RemoveAll(cache); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cache, []byte("fixture obstructs cache directory"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink(payload, filepath.Join(repo, "snapshots", "zz-fixture-link")); err != nil {
					t.Fatal(err)
				}
			}
			stderr.Reset()
			job, err := s.StartRepositoryJob(ctx, d.ID, "backup_check", "native-housekeeping")
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_check", CheckRequest{ReadData: "all"}, RestoreRequest{})
			if err != nil {
				t.Fatalf("healthy native check: %v\n%s", err, stderr.String())
			}
			if saved := s.state.Destinations[d.ID]; saved.DataCheck.Status != "verified" || saved.CheckHold != nil {
				t.Fatalf("healthy check evidence: %+v", saved)
			}
			events, err := s.jobs.ReplayEvents(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			warned := false
			for _, event := range events {
				warned = warned || strings.Contains(event.Message, "independently")
			}
			if !warned {
				t.Fatalf("native warning was not published: %s", stderr.String())
			}
			t.Logf("native %s stderr:\n%s", kind, stderr.String())
			if kind == "local" {
				if err := os.Remove(filepath.Join(s.directory, "cache", d.ID)); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "rclone" {
				// The same NOTICE for a valid object ID must remain a hold.
				if err := os.Symlink(payload, filepath.Join(repo, "snapshots", strings.Repeat("a", 64))); err != nil {
					t.Fatal(err)
				}
				d = runEvidenceCheck(t, s, d, "all", true)
				if d.CheckHold == nil {
					t.Fatal("skipped valid object did not retain hold")
				}
			}
			if os.Getenv("STACKLAB_TEST_NATIVE_MISSING_LOCK") == "1" {
				testNativeMissingLock(t, s, d)
			}
		})
	}
}

func testNativeMissingLock(t *testing.T, s *Service, d Destination) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var stderr bytes.Buffer
	previous := s.runner
	s.runner = nativeRunner{diagnosticObserver: func(data []byte) { _, _ = stderr.Write(data) }}
	defer func() { s.runner = previous }()
	_, err := s.restic(ctx, d, "", "cat", "lock", strings.Repeat("e", 64), "--no-lock")
	assertR10Outcome(t, err, "unlock_refused")
	t.Logf("native %s vanished lock:\n%s", d.Kind, stderr.String())
}
