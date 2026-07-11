package jobs

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"stacklab/internal/store"
)

var ErrNotFound = errors.New("job not found")
var ErrNotCancellable = errors.New("job is not cancellable")

const selfUpdateReconcileGracePeriod = 2 * time.Hour

type Service struct {
	store            *store.Store
	mu               sync.Mutex
	lockedByResource map[Resource]string
	resourcesByJob   map[string][]Resource
	cancelByJob      map[string]context.CancelFunc
	subsByJob        map[string]map[chan store.JobEvent]struct{}
	activitySubs     map[chan struct{}]struct{}
	onTerminal       func(store.Job)
}

func NewService(jobStore *store.Store) *Service {
	return &Service{
		store:            jobStore,
		lockedByResource: map[Resource]string{},
		resourcesByJob:   map[string][]Resource{},
		cancelByJob:      map[string]context.CancelFunc{},
		subsByJob:        map[string]map[chan store.JobEvent]struct{}{},
		activitySubs:     map[chan struct{}]struct{}{},
	}
}

// SubscribeActivity returns a coalesced signal that fires whenever any job
// publishes an event (state transitions included — every transition emits
// one). Subscribers re-read ListActive on signal; the channel never carries
// payloads, so slow consumers cannot lose state (Slice D).
func (s *Service) SubscribeActivity() (<-chan struct{}, func()) {
	signal := make(chan struct{}, 1)
	s.mu.Lock()
	s.activitySubs[signal] = struct{}{}
	s.mu.Unlock()

	unsubscribe := func() {
		s.mu.Lock()
		delete(s.activitySubs, signal)
		s.mu.Unlock()
	}
	return signal, unsubscribe
}

func (s *Service) notifyActivity() {
	s.mu.Lock()
	channels := make([]chan struct{}, 0, len(s.activitySubs))
	for channel := range s.activitySubs {
		channels = append(channels, channel)
	}
	s.mu.Unlock()

	for _, channel := range channels {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
}

// Store exposes the backing store for sibling services wired inside the
// handler (keeps constructor signatures stable).
func (s *Service) Store() *store.Store {
	return s.store
}

func (s *Service) SetTerminalHook(hook func(store.Job)) {
	s.onTerminal = hook
}

func (s *Service) RegisterCancel(jobID string, cancel context.CancelFunc) func() {
	if jobID == "" || cancel == nil {
		return func() {}
	}
	s.mu.Lock()
	s.cancelByJob[jobID] = cancel
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		delete(s.cancelByJob, jobID)
		s.mu.Unlock()
	}
}

func (s *Service) Start(ctx context.Context, stackID, action, requestedBy string) (store.Job, error) {
	return s.StartWithResources(ctx, stackID, action, requestedBy)
}

func (s *Service) StartWithResources(ctx context.Context, stackID, action, requestedBy string, requestedResources ...Resource) (store.Job, error) {
	now := time.Now().UTC()
	job := store.Job{
		ID:          "job_" + randomToken(18),
		StackID:     stackID,
		Action:      action,
		State:       "running",
		RequestedBy: requestedBy,
		RequestedAt: now,
		StartedAt:   &now,
	}

	resources, err := normalizeResources(stackID, requestedResources)
	if err != nil {
		return store.Job{}, err
	}
	if err := s.lockMany(job.ID, resources); err != nil {
		return store.Job{}, err
	}
	if err := s.store.CreateJob(ctx, job); err != nil {
		s.unlockAll(job.ID)
		return store.Job{}, err
	}
	if err := s.PublishEvent(ctx, job, "job_started", "Job started.", "", nil); err != nil {
		s.unlockAll(job.ID)
		return store.Job{}, err
	}
	return job, nil
}

func (s *Service) FinishSucceeded(ctx context.Context, job store.Job) (store.Job, error) {
	defer s.unlockAll(job.ID)

	now := time.Now().UTC()
	job.State = "succeeded"
	job.FinishedAt = &now
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return store.Job{}, err
	}
	if err := s.PublishEvent(ctx, job, "job_finished", "Job finished successfully.", "", nil); err != nil {
		return job, err
	}
	if s.onTerminal != nil {
		s.onTerminal(job)
	}
	return job, nil
}

func (s *Service) FinishFailed(ctx context.Context, job store.Job, errorCode, errorMessage string) (store.Job, error) {
	return s.finishTerminal(ctx, job, "failed", errorCode, errorMessage, "Job finished with errors.")
}

