package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"stacklab/internal/requestid"
	"stacklab/internal/store"
)

func TestTerminalHookRunsOnFinishSucceeded(t *testing.T) {
	t.Parallel()

	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)

	var terminal store.Job
	service.SetTerminalHook(func(job store.Job) {
		terminal = job
	})

	job, err := service.Start(context.Background(), "demo", "up", "local")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	job, err = service.FinishSucceeded(context.Background(), job)
	if err != nil {
		t.Fatalf("FinishSucceeded() error = %v", err)
	}

	if terminal.ID != job.ID || terminal.State != "succeeded" {
		t.Fatalf("unexpected terminal hook payload: %#v", terminal)
	}
}

func TestStartWithWorkflowPersistsCompleteInitialization(t *testing.T) {
	t.Parallel()

	ctx := requestid.WithContext(context.Background(), "req_job_workflow")
	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)
	workflow := []store.JobWorkflowStep{
		{Action: "pull", State: "running", TargetStackID: "demo"},
		{Action: "up", State: "queued", TargetStackID: "demo"},
	}

	job, err := service.StartWithWorkflow(ctx, "demo", "update", "local", workflow)
	if err != nil {
		t.Fatalf("StartWithWorkflow() error = %v", err)
	}
	if job.Workflow == nil || len(job.Workflow.Steps) != 2 {
		t.Fatalf("started job workflow = %#v", job.Workflow)
	}
	if job.RequestID != "req_job_workflow" {
		t.Fatalf("started job request ID = %q", job.RequestID)
	}
	stored, err := jobStore.JobByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("JobByID() error = %v", err)
	}
	if stored.Workflow == nil || len(stored.Workflow.Steps) != 2 || stored.Workflow.Steps[0].Action != "pull" {
		t.Fatalf("stored workflow = %#v", stored.Workflow)
	}
	if stored.RequestID != "req_job_workflow" {
		t.Fatalf("stored job request ID = %q", stored.RequestID)
	}
	events, err := jobStore.ListJobEvents(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListJobEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Event != "job_started" || events[0].Sequence != 1 {
		t.Fatalf("initial events = %#v", events)
	}
}

func TestStartFailureReleasesLocksAndLeavesNoRunningJob(t *testing.T) {
	t.Parallel()

	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.StartWithWorkflow(cancelledCtx, "demo", "up", "local", []store.JobWorkflowStep{{Action: "up", State: "running"}}); err == nil {
		t.Fatal("StartWithWorkflow(cancelled context) error = nil, want error")
	}
	active, err := jobStore.ListActiveJobs(context.Background())
	if err != nil {
		t.Fatalf("ListActiveJobs() error = %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active jobs after failed start = %#v", active)
	}

	job, err := service.Start(context.Background(), "demo", "up", "local")
	if err != nil {
		t.Fatalf("Start(after failed initialization) error = %v", err)
	}
	if _, err := service.FinishSucceeded(context.Background(), job); err != nil {
		t.Fatalf("FinishSucceeded() error = %v", err)
	}
}

func TestFinishTimedOutMarksTerminalState(t *testing.T) {
	t.Parallel()

	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)

	var terminal store.Job
	service.SetTerminalHook(func(job store.Job) {
		terminal = job
	})

	job, err := service.Start(context.Background(), "demo", "pull", "local")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	job, err = service.FinishTimedOut(context.Background(), job, "stack_action_timed_out", "Stack action timed out.")
	if err != nil {
		t.Fatalf("FinishTimedOut() error = %v", err)
	}

	if job.State != "timed_out" || job.ErrorCode != "stack_action_timed_out" || job.FinishedAt == nil {
		t.Fatalf("unexpected timed-out job: %#v", job)
	}
	if terminal.ID != job.ID || terminal.State != "timed_out" {
		t.Fatalf("unexpected terminal hook payload: %#v", terminal)
	}

	events, err := jobStore.ListJobEvents(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ListJobEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("job events len = %d, want 3", len(events))
	}
	if events[1].Event != "job_error" || events[1].State != "timed_out" {
		t.Fatalf("job error event = %#v, want timed_out job_error", events[1])
	}
	if events[2].Event != "job_finished" || events[2].State != "timed_out" {
		t.Fatalf("job finished event = %#v, want timed_out job_finished", events[2])
	}
}

