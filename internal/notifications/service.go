package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"stacklab/internal/hostinfo"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

const (
	settingsKey                  = "notifications_v2"
	legacySettingsKey            = "notifications_webhook_v1"
	stacklabJournalStateKey      = "notifications_stacklab_journal_state_v1"
	stacklabJournalPollLimit     = 200
	stacklabJournalMaxBatchFetch = 5
)

var ErrInvalidConfig = errors.New("invalid notification config")

type Store interface {
	AppSetting(ctx context.Context, key string) (string, bool, error)
	SetAppSetting(ctx context.Context, key, valueJSON string, updatedAt time.Time) error
	ListJobEvents(ctx context.Context, jobID string) ([]store.JobEvent, error)
}

type StackInspector interface {
	Get(ctx context.Context, stackID string) (stacks.StackDetailResponse, error)
}

type StacklabLogReader interface {
	StacklabLogs(ctx context.Context, query hostinfo.LogsQuery) (hostinfo.StacklabLogsResponse, error)
}

type webhookSender func(ctx context.Context, target string, payload WebhookPayload) error
type telegramSender func(ctx context.Context, botToken, chatID, text string) error

type EventToggles struct {
	JobFailed                bool `json:"job_failed"`
	JobSucceededWithWarnings bool `json:"job_succeeded_with_warnings"`
	MaintenanceSucceeded     bool `json:"maintenance_succeeded"`
	PostUpdateRecoveryFailed bool `json:"post_update_recovery_failed"`
	StacklabServiceError     bool `json:"stacklab_service_error"`
}

type Settings struct {
	Events   EventToggles `json:"events"`
	Channels Channels     `json:"channels"`
}

type Channels struct {
	Webhook  WebhookSettings  `json:"webhook"`
	Telegram TelegramSettings `json:"telegram"`
}

type WebhookSettings struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

type TelegramSettings struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type SettingsResponse struct {
	// Legacy v1 webhook fields kept for backward compatibility with the current UI.
	Enabled    bool         `json:"enabled"`
	Configured bool         `json:"configured"`
	WebhookURL string       `json:"webhook_url"`
	Events     EventToggles `json:"events"`
	Channels   ChannelsView `json:"channels"`
}

type ChannelsView struct {
	Webhook  WebhookView  `json:"webhook"`
	Telegram TelegramView `json:"telegram"`
}

type WebhookView struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	URL        string `json:"url"`
}

type TelegramView struct {
	Enabled            bool   `json:"enabled"`
	Configured         bool   `json:"configured"`
	BotTokenConfigured bool   `json:"bot_token_configured"`
	ChatID             string `json:"chat_id"`
}

type UpdateSettingsRequest struct {
	// Legacy v1 webhook fields kept for backward compatibility with the current UI.
	Enabled    bool             `json:"enabled"`
	WebhookURL string           `json:"webhook_url"`
	Events     EventToggles     `json:"events"`
	Channels   *ChannelsRequest `json:"channels,omitempty"`
}

type ChannelsRequest struct {
	Webhook  *WebhookRequest  `json:"webhook,omitempty"`
	Telegram *TelegramRequest `json:"telegram,omitempty"`
}

type WebhookRequest struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

type TelegramRequest struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type TestRequest struct {
	Channel    string           `json:"channel,omitempty"`
	Enabled    bool             `json:"enabled"`
	WebhookURL string           `json:"webhook_url"`
	Events     EventToggles     `json:"events"`
	Channels   *ChannelsRequest `json:"channels,omitempty"`
}

type TestResponse struct {
	Sent    bool   `json:"sent"`
	Channel string `json:"channel"`
}

type WebhookPayload struct {
	Event           string                `json:"event"`
	SentAt          time.Time             `json:"sent_at"`
	Source          string                `json:"source"`
	Summary         string                `json:"summary"`
	Job             *WebhookJob           `json:"job,omitempty"`
	WarningCount    int                   `json:"warning_count,omitempty"`
	PostUpdate      *PostUpdateSummary    `json:"post_update,omitempty"`
	StacklabService *StacklabServiceAlert `json:"stacklab_service,omitempty"`
}

type WebhookJob struct {
	ID           string     `json:"id"`
	Action       string     `json:"action"`
	State        string     `json:"state"`
	StackID      *string    `json:"stack_id,omitempty"`
	RequestedAt  time.Time  `json:"requested_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	ErrorCode    string     `json:"error_code,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	DurationMS   *int       `json:"duration_ms,omitempty"`
}

