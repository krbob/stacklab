package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestJobMarshalJSONExposesOnlyPublicReadModel(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	finishedAt := now.Add(time.Minute)
	tests := []struct {
		name        string
		job         Job
		wantStackID any
		wantFlow    bool
	}{
		{
			name: "unscoped without workflow",
			job: Job{
				ID: "job-system", Action: "prune", State: "succeeded", RequestedBy: "operator",
				RequestedAt: now, StartedAt: &now, FinishedAt: &finishedAt,
				ErrorCode: "private_code", ErrorMessage: "private message",
			},
			wantStackID: nil,
		},
		{
			name: "stack with workflow",
			job: Job{
				ID: "job-stack", StackID: "alpha", Action: "up", State: "running", RequestID: "req-123",
				RequestedBy: "operator", RequestedAt: now, StartedAt: &now,
				Workflow: &JobWorkflow{Steps: []JobWorkflowStep{{Action: "pull", State: "running", TargetStackID: "alpha"}}},
			},
			wantStackID: "alpha",
			wantFlow:    true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.job)
			if err != nil {
				t.Fatalf("json.Marshal(Job) error = %v", err)
			}
			var document map[string]any
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatalf("json.Unmarshal(Job) error = %v", err)
			}
			if got := document["stack_id"]; got != test.wantStackID {
				t.Fatalf("stack_id = %#v, want %#v", got, test.wantStackID)
			}
			for _, privateField := range []string{"requested_by", "error_code", "error_message"} {
				if _, exists := document[privateField]; exists {
					t.Errorf("private field %q was serialized: %s", privateField, encoded)
				}
			}
			_, hasWorkflow := document["workflow"]
			if hasWorkflow != test.wantFlow {
				t.Fatalf("workflow presence = %t, want %t: %s", hasWorkflow, test.wantFlow, encoded)
			}
		})
	}
}

func TestSettingsAndPasswordHashRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

	if hash, configured, err := testStore.PasswordHash(ctx); err != nil || configured || hash != "" {
		t.Fatalf("PasswordHash(empty) = %q, %t, %v", hash, configured, err)
	}
	if err := testStore.SetPasswordHash(ctx, "argon2-hash", now); err != nil {
		t.Fatalf("SetPasswordHash() error = %v", err)
	}
	if hash, configured, err := testStore.PasswordHash(ctx); err != nil || !configured || hash != "argon2-hash" {
		t.Fatalf("PasswordHash(configured) = %q, %t, %v", hash, configured, err)
	}

	if value, found, err := testStore.AppSetting(ctx, "notifications"); err != nil || found || value != "" {
		t.Fatalf("AppSetting(missing) = %q, %t, %v", value, found, err)
	}
	if err := testStore.SetAppSetting(ctx, "notifications", `{"enabled":false}`, now); err != nil {
		t.Fatalf("SetAppSetting(insert) error = %v", err)
	}
	if err := testStore.SetAppSetting(ctx, "notifications", `{"enabled":true}`, now.Add(time.Minute)); err != nil {
		t.Fatalf("SetAppSetting(update) error = %v", err)
	}
	if value, found, err := testStore.AppSetting(ctx, "notifications"); err != nil || !found || value != `{"enabled":true}` {
		t.Fatalf("AppSetting(configured) = %q, %t, %v", value, found, err)
	}
}

