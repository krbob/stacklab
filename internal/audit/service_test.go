package audit

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/store"
)

func TestServiceRecordsOperationalAuditEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	auditStore := openAuditStore(t)
	service := NewService(auditStore)
	requestedAt := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	startedAt := requestedAt.Add(time.Second)
	finishedAt := startedAt.Add(1500 * time.Millisecond)

	stackJob := store.Job{
		ID:           "job-stack",
		StackID:      "alpha",
		Action:       "up",
		State:        "failed",
		RequestedBy:  "operator",
		RequestedAt:  requestedAt,
		StartedAt:    &startedAt,
		FinishedAt:   &finishedAt,
		ErrorCode:    "compose_failed",
		ErrorMessage: "compose exited with status 1",
	}
	if err := service.RecordJob(ctx, stackJob, map[string]any{"attempt": 2}); err != nil {
		t.Fatalf("RecordJob(stack) error = %v", err)
	}
	if err := service.RecordStackJob(ctx, store.Job{
		ID:          "job-workspace",
		Action:      "update_stacks",
		State:       "succeeded",
		RequestedAt: requestedAt.Add(time.Minute),
	}); err != nil {
		t.Fatalf("RecordStackJob(workspace) error = %v", err)
	}

	stackID := "alpha"
	recorders := []struct {
		name string
		run  func() error
	}{
		{
			name: "system event",
			run: func() error {
				return service.RecordSystemEvent(ctx, "startup", "", "succeeded", requestedAt, &finishedAt, map[string]any{"version": "1.2.3"})
			},
		},
		{
			name: "terminal event",
			run: func() error {
				return service.RecordTerminalEvent(ctx, "alpha", "terminal-1", "container-1", "operator", "terminal_open", "succeeded", map[string]any{"service": "web"})
			},
		},
		{
			name: "config file save",
			run: func() error {
				return service.RecordConfigFileSave(ctx, "shared.env", &stackID, "", map[string]any{"bytes": 42})
			},
		},
		{
			name: "stack file save",
			run: func() error {
				return service.RecordStackFileSave(ctx, "alpha", "compose.yaml", "operator", map[string]any{"bytes": 128})
			},
		},
		{
			name: "config permission repair",
			run: func() error {
				return service.RecordConfigPermissionRepair(ctx, "shared.env", "operator", map[string]any{"mode": "0600"})
			},
		},
		{
			name: "stack permission repair",
			run: func() error {
				return service.RecordStackPermissionRepair(ctx, "alpha", "compose.yaml", "operator", map[string]any{"mode": "0640"})
			},
		},
		{
			name: "git commit",
			run: func() error {
				return service.RecordGitCommit(ctx, "operator", "deadbeef", "Update alpha", []string{"alpha/compose.yaml"}, 1, nil)
			},
		},
		{
			name: "git push",
			run: func() error {
				return service.RecordGitPush(ctx, "operator", "origin", "main", "origin/main", "deadbeef", true, 0, 0, nil)
			},
		},
	}
	for _, recorder := range recorders {
		if err := recorder.run(); err != nil {
			t.Fatalf("record %s error = %v", recorder.name, err)
		}
	}

	result, err := service.List(ctx, ListQuery{Limit: 20})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Items) != 10 {
		t.Fatalf("List() item count = %d, want 10", len(result.Items))
	}

	byAction := make(map[string][]store.AuditEntry)
	for _, entry := range result.Items {
		if !strings.HasPrefix(entry.ID, "audit_") {
			t.Errorf("audit entry ID = %q, want audit_ prefix", entry.ID)
		}
		byAction[entry.Action] = append(byAction[entry.Action], entry)
	}

	stackEntry := findAuditEntry(t, byAction["up"], "job-stack")
	if stackEntry.StackID == nil || *stackEntry.StackID != "alpha" || stackEntry.TargetType != "stack" || stackEntry.TargetID == nil || *stackEntry.TargetID != "alpha" {
		t.Fatalf("stack job target = %#v", stackEntry)
	}
	if stackEntry.DurationMS == nil || *stackEntry.DurationMS != 1500 {
		t.Fatalf("stack job duration = %#v, want 1500ms", stackEntry.DurationMS)
	}
	assertAuditDetails(t, stackEntry, map[string]any{
		"attempt":       float64(2),
		"error_code":    "compose_failed",
		"error_message": "compose exited with status 1",
	})

	workspaceEntry := findAuditEntry(t, byAction["update_stacks"], "job-workspace")
	if workspaceEntry.StackID != nil || workspaceEntry.TargetID != nil || workspaceEntry.TargetType != "workspace" || workspaceEntry.RequestedBy != "local" {
		t.Fatalf("workspace job entry = %#v", workspaceEntry)
	}
	if workspaceEntry.DetailJSON != nil || workspaceEntry.DurationMS != nil {
		t.Fatalf("workspace job optional details = detail %#v duration %#v, want nil", workspaceEntry.DetailJSON, workspaceEntry.DurationMS)
	}

	systemEntry := byAction["startup"][0]
	if systemEntry.TargetType != "system" || systemEntry.RequestedBy != "local" || systemEntry.DurationMS == nil || *systemEntry.DurationMS != 2500 {
		t.Fatalf("system entry = %#v", systemEntry)
	}
	terminalEntry := byAction["terminal_open"][0]
	if terminalEntry.StackID == nil || *terminalEntry.StackID != "alpha" || terminalEntry.TargetType != "terminal_session" || terminalEntry.TargetID == nil || *terminalEntry.TargetID != "terminal-1" {
		t.Fatalf("terminal entry = %#v", terminalEntry)
	}
	if entry := byAction["save_config_file"][0]; entry.TargetType != "config_file" || entry.TargetID == nil || *entry.TargetID != "shared.env" || entry.RequestedBy != "local" {
		t.Fatalf("config save entry = %#v", entry)
	}
	if entry := byAction["save_stack_file"][0]; entry.TargetType != "stack_file" || entry.StackID == nil || *entry.StackID != "alpha" {
		t.Fatalf("stack save entry = %#v", entry)
	}
	if entry := byAction["repair_config_workspace_permissions"][0]; entry.TargetType != "config_file" {
		t.Fatalf("config permission entry = %#v", entry)
	}
	if entry := byAction["repair_stack_workspace_permissions"][0]; entry.TargetType != "stack_file" || entry.StackID == nil || *entry.StackID != "alpha" {
		t.Fatalf("stack permission entry = %#v", entry)
	}
	assertAuditDetails(t, byAction["git_commit"][0], map[string]any{
		"commit":            "deadbeef",
		"summary":           "Update alpha",
		"path_count":        float64(1),
		"remaining_changes": float64(1),
	})
	assertAuditDetails(t, byAction["git_push"][0], map[string]any{
		"remote":        "origin",
		"branch":        "main",
		"upstream_name": "origin/main",
		"head_commit":   "deadbeef",
		"pushed":        true,
	})
}

func TestServiceLastActionsSkipsUnfinishedEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	auditStore := openAuditStore(t)
	service := NewService(auditStore)
	requestedAt := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	finishedAt := requestedAt.Add(time.Minute)

	if err := service.RecordJob(ctx, store.Job{
		ID: "job-alpha", StackID: "alpha", Action: "restart", State: "succeeded",
		RequestedAt: requestedAt, StartedAt: &requestedAt, FinishedAt: &finishedAt,
	}, nil); err != nil {
		t.Fatalf("RecordJob(alpha) error = %v", err)
	}
	if err := service.RecordJob(ctx, store.Job{
		ID: "job-beta", StackID: "beta", Action: "pull", State: "running",
		RequestedAt: requestedAt.Add(time.Minute), StartedAt: &finishedAt,
	}, nil); err != nil {
		t.Fatalf("RecordJob(beta) error = %v", err)
	}

	actions, err := service.LastActions(ctx, []string{"alpha", "beta", "missing"})
	if err != nil {
		t.Fatalf("LastActions() error = %v", err)
	}
	if len(actions) != 1 || actions["alpha"] == nil {
		t.Fatalf("LastActions() = %#v, want only alpha", actions)
	}
	if action := actions["alpha"]; action.Action != "restart" || action.Result != "succeeded" || !action.FinishedAt.Equal(finishedAt) {
		t.Fatalf("LastActions(alpha) = %#v", action)
	}
}

func TestServiceReturnsMarshalAndStoreErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	auditStore := openAuditStore(t)
	service := NewService(auditStore)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

	err := service.RecordSystemEvent(ctx, "invalid", "local", "failed", now, nil, map[string]any{"unsupported": make(chan int)})
	if err == nil || !strings.Contains(err.Error(), "marshal audit details") {
		t.Fatalf("RecordSystemEvent(invalid details) error = %v", err)
	}

	if err := auditStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	err = service.RecordSystemEvent(ctx, "closed_store", "local", "failed", now, nil, nil)
	if err == nil {
		t.Fatal("RecordSystemEvent(closed store) returned nil error")
	}
	if _, err := service.List(ctx, ListQuery{Limit: 10}); err == nil {
		t.Fatal("List(closed store) returned nil error")
	}
	if _, err := service.LastActions(ctx, []string{"alpha"}); err == nil {
		t.Fatal("LastActions(closed store) returned nil error")
	}
}

func findAuditEntry(t *testing.T, entries []store.AuditEntry, jobID string) store.AuditEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.JobID != nil && *entry.JobID == jobID {
			return entry
		}
	}
	t.Fatalf("audit entry for job %q not found in %#v", jobID, entries)
	return store.AuditEntry{}
}

func assertAuditDetails(t *testing.T, entry store.AuditEntry, want map[string]any) {
	t.Helper()
	if entry.DetailJSON == nil {
		t.Fatalf("entry %q detail JSON is nil", entry.Action)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(*entry.DetailJSON), &got); err != nil {
		t.Fatalf("unmarshal entry %q details: %v", entry.Action, err)
	}
	for key, wantValue := range want {
		if gotValue, exists := got[key]; !exists || !equalAuditDetail(gotValue, wantValue) {
			t.Errorf("entry %q detail %q = %#v, want %#v", entry.Action, key, gotValue, wantValue)
		}
	}
}

func equalAuditDetail(got, want any) bool {
	gotJSON, gotErr := json.Marshal(got)
	wantJSON, wantErr := json.Marshal(want)
	return gotErr == nil && wantErr == nil && string(gotJSON) == string(wantJSON)
}

func openAuditStore(t *testing.T) *store.Store {
	t.Helper()
	auditStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = auditStore.Close()
	})
	return auditStore
}
