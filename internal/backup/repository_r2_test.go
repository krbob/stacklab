package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"stacklab/internal/jobs"
)

func TestRetentionCalendarBucketsAndEqualTimes(t *testing.T) {
	at := func(raw string) time.Time {
		t.Helper()
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	snapshots := []Snapshot{
		{ID: "new", Time: at("2026-01-05T12:00:00Z")},
		{ID: "same-day", Time: at("2026-01-05T10:00:00Z")},
		{ID: "previous-week", Time: at("2026-01-04T20:00:00Z")},
		{ID: "previous-month", Time: at("2025-12-31T23:00:00Z")},
		{ID: "oldest", Time: at("2025-12-01T12:00:00Z")},
	}
	keep := retainedSnapshotIDs(snapshots, Retention{Daily: 1, Weekly: 2, Monthly: 2})
	want := map[string]bool{"new": true, "previous-week": true, "previous-month": true}
	if !reflect.DeepEqual(keep, want) {
		t.Fatalf("calendar union=%v, want %v", keep, want)
	}
	keep = retainedSnapshotIDs(snapshots, Retention{Monthly: 3})
	if !keep["oldest"] {
		t.Fatal("fewer available periods lost oldest recovery point")
	}
	tied := []Snapshot{{ID: "bbbbbbbb", Time: snapshots[0].Time}, {ID: "aaaaaaaa", Time: snapshots[0].Time}}
	if keep := retainedSnapshotIDs(tied, Retention{Daily: 1}); len(keep) != 1 || !keep["aaaaaaaa"] {
		t.Fatalf("unstable equal-time selection: %v", keep)
	}
	// Calendar boundaries use UTC rather than the host's timezone or DST rules.
	zone := []Snapshot{{ID: "later", Time: at("2026-01-05T00:30:00+02:00")}, {ID: "earlier", Time: at("2026-01-04T20:00:00Z")}}
	if keep := retainedSnapshotIDs(zone, Retention{Daily: 1}); len(keep) != 1 || !keep["later"] {
		t.Fatalf("UTC day bucket=%v", keep)
	}
}

type retentionFixtureRunner struct {
	snapshots []Snapshot
	deleted   []string
	prunes    int
}

func (r *retentionFixtureRunner) Run(_ context.Context, _ string, args, _ []string, _ string) ([]byte, error) {
	for i, arg := range args {
		switch arg {
		case "snapshots":
			return json.Marshal(r.snapshots)
		case "ls":
			return []byte("{\"path\":\"/\",\"type\":\"dir\"}\n"), nil // partial snapshot without manifest
		case "forget":
			r.deleted = append(r.deleted, args[i+1:]...)
			return []byte(`{}`), nil
		case "prune":
			r.prunes++
			return []byte(`{}`), nil
		}
	}
	return nil, errors.New("unexpected command")
}
func TestRetentionIgnoresIncompleteAndFutureSnapshotsWithoutDeletingThem(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Retention")
	p := addPlan(t, s, d)
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	partial, complete, old, future := "aaaaaaaa", "bbbbbbbb", "cccccccc", "dddddddd"
	r := &retentionFixtureRunner{snapshots: []Snapshot{{ID: partial, Time: now}, {ID: complete, Time: now}, {ID: old, Time: now.Add(-24 * time.Hour)}, {ID: future, Time: now.Add(24 * time.Hour)}}}
	s.runner = r
	for _, id := range []string{complete, old, future} {
		rememberSnapshotEvidence(&s.state.VerifiedSnapshots, snapshotEvidenceKey(d.ID, id), now)
	}
	if err := s.retain(context.Background(), d, p.ID, Retention{Daily: 1}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.deleted, []string{old}) || r.prunes != 1 {
		t.Fatalf("deleted=%v prunes=%d", r.deleted, r.prunes)
	}
}

type verificationFailureRunner struct {
	inner  *recordingRunner
	fail   bool
	budget time.Duration
}

func (r *verificationFailureRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	if r.fail && commandIn(args, "ls") {
		if deadline, ok := ctx.Deadline(); ok {
			r.budget = time.Until(deadline)
		}
		return nil, commandContextError(context.DeadlineExceeded)
	}
	return r.inner.Run(ctx, name, args, env, dir)
}
func TestTransientVerificationPreservesSnapshotAndRetryDoesNotUpload(t *testing.T) {
	s, _, base := newTestService(t)
	d := addDestination(t, s, "Verification")
	p := addPlan(t, s, d)
	r := &verificationFailureRunner{inner: base, fail: true}
	s.runner = r
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	copy := s.state.Runs[run.ID].Copies[0]
	if copy.SnapshotID == "" || copy.Verification != "unavailable" || !strings.Contains(copy.Error, "timeout") {
		t.Fatalf("transient verification=%+v", copy)
	}
	if r.budget < 14*time.Minute || r.budget > 15*time.Minute {
		t.Fatalf("verification budget=%s", r.budget)
	}
	r.fail = false
	job, _, err = s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	if base.count("backup", d.Repository) != 1 || s.state.Runs[run.ID].State != "succeeded" {
		t.Fatalf("retry uploaded again: %+v", s.state.Runs[run.ID])
	}
}
func TestTransientPriorCandidateVerificationDoesNotCreateDuplicate(t *testing.T) {
	s, _, base := newTestService(t)
	d := addDestination(t, s, "Prior")
	p := addPlan(t, s, d)
	base.priorSnapshot = "aabbccddeeff0011"
	s.runner = &verificationFailureRunner{inner: base, fail: true}
	stage := t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err := s.upload(context.Background(), d, p, Run{ID: "run_existing", CapturedAt: &now}, stage)
	if !errors.Is(err, context.DeadlineExceeded) || base.count("backup", d.Repository) != 0 {
		t.Fatalf("candidate verification spawned duplicate: %v", err)
	}
}
func TestNewestSuccessfulAttemptClearsHistoricCopyError(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Evidence")
	p := addPlan(t, s, d)
	now := time.Now().UTC()
	old := completedTestRun(p, d, "run_old_error", now.Add(-time.Hour), now.Add(-time.Hour))
	old.State = "failed"
	old.Copies[0].State = "failed"
	old.Copies[0].Error = "old failure"
	current := completedTestRun(p, d, "run_good", now, now)
	if err := s.saveRun(old, p); err != nil {
		t.Fatal(err)
	}
	if err := s.saveRun(current, p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		view := protectionFor(t, s)
		if view.State != "manual" || view.Copies[0].LastError != "" {
			t.Fatalf("stale failure survived latest success: %+v", view)
		}
	}
}
func TestRestoreEvidencePersistsAcrossSnapshotsAndServices(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Shared")
	now := time.Now().UTC()
	for i, id := range []string{"aaaaaaaa", "bbbbbbbb"} {
		rememberSnapshotEvidence(&s.state.RestoreTests, snapshotEvidenceKey(d.ID, id), now.Add(time.Duration(i)*time.Hour))
	}
	if err := s.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	fresh := NewService(s.cfg, s.jobs, s.capture)
	if fresh.restoreTestedAtLocked(d, "aaaaaaaa") == nil || fresh.restoreTestedAtLocked(d, "bbbbbbbb") == nil || fresh.restoreTestedAtLocked(d, "cccccccc") != nil {
		t.Fatal("restore evidence leaked or erased another snapshot")
	}
}

// Admission returns while an upload is stalled; another plan and maintenance
// can start, while shutdown still waits for the cancelled worker to recover.
func TestScheduledWorkersDoNotSerializeIndependentPlans(t *testing.T) {
	s, _, base := newTestService(t)
	slow := addDestination(t, s, "Slow")
	fast := addDestination(t, s, "Fast")
	pSlow, pFast := addPlan(t, s, slow), addPlan(t, s, fast)
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	for _, p := range []Plan{pSlow, pFast} {
		p.Destinations[0].Schedule = Schedule{Frequency: "interval", IntervalHours: 1}
		p.Destinations[0].LastScheduledAt = nil
		s.state.Plans[p.ID] = p
	}
	gate := &gatedCopyRunner{inner: base, slow: slow.Repository, started: make(chan struct{}), release: make(chan struct{})}
	s.runner = gate
	ctx, cancel := context.WithCancel(context.Background())
	workers := newBackgroundWorkers(2)
	defer func() { cancel(); workers.wait() }()
	// Stable order is controlled to start the stalled repository first.
	pSlow.ID = "plan_a"
	pFast.ID = "plan_b"
	s.state.Plans = map[string]Plan{pSlow.ID: pSlow, pFast.ID: pFast}
	s.scheduleRuns(ctx, workers)
	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("slow run did not upload")
	}
	s.scheduleRuns(ctx, workers)
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		done := false
		for _, r := range s.state.Runs {
			if r.PlanID == pFast.ID && r.State == "succeeded" {
				done = true
			}
		}
		s.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("independent scheduled backup blocked by slow upload")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Global admission is available during transfer, and repository ownership
	// remains protected until the corresponding worker actually exits.
	job, err := s.jobs.StartWithResources(ctx, "unrelated", "restart", "test", jobs.StackResource("unrelated"))
	if err != nil {
		t.Fatal(err)
	}
	s.finish(ctx, job, nil)
}

