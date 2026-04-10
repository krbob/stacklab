package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"stacklab/internal/hostinfo"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

func TestServiceGetUpdateAndDispatchJob(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var sent []WebhookPayload
	service.sendWebhook = func(_ context.Context, _ string, payload WebhookPayload) error {
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
	if settings.Enabled || !settings.Events.JobFailed || !settings.Events.JobSucceededWithWarnings || settings.Events.PostUpdateRecoveryFailed || settings.Events.RuntimeHealthDegraded {
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
	if !updated.Enabled || updated.WebhookURL != "https://hooks.example.test/stacklab" || !updated.Configured || !updated.Channels.Webhook.Enabled {
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
	if !testResult.Sent || testResult.Channel != "webhook" || len(sent) != 2 || sent[1].Event != "test_notification" {
		t.Fatalf("unexpected test payload sequence: %#v", sent)
	}
}

func TestPollStacklabServiceErrorsPrimesCursorWithoutSending(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.SetStacklabLogReader(&fakeStacklabLogReader{
		responses: []hostinfo.StacklabLogsResponse{
			{
				Items: []hostinfo.StacklabLogEntry{
					{Cursor: "s=cursor-1", Level: "info", Message: "started", Timestamp: time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC)},
				},
			},
		},
	})

	sent := 0
	service.sendWebhook = func(_ context.Context, _ string, _ WebhookPayload) error {
		sent++
		return nil
	}

	if err := service.pollStacklabServiceErrors(context.Background()); err != nil {
		t.Fatalf("pollStacklabServiceErrors() error = %v", err)
	}
	if sent != 0 {
		t.Fatalf("sent = %d, want 0", sent)
	}

	state, err := service.loadStacklabJournalState(context.Background())
	if err != nil {
		t.Fatalf("loadStacklabJournalState() error = %v", err)
	}
	if state.Cursor != "s=cursor-1" {
		t.Fatalf("state.Cursor = %q, want %q", state.Cursor, "s=cursor-1")
	}
}

func TestPollStacklabServiceErrorsSendsAlert(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.now = func() time.Time {
		return time.Date(2026, 4, 10, 8, 15, 0, 0, time.UTC)
	}
	service.SetStacklabLogReader(&fakeStacklabLogReader{
		responses: []hostinfo.StacklabLogsResponse{
			{
				Items: []hostinfo.StacklabLogEntry{
					{Cursor: "s=cursor-2", Level: "error", Message: "failed to bind socket", Timestamp: time.Date(2026, 4, 10, 8, 14, 0, 0, time.UTC)},
					{Cursor: "s=cursor-3", Level: "error", Message: "failed to bind socket", Timestamp: time.Date(2026, 4, 10, 8, 14, 5, 0, time.UTC)},
					{Cursor: "s=cursor-4", Level: "info", Message: "retrying", Timestamp: time.Date(2026, 4, 10, 8, 14, 6, 0, time.UTC)},
				},
			},
		},
	})

	_, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Channels: &ChannelsRequest{
			Webhook: &WebhookRequest{
				Enabled: true,
				URL:     "https://hooks.example.test/stacklab",
			},
		},
		Events: EventToggles{
			JobFailed:                true,
			JobSucceededWithWarnings: true,
			MaintenanceSucceeded:     false,
			PostUpdateRecoveryFailed: false,
			StacklabServiceError:     true,
		},
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	if err := service.saveStacklabJournalState(context.Background(), stacklabJournalState{Cursor: "s=cursor-1"}); err != nil {
		t.Fatalf("saveStacklabJournalState() error = %v", err)
	}

	var sent []WebhookPayload
	service.sendWebhook = func(_ context.Context, _ string, payload WebhookPayload) error {
		sent = append(sent, payload)
		return nil
	}

	if err := service.pollStacklabServiceErrors(context.Background()); err != nil {
		t.Fatalf("pollStacklabServiceErrors() error = %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(sent))
	}
	if sent[0].Event != "stacklab_service_error" || sent[0].StacklabService == nil || sent[0].StacklabService.EntryCount != 2 {
		t.Fatalf("unexpected payload: %#v", sent[0])
	}
	if len(sent[0].StacklabService.SampleMessages) == 0 || sent[0].StacklabService.SampleMessages[0] != "failed to bind socket (x2)" {
		t.Fatalf("unexpected sample messages: %#v", sent[0].StacklabService.SampleMessages)
	}

	state, err := service.loadStacklabJournalState(context.Background())
	if err != nil {
		t.Fatalf("loadStacklabJournalState() error = %v", err)
	}
	if state.Cursor != "s=cursor-4" {
		t.Fatalf("state.Cursor = %q, want %q", state.Cursor, "s=cursor-4")
	}
	if state.LastFingerprint == "" || state.LastNotifiedAt.IsZero() {
		t.Fatalf("unexpected state after alert: %#v", state)
	}
}

func TestPollStacklabServiceErrorsDedupesDuringCooldown(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Date(2026, 4, 10, 8, 15, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	entries := []hostinfo.StacklabLogEntry{
		{Cursor: "s=cursor-2", Level: "error", Message: "failed to bind socket", Timestamp: time.Date(2026, 4, 10, 8, 14, 0, 0, time.UTC)},
		{Cursor: "s=cursor-3", Level: "error", Message: "failed to bind socket", Timestamp: time.Date(2026, 4, 10, 8, 14, 5, 0, time.UTC)},
	}
	service.SetStacklabLogReader(&fakeStacklabLogReader{
		responses: []hostinfo.StacklabLogsResponse{
			{Items: entries},
		},
	})

	_, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Channels: &ChannelsRequest{
			Webhook: &WebhookRequest{
				Enabled: true,
				URL:     "https://hooks.example.test/stacklab",
			},
		},
		Events: EventToggles{
			JobFailed:                true,
			JobSucceededWithWarnings: true,
			MaintenanceSucceeded:     false,
			PostUpdateRecoveryFailed: false,
			StacklabServiceError:     true,
		},
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	if err := service.saveStacklabJournalState(context.Background(), stacklabJournalState{
		Cursor:          "s=cursor-1",
		LastFingerprint: stacklabErrorFingerprint(entries),
		LastNotifiedAt:  now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("saveStacklabJournalState() error = %v", err)
	}

	sent := 0
	service.sendWebhook = func(_ context.Context, _ string, _ WebhookPayload) error {
		sent++
		return nil
	}

	if err := service.pollStacklabServiceErrors(context.Background()); err != nil {
		t.Fatalf("pollStacklabServiceErrors() error = %v", err)
	}
	if sent != 0 {
		t.Fatalf("sent = %d, want 0", sent)
	}
}

func TestPollRuntimeHealthAlertsPrimesBaselineWithoutSending(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.SetStackInspector(fakeStackInspector{
		items: map[string]stacks.StackDetailResponse{
			"demo": runtimeHealthStack("demo", 1, 0),
		},
	})

	sent := 0
	service.sendWebhook = func(_ context.Context, _ string, _ WebhookPayload) error {
		sent++
		return nil
	}

	if err := service.pollRuntimeHealthAlerts(context.Background()); err != nil {
		t.Fatalf("pollRuntimeHealthAlerts() error = %v", err)
	}
	if sent != 0 {
		t.Fatalf("sent = %d, want 0", sent)
	}

	state, err := service.loadRuntimeHealthState(context.Background())
	if err != nil {
		t.Fatalf("loadRuntimeHealthState() error = %v", err)
	}
	if state.Fingerprint == "" {
		t.Fatalf("expected baseline fingerprint to be saved")
	}
}

func TestPollRuntimeHealthAlertsSendsOnNewDegradation(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.SetStackInspector(fakeStackInspector{
		items: map[string]stacks.StackDetailResponse{
			"demo":    runtimeHealthStack("demo", 1, 0),
			"worker":  runtimeHealthStack("worker", 0, 1),
			"healthy": runtimeHealthHealthyStack("healthy"),
		},
	})

	_, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Channels: &ChannelsRequest{
			Webhook: &WebhookRequest{
				Enabled: true,
				URL:     "https://hooks.example.test/stacklab",
			},
		},
		Events: EventToggles{
			JobFailed:                true,
			JobSucceededWithWarnings: true,
			MaintenanceSucceeded:     false,
			PostUpdateRecoveryFailed: false,
			StacklabServiceError:     false,
			RuntimeHealthDegraded:    true,
		},
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if err := service.saveRuntimeHealthState(context.Background(), runtimeHealthState{Fingerprint: "healthy-baseline"}); err != nil {
		t.Fatalf("saveRuntimeHealthState() error = %v", err)
	}

	var sent []WebhookPayload
	service.sendWebhook = func(_ context.Context, _ string, payload WebhookPayload) error {
		sent = append(sent, payload)
		return nil
	}

	if err := service.pollRuntimeHealthAlerts(context.Background()); err != nil {
		t.Fatalf("pollRuntimeHealthAlerts() error = %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(sent))
	}
	if sent[0].Event != "runtime_health_degraded" || sent[0].RuntimeHealth == nil || len(sent[0].RuntimeHealth.AffectedStacks) != 2 {
		t.Fatalf("unexpected payload: %#v", sent[0])
	}

	state, err := service.loadRuntimeHealthState(context.Background())
	if err != nil {
		t.Fatalf("loadRuntimeHealthState() error = %v", err)
	}
	if state.Fingerprint == "" || state.LastNotifiedAt.IsZero() {
		t.Fatalf("unexpected runtime health state: %#v", state)
	}
}

func TestPollRuntimeHealthAlertsDedupesDuringCooldown(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	failures := []RuntimeHealthFailure{{
		StackID:                  "demo",
		RuntimeState:             string(stacks.RuntimeStateError),
		DisplayState:             string(stacks.RuntimeStateError),
		UnhealthyContainerCount:  1,
		RestartingContainerCount: 0,
		RunningContainerCount:    0,
		TotalContainerCount:      1,
		Reasons:                  []string{"unhealthy_containers"},
	}}
	service.SetStackInspector(fakeStackInspector{
		items: map[string]stacks.StackDetailResponse{
			"demo": runtimeHealthStack("demo", 1, 0),
		},
	})

	_, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Channels: &ChannelsRequest{
			Webhook: &WebhookRequest{
				Enabled: true,
				URL:     "https://hooks.example.test/stacklab",
			},
		},
		Events: EventToggles{
			JobFailed:                true,
			JobSucceededWithWarnings: true,
			MaintenanceSucceeded:     false,
			PostUpdateRecoveryFailed: false,
			StacklabServiceError:     false,
			RuntimeHealthDegraded:    true,
		},
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if err := service.saveRuntimeHealthState(context.Background(), runtimeHealthState{
		Fingerprint:    runtimeHealthFingerprint(failures),
		LastNotifiedAt: now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("saveRuntimeHealthState() error = %v", err)
	}

	sent := 0
	service.sendWebhook = func(_ context.Context, _ string, _ WebhookPayload) error {
		sent++
		return nil
	}

	if err := service.pollRuntimeHealthAlerts(context.Background()); err != nil {
		t.Fatalf("pollRuntimeHealthAlerts() error = %v", err)
	}
	if sent != 0 {
		t.Fatalf("sent = %d, want 0", sent)
	}
}

func TestValidateSettings(t *testing.T) {
	t.Parallel()

	valid := Settings{
		Channels: Channels{
			Webhook: WebhookSettings{
				Enabled: true,
				URL:     "https://hooks.example.test/stacklab",
			},
		},
	}
	if err := validateSettings(valid); err != nil {
		t.Fatalf("validateSettings(valid) error = %v", err)
	}

	cases := []Settings{
		{Channels: Channels{Webhook: WebhookSettings{Enabled: true, URL: ""}}},
		{Channels: Channels{Webhook: WebhookSettings{Enabled: true, URL: "ftp://hooks.example.test/stacklab"}}},
		{Channels: Channels{Webhook: WebhookSettings{Enabled: true, URL: "/relative"}}},
		{Channels: Channels{Telegram: TelegramSettings{Enabled: true, BotToken: "", ChatID: "-123"}}},
		{Channels: Channels{Telegram: TelegramSettings{Enabled: true, BotToken: "bot:token", ChatID: ""}}},
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
		Events: EventToggles{
			JobFailed:                true,
			JobSucceededWithWarnings: false,
			MaintenanceSucceeded:     true,
		},
		Channels: Channels{
			Webhook: WebhookSettings{
				Enabled: true,
				URL:     "https://hooks.example.test/stacklab",
			},
			Telegram: TelegramSettings{
				Enabled:  true,
				BotToken: "bot:token",
				ChatID:   "-123",
			},
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
	if decoded.Channels.Webhook.URL != payload.Channels.Webhook.URL || decoded.Channels.Telegram.ChatID != payload.Channels.Telegram.ChatID || decoded.Events.MaintenanceSucceeded != payload.Events.MaintenanceSucceeded {
		t.Fatalf("unexpected decoded settings: %#v", decoded)
	}
}

func TestLoadLegacySettingsMigratesWebhook(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	raw := `{"enabled":true,"webhook_url":"https://hooks.example.test/stacklab","events":{"job_failed":true,"job_succeeded_with_warnings":true,"maintenance_succeeded":false}}`
	if err := testStore.SetAppSetting(context.Background(), settingsKey, raw, time.Now().UTC()); err != nil {
		t.Fatalf("SetAppSetting() error = %v", err)
	}

	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	settings, err := service.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}
	if !settings.Enabled || settings.WebhookURL != "https://hooks.example.test/stacklab" || !settings.Channels.Webhook.Enabled {
		t.Fatalf("unexpected migrated settings: %#v", settings)
	}
}

func TestSendTelegramTestNotification(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var gotToken, gotChatID, gotText string
	service.sendTelegram = func(_ context.Context, botToken, chatID, text string) error {
		gotToken = botToken
		gotChatID = chatID
		gotText = text
		return nil
	}

	result, err := service.SendTest(context.Background(), TestRequest{
		Channel: "telegram",
		Events:  defaultSettings().Events,
		Channels: &ChannelsRequest{
			Telegram: &TelegramRequest{
				Enabled:  true,
				BotToken: "bot:token",
				ChatID:   "-100123",
			},
		},
	})
	if err != nil {
		t.Fatalf("SendTest() error = %v", err)
	}
	if !result.Sent || result.Channel != "telegram" {
		t.Fatalf("unexpected test result: %#v", result)
	}
	if gotToken != "bot:token" || gotChatID != "-100123" || gotText == "" {
		t.Fatalf("unexpected telegram send args: token=%q chat=%q text=%q", gotToken, gotChatID, gotText)
	}
}

func TestDispatchJobPostUpdateRecoveryFailed(t *testing.T) {
	t.Parallel()

	testStore := openNotificationTestStore(t)
	service := NewService(testStore, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var sent []WebhookPayload
	service.sendWebhook = func(_ context.Context, _ string, payload WebhookPayload) error {
		sent = append(sent, payload)
		return nil
	}
	service.SetStackInspector(fakeStackInspector{
		items: map[string]stacks.StackDetailResponse{
			"demo": {
				Stack: stacks.StackDetail{
					StackHeader: stacks.StackHeader{
						ID:           "demo",
						DisplayState: stacks.RuntimeStateError,
						RuntimeState: stacks.RuntimeStateError,
						HealthSummary: stacks.HealthSummary{
							UnhealthyContainerCount: 1,
						},
					},
					Containers: []stacks.Container{
						{ID: "c1", Status: "restarting"},
					},
				},
			},
		},
	})

	_, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		Channels: &ChannelsRequest{
			Webhook: &WebhookRequest{
				Enabled: true,
				URL:     "https://hooks.example.test/stacklab",
			},
		},
		Events: EventToggles{
			JobFailed:                true,
			JobSucceededWithWarnings: true,
			MaintenanceSucceeded:     true,
			PostUpdateRecoveryFailed: true,
		},
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	startedAt := time.Date(2026, 4, 9, 18, 59, 0, 0, time.UTC)
	finishedAt := time.Date(2026, 4, 9, 19, 0, 0, 0, time.UTC)
	job := store.Job{
		ID:          "job_update",
		Action:      "update_stacks",
		State:       "succeeded",
		RequestedBy: "local",
		RequestedAt: startedAt,
		StartedAt:   &startedAt,
		FinishedAt:  &finishedAt,
		Workflow: &store.JobWorkflow{
			Steps: []store.JobWorkflowStep{
				{Action: "pull", State: "succeeded", TargetStackID: "demo"},
				{Action: "up", State: "succeeded", TargetStackID: "demo"},
			},
		},
	}
	if err := testStore.CreateJob(context.Background(), job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}

	if err := service.DispatchJob(context.Background(), job); err != nil {
		t.Fatalf("DispatchJob() error = %v", err)
	}
	if len(sent) != 1 || sent[0].Event != "post_update_recovery_failed" || sent[0].PostUpdate == nil || len(sent[0].PostUpdate.FailedStacks) != 1 {
		t.Fatalf("unexpected post-update payload: %#v", sent)
	}
}

type fakeStackInspector struct {
	items map[string]stacks.StackDetailResponse
}

func (f fakeStackInspector) List(_ context.Context, _ stacks.ListQuery) (stacks.StackListResponse, error) {
	items := make([]stacks.StackListItem, 0, len(f.items))
	for _, item := range f.items {
		items = append(items, stacks.StackListItem{
			StackHeader: item.Stack.StackHeader,
		})
	}
	return stacks.StackListResponse{Items: items}, nil
}

func (f fakeStackInspector) Get(_ context.Context, stackID string) (stacks.StackDetailResponse, error) {
	if item, ok := f.items[stackID]; ok {
		return item, nil
	}
	return stacks.StackDetailResponse{}, stacks.ErrNotFound
}

func runtimeHealthHealthyStack(stackID string) stacks.StackDetailResponse {
	return stacks.StackDetailResponse{
		Stack: stacks.StackDetail{
			StackHeader: stacks.StackHeader{
				ID:            stackID,
				DisplayState:  stacks.RuntimeStateRunning,
				RuntimeState:  stacks.RuntimeStateRunning,
				HealthSummary: stacks.HealthSummary{},
			},
			Containers: []stacks.Container{
				{ID: stackID + "-c1", Status: "running"},
			},
		},
	}
}

func runtimeHealthStack(stackID string, unhealthyCount, restartingCount int) stacks.StackDetailResponse {
	containers := make([]stacks.Container, 0, max(1, unhealthyCount+restartingCount))
	for i := 0; i < unhealthyCount; i++ {
		status := "running"
		health := "unhealthy"
		containers = append(containers, stacks.Container{
			ID:           fmt.Sprintf("%s-u%d", stackID, i),
			Status:       status,
			HealthStatus: &health,
		})
	}
	for i := 0; i < restartingCount; i++ {
		containers = append(containers, stacks.Container{
			ID:     fmt.Sprintf("%s-r%d", stackID, i),
			Status: "restarting",
		})
	}
	runtimeState := stacks.RuntimeStateRunning
	if restartingCount > 0 {
		runtimeState = stacks.RuntimeStateError
	}
	return stacks.StackDetailResponse{
		Stack: stacks.StackDetail{
			StackHeader: stacks.StackHeader{
				ID:           stackID,
				DisplayState: runtimeState,
				RuntimeState: runtimeState,
				HealthSummary: stacks.HealthSummary{
					UnhealthyContainerCount: unhealthyCount,
				},
			},
			Containers: containers,
		},
	}
}

type fakeStacklabLogReader struct {
	responses []hostinfo.StacklabLogsResponse
	errors    []error
	calls     int
}

func (f *fakeStacklabLogReader) StacklabLogs(_ context.Context, _ hostinfo.LogsQuery) (hostinfo.StacklabLogsResponse, error) {
	if f.calls < len(f.errors) && f.errors[f.calls] != nil {
		err := f.errors[f.calls]
		f.calls++
		return hostinfo.StacklabLogsResponse{}, err
	}
	if f.calls < len(f.responses) {
		response := f.responses[f.calls]
		f.calls++
		return response, nil
	}
	f.calls++
	return hostinfo.StacklabLogsResponse{}, nil
}
