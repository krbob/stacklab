package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RecoveryHelperProtocol changes when sealed recovery inputs or required
// privileged actions change incompatibly. Capture remains independently usable.
const RecoveryHelperProtocol = 2

type recoveryHelperChecker interface {
	CheckRecoveryHelper(context.Context) error
}

func (c *NativeCapture) CheckRecoveryHelper(ctx context.Context) error {
	if c.cfg.BackupHelperPath == "" {
		return fmt.Errorf("configure the backup permission helper before in-place recovery")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for _, action := range []string{"prepare-restore", "check-restore", "check-restores", "apply-restore", "release-restore", "check-target"} {
		name, args := c.helperInvocation(action, "--probe")
		output, err := c.command(ctx, name, args...)
		if err != nil {
			// Do not expose arbitrary privileged command output in the overview.
			return fmt.Errorf("in-place recovery helper check failed for %s; install the matching helper and update /etc/sudoers.d/stacklab-backup from the packaged example, validate it with visudo, and check the root-owned helper spool", action)
		}
		var result struct {
			Protocol int    `json:"protocol"`
			Action   string `json:"action"`
		}
		if json.Unmarshal(output, &result) != nil || result.Protocol != RecoveryHelperProtocol || result.Action != action {
			return fmt.Errorf("in-place recovery helper protocol is incompatible; install the helper shipped with this Stacklab version and update its sudoers example")
		}
	}
	return nil
}

func (s *Service) CheckHelperReadiness(ctx context.Context) {
	checker, ok := s.capture.(recoveryHelperChecker)
	message := "In-place recovery requires the configured native backup permission helper."
	ready := false
	if ok {
		if err := checker.CheckRecoveryHelper(ctx); err != nil {
			message = err.Error()
		} else {
			ready, message = true, ""
		}
	}
	s.recordHelperReadiness(ready, message)
}

func (s *Service) recordHelperReadiness(ready bool, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.helperReady, s.helperError, s.helperCheckedAt = ready, message, s.now()
}

func (s *Service) RefreshHelperReadiness(ctx context.Context) {
	s.mu.Lock()
	interval := time.Minute
	if s.helperReady {
		interval = 15 * time.Minute
	}
	age := s.now().Sub(s.helperCheckedAt)
	if s.helperChecking || !s.helperCheckedAt.IsZero() && age >= 0 && age < interval {
		s.mu.Unlock()
		return
	}
	s.helperChecking = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.helperChecking = false
		s.mu.Unlock()
	}()
	s.CheckHelperReadiness(ctx)
}
