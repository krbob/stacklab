package backup

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

type datedNativeArchiveCapture struct {
	*nativeArchiveCapture
	at time.Time
}

func (c *datedNativeArchiveCapture) Prepare(ctx context.Context, plan Plan, id, directory string) (Manifest, error) {
	manifest, err := c.nativeArchiveCapture.Prepare(ctx, plan, id, directory)
	manifest.CapturedAt = c.at
	return manifest, err
}

// All repository reads and writes use real restic. Only the initial maintenance
// inventory is interrupted, preserving several snapshots for later retry.
type nativeRetentionFailureRunner struct {
	failRetention            bool
	backups, forgets, prunes int
}

func (r *nativeRetentionFailureRunner) Run(ctx context.Context, name string, args, env []string, directory string) ([]byte, error) {
	if commandIn(args, "backup") {
		r.backups++
	}
	if commandIn(args, "forget") {
		r.forgets++
	}
	if commandIn(args, "prune") {
		r.prunes++
	}
	joined := strings.Join(args, " ")
	if r.failRetention && commandIn(args, "snapshots") && strings.Contains(joined, ",plan:") && !strings.Contains(joined, ",run:") {
		return nil, errors.New("fixture: retention inventory interrupted")
	}
	return (nativeRunner{}).Run(ctx, name, args, env, directory)
}

func TestNativeMaintenanceRetryUsesCurrentRetentionAcrossUTCDays(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	s, capture, _ := newTestService(t)
	runner := &nativeRetentionFailureRunner{failRetention: true}
	s.runner = runner
	datedCapture := &datedNativeArchiveCapture{nativeArchiveCapture: &nativeArchiveCapture{fakeCapture: capture}}
	s.capture = datedCapture
	d := addDestination(t, s, "Native current retention")
	p := addPlan(t, s, d)
	p.Destinations[0].Retention = Retention{Daily: 1}
	var err error
	p, err = s.SavePlan(ctx, p.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.restic(ctx, d, "", "init"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	firstDay := now.Truncate(24 * time.Hour).Add(-72 * time.Hour).Add(12 * time.Hour)
	runs := []Run{}
	for index := 0; index < 3; index++ {
		at := firstDay.Add(time.Duration(index) * 24 * time.Hour)
		datedCapture.at = at
		s.now = func() time.Time { return at }
		job, run, err := s.StartRun(ctx, p.ID, RunRequest{}, "test")
		if err != nil {
			t.Fatal(err)
		}
		s.ExecuteRun(ctx, job, run.ID)
		saved := s.state.Runs[run.ID].Run
		if saved.State != "failed" || !verifiedCopy(saved.Copies[0]) || saved.Copies[0].Retention != "failed" {
			t.Fatalf("native setup did not preserve a verified copy with failed retention: %+v", saved)
		}
		runs = append(runs, saved)
	}
	s.now = func() time.Time { return now }
	runner.failRetention = false
	listSnapshots := func() map[string]string {
		t.Helper()
		snapshots, err := s.Snapshots(ctx, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		result := map[string]string{}
		for _, snapshot := range snapshots {
			result[snapshot.ID] = snapshot.Time.UTC().Format("2006-01-02")
		}
		return result
	}
	original := listSnapshots()
	days := map[string]bool{}
	for _, day := range original {
		days[day] = true
	}
	if len(original) != 3 || len(days) != 3 {
		t.Fatalf("fixture needs three distinct UTC calendar days, got %v", original)
	}

	// The retained run still records daily=1, while current policy now keeps all
	// three populated days. Using its historical policy would delete one of the
	// older snapshots even with the latest protection checkpoint pinned.
	p.Destinations[0].Retention = Retention{Daily: 30}
	if _, err := s.SavePlan(ctx, p.ID, p); err != nil {
		t.Fatal(err)
	}
	job, _, err := s.StartRetry(ctx, runs[0].ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(ctx, job, runs[0].ID)
	firstRetry := s.state.Runs[runs[0].ID]
	if firstRetry.State != "succeeded" || firstRetry.PlanSnapshot.Destinations[0].Retention.Daily != 1 || !reflect.DeepEqual(transferEvidence(firstRetry.Copies[0]), transferEvidence(runs[0].Copies[0])) {
		t.Fatalf("retry changed immutable transfer evidence or failed: %+v", firstRetry)
	}
	if actual := listSnapshots(); !reflect.DeepEqual(actual, original) || runner.forgets != 0 || runner.prunes != 0 {
		t.Fatalf("current daily=30 lost native snapshots: before=%v after=%v forget=%d prune=%d", original, actual, runner.forgets, runner.prunes)
	}

	// Verify deletion is actually functional: another run still needs maintenance,
	// and a newly chosen daily=1 must now forget the two older native snapshots.
	p.Destinations[0].Retention = Retention{Daily: 1}
	if _, err := s.SavePlan(ctx, p.ID, p); err != nil {
		t.Fatal(err)
	}
	job, _, err = s.StartRetry(ctx, runs[2].ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(ctx, job, runs[2].ID)
	if final := s.state.Runs[runs[2].ID]; final.State != "succeeded" {
		t.Fatalf("native destructive retention failed: %+v", final)
	}
	remaining := listSnapshots()
	newestID := runs[2].Copies[0].SnapshotID
	if len(remaining) != 1 || remaining[newestID] != original[newestID] || runner.forgets != 1 || runner.prunes != 1 {
		t.Fatalf("current daily=1 did not remove precisely the older snapshots: remaining=%v forget=%d prune=%d", remaining, runner.forgets, runner.prunes)
	}
	if runner.backups != 3 || capture.calls != 3 {
		t.Fatalf("maintenance retried capture/upload: capture=%d backup=%d", capture.calls, runner.backups)
	}
}
