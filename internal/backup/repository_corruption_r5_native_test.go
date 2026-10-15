package backup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeRepositoryMissingIndexAndTruncatedPack(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic is not installed")
	}
	s, capture, _ := newTestService(t)
	s.runner = nativeRunner{}
	s.capture = &nativeArchiveCapture{fakeCapture: capture}
	d := addDestination(t, s, "Native missing index")
	p := addPlan(t, s, d)
	if _, err := s.restic(context.Background(), d, "", "init"); err != nil {
		t.Fatal(err)
	}
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	if saved := s.state.Runs[run.ID]; saved.State != "succeeded" {
		t.Fatalf("backup: %+v", saved)
	}
	d = runEvidenceCheck(t, s, d, "all", false)
	for _, mutation := range []string{"missing-index", "truncated-pack"} {
		t.Run(mutation, func(t *testing.T) {
			paths, err := filepath.Glob(filepath.Join(d.Repository, "index", "*"))
			if err != nil || len(paths) == 0 {
				t.Fatalf("fixture has no index: %v", err)
			}
			if mutation == "truncated-pack" {
				packs, err := filepath.Glob(filepath.Join(d.Repository, "data", "*", "*"))
				if err != nil || len(packs) == 0 {
					t.Fatalf("fixture has no pack: %v", err)
				}
				paths = packs[:1]
			}
			original := map[string][]byte{}
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				original[path] = data
				if mutation == "missing-index" {
					err = os.Remove(path)
				} else {
					if err = os.Chmod(path, 0600); err == nil {
						err = os.Truncate(path, int64(len(data)/2))
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, scope := range []string{"all", "metadata"} {
				d = runEvidenceCheck(t, s, d, scope, true)
				if d.CheckStatus != "failed" || !strings.Contains(d.LastCheckAttemptError, "integrity_failed") || !repositoryMaintenanceBlocked(d) || d.MaintenanceReviewToken == "" {
					t.Fatalf("native %s/%s failed to establish integrity failure: %+v", mutation, scope, d)
				}
			}
			for path, data := range original {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			d = runEvidenceCheck(t, s, d, "all", false)
			if d.CheckStatus != "verified" || repositoryMaintenanceBlocked(d) || d.MaintenanceReviewToken != "" {
				t.Fatalf("native full check did not resolve repaired corruption: %+v", d)
			}
		})
	}
}
