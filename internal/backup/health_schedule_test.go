package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"stacklab/internal/jobs"
)

func parseTestTime(t *testing.T, value string) time.Time {
	t.Helper()
	result, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func completedTestRun(plan Plan, destination Destination, id string, captured, uploaded time.Time) Run {
	return Run{ID: id, PlanID: plan.ID, PlanName: plan.Name, State: "succeeded", CaptureState: "succeeded", CapturedAt: &captured, CreatedAt: captured, ExpiresAt: captured.Add(time.Hour), Copies: []CopyResult{{DestinationID: destination.ID, State: "succeeded", SnapshotID: "aabbccddeeff0011", UploadedAt: &uploaded, Verification: recoveryObjectsVerified, VerifiedAt: &uploaded, Retention: "succeeded"}}}
}
func protectionFor(t *testing.T, s *Service) PlanProtection {
	t.Helper()
	view, err := s.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Protection) != 1 {
		t.Fatalf("protection=%#v", view.Protection)
	}
	return view.Protection[0]
}

func TestProtectionUsesCaptureAgeSurvivesRetentionAndHistoryCleanup(t *testing.T) {
	s, _, _ := newTestService(t)
	destination := addDestination(t, s, "Independent copy")
	plan := addPlan(t, s, destination)
	plan.Destinations[0].Schedule = Schedule{Frequency: "daily", Time: "09:00", Timezone: "UTC"}
	plan, err := s.SavePlan(context.Background(), plan.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	now := parseTestTime(t, "2026-10-13T12:00:00Z")
	s.now = func() time.Time { return now }
	capture := now.Add(-25 * time.Hour)
	run := completedTestRun(plan, destination, "run_verified", capture, now)
	run.State = "failed"
	run.Copies[0].State = "failed"
	run.Copies[0].Retention = "failed"
	run.Copies[0].Error = "Retention failed"
	if err = s.saveRun(run, plan); err != nil {
		t.Fatal(err)
	}
	view := protectionFor(t, s)
	if view.State != "attention" || view.CurrentCopies != 0 || view.Copies[0].State != "overdue" || !view.Copies[0].LastCaptureAt.Equal(capture) || view.Copies[0].LastError != "Retention failed" {
		t.Fatalf("late retry protection=%#v", view)
	}
	// Removing bounded history must not erase the last usable recovery point.
	s.mu.Lock()
	delete(s.state.Runs, run.ID)
	err = s.persist(context.Background())
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	fresh := NewService(s.cfg, s.jobs, s.capture)
	fresh.now = s.now
	view = protectionFor(t, fresh)
	if view.Copies[0].SnapshotID == "" || view.Copies[0].LastCaptureAt == nil || view.Copies[0].State != "overdue" {
		t.Fatalf("checkpoint lost=%#v", view)
	}
	current := completedTestRun(plan, destination, "run_current", now, now)
	if err = fresh.saveRun(current, plan); err != nil {
		t.Fatal(err)
	}
	view = protectionFor(t, fresh)
	if view.State != "healthy" || view.CurrentCopies != 1 {
		t.Fatalf("current=%#v", view)
	}
	plan.Sources[0].Excludes = []string{"cache"}
	if _, err = fresh.SavePlan(context.Background(), plan.ID, plan); err != nil {
		t.Fatal(err)
	}
	view = protectionFor(t, fresh)
	if view.State != "attention" || view.Copies[0].State != "outdated" || view.CurrentCopies != 0 {
		t.Fatalf("changed selection=%#v", view)
	}
}

func TestProtectionManualDisabledAndLatestAttemptAreDeterministic(t *testing.T) {
	s, _, _ := newTestService(t)
	a := addDestination(t, s, "A")
	b := addDestination(t, s, "B")
	plan := addPlan(t, s, a, b)
	plan.Destinations[1].Enabled = false
	plan.Destinations[1].Schedule = Schedule{Frequency: "daily", Time: "01:00"}
	plan, err := s.SavePlan(context.Background(), plan.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	old := completedTestRun(plan, a, "run_old", now.Add(-time.Hour), now.Add(-time.Hour))
	old.State = "running"
	recent := completedTestRun(plan, a, "run_recent", now, now)
	s.state.Runs[old.ID] = persistedRun{Run: old, PlanSnapshot: plan}
	if err = s.saveRun(recent, plan); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		view := protectionFor(t, s)
		if view.State != "manual" || view.CurrentCopies != 1 || view.ExpectedCopies != 1 || view.Copies[0].LastRunID != recent.ID || view.Copies[0].State != "manual" || view.Copies[1].State != "disabled" {
			t.Fatalf("manual/latest=%#v", view)
		}
	}
	active := completedTestRun(plan, a, "run_active", now.Add(time.Minute), now)
	active.State = "running"
	active.Copies[0].Verification = "pending"
	active.Copies[0].SnapshotID = ""
	if err = s.saveRun(active, plan); err != nil {
		t.Fatal(err)
	}
	view := protectionFor(t, s)
	if view.State != "running" || view.CurrentCopies != 1 {
		t.Fatalf("new run hid saved copy=%#v", view)
	}
}

func TestSchedulePreviewMatchesDueAcrossDSTAndIntervals(t *testing.T) {
	tests := []struct {
		schedule Schedule
		from     string
		next     string
	}{
		{Schedule{Frequency: "interval", IntervalHours: 4}, "2026-10-13T09:33:00Z", "2026-10-13T12:00:00Z"},
		{Schedule{Frequency: "daily", Time: "09:30", Timezone: "Europe/Warsaw"}, "2026-03-28T10:00:00Z", "2026-03-29T07:30:00Z"},
		{Schedule{Frequency: "daily", Time: "02:30", Timezone: "Europe/Warsaw"}, "2026-03-28T10:00:00Z", "2026-03-30T00:30:00Z"},
		{Schedule{Frequency: "weekly", Time: "09:30", Weekday: 1, Timezone: "Europe/Warsaw"}, "2026-10-24T10:00:00Z", "2026-10-26T08:30:00Z"},
	}
	for _, test := range tests {
		from := parseTestTime(t, test.from)
		want := parseTestTime(t, test.next)
		preview, err := PreviewSchedule(test.schedule, from, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(preview) != 3 || !preview[0].Equal(want) {
			t.Fatalf("preview %v=%v want %v", test.schedule, preview, want)
		}
		for _, occurrence := range preview {
			due, ok := latestDue(test.schedule, occurrence)
			if !ok || !due.Equal(occurrence) {
				t.Fatalf("due=%v occurrence=%v", due, occurrence)
			}
		}
	}
	for _, schedule := range []Schedule{{Frequency: "interval", IntervalHours: 0}, {Frequency: "interval", IntervalHours: 169}, {Frequency: "daily", Time: "25:00"}, {Frequency: "weekly", Time: "02:00", Timezone: "invalid"}} {
		if _, err := PreviewSchedule(schedule, time.Now(), 3); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid schedule %+v", schedule)
		}
	}
	if result, err := PreviewSchedule(Schedule{Frequency: "manual"}, time.Now(), 3); err != nil || result == nil || len(result) != 0 {
		t.Fatalf("manual=%v,%v", result, err)
	}
}

func TestMaintenanceConflictCatchupAndIndependentPrune(t *testing.T) {
	s, _, runner := newTestService(t)
	destination := addDestination(t, s, "Scheduled")
	now := parseTestTime(t, "2026-10-13T12:34:00Z")
	s.now = func() time.Time { return now }
	schedule := Schedule{Frequency: "interval", IntervalHours: 4}
	destination, err := s.SaveDestination(context.Background(), destination.ID, DestinationInput{Name: destination.Name, Kind: destination.Kind, Repository: destination.Repository, ExpectedFilesystem: destination.ExpectedFilesystem, CheckSchedule: schedule, CheckReadData: "subset", PruneSchedule: schedule})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Hour) // Newly saved schedules start at a future occurrence.
	conflict, err := s.jobs.StartDraining(context.Background(), "fixture", "tester", jobs.GlobalResource())
	if err != nil {
		t.Fatal(err)
	}
	s.runMaintenance(context.Background())
	if runner.count("check", "") != 0 {
		t.Fatal("maintenance bypassed global admission")
	}
	_, _ = s.jobs.FinishSucceeded(context.Background(), conflict)
	s.runMaintenance(context.Background())
	s.runMaintenance(context.Background())
	if runner.count("check", "") != 1 || runner.count("prune", "") != 1 {
		t.Fatalf("catchup repeated check=%d prune=%d", runner.count("check", ""), runner.count("prune", ""))
	}
	view, _ := s.Overview(context.Background())
	d := view.Destinations[0]
	if d.LastCheckAt == nil || d.LastPruneAt == nil || d.CheckStatus != "verified" || d.LastCheckScheduledAt == nil || d.LastPruneScheduledAt == nil {
		t.Fatalf("maintenance state=%#v", d)
	}
	if err = s.retain(context.Background(), d, "plan_fixture", Retention{Daily: 7}); err != nil {
		t.Fatal(err)
	}
	last := strings.Join(runner.calls[len(runner.calls)-1], " ")
	if strings.Contains(last, "--prune") || !strings.Contains(last, "--tag stacklab,instance:") {
		t.Fatalf("scheduled prune coupled to retention: %s", last)
	}
	now = now.Add(24 * time.Hour)
	s.runMaintenance(context.Background())
	if runner.count("check", "") != 2 || runner.count("prune", "") != 2 {
		t.Fatal("missed maintenance replayed more than one occurrence")
	}
}