type PostUpdateSummary struct {
	FailedStacks []PostUpdateFailure `json:"failed_stacks"`
}

type PostUpdateFailure struct {
	StackID                 string `json:"stack_id"`
	RuntimeState            string `json:"runtime_state,omitempty"`
	DisplayState            string `json:"display_state,omitempty"`
	UnhealthyContainerCount int    `json:"unhealthy_container_count,omitempty"`
	RunningContainerCount   int    `json:"running_container_count,omitempty"`
	TotalContainerCount     int    `json:"total_container_count,omitempty"`
	Reason                  string `json:"reason,omitempty"`
}

type StacklabServiceAlert struct {
	EntryCount      int       `json:"entry_count"`
	FirstTimestamp  time.Time `json:"first_timestamp,omitempty"`
	LastTimestamp   time.Time `json:"last_timestamp,omitempty"`
	SampleMessages  []string  `json:"sample_messages,omitempty"`
	LatestCursor    string    `json:"latest_cursor,omitempty"`
	CooldownSeconds int       `json:"cooldown_seconds,omitempty"`
}

type stacklabJournalState struct {
	Cursor          string    `json:"cursor"`
	LastFingerprint string    `json:"last_fingerprint,omitempty"`
	LastNotifiedAt  time.Time `json:"last_notified_at,omitempty"`
}

type Service struct {
	store          Store
	logger         *slog.Logger
	stackInspector StackInspector
	stacklabLogs   StacklabLogReader
	sendWebhook    webhookSender
	sendTelegram   telegramSender
	now            func() time.Time
	pollInterval   time.Duration
	alertCooldown  time.Duration
}

func NewService(settingStore Store, logger *slog.Logger) *Service {
	client := &http.Client{Timeout: 5 * time.Second}
	return &Service{
		store:  settingStore,
		logger: logger,
		sendWebhook: func(ctx context.Context, target string, payload WebhookPayload) error {
			body, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal webhook payload: %w", err)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("build webhook request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "Stacklab-Notifications/1")
			req.Header.Set("X-Stacklab-Event", payload.Event)
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("send webhook request: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("webhook returned status %d", resp.StatusCode)
			}
			return nil
		},
		sendTelegram: func(ctx context.Context, botToken, chatID, text string) error {
			body, err := json.Marshal(map[string]any{
				"chat_id":                  chatID,
				"text":                     text,
				"disable_web_page_preview": true,
			})
			if err != nil {
				return fmt.Errorf("marshal telegram payload: %w", err)
			}
			endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("build telegram request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "Stacklab-Notifications/1")
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("send telegram request: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("telegram returned status %d", resp.StatusCode)
			}
			return nil
		},
		now:           time.Now().UTC,
		pollInterval:  30 * time.Second,
		alertCooldown: 15 * time.Minute,
	}
}

func (s *Service) SetStackInspector(inspector StackInspector) {
	s.stackInspector = inspector
}

func (s *Service) SetStacklabLogReader(reader StacklabLogReader) {
	s.stacklabLogs = reader
}

func (s *Service) StartBackground(ctx context.Context) {
	if s.stacklabLogs == nil {
		return
	}
	go s.runStacklabSelfHealthLoop(ctx)
}

func (s *Service) GetSettings(ctx context.Context) (SettingsResponse, error) {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}
	return settingsResponse(settings), nil
}

func (s *Service) UpdateSettings(ctx context.Context, request UpdateSettingsRequest) (SettingsResponse, error) {
	settings, err := s.settingsFromUpdateRequest(ctx, request)
	if err != nil {
		return SettingsResponse{}, err
	}
	if err := validateSettings(settings); err != nil {
		return SettingsResponse{}, err
	}
	if err := s.saveSettings(ctx, settings); err != nil {
		return SettingsResponse{}, err
	}
	return settingsResponse(settings), nil
}