func TestCancelRequestsCancellableJob(t *testing.T) {
	t.Parallel()

	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)

	job, err := service.Start(context.Background(), "demo", "pull", "local")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	workflow := []store.JobWorkflowStep{{Action: "pull", State: "running", TargetStackID: "demo"}}
	job, err = service.UpdateWorkflow(context.Background(), job, workflow)
	if err != nil {
		t.Fatalf("UpdateWorkflow() error = %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	unregister := service.RegisterCancel(job.ID, cancel)
	defer unregister()

	cancelledJob, err := service.Cancel(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if cancelledJob.State != "cancel_requested" {
		t.Fatalf("cancelled job state = %q, want cancel_requested", cancelledJob.State)
	}
	if cancelledJob.Workflow == nil || cancelledJob.Workflow.Steps[0].State != "cancel_requested" {
		t.Fatalf("cancelled job workflow = %#v", cancelledJob.Workflow)
	}
	if runCtx.Err() != context.Canceled {
		t.Fatalf("run context error = %v, want context.Canceled", runCtx.Err())
	}

	events, err := jobStore.ListJobEvents(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ListJobEvents() error = %v", err)
	}
	if events[len(events)-1].Event != "job_cancel_requested" || events[len(events)-1].State != "cancel_requested" {
		t.Fatalf("last event = %#v, want cancel_requested event", events[len(events)-1])
	}
}

func TestUpdateWorkflowCannotRestoreRunningStateAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)
	job, err := service.StartWithWorkflow(ctx, "demo", "pull", "local", []store.JobWorkflowStep{{Action: "pull", State: "running"}})
	if err != nil {
		t.Fatalf("StartWithWorkflow() error = %v", err)
	}
	_, cancel := context.WithCancel(context.Background())
	unregister := service.RegisterCancel(job.ID, cancel)
	defer unregister()
	if _, err := service.Cancel(ctx, job.ID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	if _, err := service.UpdateWorkflow(ctx, job, []store.JobWorkflowStep{{Action: "pull", State: "succeeded"}}); err != nil {
		t.Fatalf("UpdateWorkflow(stale job) error = %v", err)
	}
	stored, err := service.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.State != "cancel_requested" {
		t.Fatalf("job state after stale workflow update = %q, want cancel_requested", stored.State)
	}
}

func TestPublishEventAfterCancellationUsesPersistedState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)
	job, err := service.Start(ctx, "demo", "pull", "local")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	_, cancel := context.WithCancel(context.Background())
	unregister := service.RegisterCancel(job.ID, cancel)
	defer unregister()
	if _, err := service.Cancel(ctx, job.ID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if err := service.PublishEvent(ctx, job, "job_log", "late worker output", "", nil); err != nil {
		t.Fatalf("PublishEvent() error = %v", err)
	}

	events, err := jobStore.ListJobEvents(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListJobEvents() error = %v", err)
	}
	last := events[len(events)-1]
	if last.Event != "job_log" || last.State != "cancel_requested" {
		t.Fatalf("late event = %#v, want cancel_requested job_log", last)
	}
}

