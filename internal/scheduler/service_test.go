package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"stacklab/internal/audit"
	"stacklab/internal/maintenance"
	"stacklab/internal/maintenancejobs"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

type fakeRunner struct {
	resolveErr error
	updateErr  error
	pruneErr   error

	updateCalls int
	pruneCalls  int
	updateReq   maintenancejobs.UpdateRequest
	pruneReq    maintenancejobs.PruneRequest
	updateDone  chan struct{}
	pruneDone   chan struct{}
}

func (f *fakeRunner) ResolveTargetStacks(ctx context.Context, mode string, stackIDs []string) ([]string, error) {
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	if mode == "selected" {
		return append([]string(nil), stackIDs...), nil
	}
	return []string{"demo"}, nil
}

func (f *fakeRunner) RunUpdate(ctx context.Context, request maintenancejobs.UpdateRequest, requestedBy string) (store.Job, error) {
	f.updateCalls++
	f.updateReq = request
	if f.updateDone != nil {
		defer close(f.updateDone)
	}
	if f.updateErr != nil {
		return store.Job{}, f.updateErr
	}
	now := time.Now().UTC()
	return store.Job{
		ID:          "job_sched_update",
		Action:      "update_stacks",
		State:       "succeeded",
		RequestedBy: requestedBy,
		RequestedAt: now,
		StartedAt:   &now,
		FinishedAt:  &now,
	}, nil
}

func (f *fakeRunner) RunPrune(ctx context.Context, request maintenancejobs.PruneRequest, requestedBy string, lockStackIDs []string) (store.Job, error) {
	f.pruneCalls++
	f.pruneReq = request
	if f.pruneDone != nil {
		defer close(f.pruneDone)
	}
	if f.pruneErr != nil {
		return store.Job{}, f.pruneErr
	}
	now := time.Now().UTC()
	return store.Job{
		ID:          "job_sched_prune",
		Action:      "prune",
		State:       "succeeded",
		RequestedBy: requestedBy,
		RequestedAt: now,
		StartedAt:   &now,
		FinishedAt:  &now,
	}, nil
}

type fakeStackLister struct {
	items []stacks.StackListItem
}

func (f *fakeStackLister) List(ctx context.Context, query stacks.ListQuery) (stacks.StackListResponse, error) {
	return stacks.StackListResponse{Items: append([]stacks.StackListItem(nil), f.items...)}, nil
}

func openSchedulerTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestUpdateSettingsRejectsInvalidWeeklySchedule(t *testing.T) {
	testStore := openSchedulerTestStore(t)
	service := NewService(testStore, audit.NewService(testStore), &fakeRunner{}, &fakeStackLister{}, nil)

	_, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Update: UpdateScheduleConfig{
			Enabled:   true,
			Frequency: FrequencyWeekly,
			Time:      "25:00",
			Target:    maintenancejobs.UpdateTarget{Mode: "all"},
			Options: maintenancejobs.UpdateOptions{
				PullImages:    true,
				BuildImages:   true,
				RemoveOrphans: true,
			},
		},
		Prune: defaultSettings().Prune,
	})
	if err == nil {
		t.Fatal("UpdateSettings() error = nil, want validation error")
	}
}

func TestRunDueSchedulesDispatchesUpdateOncePerSlot(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("UTC", 0)
	defer func() { time.Local = previousLocal }()

	testStore := openSchedulerTestStore(t)
	runner := &fakeRunner{updateDone: make(chan struct{})}
	service := NewService(testStore, audit.NewService(testStore), runner, &fakeStackLister{
		items: []stacks.StackListItem{{StackHeader: stacks.StackHeader{ID: "demo"}}},
	}, nil)
	service.now = func() time.Time { return time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC) }

	if _, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Update: UpdateScheduleConfig{
			Enabled:   true,
			Frequency: FrequencyDaily,
			Time:      "03:30",
			Target:    maintenancejobs.UpdateTarget{Mode: "all"},
			Options: maintenancejobs.UpdateOptions{
				PullImages:    true,
				BuildImages:   true,
				RemoveOrphans: true,
			},
		},
		Prune: defaultSettings().Prune,
	}); err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	if err := service.updateRuntime(context.Background(), "update", func(state *scheduleRuntimeState) {
		previous := time.Date(2026, 4, 9, 3, 30, 0, 0, time.UTC)
		state.LastScheduledFor = &previous
	}); err != nil {
		t.Fatalf("updateRuntime() error = %v", err)
	}

	service.runDueSchedules(context.Background())
	<-runner.updateDone

	if runner.updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", runner.updateCalls)
	}
	if runner.updateReq.Trigger != "scheduled" || runner.updateReq.ScheduleKey != "update" {
		t.Fatalf("unexpected update request: %#v", runner.updateReq)
	}

	service.runDueSchedules(context.Background())
	if runner.updateCalls != 1 {
		t.Fatalf("updateCalls after second poll = %d, want 1", runner.updateCalls)
	}

	response, err := service.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}
	if response.Update.Status.LastResult != "succeeded" {
		t.Fatalf("last_result = %q, want succeeded", response.Update.Status.LastResult)
	}
	if response.Update.Status.LastJobID == nil || *response.Update.Status.LastJobID != "job_sched_update" {
		t.Fatalf("last_job_id = %#v, want job_sched_update", response.Update.Status.LastJobID)
	}
	if response.Update.Status.NextRunAt == nil {
		t.Fatal("next_run_at = nil, want value")
	}
}

