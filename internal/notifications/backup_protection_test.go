package notifications

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestBackupOverdueDeliveryIncludesScopeAndUsesExistingChannels(t *testing.T) {
	ctx := context.Background()
	st := openNotificationTestStore(t)
	s := NewService(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	due := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	if err := s.DispatchBackupAlert(ctx, "p", "Database", "d", "Offsite", due); err == nil {
		t.Fatal("disabled delivery marked sent")
	}
	if _, err := s.UpdateSettings(ctx, UpdateSettingsRequest{Enabled: true, WebhookURL: "https://example.test/hook", Events: EventToggles{JobFailed: true}}); err != nil {
		t.Fatal(err)
	}
	sent := []WebhookPayload{}
	s.sendWebhook = func(_ context.Context, _ string, p WebhookPayload) error { sent = append(sent, p); return nil }
	if err := s.DispatchBackupAlert(ctx, "p", "Database", "d", "Offsite", due); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].Event != "backup_copy_overdue" || sent[0].Backup == nil || sent[0].Backup.PlanID != "p" || sent[0].Backup.DestinationName != "Offsite" || !sent[0].Backup.DueAt.Equal(due) {
		t.Fatalf("payload %+v", sent)
	}
}
func TestNotificationClockAdvancesAndEmptySecretDoesNotCorruptError(t *testing.T) {
	s := NewService(openNotificationTestStore(t), nil)
	first := s.now()
	time.Sleep(5 * time.Millisecond)
	between := time.Now().UTC()
	second := s.now()
	if !second.After(first) || second.Before(between) {
		t.Fatalf("notification time is fixed at construction: first=%s between=%s second=%s", first.Format(time.RFC3339Nano), between.Format(time.RFC3339Nano), second.Format(time.RFC3339Nano))
	}
	original := errors.New("invalid webhook URL")
	if got := redactWebhookError(original, ""); got.Error() != original.Error() {
		t.Fatalf("empty secret corrupted message: %s", got)
	}
}