func TestTransferWatchdogHasBoundedFinalizationAndIgnoresHeartbeats(t *testing.T) {
	now := time.Now()
	watchdog := newTransferWatchdog(now)
	status := transferProgress{BytesProcessed: 10, BytesTotal: 100, FilesProcessed: 1}
	if !watchdog.observe(now, status) || watchdog.deadline.Sub(now) != 5*time.Minute {
		t.Fatal("reading deadline not established")
	}
	if watchdog.observe(now.Add(4*time.Minute), status) || !watchdog.deadline.Equal(now.Add(5*time.Minute)) {
		t.Fatal("unchanged heartbeat extended stalled read")
	}
	status.BytesProcessed = 100
	finalAt := now.Add(4 * time.Minute)
	if !watchdog.observe(finalAt, status) || watchdog.deadline.Sub(finalAt) != 30*time.Minute {
		t.Fatal("final pack upload did not receive bounded grace")
	}
	if watchdog.observe(finalAt.Add(29*time.Minute), status) || !watchdog.deadline.Equal(finalAt.Add(30*time.Minute)) {
		t.Fatal("unchanged heartbeat hid stalled finalization")
	}
	status.BytesAdded = 1
	if !watchdog.observe(finalAt.Add(29*time.Minute), status) {
		t.Fatal("new uploaded data did not reset watchdog")
	}
}