func TestRunDueSchedulesDispatchesPruneWithManagedLocks(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("UTC", 0)
	defer func() { time.Local = previousLocal }()

	testStore := openSchedulerTestStore(t)
	runner := &fakeRunner{pruneDone: make(chan struct{})}
	service := NewService(testStore, audit.NewService(testStore), runner, &fakeStackLister{
		items: []stacks.StackListItem{
			{StackHeader: stacks.StackHeader{ID: "alpha"}},
			{StackHeader: stacks.StackHeader{ID: "zeta"}},
		},
	}, nil)
	service.now = func() time.Time { return time.Date(2026, 4, 10, 5, 0, 0, 0, time.UTC) }

	if _, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Update: defaultSettings().Update,
		Prune: PruneScheduleConfig{
			Enabled:   true,
			Frequency: FrequencyDaily,
			Time:      "04:30",
			Scope: maintenance.PruneScope{
				Images:            true,
				BuildCache:        true,
				StoppedContainers: false,
				Volumes:           false,
			},
		},
	}); err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	if err := service.updateRuntime(context.Background(), "prune", func(state *scheduleRuntimeState) {
		previous := time.Date(2026, 4, 9, 4, 30, 0, 0, time.UTC)
		state.LastScheduledFor = &previous
	}); err != nil {
		t.Fatalf("updateRuntime() error = %v", err)
	}

	service.runDueSchedules(context.Background())
	<-runner.pruneDone
	if runner.pruneCalls != 1 {
		t.Fatalf("pruneCalls = %d, want 1", runner.pruneCalls)
	}
	if runner.pruneReq.Trigger != "scheduled" || runner.pruneReq.ScheduleKey != "prune" {
		t.Fatalf("unexpected prune request: %#v", runner.pruneReq)
	}
}

func TestUpdateSettingsSeedsRuntimeToAvoidImmediateCatchUp(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("UTC", 0)
	defer func() { time.Local = previousLocal }()

	testStore := openSchedulerTestStore(t)
	runner := &fakeRunner{}
	service := NewService(testStore, audit.NewService(testStore), runner, &fakeStackLister{}, nil)
	service.now = func() time.Time { return time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC) }

	if _, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Update: UpdateScheduleConfig{
			Enabled:   true,
			Frequency: FrequencyDaily,
			Time:      "03:30",
			Target:    maintenancejobs.UpdateTarget{Mode: "all"},
			Options: maintenancejobs.UpdateOptions{
				PullImages:    true,
				BuildImages:   true,
				RemoveOrphans: true,
			},
		},
		Prune: defaultSettings().Prune,
	}); err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	service.runDueSchedules(context.Background())
	if runner.updateCalls != 0 {
		t.Fatalf("updateCalls = %d, want 0", runner.updateCalls)
	}

	response, err := service.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}
	if response.Update.Status.LastScheduledFor == nil || !response.Update.Status.LastScheduledFor.Equal(time.Date(2026, 4, 10, 3, 30, 0, 0, time.UTC)) {
		t.Fatalf("last_scheduled_for = %#v, want 2026-04-10T03:30:00Z", response.Update.Status.LastScheduledFor)
	}
	if response.Update.Status.LastResult != "" {
		t.Fatalf("last_result = %q, want empty", response.Update.Status.LastResult)
	}
}
