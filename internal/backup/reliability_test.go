package backup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"stacklab/internal/jobs"
)

type omittedObjectRunner struct {
	calls      int
	unreadable bool
}

func (r *omittedObjectRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	if commandIn(args, "backup") {
		r.calls++
		if r.calls == 1 {
			if r.unreadable {
				archive := filepath.Join(dir, "sources", "source-a.tar")
				if err := os.Chmod(archive, 0); err != nil {
					return nil, err
				}
				defer os.Chmod(archive, 0600)
			} else {
				args = append(args, "--exclude", "sources/source-a.tar")
			}
		}
	}
	return (nativeRunner{}).Run(ctx, name, args, env, dir)
}

// The snapshot tag and manifest can both be valid while a recovery archive is
// absent. Exercise real restic trees, including retry lookup of that snapshot.
func TestNativeIncompleteSnapshotCannotBeAdoptedOnRetry(t *testing.T) {
	exerciseIncompleteSnapshot(t, false)
}
func TestNativePartialReadSnapshotCannotBeAdoptedOnRetry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses source read permission fixture")
	}
	exerciseIncompleteSnapshot(t, true)
}
func exerciseIncompleteSnapshot(t *testing.T, unreadable bool) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic unavailable")
	}
	s, capture, _ := newTestService(t)
	runner := &omittedObjectRunner{unreadable: unreadable}
	s.runner = runner
	s.capture = &nativeArchiveCapture{fakeCapture: capture}
	d := addDestination(t, s, "Incomplete snapshot")
	p := addPlan(t, s, d)
	p.Destinations[0].Retention = Retention{Daily: 1}
	p, err := s.SavePlan(context.Background(), p.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.restic(context.Background(), d, "", "init"); err != nil {
		t.Fatal(err)
	}
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	view, _ := s.Overview(context.Background())
	if view.Runs[0].State != "failed" || view.Protection[0].CurrentCopies != 0 {
		t.Fatalf("incomplete snapshot accepted: %#v", view)
	}
	job, _, err = s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	view, _ = s.Overview(context.Background())
	if runner.calls != 2 || capture.calls != 1 || view.Runs[0].State != "succeeded" || view.Protection[0].CurrentCopies != 1 {
		t.Fatalf("retry did not rebuild the incomplete copy: uploads=%d captures=%d run=%#v", runner.calls, capture.calls, view.Runs[0])
	}
	snapshots, inventoryErr := s.Snapshots(context.Background(), d.ID)
	if inventoryErr != nil {
		t.Fatal(inventoryErr)
	}
	verifiedID := view.Runs[0].Copies[0].SnapshotID
	found := false
	for _, snapshot := range snapshots {
		if snapshot.ID == verifiedID {
			found = true
		}
	}
	if !found {
		t.Fatalf("retention removed the verified retry: %s, snapshots=%v", verifiedID, snapshots)
	}
	if err := s.verifySavedSnapshot(context.Background(), d, verifiedID); err != nil {
		t.Fatalf("retained retry is incomplete: %v", err)
	}
	if _, err = os.Stat(filepath.Join(s.directory, "staging", run.ID)); !os.IsNotExist(err) {
		t.Fatalf("successful staging not removed: %v", err)
	}
}

