package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"stacklab/internal/store"
)

// BackupSummary contains identities and names, never repository addresses or credentials.
type BackupSummary struct {
	PlanID          string     `json:"plan_id,omitempty"`
	PlanName        string     `json:"plan_name,omitempty"`
	DestinationID   string     `json:"destination_id,omitempty"`
	DestinationName string     `json:"destination_name,omitempty"`
	Destinations    []string   `json:"destinations,omitempty"`
	DueAt           *time.Time `json:"due_at,omitempty"`
}

func (s *Service) DispatchBackupAlert(ctx context.Context, planID, planName, destinationID, destinationName string, due time.Time) error {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Events.JobFailed || !hasConfiguredNotificationChannel(settings) {
		return errors.New("backup protection notifications are not enabled")
	}
	return s.dispatchPayload(ctx, settings, WebhookPayload{Event: "backup_copy_overdue", SentAt: s.now(), Source: "stacklab", Summary: fmt.Sprintf("Backup copy overdue: %s → %s (due %s)", planName, destinationName, due.Format(time.RFC3339)), Backup: &BackupSummary{PlanID: planID, PlanName: planName, DestinationID: destinationID, DestinationName: destinationName, DueAt: &due}})
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
