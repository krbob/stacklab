package maintenancejobs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"stacklab/internal/backup"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

type fakeUpdateBackups struct {
	fingerprint string
	result      backup.UpdateBackupResult
	captureErr  error
	called      bool
	parentID    string
	sources     []string
}

func (b *fakeUpdateBackups) ValidateUpdateBackup(context.Context, string, string, []string) (string, error) {
	return b.fingerprint, nil
}
func (b *fakeUpdateBackups) CreateUpdateBackup(_ context.Context, job store.Job, _ string, _ string, sources []string, fingerprint string) (backup.UpdateBackupResult, error) {
	b.called = true
	b.parentID = job.ID
	b.sources = slices.Clone(sources)
	if fingerprint != b.fingerprint {
		return backup.UpdateBackupResult{}, errors.New("backup plan changed after admission")
	}
	return b.result, b.captureErr
}

func TestUpdateProtectionPolicyPersistsAndFreezesAtAdmission(t *testing.T) {
	ctx := t.Context()
	backups := &fakeUpdateBackups{fingerprint: strings.Repeat("c", 64), result: backup.UpdateBackupResult{RunID: "run1", DestinationID: "repo1", SnapshotID: "snapshot1"}}
	reader := fakeMaintenanceReader()
	s := newMaintenanceTestService(t, reader)
	s.ConfigureUpdateProtection(backups)
	initial, err := s.GetUpdateProtection(ctx, "demo")
	if err != nil || initial.AutoRollback || initial.BackupPlanID != "" {
		t.Fatalf("initial policy %+v, %v", initial, err)
	}
	policy := UpdateProtectionPolicy{AutoRollback: true, BackupPlanID: "plan1", BackupSourceIDs: []string{"B", "A"}}
	if _, err = s.SetUpdateProtection(ctx, "demo", policy); !errors.Is(err, ErrInvalidUpdateProtection) {
		t.Fatalf("missing review fingerprint: %v", err)
	}
	policy.BackupPlanFingerprint = backups.fingerprint
	if _, err = s.SetUpdateProtection(ctx, "demo", policy); err != nil {
		t.Fatal(err)
	}
	reloaded := NewService(nil, s.jobs, s.audit, reader, s.maintenance)
	saved, err := reloaded.GetUpdateProtection(ctx, "demo")
	if err != nil || !saved.AutoRollback || saved.BackupPlanFingerprint != policy.BackupPlanFingerprint || !slices.Equal(saved.BackupSourceIDs, []string{"A", "B"}) {
		t.Fatalf("stored policy: %+v %v", saved, err)
	}
	job, run, err := s.StartUpdate(ctx, UpdateRequest{Target: UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}}, Options: UpdateOptions{PullImages: true}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetUpdateProtection(ctx, "demo", UpdateProtectionPolicy{}); err != nil {
		t.Fatal(err)
	}
	finished, err := s.ExecuteUpdate(ctx, job, run)
	if err != nil || finished.State != "succeeded" {
		t.Fatalf("job result %+v, %v", finished, err)
	}
	if !backups.called || backups.parentID != job.ID || !slices.Equal(backups.sources, []string{"A", "B"}) {
		t.Fatalf("capture %+v", backups)
	}
	persisted, err := s.jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	steps := persisted.Workflow.Steps
	var actions []string
	for _, step := range steps {
		actions = append(actions, step.Action)
	}
	if !slices.Equal(actions, []string{"verify_before", "backup_before", "prepare_rollback", "pull", "up", "verify_after", "rollback_images"}) {
		t.Fatalf("workflow %v", actions)
	}
	capture := steps[1]
	if capture.State != "succeeded" || capture.BackupRunID != "run1" || capture.BackupDestinationID != "repo1" || capture.BackupSnapshotID != "snapshot1" || capture.BackupPlanID != "plan1" || capture.BackupPlanFingerprint != policy.BackupPlanFingerprint {
		t.Fatalf("backup references %+v", capture)
	}
	if steps[len(steps)-1].State != "skipped" || reader.released != 1 || len(reader.rollbackCalls) != 0 {
		t.Fatalf("unexpected image recovery, steps %v/releases %d", steps, reader.released)
	}
}

