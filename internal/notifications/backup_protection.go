package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"stacklab/internal/store"
)

// BackupSummary contains identities and names, never repository addresses or credentials.
type BackupSummary struct {
	PauseReasons    []string   `json:"pause_reasons,omitempty"`
	PlanID          string     `json:"plan_id,omitempty"`
	PlanName        string     `json:"plan_name,omitempty"`
	DestinationID   string     `json:"destination_id,omitempty"`
	DestinationName string     `json:"destination_name,omitempty"`
	Destinations    []string   `json:"destinations,omitempty"`
	DueAt           *time.Time `json:"due_at,omitempty"`
}

func (s *Service) DispatchBackupAlert(ctx context.Context, planID, planName, destinationID, destinationName string, due time.Time) error {
	return s.DispatchBackupProtection(ctx, "backup_copy_overdue", BackupSummary{PlanID: planID, PlanName: planName, DestinationID: destinationID, DestinationName: destinationName, DueAt: &due})
}

// Protection transitions share the existing backup failure notification channel.
// Only stable reason codes and display identities are sent, never raw errors,
// heartbeat URLs, helper tokens or repository credentials.
func (s *Service) DispatchBackupProtection(ctx context.Context, event string, backup BackupSummary) error {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Events.JobFailed || !hasConfiguredNotificationChannel(settings) {
		return errors.New("backup protection notifications are not enabled")
	}
	var summary string
	switch event {
	case "backup_copy_overdue":
		if backup.DueAt == nil {
			return errors.New("backup overdue notification requires a due time")
		}
		summary = fmt.Sprintf("Backup copy overdue: %s → %s (due %s)", backup.PlanName, backup.DestinationName, backup.DueAt.Format(time.RFC3339))
	case "backup_copy_resolved":
		summary = fmt.Sprintf("Backup protection restored: %s → %s has a fresh, usable copy.", backup.PlanName, backup.DestinationName)
	case "backup_protection_paused":
		summary = "Automatic backup and retention are paused: " + strings.Join(backup.PauseReasons, ", ") + ". Review Backup settings and Service recovery; protection monitoring continues."
	case "backup_protection_resumed":
		summary = "Automatic backup and retention have resumed. Copy freshness is monitored separately."
	default:
		return errors.New("unsupported backup protection notification")
	}
	return s.dispatchPayload(ctx, settings, WebhookPayload{Event: event, SentAt: s.now(), Source: "stacklab", Summary: summary, Backup: &backup})
}
func (s *Service) backupJobSummary(ctx context.Context, job store.Job) *BackupSummary {
	raw, found, err := s.store.AppSetting(ctx, "backup.v1")
	if err != nil || !found {
		return nil
	}
	var state struct {
		Runs map[string]struct {
			JobID    string `json:"job_id"`
			PlanID   string `json:"plan_id"`
			PlanName string `json:"plan_name"`
			Copies   []struct {
				DestinationID string `json:"destination_id"`
			} `json:"copies"`
		} `json:"runs"`
		Destinations map[string]struct {
			Name string `json:"name"`
		} `json:"destinations"`
	}
	if json.Unmarshal([]byte(raw), &state) != nil {
		return nil
	}
	for _, run := range state.Runs {
		if run.JobID != job.ID {
			continue
		}
		result := &BackupSummary{PlanID: run.PlanID, PlanName: run.PlanName}
		for _, copy := range run.Copies {
			if d, ok := state.Destinations[copy.DestinationID]; ok {
				result.Destinations = append(result.Destinations, d.Name)
			}
		}
		return result
	}
	return nil
}
