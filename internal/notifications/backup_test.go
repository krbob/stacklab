package notifications

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"stacklab/internal/store"
)

func TestBackupNotificationsRespectFailureAndRecoveryToggles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, action, state, event string
		toggles                    EventToggles
	}{
		{name: "scheduled timeout", action: "backup_run", state: "timed_out", event: "job_failed", toggles: EventToggles{JobFailed: true}},
		{name: "retry timeout", action: "backup_retry", state: "timed_out", event: "job_failed", toggles: EventToggles{JobFailed: true}},
		{name: "restore timeout", action: "backup_restore", state: "timed_out", event: "job_failed", toggles: EventToggles{JobFailed: true}},
		{name: "failed copy", action: "backup_run", state: "failed", event: "job_failed", toggles: EventToggles{JobFailed: true}},
		{name: "timeout disabled", action: "backup_run", state: "timed_out", toggles: EventToggles{MaintenanceSucceeded: true}},
		{name: "successful restore", action: "backup_restore", state: "succeeded", event: "maintenance_succeeded", toggles: EventToggles{MaintenanceSucceeded: true}},
		{name: "restore success disabled", action: "backup_restore", state: "succeeded", toggles: EventToggles{JobFailed: true}},
		{name: "operator cancellation", action: "backup_run", state: "cancelled", toggles: EventToggles{JobFailed: true, MaintenanceSucceeded: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			testStore := openNotificationTestStore(t)
			service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
			sent := []WebhookPayload{}
			service.sendWebhook = func(_ context.Context, _ string, payload WebhookPayload) error {
				sent = append(sent, payload)
				return nil
			}
			_, err := service.UpdateSettings(ctx, UpdateSettingsRequest{Enabled: true, WebhookURL: "https://hooks.example.test/stacklab", Events: test.toggles})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			job := store.Job{ID: "job_backup_notification", Action: test.action, State: test.state, RequestedBy: "scheduler", RequestedAt: now, StartedAt: &now, FinishedAt: &now}
			if err = testStore.CreateJob(ctx, job); err != nil {
				t.Fatal(err)
			}
			if err = service.DispatchJob(ctx, job); err != nil {
				t.Fatal(err)
			}
			if test.event == "" {
				if len(sent) != 0 {
					t.Fatalf("unexpected notification: %#v", sent)
				}
				return
			}
			if len(sent) != 1 || sent[0].Event != test.event || sent[0].Job == nil || sent[0].Job.State != test.state {
				t.Fatalf("notification=%#v, want %s preserving %s", sent, test.event, test.state)
			}
		})
	}
}