func (s *Service) FinishTimedOut(ctx context.Context, job store.Job, errorCode, errorMessage string) (store.Job, error) {
	return s.finishTerminal(ctx, job, "timed_out", errorCode, errorMessage, "Job timed out.")
}

func (s *Service) FinishCancelled(ctx context.Context, job store.Job, errorCode, errorMessage string) (store.Job, error) {
	return s.finishTerminal(ctx, job, "cancelled", errorCode, errorMessage, "Job was cancelled.")
}

func (s *Service) finishTerminal(ctx context.Context, job store.Job, state, errorCode, errorMessage, finishMessage string) (store.Job, error) {
	defer s.unlockAll(job.ID)

	now := time.Now().UTC()
	job.State = state
	job.FinishedAt = &now
	job.ErrorCode = errorCode
	job.ErrorMessage = errorMessage
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return store.Job{}, err
	}
	if err := s.PublishEvent(ctx, job, "job_error", errorMessage, "", nil); err != nil {
		return job, err
	}
	if err := s.PublishEvent(ctx, job, "job_finished", finishMessage, "", nil); err != nil {
		return job, err
	}
	if s.onTerminal != nil {
		s.onTerminal(job)
	}
	return job, nil
}

func (s *Service) ReconcileInterrupted(ctx context.Context) ([]store.Job, error) {
	storedJobs, err := s.store.ListActiveJobs(ctx)
	if err != nil {
		return nil, err
	}

	reconciled := make([]store.Job, 0, len(storedJobs))
	for _, job := range storedJobs {
		if shouldSkipReconcile(job) {
			continue
		}

		failedJob, err := s.reconcileInterruptedJob(ctx, job)
		if err != nil {
			return nil, err
		}
		reconciled = append(reconciled, failedJob)
	}

	return reconciled, nil
}

func (s *Service) UpdateWorkflow(ctx context.Context, job store.Job, steps []store.JobWorkflowStep) (store.Job, error) {
	job.Workflow = &store.JobWorkflow{
		Steps: append([]store.JobWorkflowStep(nil), steps...),
	}
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return store.Job{}, err
	}
	return job, nil
}

func (s *Service) PublishEvent(ctx context.Context, job store.Job, eventType, message, data string, step *store.JobEventStep) error {
	return s.PublishEventWithProgress(ctx, job, eventType, message, data, step, nil)
}

func (s *Service) PublishEventWithProgress(ctx context.Context, job store.Job, eventType, message, data string, step *store.JobEventStep, progress *store.JobProgress) error {
	sequence, err := s.store.NextJobEventSequence(ctx, job.ID)
	if err != nil {
		return err
	}

	event := store.JobEvent{
		JobID:     job.ID,
		Sequence:  sequence,
		Event:     eventType,
		State:     job.State,
		Message:   message,
		Data:      data,
		Step:      step,
		Progress:  progress,
		Timestamp: time.Now().UTC(),
	}
	if err := s.store.CreateJobEvent(ctx, event); err != nil {
		return err
	}

	s.publishLive(event)
	s.notifyActivity()
	return nil
}

func (s *Service) ReplayEvents(ctx context.Context, jobID string) ([]store.JobEvent, error) {
	return s.store.ListJobEvents(ctx, jobID)
}

func (s *Service) Subscribe(jobID string) (<-chan store.JobEvent, func()) {
	events := make(chan store.JobEvent, 64)

	s.mu.Lock()
	if _, ok := s.subsByJob[jobID]; !ok {
		s.subsByJob[jobID] = map[chan store.JobEvent]struct{}{}
	}
	s.subsByJob[jobID][events] = struct{}{}
	s.mu.Unlock()

	unsubscribe := func() {
		s.mu.Lock()
		subs := s.subsByJob[jobID]
		if subs != nil {
			delete(subs, events)
			if len(subs) == 0 {
				delete(s.subsByJob, jobID)
			}
		}
		s.mu.Unlock()
	}

	return events, unsubscribe
}

func (s *Service) Get(ctx context.Context, id string) (store.Job, error) {
	job, err := s.store.JobByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Job{}, ErrNotFound
		}
		return store.Job{}, err
	}
	return job, nil
}