type cancellationRecoveryCapture struct {
	started   chan struct{}
	cancelled chan struct{}
	recovered chan struct{}
}

func (c *cancellationRecoveryCapture) Recover(context.Context) error { return nil }
func (c *cancellationRecoveryCapture) Prepare(ctx context.Context, _ Plan, _, _ string) (Manifest, error) {
	close(c.started)
	<-ctx.Done()
	close(c.cancelled)
	<-c.recovered // Simulate mandatory writer recovery outside cancelled capture context.
	return Manifest{}, ctx.Err()
}
func TestBackgroundShutdownWaitsForCaptureRecovery(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Shutdown")
	p := addPlan(t, s, d)
	p.Destinations[0].Schedule = Schedule{Frequency: "interval", IntervalHours: 1}
	p.Destinations[0].LastScheduledAt = nil
	s.state.Plans[p.ID] = p
	capture := &cancellationRecoveryCapture{started: make(chan struct{}), cancelled: make(chan struct{}), recovered: make(chan struct{})}
	s.capture = capture
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.RunBackground(ctx); close(done) }()
	select {
	case <-capture.started:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled capture did not start")
	}
	cancel()
	select {
	case <-capture.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel capture")
	}
	select {
	case <-done:
		t.Fatal("background exited before writer recovery")
	case <-time.After(100 * time.Millisecond):
	}
	close(capture.recovered)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join recovered capture")
	}
	active, err := s.jobs.Store().ListActiveJobs(context.Background())
	if err != nil || len(active) != 0 {
		t.Fatalf("shutdown left active jobs: %v %v", active, err)
	}
}