func TestProtectionPoliciesAreIndependentAndApplyOnlyToDeployedStacks(t *testing.T) {
	reader := fakeMaintenanceReader()
	stopped := reader.details["demo"]
	stopped.Stack.ID = "stopped"
	stopped.Stack.RuntimeState = stacks.RuntimeStateStopped
	reader.details["stopped"] = stopped
	s := newMaintenanceTestService(t, reader)
	ctx := t.Context()
	// Both saves must survive even if the settings writes overlap.
	done := make(chan error, 2)
	for _, id := range []string{"demo", "stopped"} {
		go func() {
			_, err := s.SetUpdateProtection(ctx, id, UpdateProtectionPolicy{AutoRollback: true})
			done <- err
		}()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"demo", "stopped"} {
		policy, err := s.GetUpdateProtection(ctx, id)
		if err != nil || !policy.AutoRollback {
			t.Fatalf("policy for %s: %+v, %v", id, policy, err)
		}
	}
	job, err := s.RunUpdate(ctx, UpdateRequest{Target: UpdateTarget{
		Mode: "all", ExcludedServices: map[string][]string{"demo": {"app", "db"}},
	}}, "test")
	if err != nil || job.State != "succeeded" {
		t.Fatalf("job %+v, %v", job, err)
	}
	steps := job.Workflow.Steps
	if len(steps) != 2 || steps[0].Action != "skip" || steps[1].Action != "preserve_inactive" || reader.released != 0 {
		t.Fatalf("unexpected work for excluded and inactive stacks: %+v", steps)
	}
}

func TestUnprotectedTargetRecordsWhyNoImagesWereSaved(t *testing.T) {
	reader := fakeMaintenanceReader()
	reader.prepareWarning = "No verified image for app."
	s := newMaintenanceTestService(t, reader)
	ctx := t.Context()
	if _, err := s.SetUpdateProtection(ctx, "demo", UpdateProtectionPolicy{AutoRollback: true}); err != nil {
		t.Fatal(err)
	}
	job, err := s.RunUpdate(ctx, UpdateRequest{Target: UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}}}, "test")
	if err != nil || job.State != "succeeded" {
		t.Fatalf("job %+v %v", job, err)
	}
	step := job.Workflow.Steps[1]
	if step.Action != "prepare_rollback" || step.State != "skipped" {
		t.Fatalf("no eligible images reported as prepared: %+v", step)
	}
	events, err := s.jobs.ReplayEvents(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Event == "job_warning" && event.Step != nil && event.Step.Action == step.Action && event.Data == reader.prepareWarning {
			return
		}
	}
	t.Fatal("missing explanation for ineligible services")
}

