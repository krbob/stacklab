package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"stacklab/internal/store"
)

func TestServiceReadModelsAndActivitySubscriptions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)
	if service.Store() != jobStore {
		t.Fatal("Store() did not return the backing store")
	}

	activity, unsubscribeActivity := service.SubscribeActivity()
	defer unsubscribeActivity()
	workflow := []store.JobWorkflowStep{
		{Action: "pull", State: "running", TargetStackID: "alpha"},
		{Action: "up", State: "queued", TargetStackID: "alpha"},
	}
	job, err := service.StartWithResourcesAndWorkflow(ctx, "alpha", "update", "operator", workflow, StackResource("alpha"))
	if err != nil {
		t.Fatalf("StartWithResourcesAndWorkflow() error = %v", err)
	}
	receiveActivitySignal(t, activity)

	liveEvents, unsubscribeEvents := service.Subscribe(job.ID)
	step := &store.JobEventStep{Index: 1, Total: 2, Action: "pull", TargetStackID: "alpha", TargetServiceNames: []string{"web"}}
	progress := &store.JobProgress{Phase: "pulling", Completed: 1, Total: 2, Unit: "images", Detail: "web"}
	if err := service.PublishEventWithProgress(ctx, job, "job_progress", "Pulled web.", "sha256:abc", step, progress); err != nil {
		t.Fatalf("PublishEventWithProgress() error = %v", err)
	}
	receiveActivitySignal(t, activity)
	live := receiveJobEvent(t, liveEvents)
	if live.Event != "job_progress" || live.Sequence != 2 || live.Progress == nil || live.Progress.Completed != 1 {
		t.Fatalf("live event = %#v", live)
	}

	replayed, err := service.ReplayEvents(ctx, job.ID)
	if err != nil {
		t.Fatalf("ReplayEvents() error = %v", err)
	}
	if len(replayed) != 2 || replayed[1].Step == nil || replayed[1].Progress == nil {
		t.Fatalf("ReplayEvents() = %#v", replayed)
	}
	events, err := service.Events(ctx, job.ID)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if !events.Retained || events.Message != "" || len(events.Items) != 2 {
		t.Fatalf("Events() = %#v", events)
	}
	lastEvent := events.Items[1]
	if lastEvent.Step == nil || lastEvent.Step.Index != 1 || lastEvent.Step.Action != "pull" || lastEvent.Step.TargetStackID != "alpha" {
		t.Fatalf("Events() step = %#v", lastEvent.Step)
	}
	if lastEvent.Progress == nil || lastEvent.Progress.Detail != "web" || lastEvent.Data != "sha256:abc" {
		t.Fatalf("Events() progress item = %#v", lastEvent)
	}

	cancelJob, err := service.Start(ctx, "beta", "pull", "operator")
	if err != nil {
		t.Fatalf("Start(beta) error = %v", err)
	}
	_, cancelWorker := context.WithCancel(context.Background())
	unregisterCancel := service.RegisterCancel(cancelJob.ID, cancelWorker)
	defer unregisterCancel()
	cancelJob, err = service.Cancel(ctx, cancelJob.ID)
	if err != nil {
		t.Fatalf("Cancel(beta) error = %v", err)
	}

	queuedAt := time.Now().UTC()
	queuedJob := store.Job{
		ID: "job-queued-read-model", StackID: "gamma", Action: "build", State: "queued",
		RequestedBy: "operator", RequestID: "req-queued", RequestedAt: queuedAt,
	}
	if err := jobStore.CreateJob(ctx, queuedJob); err != nil {
		t.Fatalf("CreateJob(queued) error = %v", err)
	}

	active, err := service.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if active.Summary.ActiveCount != 3 || active.Summary.RunningCount != 1 || active.Summary.QueuedCount != 1 || active.Summary.CancelRequestedCount != 1 {
		t.Fatalf("ListActive() summary = %#v", active.Summary)
	}
	running := findActiveJob(t, active.Items, job.ID)
	if running.StackID == nil || *running.StackID != "alpha" || running.Workflow == nil || len(running.Workflow.Steps) != 2 {
		t.Fatalf("running active job = %#v", running)
	}
	if running.LatestEvent == nil || running.LatestEvent.Event != "job_progress" || running.CurrentStep == nil || running.CurrentStep.Action != "pull" {
		t.Fatalf("running latest activity = %#v", running)
	}
	queued := findActiveJob(t, active.Items, queuedJob.ID)
	if queued.StackID == nil || *queued.StackID != "gamma" || queued.RequestID != "req-queued" || queued.LatestEvent != nil {
		t.Fatalf("queued active job = %#v", queued)
	}

	retained, err := service.Events(ctx, queuedJob.ID)
	if err != nil {
		t.Fatalf("Events(queued without events) error = %v", err)
	}
	if retained.Retained || retained.Message != "Detailed output for this job is no longer retained." || len(retained.Items) != 0 {
		t.Fatalf("Events(queued without events) = %#v", retained)
	}

	unsubscribeEvents()
	service.publishLive(store.JobEvent{JobID: job.ID, Event: "ignored"})
	select {
	case event := <-liveEvents:
		t.Fatalf("unsubscribed live channel received %#v", event)
	default:
	}
	unsubscribeEvents()

	select {
	case <-activity:
	default:
	}
	unsubscribeActivity()
	service.notifyActivity()
	select {
	case <-activity:
		t.Fatal("unsubscribed activity channel received a signal")
	default:
	}

	if _, err := service.FinishCancelled(ctx, cancelJob, "cancelled_by_operator", "Operator cancelled the job."); err != nil {
		t.Fatalf("FinishCancelled() error = %v", err)
	}
}

func TestServiceWorkflowEntryPointsAndQueryErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)
	workflow := []store.JobWorkflowStep{{Action: "self_update", State: "running"}}
	drain, err := service.StartDrainingWithWorkflow(ctx, "self_update_stacklab", "operator", SelfUpdateResource(), workflow)
	if err != nil {
		t.Fatalf("StartDrainingWithWorkflow() error = %v", err)
	}
	if drain.Workflow == nil || len(drain.Workflow.Steps) != 1 || drain.Workflow.Steps[0].Action != "self_update" {
		t.Fatalf("draining workflow = %#v", drain.Workflow)
	}
	if _, err := service.FinishCancelled(ctx, drain, "cancelled", "Cancelled for test."); err != nil {
		t.Fatalf("FinishCancelled(drain) error = %v", err)
	}

	if _, err := service.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := service.Events(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Events(missing) error = %v, want ErrNotFound", err)
	}

	service.RegisterCancel("", nil)()
	service.RegisterCancel("job", nil)()
	_, unsubscribe := service.Subscribe("missing")
	unsubscribe()
	unsubscribe()

	if err := jobStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := service.ReplayEvents(ctx, drain.ID); err == nil {
		t.Fatal("ReplayEvents(closed store) returned nil error")
	}
	if _, err := service.ListActive(ctx); err == nil {
		t.Fatal("ListActive(closed store) returned nil error")
	}
	if _, err := service.ReconcileInterrupted(ctx); err == nil {
		t.Fatal("ReconcileInterrupted(closed store) returned nil error")
	}
}

func receiveActivitySignal(t *testing.T, signals <-chan struct{}) {
	t.Helper()
	select {
	case <-signals:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for job activity signal")
	}
}

func receiveJobEvent(t *testing.T, events <-chan store.JobEvent) store.JobEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live job event")
		return store.JobEvent{}
	}
}

func findActiveJob(t *testing.T, items []ActiveJobItem, id string) ActiveJobItem {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("active job %q not found in %#v", id, items)
	return ActiveJobItem{}
}