func TestJobUpdatesEventsAndActiveReadModel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	job := Job{
		ID: "job-lifecycle", StackID: "alpha", Action: "update", State: "running",
		RequestedBy: "operator", RequestID: "req-job", RequestedAt: now, StartedAt: &now,
		Workflow: &JobWorkflow{Steps: []JobWorkflowStep{{Action: "pull", State: "running", TargetStackID: "alpha"}}},
	}
	if err := testStore.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	if next, err := testStore.NextJobEventSequence(ctx, job.ID); err != nil || next != 1 {
		t.Fatalf("NextJobEventSequence(empty) = %d, %v; want 1", next, err)
	}
	step := &JobEventStep{Index: 1, Total: 1, Action: "pull", TargetStackID: "alpha", TargetServiceNames: []string{"web"}}
	progress := &JobProgress{Phase: "pulling", Completed: 1, Total: 1, Unit: "image", Detail: "web"}
	event := JobEvent{
		JobID: job.ID, Sequence: 1, Event: "job_progress", State: "running", Message: "Pulled web.", Data: "sha256:abc",
		Step: step, Progress: progress, Timestamp: now.Add(time.Second),
	}
	if err := testStore.CreateJobEvent(ctx, event); err != nil {
		t.Fatalf("CreateJobEvent() error = %v", err)
	}
	if next, err := testStore.NextJobEventSequence(ctx, job.ID); err != nil || next != 2 {
		t.Fatalf("NextJobEventSequence(after event) = %d, %v; want 2", next, err)
	}
	latest, found, err := testStore.LatestJobEvent(ctx, job.ID)
	if err != nil || !found {
		t.Fatalf("LatestJobEvent() = %#v, %t, %v", latest, found, err)
	}
	if latest.Step == nil || latest.Step.TargetServiceNames[0] != "web" || latest.Progress == nil || latest.Progress.Detail != "web" || latest.Data != "sha256:abc" {
		t.Fatalf("LatestJobEvent() = %#v", latest)
	}
	if _, found, err := testStore.LatestJobEvent(ctx, "missing"); err != nil || found {
		t.Fatalf("LatestJobEvent(missing) found = %t, error = %v", found, err)
	}

	if updated, err := testStore.UpdateJobIfStateIn(ctx, job, nil); err != nil || updated {
		t.Fatalf("UpdateJobIfStateIn(empty) = %t, %v", updated, err)
	}
	job.State = "cancel_requested"
	job.Workflow = &JobWorkflow{Steps: []JobWorkflowStep{{Action: "pull", State: "cancel_requested", TargetStackID: "alpha"}}}
	if updated, err := testStore.UpdateJobIfStateIn(ctx, job, []string{"running"}); err != nil || !updated {
		t.Fatalf("UpdateJobIfStateIn(match) = %t, %v", updated, err)
	}
	job.State = "queued"
	if updated, err := testStore.UpdateJobIfStateIn(ctx, job, []string{"running"}); err != nil || updated {
		t.Fatalf("UpdateJobIfStateIn(mismatch) = %t, %v", updated, err)
	}

	finishedAt := now.Add(2 * time.Minute)
	job.State = "failed"
	job.FinishedAt = &finishedAt
	job.ErrorCode = "compose_failed"
	job.ErrorMessage = "compose failed"
	if err := testStore.UpdateJob(ctx, job); err != nil {
		t.Fatalf("UpdateJob() error = %v", err)
	}
	stored, err := testStore.JobByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("JobByID() error = %v", err)
	}
	if stored.State != "failed" || stored.FinishedAt == nil || !stored.FinishedAt.Equal(finishedAt) || stored.ErrorCode != "compose_failed" || stored.ErrorMessage != "compose failed" {
		t.Fatalf("updated job = %#v", stored)
	}
	if err := testStore.UpdateJob(ctx, Job{ID: "missing", State: "failed", RequestedAt: now}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateJob(missing) error = %v, want ErrNotFound", err)
	}

	queued := Job{ID: "job-queued", StackID: "beta", Action: "build", State: "queued", RequestedBy: "operator", RequestedAt: now.Add(3 * time.Minute)}
	if err := testStore.CreateJob(ctx, queued); err != nil {
		t.Fatalf("CreateJob(queued) error = %v", err)
	}
	if err := testStore.UpdateJobWorkflow(ctx, queued.ID, &JobWorkflow{Steps: []JobWorkflowStep{{Action: "build", State: "queued", TargetStackID: "beta"}}}); err != nil {
		t.Fatalf("UpdateJobWorkflow() error = %v", err)
	}
	active, err := testStore.ListActiveJobs(ctx)
	if err != nil {
		t.Fatalf("ListActiveJobs() error = %v", err)
	}
	if len(active) != 1 || active[0].ID != queued.ID || active[0].Workflow == nil || active[0].Workflow.Steps[0].Action != "build" {
		t.Fatalf("ListActiveJobs() = %#v", active)
	}
	if err := testStore.UpdateJobWorkflow(ctx, queued.ID, nil); err != nil {
		t.Fatalf("UpdateJobWorkflow(nil) error = %v", err)
	}
	if err := testStore.UpdateJobWorkflow(ctx, "missing", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateJobWorkflow(missing) error = %v, want ErrNotFound", err)
	}

	if _, err := testStore.AppendJobEvent(ctx, JobEvent{JobID: "missing", Event: "job_log", Timestamp: now}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AppendJobEvent(missing) error = %v, want ErrNotFound", err)
	}
	if err := testStore.CreateJobWithInitialEvent(ctx, queued, JobEvent{JobID: "other", Sequence: 1}); err == nil {
		t.Fatal("CreateJobWithInitialEvent(mismatched event) returned nil error")
	}
}

func TestAuditCursorErrorsAndRetentionSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	cursors := []string{
		"not-base64!",
		base64.RawURLEncoding.EncodeToString([]byte("missing-separator")),
		base64.RawURLEncoding.EncodeToString([]byte("not-a-time|audit-id")),
	}
	for _, cursor := range cursors {
		if _, err := testStore.ListAuditEntries(ctx, AuditQuery{Cursor: cursor}); err == nil {
			t.Errorf("ListAuditEntries(cursor %q) returned nil error", cursor)
		}
	}
	entries, err := testStore.LatestAuditEntriesByStackIDs(ctx, nil)
	if err != nil || len(entries) != 0 {
		t.Fatalf("LatestAuditEntriesByStackIDs(nil) = %#v, %v", entries, err)
	}

	summary := OperationalRetentionSummary{AuditEntriesDeleted: 1, JobsDeleted: 2, JobEventsDeleted: 3, SessionsDeleted: 4}
	if got := summary.TotalDeleted(); got != 10 {
		t.Fatalf("TotalDeleted() = %d, want 10", got)
	}
}
