package backup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A real restic repair, not restoration of previously saved index bytes. The
// fixture runs as the invoking service/test identity with its saved credentials.
func TestNativeIndexRepairAndStacklabRecovery(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s, capture, _ := newTestService(t)
	s.runner = nativeRunner{}
	s.capture = &nativeArchiveCapture{fakeCapture: capture}
	d := addDestination(t, s, "Index repair")
	p := addPlan(t, s, d)
	if _, err := s.restic(ctx, d, "", "init"); err != nil {
		t.Fatal(err)
	}
	job, run, err := s.StartRun(ctx, p.ID, RunRequest{}, "repair-fixture")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(ctx, job, run.ID)
	if saved := s.state.Runs[run.ID]; saved.State != "succeeded" {
		t.Fatalf("capture: %+v", saved)
	}
	d = runEvidenceCheck(t, s, d, "all", false)
	if err := os.CopyFS(filepath.Join(t.TempDir(), "independent-repository-copy"), os.DirFS(d.Repository)); err != nil {
		t.Fatal(err)
	}
	indexes, err := filepath.Glob(filepath.Join(d.Repository, "index", "*"))
	if err != nil || len(indexes) == 0 {
		t.Fatalf("no index to remove: %v", err)
	}
	for _, path := range indexes {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	d = runEvidenceCheck(t, s, d, "all", true)
	if !repositoryMaintenanceBlocked(d) {
		t.Fatal("missing index did not block maintenance")
	}
	version, err := exec.CommandContext(ctx, "restic", "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"repair", "index"}
	if strings.HasPrefix(string(version), "restic 0.14.") || strings.HasPrefix(string(version), "restic 0.15.") {
		args = []string{"rebuild-index"}
	}
	if _, err := s.restic(ctx, d, "", args...); err != nil {
		t.Fatalf("native index repair: %v", err)
	}
	d = runEvidenceCheck(t, s, d, "all", false)
	if d.CheckStatus != "verified" || repositoryMaintenanceBlocked(d) {
		t.Fatalf("repair did not restore usable evidence: %+v", d)
	}
	snapshots, err := s.Snapshots(ctx, d.ID)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("repaired snapshot inventory: %v %v", snapshots, err)
	}
	job, err = s.StartRepositoryJob(ctx, d.ID, "backup_restore", "repair-fixture")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_restore", CheckRequest{}, RestoreRequest{SnapshotID: snapshots[0].ID, Test: true})
	if err != nil || result.Manifest == nil {
		t.Fatalf("Stacklab recovery after index repair: %+v %v", result, err)
	}
	restored, err := os.ReadFile(filepath.Join(result.Target, "files", "source-a", "data.txt"))
	if err != nil || string(restored) != "archived content" {
		t.Fatalf("repaired snapshot lost payload: %q %v", restored, err)
	}
	t.Logf("%s; native %s rebuilt a removed index, full check and Stacklab archive recovery passed as UID %d", strings.TrimSpace(string(version)), strings.Join(args, " "), os.Getuid())
}
