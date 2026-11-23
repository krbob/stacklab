package maintenancejobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"stacklab/internal/backup"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

var ErrInvalidUpdateProtection = errors.New("invalid update protection policy")
var ErrUpdateProtectionChanged = errors.New("update protection changed; reload and review the current settings before saving")

const initialProtectionRevision = "new"

type UpdateProtectionPolicy struct {
	Revision              string   `json:"revision,omitempty"`
	AutoRollback          bool     `json:"auto_rollback"`
	BackupPlanID          string   `json:"backup_plan_id,omitempty"`
	BackupPlanFingerprint string   `json:"backup_plan_fingerprint,omitempty"`
	BackupSourceIDs       []string `json:"backup_source_ids,omitempty"`
}

type preUpdateBackups interface {
	ValidateUpdateBackup(context.Context, string, string, []string) (string, error)
	CreateUpdateBackup(context.Context, store.Job, string, string, []string, string) (backup.UpdateBackupResult, error)
}

// ConfigureUpdateProtection connects the backup service at application startup.
func (s *Service) ConfigureUpdateProtection(backups preUpdateBackups) {
	s.backups = backups
}

func (s *Service) requireProtectionDefinition(ctx context.Context, stackID string) error {
	stack, err := s.stackReader.Get(ctx, stackID)
	if err != nil {
		return err
	}
	if stack.Stack.RuntimeState == stacks.RuntimeStateOrphaned {
		return stacks.ErrNotFound
	}
	return nil
}

func (s *Service) GetUpdateProtection(ctx context.Context, stackID string) (UpdateProtectionPolicy, error) {
	if err := s.requireProtectionDefinition(ctx, stackID); err != nil {
		return UpdateProtectionPolicy{}, err
	}
	return s.loadUpdateProtection(ctx, stackID)
}

func (s *Service) loadUpdateProtection(ctx context.Context, stackID string) (UpdateProtectionPolicy, error) {
	var policy UpdateProtectionPolicy
	value, revision, err := s.jobs.Store().AppSettingWithRevision(ctx, store.StackUpdateProtectionKey(stackID))
	if err != nil {
		return policy, err
	}
	if revision == "" {
		policy.Revision = initialProtectionRevision
		return policy, nil
	}
	if err := json.Unmarshal([]byte(value), &policy); err != nil {
		return policy, fmt.Errorf("read update protection for %s: %w", stackID, err)
	}
	policy.Revision = revision
	return policy, nil
}

func (s *Service) SetUpdateProtection(ctx context.Context, stackID string, policy UpdateProtectionPolicy) (UpdateProtectionPolicy, error) {
	return s.SetUpdateProtectionIfCurrent(ctx, stackID, policy, nil)
}

func (s *Service) SetUpdateProtectionIfCurrent(ctx context.Context, stackID string, policy UpdateProtectionPolicy, expectedRevision *string) (UpdateProtectionPolicy, error) {
	// Take the revision before checking the definition. Stack deletion writes
	// a new revision even for an unconfigured stack; it fences in-flight edits.
	key := store.StackUpdateProtectionKey(stackID)
	_, revision, err := s.jobs.Store().AppSettingWithRevision(ctx, key)
	if err != nil {
		return UpdateProtectionPolicy{}, err
	}
	if err := s.requireProtectionDefinition(ctx, stackID); err != nil {
		return UpdateProtectionPolicy{}, err
	}
	if expectedRevision != nil {
		visibleRevision := revision
		if visibleRevision == "" {
			visibleRevision = initialProtectionRevision
		}
		if *expectedRevision != visibleRevision {
			return UpdateProtectionPolicy{}, ErrUpdateProtectionChanged
		}
	}
	policy.Revision = ""
	policy.BackupPlanID = strings.TrimSpace(policy.BackupPlanID)
	if (policy.BackupPlanID == "") != (len(policy.BackupSourceIDs) == 0) {
		return UpdateProtectionPolicy{}, fmt.Errorf("%w: select both a backup plan and its sources", ErrInvalidUpdateProtection)
	}
	if policy.BackupPlanID == "" && policy.BackupPlanFingerprint != "" {
		return UpdateProtectionPolicy{}, fmt.Errorf("%w: a fingerprint requires a backup plan", ErrInvalidUpdateProtection)
	}
	if policy.BackupPlanID != "" {
		if s.backups == nil {
			return UpdateProtectionPolicy{}, errors.New("pre-update backups are unavailable")
		}
		fingerprint, err := s.backups.ValidateUpdateBackup(ctx, stackID, policy.BackupPlanID, policy.BackupSourceIDs)
		if err != nil {
			return UpdateProtectionPolicy{}, fmt.Errorf("%w: %w", ErrInvalidUpdateProtection, err)
		}
		if fingerprint == "" || policy.BackupPlanFingerprint != fingerprint {
			return UpdateProtectionPolicy{}, fmt.Errorf("%w: the backup plan changed or its fingerprint is missing; review the current sources and destinations", ErrInvalidUpdateProtection)
		}
		policy.BackupPlanFingerprint = fingerprint
		policy.BackupSourceIDs = slices.Clone(policy.BackupSourceIDs)
		slices.Sort(policy.BackupSourceIDs)
	}
	data, err := json.Marshal(policy)
	if err != nil {
		return UpdateProtectionPolicy{}, err
	}
	now := time.Now().UTC()
	if now.Format(time.RFC3339Nano) == revision {
		now = now.Add(time.Nanosecond)
	}
	changed, err := s.jobs.Store().CompareAndSetAppSetting(ctx, key, string(data), revision, now)
	if err != nil {
		return UpdateProtectionPolicy{}, err
	}
	if !changed {
		return UpdateProtectionPolicy{}, ErrUpdateProtectionChanged
	}
	policy.Revision = now.Format(time.RFC3339Nano)
	return policy, nil
}

