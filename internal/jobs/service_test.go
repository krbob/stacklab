package jobs

import (
	"context"
	"testing"

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

	job, err := service.Start(context.Background(), "", "self_update_stacklab", "local")
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