type blockedMaintenanceRunner struct {
	inner   *recordingRunner
	started chan struct{}
}

func (r *blockedMaintenanceRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	if commandIn(args, "check") {
		r.started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return r.inner.Run(ctx, name, args, env, dir)
}
func TestMaintenanceWorkersLeaveSaturatedOccurrencesDue(t *testing.T) {
	s, _, base := newTestService(t)
	for _, name := range []string{"one", "two", "three"} {
		d := addDestination(t, s, name)
		d.CheckSchedule = Schedule{Frequency: "interval", IntervalHours: 1}
		d.LastCheckScheduledAt = nil
		s.state.Destinations[d.ID] = d
	}
	fast := addDestination(t, s, "Independent backup")
	p := addPlan(t, s, fast)
	runner := &blockedMaintenanceRunner{inner: base, started: make(chan struct{}, 2)}
	s.runner = runner
	ctx, cancel := context.WithCancel(context.Background())
	workers := newBackgroundWorkers(2)
	defer func() { cancel(); workers.wait() }()
	s.scheduleMaintenance(ctx, workers)
	for i := 0; i < 2; i++ {
		select {
		case <-runner.started:
		case <-time.After(5 * time.Second):
			t.Fatal("maintenance did not start")
		}
	}
	s.mu.Lock()
	started := 0
	for _, d := range s.state.Destinations {
		if d.LastCheckScheduledAt != nil {
			started++
		}
	}
	s.mu.Unlock()
	if started != 2 {
		t.Fatalf("saturated scheduler consumed %d occurrences, want2", started)
	}
	job, run, err := s.StartRun(ctx, p.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(ctx, job, run.ID)
	s.mu.Lock()
	state := s.state.Runs[run.ID].State
	s.mu.Unlock()
	if state != "succeeded" {
		t.Fatalf("maintenance blocked independent backup: %s", state)
	}
}

func TestOlderRetryActivityDoesNotChangeNewerProtectionEvidence(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Retry activity")
	p := addPlan(t, s, d)
	now := time.Now().UTC()
	older := completedTestRun(p, d, "run_older", now.Add(-time.Hour), now.Add(-time.Hour))
	older.State = "failed"
	older.Copies[0].State = "failed"
	older.Copies[0].Error = "old attempt failed"
	current := completedTestRun(p, d, "run_current", now, now)
	current.Copies[0].SnapshotID = "bbbbbbbbcccccccc"
	if err := s.saveRun(older, p); err != nil {
		t.Fatal(err)
	}
	if err := s.saveRun(current, p); err != nil {
		t.Fatal(err)
	}
	older.State = "running"
	older.Copies[0].Error = ""
	for _, fixture := range []struct{ state, activity string }{{"pending", "queued"}, {"running", "running"}, {"succeeded", "idle"}} {
		older.Copies[0].State = fixture.state
		if err := s.saveRun(older, p); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 50; i++ {
			view := protectionFor(t, s)
			if view.Activity != "running" || view.Copies[0].Activity != fixture.activity || view.State != "manual" || view.Copies[0].LastError != "" || view.Copies[0].SnapshotID != current.Copies[0].SnapshotID || view.LatestRunID != current.ID {
				t.Fatalf("older %s retry changed newest proof or hid activity: %+v", fixture.state, view)
			}
		}
	}
}