// The stored workflow freezes the selected plan, sources, and image policy
// for this job. A subsequent policy edit applies to the next update.
func (s *Service) protectUpdateWorkflow(ctx context.Context, steps []store.JobWorkflowStep) ([]store.JobWorkflowStep, error) {
	var policy UpdateProtectionPolicy
	protected := make([]store.JobWorkflowStep, 0, len(steps))
	for _, step := range steps {
		protected = append(protected, step)
		switch step.Action {
		case "verify_before":
			var err error
			policy, err = s.loadUpdateProtection(ctx, step.TargetStackID)
			if err != nil {
				return nil, err
			}
			if policy.BackupPlanID != "" {
				capture := step
				capture.Action = "backup_before"
				capture.BackupPlanID = policy.BackupPlanID
				capture.BackupPlanFingerprint = policy.BackupPlanFingerprint
				capture.BackupSourceIDs = policy.BackupSourceIDs
				protected = append(protected, capture)
			}
			if policy.AutoRollback {
				prepare := step
				prepare.Action = "prepare_rollback"
				protected = append(protected, prepare)
			}
		case "verify_after":
			if policy.AutoRollback {
				rollback := step
				rollback.Action = "rollback_images"
				protected = append(protected, rollback)
			}
		}
	}
	return protected, nil
}

type stackUpdateState struct {
	health      stacks.MaintenanceHealthBaseline
	hasBaseline bool
	after       stacks.MaintenanceHealthReport
	recovery    stacks.MaintenanceImageRecovery
	images      stacks.MaintenanceRollback
	prepared    bool
	deployBegan bool
	pullPending bool
	failedStep  string
	backup      backup.UpdateBackupResult
}

func (s *Service) releaseRollback(ctx context.Context, job store.Job, step *store.JobEventStep, state *stackUpdateState) {
	if !state.prepared {
		return
	}
	if err := s.stackReader.ReleaseMaintenanceRollback(ctx, state.images); err != nil {
		_ = s.jobs.PublishEvent(ctx, job, "job_warning", "Could not clean up saved rollback images.", err.Error(), step)
	}
	state.prepared = false
}

func (s *Service) updateDashboardImageWarnings(ctx context.Context, job store.Job, step store.JobWorkflowStep, state *stackUpdateState, stepErr error) error {
	if !state.hasBaseline || ctx.Err() != nil || errors.Is(stepErr, context.Canceled) {
		return nil
	}
	if step.Action == "rollback_images" {
		return s.stackReader.RecordMaintenanceImageFailure(ctx, job, state.health, state.recovery, state.pullPending)
	}
	// When the workflow will attempt recovery, take a single observation
	// afterwards. Diagnostic I/O must not delay the actual rollback.
	if stepErr != nil && state.prepared && (step.Action == "up" || step.Action == "verify_after") {
		return nil
	}
	if stepErr != nil {
		return s.stackReader.RecordMaintenanceImageFailure(ctx, job, state.health, stacks.MaintenanceImageRecovery{}, state.pullPending)
	}
	if step.Action == "verify_after" {
		return s.stackReader.ClearMaintenanceImageWarnings(ctx, step.TargetStackID, state.after)
	}
	return nil
}