func TestProtectionReconcilesDeletionIntegrityAndClockSkew(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Evidence")
	p := addPlan(t, s, d)
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	run := completedTestRun(p, d, "run_evidence", now, now)
	if err := s.saveRun(run, p); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcileInventory(context.Background(), d, nil); err != nil {
		t.Fatal(err)
	}
	if result := protectionFor(t, s); result.CurrentCopies != 0 || result.Copies[0].State != "missing" {
		t.Fatalf("deleted snapshot looks usable: %#v", result)
	}
	// Persisting the old run must not undo newer inventory evidence.
	if err := s.saveRun(run, p); err != nil {
		t.Fatal(err)
	}
	if protectionFor(t, s).Copies[0].State != "missing" {
		t.Fatal("old run replaced inventory evidence")
	}
	if err := s.reconcileInventory(context.Background(), d, []Snapshot{{ID: run.Copies[0].SnapshotID}}); err != nil {
		t.Fatal(err)
	}
	d.CheckStatus, d.CheckError = "failed", "corrupt pack"
	s.state.Destinations[d.ID] = d
	if result := protectionFor(t, s); result.CurrentCopies != 0 || result.Copies[0].State != "integrity_failed" {
		t.Fatalf("failed integrity looks usable: %#v", result)
	}
	d.CheckStatus, d.CheckError = "verified", ""
	s.state.Destinations[d.ID] = d
	now = now.Add(-24 * time.Hour)
	if result := protectionFor(t, s); result.CurrentCopies != 0 || result.Copies[0].State != "clock_skew" {
		t.Fatalf("future copy looks current: %#v", result)
	}
	newRun := completedTestRun(p, d, "run_correct_clock", now, now)
	newRun.Copies[0].SnapshotID = "bbccddeeff001122"
	if err := s.saveRun(newRun, p); err != nil {
		t.Fatal(err)
	}
	if result := protectionFor(t, s); result.CurrentCopies != 1 || result.Copies[0].SnapshotID != newRun.Copies[0].SnapshotID {
		t.Fatalf("future checkpoint suppressed corrected capture: %#v", result)
	}
}

func TestClockCorrectionDoesNotSuppressFutureOccurrences(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(7 * 24 * time.Hour)
	due, ok := latestDue(Schedule{Frequency: "interval", IntervalHours: 1}, now)
	if !ok || !occurrencePending(&future, due, now) {
		t.Fatal("future watermark blocked corrected clock")
	}
	if occurrencePending(&due, due, now) {
		t.Fatal("same occurrence would loop")
	}
	preview, err := PreviewSchedule(Schedule{Frequency: "daily", Time: "09:00"}, now, 1)
	if err != nil || len(preview) != 1 {
		t.Fatal(err)
	}
}

type gatedCopyRunner struct {
	inner   *recordingRunner
	slow    string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *gatedCopyRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	if len(args) > 1 && args[1] == r.slow && commandIn(args, "backup") {
		r.once.Do(func() { close(r.started) })
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.release:
		}
	}
	return r.inner.Run(ctx, name, args, env, dir)
}