func TestCancelAndWorkerFinishSerializeTransitionEvents(t *testing.T) {
	t.Parallel()

	const iterations = 50
	for iteration := 0; iteration < iterations; iteration++ {
		jobStore := openJobsTestStore(t)
		service := NewService(jobStore)
		job, err := service.Start(context.Background(), "demo", "up", "local")
		if err != nil {
			t.Fatalf("iteration %d Start() error = %v", iteration, err)
		}
		_, cancel := context.WithCancel(context.Background())
		unregister := service.RegisterCancel(job.ID, cancel)
		liveEvents, unsubscribe := service.Subscribe(job.ID)

		start := make(chan struct{})
		var waitGroup sync.WaitGroup
		waitGroup.Add(2)
		var cancelErr error
		var finishErr error
		go func() {
			defer waitGroup.Done()
			<-start
			_, cancelErr = service.Cancel(context.Background(), job.ID)
		}()
		go func() {
			defer waitGroup.Done()
			<-start
			_, finishErr = service.FinishSucceeded(context.Background(), job)
		}()
		close(start)
		waitGroup.Wait()
		unregister()
		unsubscribe()

		if cancelErr != nil && !errors.Is(cancelErr, ErrNotCancellable) {
			t.Fatalf("iteration %d Cancel() error = %v", iteration, cancelErr)
		}
		if finishErr != nil {
			t.Fatalf("iteration %d FinishSucceeded() error = %v", iteration, finishErr)
		}
		stored, err := service.Get(context.Background(), job.ID)
		if err != nil {
			t.Fatalf("iteration %d Get() error = %v", iteration, err)
		}
		if stored.State != "succeeded" {
			t.Fatalf("iteration %d final state = %q, want succeeded", iteration, stored.State)
		}
		events, err := jobStore.ListJobEvents(context.Background(), job.ID)
		if err != nil {
			t.Fatalf("iteration %d ListJobEvents() error = %v", iteration, err)
		}
		if len(events) < 2 || len(events) > 3 {
			t.Fatalf("iteration %d events = %#v", iteration, events)
		}
		for index, event := range events {
			if event.Sequence != index+1 {
				t.Fatalf("iteration %d events[%d].Sequence = %d, want %d", iteration, index, event.Sequence, index+1)
			}
		}
		receivedLive := make([]store.JobEvent, 0, len(events)-1)
		for {
			select {
			case event := <-liveEvents:
				receivedLive = append(receivedLive, event)
			default:
				goto liveEventsDrained
			}
		}
	liveEventsDrained:
		if len(receivedLive) != len(events)-1 {
			t.Fatalf("iteration %d live events len = %d, want %d", iteration, len(receivedLive), len(events)-1)
		}
		for index, event := range receivedLive {
			if event.Sequence != events[index+1].Sequence {
				t.Fatalf("iteration %d live event %d sequence = %d, want %d", iteration, index, event.Sequence, events[index+1].Sequence)
			}
		}
		if events[len(events)-1].Event != "job_finished" || events[len(events)-1].State != "succeeded" {
			t.Fatalf("iteration %d final event = %#v", iteration, events[len(events)-1])
		}
	}
}

func TestCancelRejectsUnregisteredJob(t *testing.T) {
	t.Parallel()

	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)

	job, err := service.Start(context.Background(), "demo", "pull", "local")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if _, err := service.Cancel(context.Background(), job.ID); !errors.Is(err, ErrNotCancellable) {
		t.Fatalf("Cancel() error = %v, want ErrNotCancellable", err)
	}
}

func TestCancelDoesNotOverwriteTerminalJob(t *testing.T) {
	t.Parallel()

	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)

	job, err := service.Start(context.Background(), "demo", "pull", "local")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	unregister := service.RegisterCancel(job.ID, cancel)
	defer unregister()

	now := time.Now().UTC()
	job.State = "succeeded"
	job.FinishedAt = &now
	if err := jobStore.UpdateJob(context.Background(), job); err != nil {
		t.Fatalf("UpdateJob(succeeded) error = %v", err)
	}

	if _, err := service.Cancel(context.Background(), job.ID); !errors.Is(err, ErrNotCancellable) {
		t.Fatalf("Cancel(terminal) error = %v, want ErrNotCancellable", err)
	}
	if runCtx.Err() != nil {
		t.Fatalf("run context error = %v, want nil", runCtx.Err())
	}
	stored, err := jobStore.JobByID(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("JobByID() error = %v", err)
	}
	if stored.State != "succeeded" {
		t.Fatalf("stored job state = %q, want succeeded", stored.State)
	}
}

