package jobs

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"stacklab/internal/store"
)

var ErrNotFound = errors.New("job not found")

type Service struct {
	store *store.Store
}

func NewService(jobStore *store.Store) *Service {
	return &Service{store: jobStore}
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
	if err := s.store.CreateJob(ctx, job); err != nil {
		return store.Job{}, err
	}
	return job, nil
}

func (s *Service) FinishSucceeded(ctx context.Context, job store.Job) (store.Job, error) {
	now := time.Now().UTC()
	job.State = "succeeded"
	job.FinishedAt = &now
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return store.Job{}, err
	}
	return job, nil
}

func (s *Service) FinishFailed(ctx context.Context, job store.Job, errorCode, errorMessage string) (store.Job, error) {
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
