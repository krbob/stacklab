package backup

import (
	"context"
	"fmt"
	"time"

	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

type RecoveryResolution struct {
	Action string `json:"action"`
}

func (s *Service) StartRecoveryResolution(ctx context.Context, runID string, input RecoveryResolution) (store.Job, error) {
	if !safeID.MatchString(runID) || input.Action != "retry" && input.Action != "acknowledge_hold" {
		return store.Job{}, fmt.Errorf("%w: invalid recovery resolution", ErrInvalid)
	}
	return s.jobs.StartWithResources(ctx, "", "backup_service_recovery", "local", jobs.GlobalResource())
}
func (s *Service) ExecuteRecoveryResolution(ctx context.Context, job store.Job, runID string, input RecoveryResolution) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	unregister := s.jobs.RegisterCancel(job.ID, cancel)
	defer unregister()
	resolver, ok := s.capture.(interface {
		ResolveRecovery(context.Context, string, string) error
	})
	var err error
	if !ok {
		err = fmt.Errorf("%w: service recovery is unavailable", ErrInvalid)
	} else {
		err = resolver.ResolveRecovery(ctx, runID, input.Action)
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
