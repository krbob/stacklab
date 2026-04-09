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
var ErrStackLocked = errors.New("stack locked")

type Service struct {
	store      *store.Store
	mu         sync.Mutex
	lockedByID map[string]string
	locksByJob map[string][]string
	subsByJob  map[string]map[chan store.JobEvent]struct{}
}

func NewService(jobStore *store.Store) *Service {
	return &Service{
		store:      jobStore,
		lockedByID: map[string]string{},
		locksByJob: map[string][]string{},
		subsByJob:  map[string]map[chan store.JobEvent]struct{}{},
	}
}

func (s *Service) Start(ctx context.Context, stackID, action, requestedBy string) (store.Job, error) {
	return s.StartWithLocks(ctx, stackID, action, requestedBy, nil)
}

func (s *Service) StartWithLocks(ctx context.Context, stackID, action, requestedBy string, lockStackIDs []string) (store.Job, error) {
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

	locks := normalizeLockTargets(stackID, lockStackIDs)
	if len(locks) > 0 {
		if err := s.lockMany(job.ID, locks); err != nil {
			return store.Job{}, err
		}
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
		return store.Job{}, err
	}
	return job, nil
}

func (s *Service) FinishFailed(ctx context.Context, job store.Job, errorCode, errorMessage string) (store.Job, error) {
	defer s.unlockAll(job.ID)

	now := time.Now().UTC()
	job.State = "failed"
	job.FinishedAt = &now
	job.ErrorCode = errorCode
	job.ErrorMessage = errorMessage
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return store.Job{}, err
	}
	if err := s.PublishEvent(ctx, job, "job_error", errorMessage, "", nil); err != nil {
		return store.Job{}, err
	}
	if err := s.PublishEvent(ctx, job, "job_finished", "Job finished with errors.", "", nil); err != nil {
		return store.Job{}, err
	}
	return job, nil
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
		Timestamp: time.Now().UTC(),
	}
	if err := s.store.CreateJobEvent(ctx, event); err != nil {
		return err
	}

	s.publishLive(event)
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
			Timestamp: event.Timestamp,
		})
	}

	if !response.Retained {
		response.Message = "Detailed output for this job is no longer retained."
	}

	return response, nil
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

func normalizeLockTargets(primaryStackID string, additional []string) []string {
	unique := map[string]struct{}{}
	if primaryStackID != "" {
		unique[primaryStackID] = struct{}{}
	}
	for _, stackID := range additional {
		if stackID == "" {
			continue
		}
		unique[stackID] = struct{}{}
	}
	if len(unique) == 0 {
		return nil
	}

	locks := make([]string, 0, len(unique))
	for stackID := range unique {
		locks = append(locks, stackID)
	}
	return locks
}

func (s *Service) lockMany(jobID string, stackIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	acquired := make([]string, 0, len(stackIDs))
	for _, stackID := range stackIDs {
		if currentJobID, ok := s.lockedByID[stackID]; ok && currentJobID != "" && currentJobID != jobID {
			for _, acquiredID := range acquired {
				delete(s.lockedByID, acquiredID)
			}
			return ErrStackLocked
		}
		s.lockedByID[stackID] = jobID
		acquired = append(acquired, stackID)
	}

	s.locksByJob[jobID] = acquired
	return nil
}

func (s *Service) unlockAll(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stackIDs, ok := s.locksByJob[jobID]
	if !ok {
		return
	}
	for _, stackID := range stackIDs {
		currentJobID, ok := s.lockedByID[stackID]
		if !ok || currentJobID != jobID {
			continue
		}
		delete(s.lockedByID, stackID)
	}
	delete(s.locksByJob, jobID)
}
