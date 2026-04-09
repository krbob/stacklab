package notifications

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"stacklab/internal/store"
)

func TestServiceGetUpdateAndDispatchJob(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var sent []WebhookPayload
	service.send = func(_ context.Context, _ string, payload WebhookPayload) error {
		sent = append(sent, payload)
		return nil
	}
	service.now = func() time.Time {
		return time.Date(2026, 4, 9, 19, 0, 0, 0, time.UTC)
	}

	settings, err := service.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}
	if settings.Enabled || !settings.Events.JobFailed || !settings.Events.JobSucceededWithWarnings {
		t.Fatalf("unexpected default settings: %#v", settings)
	}

	updated, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Enabled:    true,
		WebhookURL: "https://hooks.example.test/stacklab",
		Events: EventToggles{
			JobFailed:                true,
			JobSucceededWithWarnings: true,
			MaintenanceSucceeded:     true,
		},
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if !updated.Enabled || updated.WebhookURL != "https://hooks.example.test/stacklab" || !updated.Configured {
		t.Fatalf("unexpected updated settings: %#v", updated)
	}

	startedAt := time.Date(2026, 4, 9, 18, 59, 0, 0, time.UTC)
	finishedAt := time.Date(2026, 4, 9, 19, 0, 0, 0, time.UTC)
	job := store.Job{
		ID:           "job_123",
		StackID:      "demo",
		Action:       "update_stacks",
		State:        "succeeded",
		RequestedBy:  "local",
		RequestedAt:  startedAt,
		StartedAt:    &startedAt,
		FinishedAt:   &finishedAt,
		ErrorCode:    "",
		ErrorMessage: "",
	}
	if err := testStore.CreateJob(context.Background(), job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	if err := testStore.CreateJobEvent(context.Background(), store.JobEvent{
		JobID:     job.ID,
		Sequence:  1,
		Event:     "job_warning",
		State:     job.State,
		Message:   "warning",
		Timestamp: finishedAt,
	}); err != nil {
		t.Fatalf("CreateJobEvent(warning) error = %v", err)
	}

	if err := service.DispatchJob(context.Background(), job); err != nil {
		t.Fatalf("DispatchJob() error = %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("sent payload count = %d, want 1", len(sent))
	}
	if sent[0].Event != "job_succeeded_with_warnings" || sent[0].WarningCount != 1 {
		t.Fatalf("unexpected payload: %#v", sent[0])
	}
	if sent[0].Job == nil || sent[0].Job.StackID == nil || *sent[0].Job.StackID != "demo" {
		t.Fatalf("unexpected job payload: %#v", sent[0].Job)
	}

	testResult, err := service.SendTest(context.Background(), TestRequest{
		Enabled:    true,
		WebhookURL: "https://hooks.example.test/stacklab",
		Events:     updated.Events,
	})
	if err != nil {
		t.Fatalf("SendTest() error = %v", err)
	}
	if !testResult.Sent || len(sent) != 2 || sent[1].Event != "test_notification" {
		t.Fatalf("unexpected test payload sequence: %#v", sent)
	}
}

func TestValidateSettings(t *testing.T) {
	t.Parallel()

	valid := Settings{
		Enabled:    true,
		WebhookURL: "https://hooks.example.test/stacklab",
	}
	if err := validateSettings(valid); err != nil {
		t.Fatalf("validateSettings(valid) error = %v", err)
	}

	cases := []Settings{
		{Enabled: true, WebhookURL: ""},
		{Enabled: true, WebhookURL: "ftp://hooks.example.test/stacklab"},
		{Enabled: true, WebhookURL: "/relative"},
	}
	for _, tc := range cases {
		if err := validateSettings(tc); err == nil {
			t.Fatalf("validateSettings(%#v) expected error", tc)
		}
	}
}

func openNotificationTestStore(t *testing.T) *store.Store {
	t.Helper()
	db := t.TempDir() + "/stacklab.db"
	s, err := store.Open(db)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func TestStoreSettingsRoundTripJSON(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	payload := Settings{
		Enabled:    true,
		WebhookURL: "https://hooks.example.test/stacklab",
		Events: EventToggles{
			JobFailed:                true,
			JobSucceededWithWarnings: false,
			MaintenanceSucceeded:     true,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := testStore.SetAppSetting(context.Background(), settingsKey, string(raw), time.Now().UTC()); err != nil {
		t.Fatalf("SetAppSetting() error = %v", err)
	}
	stored, ok, err := testStore.AppSetting(context.Background(), settingsKey)
	if err != nil || !ok {
		t.Fatalf("AppSetting() = (%q, %v, %v)", stored, ok, err)
	}
	var decoded Settings
	if err := json.Unmarshal([]byte(stored), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded.WebhookURL != payload.WebhookURL || decoded.Events.MaintenanceSucceeded != payload.Events.MaintenanceSucceeded {
		t.Fatalf("unexpected decoded settings: %#v", decoded)
	}
}
