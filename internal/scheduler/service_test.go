package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
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
	updateStart chan struct{}
	updateBlock <-chan struct{}
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
	if f.updateStart != nil {
		close(f.updateStart)
	}
	if f.updateBlock != nil {
		<-f.updateBlock
	}
	if f.updateErr != nil {
		return store.Job{}, f.updateErr
	}
	now := time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC)
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

func (f *fakeRunner) RunPrune(ctx context.Context, request maintenancejobs.PruneRequest, requestedBy string, managedStackIDs []string) (store.Job, error) {
	f.pruneCalls++
	f.pruneReq = request
	if f.pruneErr != nil {
		return store.Job{}, f.pruneErr
	}
	now := time.Date(2026, 4, 10, 5, 0, 0, 0, time.UTC)
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

type manualClock struct {
	mu            sync.Mutex
	now           time.Time
	tickerCreated chan *manualTicker
}

func newManualClock(now time.Time) *manualClock {
	return &manualClock{
		now:           now,
		tickerCreated: make(chan *manualTicker, 1),
	}
}

func (clock *manualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *manualClock) Set(now time.Time) {
	clock.mu.Lock()
	clock.now = now
	clock.mu.Unlock()
}

func (clock *manualClock) NewTicker(time.Duration) schedulerTicker {
	ticker := &manualTicker{
		ticks:   make(chan time.Time, 1),
		stopped: make(chan struct{}),
	}
	clock.tickerCreated <- ticker
	return ticker
}

type manualTicker struct {
	ticks   chan time.Time
	stopped chan struct{}
	once    sync.Once
}

func (ticker *manualTicker) C() <-chan time.Time {
	return ticker.ticks
}

func (ticker *manualTicker) Stop() {
	ticker.once.Do(func() { close(ticker.stopped) })
}

func (ticker *manualTicker) Tick(now time.Time) {
	ticker.ticks <- now
}

func newServiceWithClock(testStore *store.Store, runner runner, stackLister stackLister, clock schedulerClock, location *time.Location) *Service {
	return newService(testStore, audit.NewService(testStore), runner, stackLister, nil, clock, location)
}

func (f *fakeStackLister) Get(ctx context.Context, stackID string) (stacks.StackDetailResponse, error) {
	return stacks.StackDetailResponse{
		Stack: stacks.StackDetail{
			StackHeader: stacks.StackHeader{ID: stackID, Name: stackID},
			Services: []stacks.Service{
				{Name: "app", Mode: stacks.ServiceModeImage},
				{Name: "db", Mode: stacks.ServiceModeImage},
			},
			AvailableActions: []string{"up"},
		},
	}, nil
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

func TestUpdateSettingsPersistsServiceExclusions(t *testing.T) {
	testStore := openSchedulerTestStore(t)
	service := NewService(testStore, audit.NewService(testStore), &fakeRunner{}, &fakeStackLister{
		items: []stacks.StackListItem{{StackHeader: stacks.StackHeader{ID: "demo"}}},
	}, nil)

	response, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Update: UpdateScheduleConfig{
			Enabled:   true,
			Frequency: FrequencyDaily,
			Time:      "03:30",
			Target: maintenancejobs.UpdateTarget{
				Mode:             "selected",
				StackIDs:         []string{"demo"},
				ExcludedServices: map[string][]string{"demo": {"db", "app", "db"}},
			},
			Options: maintenancejobs.UpdateOptions{
				PullImages:    true,
				BuildImages:   true,
				RemoveOrphans: false,
			},
		},
		Prune: defaultSettings().Prune,
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	got := response.Update.Target.ExcludedServices["demo"]
	want := []string{"app", "db"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("excluded services = %#v, want %#v", got, want)
	}
}

func TestUpdateSettingsRejectsRemoveOrphansWithServiceExclusions(t *testing.T) {
	testStore := openSchedulerTestStore(t)
	service := NewService(testStore, audit.NewService(testStore), &fakeRunner{}, &fakeStackLister{
		items: []stacks.StackListItem{{StackHeader: stacks.StackHeader{ID: "demo"}}},
	}, nil)

	_, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Update: UpdateScheduleConfig{
			Enabled:   true,
			Frequency: FrequencyDaily,
			Time:      "03:30",
			Target: maintenancejobs.UpdateTarget{
				Mode:             "selected",
				StackIDs:         []string{"demo"},
				ExcludedServices: map[string][]string{"demo": {"db"}},
			},
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
	testStore := openSchedulerTestStore(t)
	runner := &fakeRunner{}
	clock := newManualClock(time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC))
	service := newServiceWithClock(testStore, runner, &fakeStackLister{
		items: []stacks.StackListItem{{StackHeader: stacks.StackHeader{ID: "demo"}}},
	}, clock, time.UTC)

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
	service.waitForWorkers()

	if runner.updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", runner.updateCalls)
	}
	if runner.updateReq.Trigger != "scheduled" || runner.updateReq.ScheduleKey != "update" {
		t.Fatalf("unexpected update request: %#v", runner.updateReq)
	}

	service.runDueSchedules(context.Background())
	service.waitForWorkers()
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

func TestRunDueSchedulesSkipsStaleCatchUp(t *testing.T) {
	testStore := openSchedulerTestStore(t)
	runner := &fakeRunner{}
	clock := newManualClock(time.Date(2026, 4, 9, 4, 0, 0, 0, time.UTC))
	service := newServiceWithClock(testStore, runner, &fakeStackLister{
		items: []stacks.StackListItem{{StackHeader: stacks.StackHeader{ID: "demo"}}},
	}, clock, time.UTC)

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

	clock.Set(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC))
	service.runDueSchedules(context.Background())
	service.waitForWorkers()

	if runner.updateCalls != 0 {
		t.Fatalf("updateCalls = %d, want 0 for stale catch-up", runner.updateCalls)
	}
}

func TestScheduleCalculationsUseInjectedLocation(t *testing.T) {
	testStore := openSchedulerTestStore(t)
	clock := newManualClock(time.Date(2026, 4, 10, 2, 0, 0, 0, time.UTC))
	location := time.FixedZone("test-local", 2*60*60)
	service := newServiceWithClock(testStore, &fakeRunner{}, &fakeStackLister{}, clock, location)

	response, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
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
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	wantLastScheduled := time.Date(2026, 4, 10, 1, 30, 0, 0, time.UTC)
	if response.Update.Status.LastScheduledFor == nil || !response.Update.Status.LastScheduledFor.Equal(wantLastScheduled) {
		t.Fatalf("last_scheduled_for = %#v, want %v", response.Update.Status.LastScheduledFor, wantLastScheduled)
	}
	wantNextRun := time.Date(2026, 4, 11, 1, 30, 0, 0, time.UTC)
	if response.Update.Status.NextRunAt == nil || !response.Update.Status.NextRunAt.Equal(wantNextRun) {
		t.Fatalf("next_run_at = %#v, want %v", response.Update.Status.NextRunAt, wantNextRun)
	}
}

func TestRunDueSchedulesDispatchesPruneWithManagedLocks(t *testing.T) {
	testStore := openSchedulerTestStore(t)
	runner := &fakeRunner{}
	clock := newManualClock(time.Date(2026, 4, 10, 5, 0, 0, 0, time.UTC))
	service := newServiceWithClock(testStore, runner, &fakeStackLister{
		items: []stacks.StackListItem{
			{StackHeader: stacks.StackHeader{ID: "alpha"}},
			{StackHeader: stacks.StackHeader{ID: "zeta"}},
		},
	}, clock, time.UTC)

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
	service.waitForWorkers()
	if runner.pruneCalls != 1 {
		t.Fatalf("pruneCalls = %d, want 1", runner.pruneCalls)
	}
	if runner.pruneReq.Trigger != "scheduled" || runner.pruneReq.ScheduleKey != "prune" {
		t.Fatalf("unexpected prune request: %#v", runner.pruneReq)
	}
}

func TestFinalizeScheduledRunTreatsMissingJobAsFailure(t *testing.T) {
	testStore := openSchedulerTestStore(t)
	clock := newManualClock(time.Date(2026, 4, 10, 6, 0, 0, 0, time.UTC))
	service := newServiceWithClock(testStore, &fakeRunner{}, &fakeStackLister{}, clock, time.UTC)

	scheduledFor := time.Date(2026, 4, 10, 5, 30, 0, 0, time.UTC)
	service.finalizeScheduledRun(context.Background(), "update", scheduledFor, store.Job{}, nil)

	runtimeState, err := service.loadRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("loadRuntimeState() error = %v", err)
	}
	if runtimeState.Update.LastResult != "failed" {
		t.Fatalf("LastResult = %q, want failed", runtimeState.Update.LastResult)
	}
	if runtimeState.Update.LastMessage != "maintenance runner did not return a job" {
		t.Fatalf("LastMessage = %q", runtimeState.Update.LastMessage)
	}
	if runtimeState.Update.LastJobID != "" {
		t.Fatalf("LastJobID = %q, want empty", runtimeState.Update.LastJobID)
	}
}