func TestRequiredUpdateBackupFailureBlocksOnlyAffectedStack(t *testing.T) {
	for _, mode := range []string{"no snapshot", "plan changed", "backup failed"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			reader := fakeMaintenanceReader()
			zeta := reader.details["demo"]
			zeta.Stack.ID = "zeta"
			reader.details["zeta"] = zeta
			pruner := &fakeMaintenancePruneRunner{}
			s := newMaintenanceTestServiceWithPruner(t, reader, pruner)
			backups := &fakeUpdateBackups{fingerprint: strings.Repeat("f", 64), result: backup.UpdateBackupResult{RunID: "backup1"}}
			s.ConfigureUpdateProtection(backups)
			_, err := s.SetUpdateProtection(ctx, "demo", UpdateProtectionPolicy{BackupPlanID: "p", BackupSourceIDs: []string{"src"}, BackupPlanFingerprint: backups.fingerprint})
			if err != nil {
				t.Fatal(err)
			}
			job, run, err := s.StartUpdate(ctx, UpdateRequest{Target: UpdateTarget{Mode: "all"}, Options: UpdateOptions{PullImages: true, PruneAfter: true}}, "test")
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "plan changed":
				backups.fingerprint = strings.Repeat("e", 64)
			case "backup failed":
				backups.captureErr = errors.New("missing destination")
			}
			job, err = s.ExecuteUpdate(ctx, job, run)
			if err != nil || job.State != "failed" || !strings.Contains(job.ErrorMessage, "demo/backup_before") {
				t.Fatalf("job %+v %v", job, err)
			}
			if !slices.Equal(reader.imageWarningJobs, []string{job.ID}) || !slices.Equal(reader.imageWarningChecks, []string{"zeta"}) {
				t.Fatalf("failed job warning checks %v, successful remediation checks %v", reader.imageWarningJobs, reader.imageWarningChecks)
			}
			var actions []string
			for _, c := range reader.stepCalls {
				actions = append(actions, c.StackID+"/"+c.Action)
			}
			if !slices.Equal(actions, []string{"demo/verify_before", "zeta/verify_before", "zeta/pull", "zeta/up", "zeta/verify_after"}) || len(pruner.systemPruneCalls) != 0 {
				t.Fatalf("actions %v prune %v", actions, pruner.systemPruneCalls)
			}
		})
	}
}

func TestUpdateImageRecoveryLeavesFailedUpdateAndSkipsPrune(t *testing.T) {
	for _, tt := range []struct {
		failure      string
		rollbackErr  error
		wantStatus   string
		wantCalls    []bool
		wantReleased int
	}{
		{"up", nil, "succeeded", []bool{true}, 1},
		{"verify_after", nil, "succeeded", []bool{false}, 1},
		{"verify_after", stacks.ErrMaintenanceRollbackNotApplicable, "skipped", []bool{false}, 1},
		{"verify_after", stacks.ErrMaintenanceRollbackUncertain, "skipped", []bool{false}, 0},
		{"verify_after", errors.New("prior version failed verification"), "failed", []bool{false}, 0},
		{"pull", nil, "skipped", nil, 1},
	} {
		t.Run(tt.failure+"/"+tt.wantStatus, func(t *testing.T) {
			reader := fakeMaintenanceReader()
			reader.stepResults["demo/"+tt.failure] = []maintenanceStepResult{{Err: errors.New("injected failure")}}
			reader.rollbackErr = tt.rollbackErr
			pruner := &fakeMaintenancePruneRunner{}
			s := newMaintenanceTestServiceWithPruner(t, reader, pruner)
			ctx := t.Context()
			if _, err := s.SetUpdateProtection(ctx, "demo", UpdateProtectionPolicy{AutoRollback: true}); err != nil {
				t.Fatal(err)
			}
			job, err := s.RunUpdate(ctx, UpdateRequest{Target: UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}}, Options: UpdateOptions{PullImages: true, PruneAfter: true}}, "test")
			if err != nil || job.State != "failed" || !strings.Contains(job.ErrorMessage, "demo/"+tt.failure) {
				t.Fatalf("job %+v, err %v", job, err)
			}
			steps := job.Workflow.Steps
			recovery := steps[len(steps)-2]
			if !slices.Equal(reader.registryAtWarning, []bool{tt.failure == "pull"}) {
				t.Fatalf("registry evidence consulted after a completed pull: %v", reader.registryAtWarning)
			}
			if !slices.Equal(reader.recoveriesAtWarning, []int{len(tt.wantCalls)}) {
				t.Fatalf("expected one dashboard observation after the recovery decision, got %v", reader.recoveriesAtWarning)
			}
			if recovery.Action != "rollback_images" || recovery.State != tt.wantStatus || !slices.Equal(reader.rollbackCalls, tt.wantCalls) || reader.released != tt.wantReleased || len(pruner.systemPruneCalls) != 0 {
				t.Fatalf("result %+v, calls %+v releases %d prune %+v", recovery, reader.rollbackCalls, reader.released, pruner.systemPruneCalls)
			}
		})
	}
}