func (s *Service) SendTest(ctx context.Context, request TestRequest) (TestResponse, error) {
	settings, err := s.settingsFromTestRequest(ctx, request)
	if err != nil {
		return TestResponse{}, err
	}
	if err := validateSettings(settings); err != nil {
		return TestResponse{}, err
	}
	payload := WebhookPayload{
		Event:   "test_notification",
		SentAt:  s.now(),
		Source:  "stacklab",
		Summary: "Stacklab test notification",
	}
	channel := strings.TrimSpace(request.Channel)
	if channel == "" {
		channel = "webhook"
	}
	switch channel {
	case "webhook":
		if err := s.sendWebhook(ctx, settings.Channels.Webhook.URL, payload); err != nil {
			return TestResponse{}, err
		}
	case "telegram":
		if err := s.sendTelegram(ctx, settings.Channels.Telegram.BotToken, settings.Channels.Telegram.ChatID, buildTelegramText(payload)); err != nil {
			return TestResponse{}, err
		}
	default:
		return TestResponse{}, fmt.Errorf("%w: unsupported channel", ErrInvalidConfig)
	}
	return TestResponse{Sent: true, Channel: channel}, nil
}

func (s *Service) DispatchJobAsync(job store.Job) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.DispatchJob(ctx, job); err != nil && s.logger != nil {
			s.logger.Warn("dispatch notification failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		}
	}()
}

func (s *Service) DispatchJob(ctx context.Context, job store.Job) error {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return err
	}

	payload, ok, err := s.buildJobPayload(ctx, job, settings)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	return s.dispatchPayload(ctx, settings, payload)
}