func TestReconcileInterruptedMarksActiveJobFailed(t *testing.T) {
	t.Parallel()

	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)

	var terminal store.Job
	service.SetTerminalHook(func(job store.Job) {
		terminal = job
	})

	job, err := service.Start(context.Background(), "demo", "update_stacks", "local")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	workflow := []store.JobWorkflowStep{
		{Action: "pull", State: "succeeded", TargetStackID: "demo"},
		{Action: "up", State: "running", TargetStackID: "demo"},
		{Action: "prune", State: "queued"},
	}
	job, err = service.UpdateWorkflow(context.Background(), job, workflow)
	if err != nil {
		t.Fatalf("UpdateWorkflow() error = %v", err)
	}

	reconciled, err := service.ReconcileInterrupted(context.Background())
	if err != nil {
		t.Fatalf("ReconcileInterrupted() error = %v", err)
	}
	if len(reconciled) != 1 {
		t.Fatalf("ReconcileInterrupted() len = %d, want 1", len(reconciled))
	}

	got, err := service.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != "failed" {
		t.Fatalf("job state = %q, want failed", got.State)
	}
	if got.ErrorCode != "job_interrupted" {
		t.Fatalf("job error code = %q, want job_interrupted", got.ErrorCode)
	}
	if got.ErrorMessage != "Job did not finish before Stacklab restarted." {
		t.Fatalf("job error message = %q", got.ErrorMessage)
	}
	if got.FinishedAt == nil {
		t.Fatal("expected finished_at to be set")
	}
	if got.Workflow == nil || len(got.Workflow.Steps) != 3 {
		t.Fatalf("job workflow = %#v", got.Workflow)
	}
	if got.Workflow.Steps[1].State != "failed" {
		t.Fatalf("workflow step state = %q, want failed", got.Workflow.Steps[1].State)
	}
	if terminal.ID != job.ID || terminal.State != "failed" {
		t.Fatalf("unexpected terminal hook payload after reconcile: %#v", terminal)
	}

	events, err := jobStore.ListJobEvents(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ListJobEvents() error = %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("job events len = %d, want 4", len(events))
	}
	if events[1].Event != "job_step_finished" || events[1].State != "failed" {
		t.Fatalf("reconcile step event = %#v, want failed job_step_finished", events[1])
	}
	if events[1].Step == nil || events[1].Step.Index != 2 || events[1].Step.Action != "up" || events[1].Step.TargetStackID != "demo" {
		t.Fatalf("reconcile step ref = %#v", events[1].Step)
	}
	if events[2].Event != "job_error" || events[2].State != "failed" {
		t.Fatalf("reconcile error event = %#v", events[2])
	}
	if events[2].Step == nil || events[2].Step.Index != 2 {
		t.Fatalf("reconcile error step = %#v", events[2].Step)
	}
	if events[3].Event != "job_finished" || events[3].State != "failed" {
		t.Fatalf("reconcile finished event = %#v", events[3])
	}
}

func TestReconcileInterruptedSkipsActiveSelfUpdate(t *testing.T) {
	t.Parallel()

	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)

	job, err := service.StartWithResources(context.Background(), "", "self_update_stacklab", "local", SelfUpdateResource())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	reconciled, err := service.ReconcileInterrupted(context.Background())
	if err != nil {
		t.Fatalf("ReconcileInterrupted() error = %v", err)
	}
	if len(reconciled) != 0 {
		t.Fatalf("ReconcileInterrupted() len = %d, want 0", len(reconciled))
	}

	got, err := service.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != "running" {
		t.Fatalf("job state = %q, want running", got.State)
	}

	events, err := jobStore.ListJobEvents(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ListJobEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Event != "job_started" {
		t.Fatalf("job events = %#v, want only job_started", events)
	}
}

func TestReconcileInterruptedFailsStaleSelfUpdate(t *testing.T) {
	t.Parallel()

	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)

	job, err := service.StartWithResources(context.Background(), "", "self_update_stacklab", "local", SelfUpdateResource())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staleStartedAt := time.Now().UTC().Add(-(selfUpdateReconcileGracePeriod + time.Minute))
	job.RequestedAt = staleStartedAt
	job.StartedAt = &staleStartedAt
	if err := jobStore.UpdateJob(context.Background(), job); err != nil {
		t.Fatalf("UpdateJob(stale self-update) error = %v", err)
	}

	reconciled, err := service.ReconcileInterrupted(context.Background())
	if err != nil {
		t.Fatalf("ReconcileInterrupted() error = %v", err)
	}
	if len(reconciled) != 1 {
		t.Fatalf("ReconcileInterrupted() len = %d, want 1", len(reconciled))
	}
	if reconciled[0].ID != job.ID || reconciled[0].State != "failed" {
		t.Fatalf("unexpected reconciled job: %#v", reconciled[0])
	}

	got, err := service.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != "failed" || got.ErrorCode != "job_interrupted" {
		t.Fatalf("job after reconcile = %#v, want failed job_interrupted", got)
	}
}

func openJobsTestStore(t *testing.T) *store.Store {
	t.Helper()

	s, err := store.Open(t.TempDir() + "/stacklab.db")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}
