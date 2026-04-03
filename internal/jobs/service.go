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
}

func NewService(jobStore *store.Store) *Service {
	return &Service{
		store:      jobStore,
		lockedByID: map[string]string{},
	}
}

func (s *Service) Start(ctx context.Context, stackID, action, requestedBy string) (store.Job, error) {
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
	if stackID != "" {
		if err := s.lockStack(stackID, job.ID); err != nil {
			return store.Job{}, err
		}
		defer func() {
			if job.State == "" {
				s.unlockStack(stackID, job.ID)
			}
		}()
	}
	if err := s.store.CreateJob(ctx, job); err != nil {
		return store.Job{}, err
	}
	return job, nil
}

func (s *Service) FinishSucceeded(ctx context.Context, job store.Job) (store.Job, error) {
	defer s.unlockStack(job.StackID, job.ID)

	now := time.Now().UTC()
	job.State = "succeeded"
	job.FinishedAt = &now
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return store.Job{}, err
	}
	return job, nil
}

func (s *Service) FinishFailed(ctx context.Context, job store.Job, errorCode, errorMessage string) (store.Job, error) {
	defer s.unlockStack(job.StackID, job.ID)

	now := time.Now().UTC()
	job.State = "failed"
	job.FinishedAt = &now
	job.ErrorCode = errorCode
	job.ErrorMessage = errorMessage
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return store.Job{}, err
	}
	return job, nil
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

func randomToken(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func (s *Service) lockStack(stackID, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if currentJobID, ok := s.lockedByID[stackID]; ok && currentJobID != "" {
		return ErrStackLocked
	}

	s.lockedByID[stackID] = jobID
	return nil
}

func (s *Service) unlockStack(stackID, jobID string) {
	if stackID == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	currentJobID, ok := s.lockedByID[stackID]
	if !ok || currentJobID != jobID {
		return
	}

	delete(s.lockedByID, stackID)
}