func (s *Service) Events(ctx context.Context, id string) (EventsResponse, error) {
	job, err := s.Get(ctx, id)
	if err != nil {
		return EventsResponse{}, err
	}

	storedEvents, err := s.store.ListJobEvents(ctx, id)
	if err != nil {
		return EventsResponse{}, err
	}

	response := EventsResponse{
		JobID:    job.ID,
		Retained: len(storedEvents) > 0,
		Items:    make([]JobEventRecord, 0, len(storedEvents)),
	}
	for _, event := range storedEvents {
		var step *ActiveJobStep
		if event.Step != nil {
			step = &ActiveJobStep{
				Index:         event.Step.Index,
				Total:         event.Step.Total,
				Action:        event.Step.Action,
				TargetStackID: event.Step.TargetStackID,
			}
		}
		response.Items = append(response.Items, JobEventRecord{
			JobID:     event.JobID,
			Sequence:  event.Sequence,
			Event:     event.Event,
			State:     event.State,
			Message:   event.Message,
			Data:      event.Data,
			Step:      step,
			Progress:  event.Progress,
			Timestamp: event.Timestamp,
		})
	}

	if !response.Retained {
		response.Message = "Detailed output for this job is no longer retained."
	}

	return response, nil
}

func (s *Service) Cancel(ctx context.Context, id string) (store.Job, error) {
	job, err := s.Get(ctx, id)
	if err != nil {
		return store.Job{}, err
	}
	switch job.State {
	case "cancel_requested":
		return job, nil
	case "queued", "running":
	default:
		return store.Job{}, ErrNotCancellable
	}

	s.mu.Lock()
	cancel := s.cancelByJob[id]
	s.mu.Unlock()
	if cancel == nil {
		return store.Job{}, ErrNotCancellable
	}

	job.State = "cancel_requested"
	if job.Workflow != nil {
		job.Workflow = &store.JobWorkflow{Steps: markWorkflowCancelRequested(job.Workflow.Steps)}
	}
	updated, err := s.store.UpdateJobIfStateIn(ctx, job, []string{"queued", "running", "cancel_requested"})
	if err != nil {
		return store.Job{}, err
	}
	if !updated {
		current, getErr := s.Get(ctx, id)
		if getErr != nil {
			return store.Job{}, getErr
		}
		if current.State == "cancel_requested" {
			return current, nil
		}
		return store.Job{}, ErrNotCancellable
	}

	cancel()
	if err := s.PublishEvent(ctx, job, "job_cancel_requested", "Cancellation requested.", "", nil); err != nil {
		return job, err
	}
	return job, nil
}

func (s *Service) ListActive(ctx context.Context) (ActiveJobsResponse, error) {
	storedJobs, err := s.store.ListActiveJobs(ctx)
	if err != nil {
		return ActiveJobsResponse{}, err
	}

	response := ActiveJobsResponse{
		Items: make([]ActiveJobItem, 0, len(storedJobs)),
	}

	for _, job := range storedJobs {
		item := ActiveJobItem{
			ID:          job.ID,
			Action:      job.Action,
			State:       job.State,
			RequestedAt: job.RequestedAt,
			StartedAt:   job.StartedAt,
		}
		if job.StackID != "" {
			stackID := job.StackID
			item.StackID = &stackID
		}
		if job.Workflow != nil {
			item.Workflow = &ActiveWorkflow{Steps: make([]ActiveWorkflowStep, 0, len(job.Workflow.Steps))}
			for _, step := range job.Workflow.Steps {
				item.Workflow.Steps = append(item.Workflow.Steps, ActiveWorkflowStep{
					Action:        step.Action,
					State:         step.State,
					TargetStackID: step.TargetStackID,
				})
			}
		}

		latestEvent, ok, err := s.store.LatestJobEvent(ctx, job.ID)
		if err != nil {
			return ActiveJobsResponse{}, err
		}
		if ok {
			item.LatestEvent = &ActiveJobEvent{
				Event:     latestEvent.Event,
				Message:   latestEvent.Message,
				Data:      latestEvent.Data,
				Timestamp: latestEvent.Timestamp,
			}
			if latestEvent.Step != nil {
				step := ActiveJobStep{
					Index:         latestEvent.Step.Index,
					Total:         latestEvent.Step.Total,
					Action:        latestEvent.Step.Action,
					TargetStackID: latestEvent.Step.TargetStackID,
				}
				item.CurrentStep = &step
				item.LatestEvent.Step = &step
			}
		}

		switch job.State {
		case "running":
			response.Summary.RunningCount++
		case "queued":
			response.Summary.QueuedCount++
		case "cancel_requested":
			response.Summary.CancelRequestedCount++
		}
		response.Items = append(response.Items, item)
	}

	response.Summary.ActiveCount = len(response.Items)
	return response, nil
}

