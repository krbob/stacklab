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
	"strings"
	"time"

	"stacklab/internal/store"
)

const settingsKey = "notifications_webhook_v1"

var ErrInvalidConfig = errors.New("invalid notification config")

type Store interface {
	AppSetting(ctx context.Context, key string) (string, bool, error)
	SetAppSetting(ctx context.Context, key, valueJSON string, updatedAt time.Time) error
	ListJobEvents(ctx context.Context, jobID string) ([]store.JobEvent, error)
}

type webhookSender func(ctx context.Context, target string, payload WebhookPayload) error

type EventToggles struct {
	JobFailed                bool `json:"job_failed"`
	JobSucceededWithWarnings bool `json:"job_succeeded_with_warnings"`
	MaintenanceSucceeded     bool `json:"maintenance_succeeded"`
}

type Settings struct {
	Enabled    bool         `json:"enabled"`
	WebhookURL string       `json:"webhook_url"`
	Events     EventToggles `json:"events"`
}

type SettingsResponse struct {
	Enabled    bool         `json:"enabled"`
	Configured bool         `json:"configured"`
	WebhookURL string       `json:"webhook_url"`
	Events     EventToggles `json:"events"`
}

type UpdateSettingsRequest struct {
	Enabled    bool         `json:"enabled"`
	WebhookURL string       `json:"webhook_url"`
	Events     EventToggles `json:"events"`
}

type TestRequest struct {
	Enabled    bool         `json:"enabled"`
	WebhookURL string       `json:"webhook_url"`
	Events     EventToggles `json:"events"`
}

type TestResponse struct {
	Sent bool `json:"sent"`
}

type WebhookPayload struct {
	Event        string      `json:"event"`
	SentAt       time.Time   `json:"sent_at"`
	Source       string      `json:"source"`
	Summary      string      `json:"summary"`
	Job          *WebhookJob `json:"job,omitempty"`
	WarningCount int         `json:"warning_count,omitempty"`
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

type Service struct {
	store  Store
	logger *slog.Logger
	send   webhookSender
	now    func() time.Time
}

func NewService(settingStore Store, logger *slog.Logger) *Service {
	client := &http.Client{Timeout: 5 * time.Second}
	return &Service{
		store:  settingStore,
		logger: logger,
		send: func(ctx context.Context, target string, payload WebhookPayload) error {
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
		now: time.Now().UTC,
	}
}

func (s *Service) GetSettings(ctx context.Context) (SettingsResponse, error) {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}
	return settingsResponse(settings), nil
}

func (s *Service) UpdateSettings(ctx context.Context, request UpdateSettingsRequest) (SettingsResponse, error) {
	settings := Settings{
		Enabled:    request.Enabled,
		WebhookURL: strings.TrimSpace(request.WebhookURL),
		Events:     request.Events,
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
	settings := Settings{
		Enabled:    request.Enabled,
		WebhookURL: strings.TrimSpace(request.WebhookURL),
		Events:     request.Events,
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
	if err := s.send(ctx, settings.WebhookURL, payload); err != nil {
		return TestResponse{}, err
	}
	return TestResponse{Sent: true}, nil
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
	if !settings.Enabled || settings.WebhookURL == "" {
		return nil
	}

	payload, ok, err := s.buildJobPayload(ctx, job, settings)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return s.send(ctx, settings.WebhookURL, payload)
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
	switch {
	case job.State == "failed" && settings.Events.JobFailed:
		eventType = "job_failed"
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
		return defaultSettings(), nil
	}
	settings := defaultSettings()
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return Settings{}, fmt.Errorf("parse notification settings: %w", err)
	}
	if settings.WebhookURL != "" {
		settings.WebhookURL = strings.TrimSpace(settings.WebhookURL)
	}
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
		Enabled:    false,
		WebhookURL: "",
		Events: EventToggles{
			JobFailed:                true,
			JobSucceededWithWarnings: true,
			MaintenanceSucceeded:     false,
		},
	}
}

func settingsResponse(settings Settings) SettingsResponse {
	return SettingsResponse{
		Enabled:    settings.Enabled,
		Configured: settings.WebhookURL != "",
		WebhookURL: settings.WebhookURL,
		Events:     settings.Events,
	}
}

func validateSettings(settings Settings) error {
	if !settings.Enabled && strings.TrimSpace(settings.WebhookURL) == "" {
		return nil
	}
	if settings.WebhookURL == "" {
		return fmt.Errorf("%w: webhook_url is required", ErrInvalidConfig)
	}
	parsed, err := url.Parse(settings.WebhookURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: webhook_url must be a valid absolute URL", ErrInvalidConfig)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: webhook_url must use http or https", ErrInvalidConfig)
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
	default:
		return "Stacklab notification"
	}
}