func TestFinalizeScheduledRunPersistsAfterContextCancellation(t *testing.T) {
	testStore := openSchedulerTestStore(t)
	clock := newManualClock(time.Date(2026, 4, 10, 6, 0, 0, 0, time.UTC))
	service := newServiceWithClock(testStore, &fakeRunner{}, &fakeStackLister{}, clock, time.UTC)

	scheduledFor := time.Date(2026, 4, 10, 5, 30, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	service.finalizeScheduledRun(ctx, "update", scheduledFor, store.Job{}, errors.New("runner failed"))

	runtimeState, err := service.loadRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("loadRuntimeState() error = %v", err)
	}
	if runtimeState.Update.LastResult != "failed" {
		t.Fatalf("LastResult = %q, want failed", runtimeState.Update.LastResult)
	}
	if runtimeState.Update.LastMessage != "runner failed" {
		t.Fatalf("LastMessage = %q", runtimeState.Update.LastMessage)
	}
}

func TestRunBackgroundWaitsForWorkerFinalizationStress(t *testing.T) {
	testStore := openSchedulerTestStore(t)
	stackLister := &fakeStackLister{
		items: []stacks.StackListItem{{StackHeader: stacks.StackHeader{ID: "demo"}}},
	}

	for iteration := 0; iteration < 20; iteration++ {
		base := time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC).AddDate(0, 0, iteration*2)
		clock := newManualClock(base)
		started := make(chan struct{})
		release := make(chan struct{})
		var releaseOnce sync.Once
		releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
		defer releaseWorker()
		runner := &fakeRunner{updateStart: started, updateBlock: release}
		service := newServiceWithClock(testStore, runner, stackLister, clock, time.UTC)

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
			t.Fatalf("iteration %d: UpdateSettings() error = %v", iteration, err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		backgroundDone := make(chan struct{})
		go func() {
			service.RunBackground(ctx)
			close(backgroundDone)
		}()
		ticker := waitForTicker(t, clock.tickerCreated, iteration)
		clock.Set(base.AddDate(0, 0, 1))
		ticker.Tick(clock.Now())
		waitForSignal(t, started, "scheduled worker start", iteration)

		cancel()
		waitForSignal(t, ticker.stopped, "scheduler loop stop", iteration)
		select {
		case <-backgroundDone:
			t.Fatalf("iteration %d: RunBackground returned before worker finalization", iteration)
		default:
		}

		releaseWorker()
		waitForSignal(t, backgroundDone, "background completion", iteration)
		runtimeState, err := service.loadRuntimeState(context.Background())
		if err != nil {
			t.Fatalf("iteration %d: loadRuntimeState() error = %v", iteration, err)
		}
		if runtimeState.Update.LastResult != "succeeded" || runtimeState.Update.LastJobID != "job_sched_update" {
			t.Fatalf("iteration %d: worker finalization = %#v", iteration, runtimeState.Update)
		}
	}
}

func TestUpdateSettingsSeedsRuntimeToAvoidImmediateCatchUp(t *testing.T) {
	testStore := openSchedulerTestStore(t)
	runner := &fakeRunner{}
	clock := newManualClock(time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC))
	service := newServiceWithClock(testStore, runner, &fakeStackLister{}, clock, time.UTC)

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
	service.waitForWorkers()
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

func waitForTicker(t *testing.T, created <-chan *manualTicker, iteration int) *manualTicker {
	t.Helper()
	select {
	case ticker := <-created:
		return ticker
	case <-time.After(5 * time.Second):
		t.Fatalf("iteration %d: timed out waiting for scheduler ticker", iteration)
		return nil
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string, iteration int) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("iteration %d: timed out waiting for %s", iteration, description)
	}
}