type failOperationRunner struct {
	wrapped *recordingRunner
	command string
}

func (r failOperationRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	for _, arg := range args {
		if arg == r.command {
			return nil, fmt.Errorf("simulated %s failure", r.command)
		}
	}
	return r.wrapped.Run(ctx, name, args, env, dir)
}
func TestRestoreFailureDoesNotPoisonAvailabilityAndIntegrityIsSeparate(t *testing.T) {
	s, _, runner := newTestService(t)
	destination := addDestination(t, s, "Repository")
	destination.Status = "available"
	s.state.Destinations[destination.ID] = destination
	job, err := s.StartRepositoryJob(context.Background(), destination.ID, "backup_restore", "tester")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ExecuteRepositoryJob(context.Background(), job, destination.ID, "backup_restore", CheckRequest{}, RestoreRequest{SnapshotID: "invalid"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("restore error=%v", err)
	}
	view, _ := s.Overview(context.Background())
	d := view.Destinations[0]
	if d.Status != "available" || d.Error != "" || d.LastOperationStatus != "failed" || d.LastOperationError == "" {
		t.Fatalf("restore poisoned availability=%#v", d)
	}
	s.runner = failOperationRunner{wrapped: runner, command: "check"}
	job, err = s.StartRepositoryJob(context.Background(), destination.ID, "backup_check", "tester")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ExecuteRepositoryJob(context.Background(), job, destination.ID, "backup_check", CheckRequest{}, RestoreRequest{})
	if err == nil {
		t.Fatal("check should fail")
	}
	view, _ = s.Overview(context.Background())
	d = view.Destinations[0]
	if d.Status != "available" || d.Error != "" || d.CheckStatus != "failed" || d.CheckError == "" {
		t.Fatalf("integrity not separated=%#v", d)
	}
}

func TestRepositoryReadLeasesPinCredentialsAndExcludeMutations(t *testing.T) {
	s, _, _ := newTestService(t)
	destination := addDestination(t, s, "Pinned")
	plan := addPlan(t, s, destination)
	_, first, err := s.beginRepositoryRead(context.Background(), destination.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := s.beginRepositoryRead(context.Background(), destination.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.StartRun(context.Background(), plan.ID, RunRequest{}, "tester"); !errors.Is(err, ErrBusy) {
		t.Fatalf("mutation raced read: %v", err)
	}
	if _, err = s.StartRepositoryJob(context.Background(), destination.ID, "backup_prune", "tester"); !errors.Is(err, ErrBusy) {
		t.Fatalf("prune raced read: %v", err)
	}
	if _, err = s.SaveDestination(context.Background(), destination.ID, DestinationInput{Name: destination.Name, Kind: destination.Kind, Repository: destination.Repository, ExpectedFilesystem: destination.ExpectedFilesystem, Secrets: &Secrets{Password: "rotated"}}); !errors.Is(err, ErrBusy) {
		t.Fatalf("credential change raced read: %v", err)
	}
	first()
	first() // Idempotent release cannot prematurely drop another reader.
	if _, _, err = s.StartRun(context.Background(), plan.ID, RunRequest{}, "tester"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second reader lost lease: %v", err)
	}
	second()
	job, err := s.StartRepositoryJob(context.Background(), destination.ID, "backup_check", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.beginRepositoryRead(context.Background(), destination.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("read raced check: %v", err)
	}
	if _, err = s.ExecuteRepositoryJob(context.Background(), job, destination.ID, "backup_check", CheckRequest{}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, release, err := s.beginRepositoryRead(context.Background(), destination.ID); err != nil {
		t.Fatal(err)
	} else {
		release()
	}
}

func TestNewSchedulesStartInFutureWhileExistingSchedulesCatchUp(t *testing.T) {
	s, _, runner := newTestService(t)
	destination := addDestination(t, s, "Future first")
	plan := addPlan(t, s, destination)
	now := parseTestTime(t, "2050-10-13T12:34:00Z")
	s.now = func() time.Time { return now }
	plan.Destinations[0].Schedule = Schedule{Frequency: "interval", IntervalHours: 4}
	var err error
	plan, err = s.SavePlan(context.Background(), plan.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	due, _ := latestDue(plan.Destinations[0].Schedule, now)
	if plan.Destinations[0].LastScheduledAt == nil || !plan.Destinations[0].LastScheduledAt.Equal(due) {
		t.Fatal("new plan did not establish future-first schedule")
	}
	destination, err = s.SaveDestination(context.Background(), destination.ID, DestinationInput{Name: destination.Name, Kind: destination.Kind, Repository: destination.Repository, ExpectedFilesystem: destination.ExpectedFilesystem, CheckSchedule: Schedule{Frequency: "interval", IntervalHours: 4}})
	if err != nil {
		t.Fatal(err)
	}
	s.runScheduled(context.Background())
	s.runMaintenance(context.Background())
	view, _ := s.Overview(context.Background())
	if len(view.Runs) != 0 || runner.count("check", "") != 0 {
		t.Fatal("new schedules ran a past occurrence")
	}
	// Editing an unrelated field must not acknowledge a due occurrence.
	now = now.Add(5 * time.Hour)
	plan.Name = "Renamed"
	plan, err = s.SavePlan(context.Background(), plan.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Destinations[0].LastScheduledAt.Equal(due) {
		t.Fatal("editing plan discarded missed occurrence")
	}
	s.runScheduled(context.Background())
	s.runMaintenance(context.Background())
	view, _ = s.Overview(context.Background())
	if len(view.Runs) != 1 || runner.count("check", "") != 1 {
		t.Fatal("existing schedules did not catch up once")
	}
	// Imported legacy nil occurrence remains due after a save with no schedule change.
	plan.Destinations[0].LastScheduledAt = nil
	s.state.Plans[plan.ID] = plan
	plan, err = s.SavePlan(context.Background(), plan.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Destinations[0].LastScheduledAt != nil {
		t.Fatal("legacy catch-up policy changed")
	}
	plan.Destinations[0].Schedule.IntervalHours = 6
	plan, err = s.SavePlan(context.Background(), plan.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	latest, _ := latestDue(plan.Destinations[0].Schedule, now)
	if plan.Destinations[0].LastScheduledAt == nil || !plan.Destinations[0].LastScheduledAt.Equal(latest) {
		t.Fatal("changed schedule did not reset future baseline")
	}
}

func TestDeletingConfigurationRemovesUnreachableProtectionCheckpoints(t *testing.T) {
	s, _, _ := newTestService(t)
	destination := addDestination(t, s, "Temporary")
	plan := addPlan(t, s, destination)
	now := time.Now().UTC()
	run := completedTestRun(plan, destination, "run_delete", now, now)
	if err := s.saveRun(run, plan); err != nil {
		t.Fatal(err)
	}
	if len(s.state.CopyCheckpoints) != 1 {
		t.Fatal("missing checkpoint")
	}
	if err := s.DeletePlan(context.Background(), plan.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.state.CopyCheckpoints) != 0 {
		t.Fatal("deleted plan leaked checkpoint")
	}
	fresh := NewService(s.cfg, s.jobs, s.capture)
	if len(fresh.state.CopyCheckpoints) != 0 {
		t.Fatal("historical run recreated deleted plan checkpoint")
	}
	if err := fresh.DeleteDestination(context.Background(), destination.ID); err != nil {
		t.Fatal(err)
	}
}