func randomToken(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func (s *Service) reconcileInterruptedJob(ctx context.Context, job store.Job) (store.Job, error) {
	defer s.unlockAll(job.ID)

	const errorCode = "job_interrupted"
	const errorMessage = "Job did not finish before Stacklab restarted."
	const finishMessage = "Job marked failed after Stacklab restarted."
	const stepMessage = "Step did not finish before Stacklab restarted."

	var stepRef *store.JobEventStep
	if job.Workflow != nil {
		workflow, failedStep := interruptedWorkflow(job.Workflow.Steps)
		job.Workflow = &store.JobWorkflow{Steps: workflow}
		stepRef = failedStep
	}

	now := time.Now().UTC()
	job.State = "failed"
	job.FinishedAt = &now
	job.ErrorCode = errorCode
	job.ErrorMessage = errorMessage
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return store.Job{}, err
	}

	if stepRef != nil {
		if err := s.PublishEvent(ctx, job, "job_step_finished", stepMessage, "", stepRef); err != nil {
			return store.Job{}, err
		}
	}
	if err := s.PublishEvent(ctx, job, "job_error", errorMessage, "", stepRef); err != nil {
		return store.Job{}, err
	}
	if err := s.PublishEvent(ctx, job, "job_finished", finishMessage, "", nil); err != nil {
		return store.Job{}, err
	}
	if s.onTerminal != nil {
		s.onTerminal(job)
	}
	return job, nil
}

func (s *Service) publishLive(event store.JobEvent) {
	s.mu.Lock()
	subs := s.subsByJob[event.JobID]
	channels := make([]chan store.JobEvent, 0, len(subs))
	for channel := range subs {
		channels = append(channels, channel)
	}
	s.mu.Unlock()

	for _, channel := range channels {
		select {
		case channel <- event:
		default:
		}
	}
}

func shouldSkipReconcile(job store.Job) bool {
	if job.Action != "self_update_stacklab" {
		return false
	}
	startedAt := job.RequestedAt
	if job.StartedAt != nil {
		startedAt = *job.StartedAt
	}
	return time.Since(startedAt) < selfUpdateReconcileGracePeriod
}

func interruptedWorkflow(steps []store.JobWorkflowStep) ([]store.JobWorkflowStep, *store.JobEventStep) {
	if len(steps) == 0 {
		return nil, nil
	}

	cloned := append([]store.JobWorkflowStep(nil), steps...)
	index := -1
	for i, step := range cloned {
		if step.State == "running" {
			index = i
			break
		}
	}
	if index == -1 {
		for i, step := range cloned {
			if step.State == "queued" || step.State == "cancel_requested" {
				index = i
				break
			}
		}
	}
	if index == -1 {
		return cloned, nil
	}

	cloned[index].State = "failed"
	return cloned, &store.JobEventStep{
		Index:         index + 1,
		Total:         len(cloned),
		Action:        cloned[index].Action,
		TargetStackID: cloned[index].TargetStackID,
	}
}

func markWorkflowCancelRequested(steps []store.JobWorkflowStep) []store.JobWorkflowStep {
	cloned := append([]store.JobWorkflowStep(nil), steps...)
	for i, step := range cloned {
		if step.State == "running" || step.State == "queued" {
			cloned[i].State = "cancel_requested"
			break
		}
	}
	return cloned
}

func (s *Service) lockMany(jobID string, resources []Resource) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, requested := range resources {
		for conflicting, currentJobID := range s.lockedByResource {
			if currentJobID != "" && currentJobID != jobID && ResourcesConflict(requested, conflicting) {
				return &ResourceConflictError{
					Reason:           ConflictReasonResourceHeld,
					Requested:        requested,
					Conflicting:      conflicting,
					ConflictingJobID: currentJobID,
				}
			}
		}
	}

	for _, resource := range resources {
		s.lockedByResource[resource] = jobID
	}
	s.resourcesByJob[jobID] = append([]Resource(nil), resources...)
	return nil
}

func (s *Service) unlockAll(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resources, ok := s.resourcesByJob[jobID]
	if !ok {
		return
	}
	for _, resource := range resources {
		currentJobID, ok := s.lockedByResource[resource]
		if !ok || currentJobID != jobID {
			continue
		}
		delete(s.lockedByResource, resource)
	}
	delete(s.resourcesByJob, jobID)
}