func (s *Service) dispatchPayload(ctx context.Context, settings Settings, payload WebhookPayload) error {
	var errs []error

	if settings.Channels.Webhook.Enabled && settings.Channels.Webhook.URL != "" {
		if err := s.sendWebhook(ctx, settings.Channels.Webhook.URL, payload); err != nil {
			errs = append(errs, err)
		}
	}
	if settings.Channels.Telegram.Enabled && settings.Channels.Telegram.BotToken != "" && settings.Channels.Telegram.ChatID != "" {
		if err := s.sendTelegram(ctx, settings.Channels.Telegram.BotToken, settings.Channels.Telegram.ChatID, buildTelegramText(payload)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) buildJobPayload(ctx context.Context, job store.Job, settings Settings) (WebhookPayload, bool, error) {
	events, err := s.store.ListJobEvents(ctx, job.ID)
	if err != nil {
		return WebhookPayload{}, false, err
	}
	warningCount := 0
	for _, event := range events {
		if event.Event == "job_warning" {
			warningCount++
		}
	}

	eventType := ""
	var postUpdate *PostUpdateSummary
	switch {
	case job.State == "failed" && settings.Events.JobFailed:
		eventType = "job_failed"
	case job.State == "succeeded" && job.Action == "update_stacks" && settings.Events.PostUpdateRecoveryFailed:
		failures, err := s.inspectPostUpdateFailures(ctx, job)
		if err != nil {
			return WebhookPayload{}, false, err
		}
		if len(failures) > 0 {
			eventType = "post_update_recovery_failed"
			postUpdate = &PostUpdateSummary{FailedStacks: failures}
			break
		}
		fallthrough
	case job.State == "succeeded" && warningCount > 0 && settings.Events.JobSucceededWithWarnings:
		eventType = "job_succeeded_with_warnings"
	case job.State == "succeeded" && isMaintenanceAction(job.Action) && settings.Events.MaintenanceSucceeded:
		eventType = "maintenance_succeeded"
	default:
		return WebhookPayload{}, false, nil
	}

	var durationMS *int
	if job.StartedAt != nil && job.FinishedAt != nil {
		duration := int(job.FinishedAt.Sub(*job.StartedAt).Milliseconds())
		durationMS = &duration
	}

	var stackID *string
	if job.StackID != "" {
		stackID = &job.StackID
	}

	summary := buildSummary(eventType, job, warningCount)
	payload := WebhookPayload{
		Event:        eventType,
		SentAt:       s.now(),
		Source:       "stacklab",
		Summary:      summary,
		WarningCount: warningCount,
		PostUpdate:   postUpdate,
		Job: &WebhookJob{
			ID:           job.ID,
			Action:       job.Action,
			State:        job.State,
			StackID:      stackID,
			RequestedAt:  job.RequestedAt,
			StartedAt:    job.StartedAt,
			FinishedAt:   job.FinishedAt,
			ErrorCode:    job.ErrorCode,
			ErrorMessage: job.ErrorMessage,
			DurationMS:   durationMS,
		},
	}
	return payload, true, nil
}

func (s *Service) loadSettings(ctx context.Context) (Settings, error) {
	raw, ok, err := s.store.AppSetting(ctx, settingsKey)
	if err != nil {
		return Settings{}, err
	}
	if !ok {
		raw, ok, err = s.store.AppSetting(ctx, legacySettingsKey)
		if err != nil {
			return Settings{}, err
		}
	}
	if !ok {
		return defaultSettings(), nil
	}

	type persistedCompat struct {
		Enabled    *bool        `json:"enabled"`
		WebhookURL string       `json:"webhook_url"`
		Events     EventToggles `json:"events"`
		Channels   Channels     `json:"channels"`
	}

	compat := persistedCompat{
		Events:   defaultSettings().Events,
		Channels: defaultSettings().Channels,
	}
	if err := json.Unmarshal([]byte(raw), &compat); err != nil {
		return Settings{}, fmt.Errorf("parse notification settings: %w", err)
	}

	settings := defaultSettings()
	settings.Events = compat.Events
	settings.Channels = compat.Channels
	if compat.Enabled != nil {
		settings.Channels.Webhook.Enabled = *compat.Enabled
	}
	if strings.TrimSpace(compat.WebhookURL) != "" {
		settings.Channels.Webhook.URL = strings.TrimSpace(compat.WebhookURL)
	}
	settings.Channels.Webhook.URL = strings.TrimSpace(settings.Channels.Webhook.URL)
	settings.Channels.Telegram.BotToken = strings.TrimSpace(settings.Channels.Telegram.BotToken)
	settings.Channels.Telegram.ChatID = strings.TrimSpace(settings.Channels.Telegram.ChatID)
	return settings, nil
}

func (s *Service) saveSettings(ctx context.Context, settings Settings) error {
	payload, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal notification settings: %w", err)
	}
	return s.store.SetAppSetting(ctx, settingsKey, string(payload), s.now())
}

func defaultSettings() Settings {
	return Settings{
		Events: EventToggles{
			JobFailed:                true,
			JobSucceededWithWarnings: true,
			MaintenanceSucceeded:     false,
			PostUpdateRecoveryFailed: false,
			StacklabServiceError:     false,
		},
		Channels: Channels{
			Webhook: WebhookSettings{
				Enabled: false,
				URL:     "",
			},
			Telegram: TelegramSettings{
				Enabled:  false,
				BotToken: "",
				ChatID:   "",
			},
		},
	}
}

func settingsResponse(settings Settings) SettingsResponse {
	return SettingsResponse{
		Enabled:    settings.Channels.Webhook.Enabled,
		Configured: settings.Channels.Webhook.URL != "",
		WebhookURL: settings.Channels.Webhook.URL,
		Events:     settings.Events,
		Channels: ChannelsView{
			Webhook: WebhookView{
				Enabled:    settings.Channels.Webhook.Enabled,
				Configured: settings.Channels.Webhook.URL != "",
				URL:        settings.Channels.Webhook.URL,
			},
			Telegram: TelegramView{
				Enabled:            settings.Channels.Telegram.Enabled,
				Configured:         settings.Channels.Telegram.BotToken != "" && settings.Channels.Telegram.ChatID != "",
				BotTokenConfigured: settings.Channels.Telegram.BotToken != "",
				ChatID:             settings.Channels.Telegram.ChatID,
			},
		},
	}
}

func validateSettings(settings Settings) error {
	webhook := settings.Channels.Webhook
	if webhook.Enabled || strings.TrimSpace(webhook.URL) != "" {
		if strings.TrimSpace(webhook.URL) == "" {
			return fmt.Errorf("%w: webhook_url is required", ErrInvalidConfig)
		}
		parsed, err := url.Parse(webhook.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("%w: webhook_url must be a valid absolute URL", ErrInvalidConfig)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("%w: webhook_url must use http or https", ErrInvalidConfig)
		}
	}

	telegram := settings.Channels.Telegram
	if telegram.Enabled || telegram.BotToken != "" || telegram.ChatID != "" {
		if telegram.BotToken == "" {
			return fmt.Errorf("%w: telegram bot_token is required", ErrInvalidConfig)
		}
		if telegram.ChatID == "" {
			return fmt.Errorf("%w: telegram chat_id is required", ErrInvalidConfig)
		}
	}
	return nil
}

func isMaintenanceAction(action string) bool {
	switch action {
	case "update_stacks", "prune":
		return true
	default:
		return false
	}
}

func buildSummary(eventType string, job store.Job, warningCount int) string {
	target := job.Action
	if job.StackID != "" {
		target += " · " + job.StackID
	}
	switch eventType {
	case "job_failed":
		return "Stacklab job failed: " + target
	case "job_succeeded_with_warnings":
		return fmt.Sprintf("Stacklab job succeeded with warnings: %s (%d warnings)", target, warningCount)
	case "maintenance_succeeded":
		return "Stacklab maintenance completed: " + target
	case "post_update_recovery_failed":
		return "Stacklab post-update recovery failed: " + target
	case "stacklab_service_error":
		return "Stacklab service logged new errors"
	default:
		return "Stacklab notification"
	}
}

func (s *Service) settingsFromUpdateRequest(ctx context.Context, request UpdateSettingsRequest) (Settings, error) {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	settings.Events = request.Events
	if request.Channels == nil {
		settings.Channels.Webhook = WebhookSettings{
			Enabled: request.Enabled,
			URL:     strings.TrimSpace(request.WebhookURL),
		}
		return settings, nil
	}
	if request.Channels.Webhook != nil {
		settings.Channels.Webhook = WebhookSettings{
			Enabled: request.Channels.Webhook.Enabled,
			URL:     strings.TrimSpace(request.Channels.Webhook.URL),
		}
	}
	if request.Channels.Telegram != nil {
		settings.Channels.Telegram = TelegramSettings{
			Enabled:  request.Channels.Telegram.Enabled,
			BotToken: strings.TrimSpace(request.Channels.Telegram.BotToken),
			ChatID:   strings.TrimSpace(request.Channels.Telegram.ChatID),
		}
	}
	return settings, nil
}

func (s *Service) settingsFromTestRequest(ctx context.Context, request TestRequest) (Settings, error) {
	return s.settingsFromUpdateRequest(ctx, UpdateSettingsRequest{
		Enabled:    request.Enabled,
		WebhookURL: request.WebhookURL,
		Events:     request.Events,
		Channels:   request.Channels,
	})
}

func (s *Service) inspectPostUpdateFailures(ctx context.Context, job store.Job) ([]PostUpdateFailure, error) {
	if s.stackInspector == nil {
		return nil, nil
	}

	targets := map[string]struct{}{}
	if job.Workflow != nil {
		for _, step := range job.Workflow.Steps {
			if step.TargetStackID != "" {
				targets[step.TargetStackID] = struct{}{}
			}
		}
	}
	if len(targets) == 0 && job.StackID != "" {
		targets[job.StackID] = struct{}{}
	}
	if len(targets) == 0 {
		return nil, nil
	}

	stackIDs := make([]string, 0, len(targets))
	for stackID := range targets {
		stackIDs = append(stackIDs, stackID)
	}
	sort.Strings(stackIDs)

	failures := make([]PostUpdateFailure, 0)
	for _, stackID := range stackIDs {
		response, err := s.stackInspector.Get(ctx, stackID)
		if err != nil {
			failures = append(failures, PostUpdateFailure{
				StackID: stackID,
				Reason:  "stack_lookup_failed",
			})
			continue
		}

		runningCount := 0
		for _, container := range response.Stack.Containers {
			if container.Status == "running" {
				runningCount++
			}
		}

		if response.Stack.RuntimeState == stacks.RuntimeStateRunning && response.Stack.HealthSummary.UnhealthyContainerCount == 0 {
			continue
		}

		failures = append(failures, PostUpdateFailure{
			StackID:                 response.Stack.ID,
			RuntimeState:            string(response.Stack.RuntimeState),
			DisplayState:            string(response.Stack.DisplayState),
			UnhealthyContainerCount: response.Stack.HealthSummary.UnhealthyContainerCount,
			RunningContainerCount:   runningCount,
			TotalContainerCount:     len(response.Stack.Containers),
			Reason:                  "stack_not_healthy_after_update",
		})
	}

	return failures, nil
}

func buildTelegramText(payload WebhookPayload) string {
	lines := []string{payload.Summary}
	if payload.Job != nil {
		lines = append(lines, fmt.Sprintf("Action: %s", payload.Job.Action))
		if payload.Job.StackID != nil {
			lines = append(lines, fmt.Sprintf("Stack: %s", *payload.Job.StackID))
		}
		lines = append(lines, fmt.Sprintf("State: %s", payload.Job.State))
		if payload.Job.ErrorMessage != "" {
			lines = append(lines, fmt.Sprintf("Error: %s", payload.Job.ErrorMessage))
		}
	}
	if payload.WarningCount > 0 {
		lines = append(lines, fmt.Sprintf("Warnings: %d", payload.WarningCount))
	}
	if payload.PostUpdate != nil && len(payload.PostUpdate.FailedStacks) > 0 {
		lines = append(lines, "Failed stacks:")
		for _, failed := range payload.PostUpdate.FailedStacks {
			reason := failed.RuntimeState
			if reason == "" {
				reason = failed.Reason
			}
			lines = append(lines, fmt.Sprintf("- %s (%s)", failed.StackID, reason))
		}
	}
	if payload.StacklabService != nil {
		lines = append(lines, fmt.Sprintf("Entries: %d", payload.StacklabService.EntryCount))
		if len(payload.StacklabService.SampleMessages) > 0 {
			lines = append(lines, "Samples:")
			for _, message := range payload.StacklabService.SampleMessages {
				lines = append(lines, fmt.Sprintf("- %s", message))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func hasConfiguredNotificationChannel(settings Settings) bool {
	if settings.Channels.Webhook.Enabled && settings.Channels.Webhook.URL != "" {
		return true
	}
	if settings.Channels.Telegram.Enabled && settings.Channels.Telegram.BotToken != "" && settings.Channels.Telegram.ChatID != "" {
		return true
	}
	return false
}

func (s *Service) runStacklabSelfHealthLoop(ctx context.Context) {
	if err := s.pollStacklabServiceErrors(ctx); err != nil && s.logger != nil {
		s.logger.Debug("stacklab self-health poll failed", slog.String("err", err.Error()))
	}

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.pollStacklabServiceErrors(ctx); err != nil && s.logger != nil {
				s.logger.Debug("stacklab self-health poll failed", slog.String("err", err.Error()))
			}
		}
	}
}

func (s *Service) pollStacklabServiceErrors(ctx context.Context) error {
	if s.stacklabLogs == nil {
		return nil
	}

	state, err := s.loadStacklabJournalState(ctx)
	if err != nil {
		return err
	}

	if state.Cursor == "" {
		latest, err := s.stacklabLogs.StacklabLogs(ctx, hostinfo.LogsQuery{Limit: 1})
		if err != nil {
			if errors.Is(err, hostinfo.ErrLogsUnavailable) {
				return nil
			}
			return err
		}
		if len(latest.Items) == 0 {
			return nil
		}
		state.Cursor = latest.Items[len(latest.Items)-1].Cursor
		return s.saveStacklabJournalState(ctx, state)
	}

	entries, nextCursor, err := s.fetchNewStacklabLogEntries(ctx, state.Cursor)
	if err != nil {
		if errors.Is(err, hostinfo.ErrLogsUnavailable) {
			return nil
		}
		return err
	}
	if nextCursor != "" {
		state.Cursor = nextCursor
	}
	if len(entries) == 0 {
		return s.saveStacklabJournalState(ctx, state)
	}

	errorEntries := filterStacklabErrorEntries(entries)
	if len(errorEntries) == 0 {
		return s.saveStacklabJournalState(ctx, state)
	}

	settings, err := s.loadSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Events.StacklabServiceError {
		return s.saveStacklabJournalState(ctx, state)
	}
	if !hasConfiguredNotificationChannel(settings) {
		return s.saveStacklabJournalState(ctx, state)
	}

	fingerprint := stacklabErrorFingerprint(errorEntries)
	now := s.now()
	if state.LastFingerprint == fingerprint && !state.LastNotifiedAt.IsZero() && now.Sub(state.LastNotifiedAt) < s.alertCooldown {
		return s.saveStacklabJournalState(ctx, state)
	}

	payload := buildStacklabServiceErrorPayload(errorEntries, nextCursor, int(s.alertCooldown.Seconds()), now)
	if err := s.dispatchPayload(ctx, settings, payload); err != nil && s.logger != nil {
		s.logger.Warn("dispatch stacklab self-health notification failed", slog.String("err", err.Error()))
	}

	state.LastFingerprint = fingerprint
	state.LastNotifiedAt = now
	return s.saveStacklabJournalState(ctx, state)
}

func (s *Service) fetchNewStacklabLogEntries(ctx context.Context, cursor string) ([]hostinfo.StacklabLogEntry, string, error) {
	currentCursor := cursor
	entries := make([]hostinfo.StacklabLogEntry, 0)

	for range stacklabJournalMaxBatchFetch {
		response, err := s.stacklabLogs.StacklabLogs(ctx, hostinfo.LogsQuery{
			Limit:  stacklabJournalPollLimit,
			Cursor: currentCursor,
		})
		if err != nil {
			return nil, currentCursor, err
		}
		if len(response.Items) == 0 {
			break
		}
		entries = append(entries, response.Items...)
		currentCursor = response.Items[len(response.Items)-1].Cursor
		if !response.HasMore {
			break
		}
	}

	return entries, currentCursor, nil
}

func filterStacklabErrorEntries(entries []hostinfo.StacklabLogEntry) []hostinfo.StacklabLogEntry {
	filtered := make([]hostinfo.StacklabLogEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Level == "error" {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func stacklabErrorFingerprint(entries []hostinfo.StacklabLogEntry) string {
	counts := map[string]int{}
	for _, entry := range entries {
		message := normalizeStacklabErrorMessage(entry.Message)
		if message == "" {
			continue
		}
		counts[message]++
	}
	keys := make([]string, 0, len(counts))
	for message, count := range counts {
		keys = append(keys, fmt.Sprintf("%s#%d", message, count))
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}

func normalizeStacklabErrorMessage(message string) string {
	normalized := strings.TrimSpace(strings.ToLower(message))
	normalized = strings.Join(strings.Fields(normalized), " ")
	return normalized
}

func buildStacklabServiceErrorPayload(entries []hostinfo.StacklabLogEntry, nextCursor string, cooldownSeconds int, sentAt time.Time) WebhookPayload {
	samples := summarizeStacklabErrorMessages(entries)
	firstTimestamp := entries[0].Timestamp
	lastTimestamp := entries[len(entries)-1].Timestamp
	return WebhookPayload{
		Event:   "stacklab_service_error",
		SentAt:  sentAt,
		Source:  "stacklab",
		Summary: fmt.Sprintf("Stacklab service logged %d new errors", len(entries)),
		StacklabService: &StacklabServiceAlert{
			EntryCount:      len(entries),
			FirstTimestamp:  firstTimestamp,
			LastTimestamp:   lastTimestamp,
			SampleMessages:  samples,
			LatestCursor:    nextCursor,
			CooldownSeconds: cooldownSeconds,
		},
	}
}

func summarizeStacklabErrorMessages(entries []hostinfo.StacklabLogEntry) []string {
	counts := map[string]int{}
	for _, entry := range entries {
		message := strings.TrimSpace(entry.Message)
		if message == "" {
			continue
		}
		counts[message]++
	}
	type messageCount struct {
		message string
		count   int
	}
	summary := make([]messageCount, 0, len(counts))
	for message, count := range counts {
		summary = append(summary, messageCount{message: message, count: count})
	}
	sort.Slice(summary, func(i, j int) bool {
		if summary[i].count == summary[j].count {
			return summary[i].message < summary[j].message
		}
		return summary[i].count > summary[j].count
	})
	limit := 3
	if len(summary) < limit {
		limit = len(summary)
	}
	out := make([]string, 0, limit)
	for _, item := range summary[:limit] {
		if item.count > 1 {
			out = append(out, fmt.Sprintf("%s (x%d)", item.message, item.count))
			continue
		}
		out = append(out, item.message)
	}
	return out
}

func (s *Service) loadStacklabJournalState(ctx context.Context) (stacklabJournalState, error) {
	raw, ok, err := s.store.AppSetting(ctx, stacklabJournalStateKey)
	if err != nil {
		return stacklabJournalState{}, err
	}
	if !ok {
		return stacklabJournalState{}, nil
	}
	var state stacklabJournalState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return stacklabJournalState{}, fmt.Errorf("parse stacklab journal state: %w", err)
	}
	return state, nil
}

func (s *Service) saveStacklabJournalState(ctx context.Context, state stacklabJournalState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal stacklab journal state: %w", err)
	}
	return s.store.SetAppSetting(ctx, stacklabJournalStateKey, string(payload), s.now())
}
