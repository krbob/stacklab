package backup

import (
	"context"
	"fmt"
	"time"

	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

type RecoveryResolution struct {
	Action    string `json:"action"`
	Inspected bool   `json:"inspected,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func (s *Service) StartRecoveryResolution(ctx context.Context, runID string, input RecoveryResolution) (store.Job, error) {
	if !safeID.MatchString(runID) || input.Action != "retry" && input.Action != "acknowledge_hold" && input.Action != "acknowledge_missing" {
		return store.Job{}, fmt.Errorf("%w: invalid recovery resolution", ErrInvalid)
	}
	checker, ok := s.capture.(interface {
		CheckRecoveryResolution(context.Context, string, RecoveryResolution) error
	})
	if !ok {
		return store.Job{}, fmt.Errorf("%w: service recovery is unavailable", ErrInvalid)
	}
	if err := checker.CheckRecoveryResolution(ctx, runID, input); err != nil {
		return store.Job{}, err
	}
	return s.jobs.StartWithResources(ctx, "", "backup_service_recovery", "local", jobs.GlobalResource())
}
func (s *Service) ExecuteRecoveryResolution(ctx context.Context, job store.Job, runID string, input RecoveryResolution) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	unregister := s.jobs.RegisterCancel(job.ID, cancel)
	defer unregister()
	resolver, ok := s.capture.(interface {
		ResolveRecoveryWithInput(context.Context, string, RecoveryResolution) error
	})
	var err error
	if !ok {
		err = fmt.Errorf("%w: service recovery is unavailable", ErrInvalid)
	} else {
		err = resolver.ResolveRecoveryWithInput(ctx, runID, input)
	}
	if err == nil {
		err = s.capture.Recover(ctx)
		s.mu.Lock()
		s.recoveryError = ""
		if err != nil {
			s.recoveryError = err.Error()
		}
		s.mu.Unlock()
	}
	s.finish(ctx, job, err)
}