func TestUploadAllowsHostMutationIndependentCopyAndUnrelatedRepositoryRead(t *testing.T) {
	s, _, runner := newTestService(t)
	a, b, c := addDestination(t, s, "Slow"), addDestination(t, s, "Fast"), addDestination(t, s, "Unrelated")
	p := addPlan(t, s, a, b)
	gate := &gatedCopyRunner{inner: runner, slow: a.Repository, started: make(chan struct{}), release: make(chan struct{})}
	s.runner = gate
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.ExecuteRun(context.Background(), job, run.ID); close(done) }()
	defer func() { close(gate.release); <-done }()
	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not begin")
	}
	mutation, err := s.jobs.StartWithResources(context.Background(), "other", "restart", "test", jobs.StackResource("other"))
	if err != nil {
		t.Fatalf("upload blocks unrelated host mutation: %v", err)
	}
	defer s.jobs.FinishSucceeded(context.Background(), mutation)
	if _, err = s.Snapshots(context.Background(), c.ID); err != nil {
		t.Fatalf("unrelated repository read blocked: %v", err)
	}
	if _, err = s.Snapshots(context.Background(), a.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("same repository read was not serialized: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		view, _ := s.Overview(context.Background())
		fast := view.Runs[0].Copies[1]
		if fast.State == "succeeded" {
			if _, err := s.Snapshots(context.Background(), b.ID); err == nil {
				operation, err := s.StartRepositoryJob(context.Background(), b.ID, "backup_destination_test", "test")
				if err != nil {
					t.Fatalf("finished copy still holds the job resource: %v", err)
				}
				s.finish(context.Background(), operation, nil)
				s.releaseRepositories(operation.ID)
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("fast copy waits for stalled destination: %#v", fast)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRepositoryDiagnosticsAreActionableWithoutEchoingSecrets(t *testing.T) {
	for _, fixture := range []struct{ diagnostic, code string }{{"Fatal: repository is already locked at https://user:SECRET@example.test", "repository_locked"}, {"Fatal: wrong password SECRET", "credentials_rejected"}, {"Fatal: repository contains errors; SECRET", "operation_failed"}, {"Fatal: no space left on device SECRET", "storage_full"}} {
		err := classifyCommandError("restic", errors.New("exit"), fixture.diagnostic)
		if !strings.Contains(err.Error(), fixture.code) || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "example.test") {
			t.Fatalf("unsafe/non-actionable diagnostic: %v", err)
		}
	}
}

func TestReplacementRunCannotHideUnusableProtectionEvidence(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Evidence")
	p := addPlan(t, s, d)
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	saved := completedTestRun(p, d, "run_saved", now.Add(-time.Hour), now.Add(-time.Hour))
	if err := s.saveRun(saved, p); err != nil {
		t.Fatal(err)
	}
	p.Sources[0].Excludes = []string{"cache"}
	p, err := s.SavePlan(context.Background(), p.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	running := Run{ID: "run_replacement", PlanID: p.ID, State: "running", CreatedAt: now, Copies: []CopyResult{{DestinationID: d.ID, State: "running", Verification: "pending"}}}
	if err := s.saveRun(running, p); err != nil {
		t.Fatal(err)
	}
	view := protectionFor(t, s)
	if view.State != "attention" || view.Activity != "running" || view.Copies[0].State != "outdated" || view.Copies[0].EvidenceUsable || view.CurrentCopies != 0 {
		t.Fatalf("running phase hid changed selection: %#v", view)
	}
}

func TestScheduleActivationIsServerOwnedAndSurvivesOrdinaryEdits(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Schedule")
	p := addPlan(t, s, d)
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	p.Destinations[0].Schedule = Schedule{Frequency: "daily", Time: "09:00", Timezone: "UTC"}
	p, err := s.SavePlan(context.Background(), p.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if activation := p.Destinations[0].ScheduleActivatedAt; activation == nil || !activation.Equal(now) {
		t.Fatalf("activation=%v", activation)
	}
	now = now.Add(time.Hour)
	p.Name = "Renamed"
	p.Destinations[0].ScheduleActivatedAt = &now
	p, err = s.SavePlan(context.Background(), p.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Destinations[0].ScheduleActivatedAt.Equal(now) {
		t.Fatal("ordinary edit reset activation")
	}
}

func TestUnavailableBackupStateKeepsDiagnosticOverviewReadable(t *testing.T) {
	s, _, _ := newTestService(t)
	s.initErr = errors.New("invalid backup state")
	overview, err := s.Overview(context.Background())
	if err != nil || !strings.Contains(overview.RecoveryError, "invalid backup state") || overview.Plans == nil || overview.Destinations == nil {
		t.Fatalf("module error hid diagnostics: %#v %v", overview, err)
	}
	if _, _, err = s.StartRun(context.Background(), "missing", RunRequest{}, "test"); err == nil {
		t.Fatal("corrupt state allowed mutation")
	}
}

func TestResticCancellationAllowsCleanupBeforeEscalation(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("shell unavailable")
	}
	dir := t.TempDir()
	executable, marker, ready := filepath.Join(dir, "restic"), filepath.Join(dir, "cleanup"), filepath.Join(dir, "ready")
	script := `#!/bin/sh
trap 'printf cleaned > "$1"; exit 130' INT
printf ready > "$2"
while :; do sleep 0.05; done
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (nativeRunner{}).Run(ctx, executable, []string{marker, ready}, cleanProcessEnvironment(), dir)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("process did not install its signal handler")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("graceful cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not allow prompt cleanup")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "cleaned" {
		t.Fatalf("process did not perform cleanup: %q %v", data, err)
	}
}
