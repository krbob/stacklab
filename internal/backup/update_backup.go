package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

// UpdateBackupResult points to an immutable, verified copy in the ordinary
// Backups recovery library. The run retains the results of every destination.
type UpdateBackupResult struct {
	RunID         string
	DestinationID string
	SnapshotID    string
}

type updateBackupRequest struct {
	parentID, stackID, fingerprint string
	sourceIDs                      []string
}

type updateBackupStackKey struct{}

// ValidateUpdateBackup pins the configured selection. Live source resolution
// and writer discovery take place during capture under the global reservation.
func (s *Service) ValidateUpdateBackup(ctx context.Context, stackID, planID string, sourceIDs []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return "", s.initErr
	}
	plan, ok := s.state.Plans[planID]
	if !ok {
		return "", ErrNotFound
	}
	return s.validateUpdateBackupLocked(stackID, plan, sourceIDs)
}

func (s *Service) validateUpdateBackupLocked(stackID string, plan Plan, sourceIDs []string) (string, error) {
	// Reusing just part of a plan would tag a partial snapshot as a complete
	// copy in the plan's retention and protection history.
	ids := make(map[string]bool, len(sourceIDs))
	for _, id := range sourceIDs {
		ids[id] = true
	}
	if len(ids) != len(sourceIDs) || len(ids) != len(plan.Sources) {
		return "", fmt.Errorf("%w: select exactly the sources in the backup plan", ErrInvalid)
	}
	for _, source := range plan.Sources {
		if !ids[source.ID] {
			return "", fmt.Errorf("%w: selected sources differ from the backup plan", ErrInvalid)
		}
	}
	return s.updateBackupFingerprintLocked(stackID, plan)
}

func (s *Service) updateBackupFingerprintLocked(stackID string, plan Plan) (string, error) {
	if !stacks.IsValidStackID(stackID) || !plan.Enabled || len(plan.Sources) == 0 {
		return "", fmt.Errorf("%w: select an enabled backup plan dedicated to this stack", ErrInvalid)
	}
	for _, src := range plan.Sources {
		if src.StackID != stackID {
			return "", fmt.Errorf("%w: every backup source must belong to the selected stack", ErrInvalid)
		}
		switch src.Kind {
		case "stack_definition":
			if src.Path != "" && filepath.Clean(src.Path) != filepath.Join(s.cfg.RootDir, "stacks", stackID) {
				return "", fmt.Errorf("%w: definition points outside the selected stack", ErrInvalid)
			}
		case "service_mount":
			if src.Path == "" || src.Service == "" || src.Mount == "" {
				return "", fmt.Errorf("%w: pre-update backup requires a pinned service mount", ErrInvalid)
			}
		case "managed_config":
			if src.Path != "" && !pathContains(filepath.Join(s.cfg.RootDir, "config", stackID), filepath.Clean(src.Path)) {
				return "", fmt.Errorf("%w: configuration points outside the selected stack", ErrInvalid)
			}
		default:
			return "", fmt.Errorf("%w: pre-update backup requires stack-scoped sources", ErrInvalid)
		}
	}
	type target struct{ ID, RepositoryKey, Filesystem, Region string }
	var targets []target
	for _, a := range plan.Destinations {
		if !a.Enabled {
			continue
		}
		d, ok := s.state.Destinations[a.DestinationID]
		if !ok || !d.CredentialsConfigured {
			return "", fmt.Errorf("%w: backup destination is not configured", ErrInvalid)
		}
		targets = append(targets, target{d.ID, repositoryKey(d), d.ExpectedFilesystem, d.Region})
	}
	if len(targets) == 0 {
		return "", fmt.Errorf("%w: enable a backup destination", ErrInvalid)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	data, err := json.Marshal(struct {
		PlanID, Selection string
		Targets           []target
	}{plan.ID, selectionFingerprint(plan), targets})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// CreateUpdateBackup runs synchronously inside a maintenance job before target
// mutation. Native capture, writer recovery, repository coordination, copy
// verification and retention keep the same lifecycle as a manual backup.
func (s *Service) CreateUpdateBackup(ctx context.Context, parent store.Job, stackID, planID string, sourceIDs []string, expectedFingerprint string) (UpdateBackupResult, error) {
	request := &updateBackupRequest{parentID: parent.ID, stackID: stackID, sourceIDs: sourceIDs, fingerprint: expectedFingerprint}
	job, run, err := s.startRun(ctx, planID, RunRequest{}, parent.RequestedBy, request)
	if err != nil {
		return UpdateBackupResult{}, err
	}
	result := UpdateBackupResult{RunID: run.ID}
	ctx = context.WithValue(ctx, updateBackupStackKey{}, stackID)
	s.ExecuteRun(ctx, job, run.ID)
	finished, err := s.jobs.Get(ctx, job.ID)
	if err != nil {
		return result, err
	}
	if finished.State != "succeeded" {
		return result, fmt.Errorf("pre-update backup %s %s: %s", run.ID, finished.State, finished.ErrorMessage)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.state.Runs[run.ID]
	if stored.State != "succeeded" || stored.CaptureState != "succeeded" || len(stored.Copies) == 0 {
		return result, fmt.Errorf("pre-update backup %s lacks a completed capture", run.ID)
	}
	for _, c := range stored.Copies {
		if !verifiedCopy(c) || len(c.SnapshotID) != 64 {
			return result, fmt.Errorf("pre-update backup %s lacks a verified snapshot", run.ID)
		}
	}
	result.DestinationID = stored.Copies[0].DestinationID
	result.SnapshotID = stored.Copies[0].SnapshotID
	return result, nil
}

func validateUpdateCapture(plan Plan, manifest Manifest) error {
	captured := Plan{Consistency: manifest.Consistency}
	for _, source := range manifest.Sources {
		if !source.Archive || len(source.RecoveryRestrictions) > 0 {
			return fmt.Errorf("pre-update source %s does not support online recovery", source.Source.ID)
		}
		captured.Sources = append(captured.Sources, source.Source)
		captured.Sources = append(captured.Sources, source.Aliases...)
	}
	if selectionFingerprint(captured) != selectionFingerprint(plan) {
		return fmt.Errorf("pre-update capture differs from the approved sources or consistency")
	}
	return nil
}
