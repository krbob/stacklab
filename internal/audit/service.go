package audit

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

type Service struct {
	store *store.Store
}

func NewService(auditStore *store.Store) *Service {
	return &Service{store: auditStore}
}

func (s *Service) RecordStackJob(ctx context.Context, job store.Job) error {
	var durationMS *int
	if job.StartedAt != nil && job.FinishedAt != nil {
		duration := int(job.FinishedAt.Sub(*job.StartedAt).Milliseconds())
		durationMS = &duration
	}

	details := map[string]any{}
	if job.ErrorCode != "" {
		details["error_code"] = job.ErrorCode
	}
	if job.ErrorMessage != "" {
		details["error_message"] = job.ErrorMessage
	}

	detailJSON, err := marshalDetails(details)
	if err != nil {
		return err
	}

	stackID := job.StackID
	jobID := job.ID
	targetType := "stack"
	targetID := job.StackID

	return s.store.CreateAuditEntry(ctx, store.AuditEntry{
		ID:          "audit_" + randomToken(18),
		StackID:     &stackID,
		JobID:       &jobID,
		Action:      job.Action,
		RequestedBy: fallback(job.RequestedBy, "local"),
		Result:      job.State,
		RequestedAt: job.RequestedAt,
		FinishedAt:  job.FinishedAt,
		DurationMS:  durationMS,
		TargetType:  targetType,
		TargetID:    &targetID,
		DetailJSON:  detailJSON,
	})
}

func (s *Service) RecordSystemEvent(ctx context.Context, action, requestedBy, result string, requestedAt time.Time, finishedAt *time.Time, details map[string]any) error {
	var durationMS *int
	if finishedAt != nil {
		duration := int(finishedAt.Sub(requestedAt).Milliseconds())
		durationMS = &duration
	}

	detailJSON, err := marshalDetails(details)
	if err != nil {
		return err
	}

	targetType := "system"

	return s.store.CreateAuditEntry(ctx, store.AuditEntry{
		ID:          "audit_" + randomToken(18),
		Action:      action,
		RequestedBy: fallback(requestedBy, "local"),
		Result:      result,
		RequestedAt: requestedAt,
		FinishedAt:  finishedAt,
		DurationMS:  durationMS,
		TargetType:  targetType,
		DetailJSON:  detailJSON,
	})
}

func (s *Service) RecordTerminalEvent(ctx context.Context, stackID, sessionID, containerID, requestedBy, action, result string, details map[string]any) error {
	detailJSON, err := marshalDetails(details)
	if err != nil {
		return err
	}

	requestedAt := time.Now().UTC()
	stackIDValue := stackID
	targetID := sessionID

	return s.store.CreateAuditEntry(ctx, store.AuditEntry{
		ID:          "audit_" + randomToken(18),
		StackID:     &stackIDValue,
		Action:      action,
		RequestedBy: fallback(requestedBy, "local"),
		Result:      result,
		RequestedAt: requestedAt,
		FinishedAt:  &requestedAt,
		TargetType:  "terminal_session",
		TargetID:    &targetID,
		DetailJSON:  detailJSON,
	})
}

func (s *Service) List(ctx context.Context, stackID, cursor string, limit int) (store.AuditListResult, error) {
	return s.store.ListAuditEntries(ctx, store.AuditQuery{
		StackID: stackID,
		Cursor:  cursor,
		Limit:   limit,
	})
}

func (s *Service) LastActions(ctx context.Context, stackIDs []string) (map[string]*stacks.LastAction, error) {
	entries, err := s.store.LatestAuditEntriesByStackIDs(ctx, stackIDs)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*stacks.LastAction, len(entries))
	for stackID, entry := range entries {
		if entry.FinishedAt == nil {
			continue
		}
		result[stackID] = &stacks.LastAction{
			Action:     entry.Action,
			Result:     entry.Result,
			FinishedAt: *entry.FinishedAt,
		}
	}

	return result, nil
}

func marshalDetails(details map[string]any) (*string, error) {
	if len(details) == 0 {
		return nil, nil
	}

	encoded, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("marshal audit details: %w", err)
	}

	value := string(encoded)
	return &value, nil
}

func fallback(value, fallbackValue string) string {
	if value == "" {
		return fallbackValue
	}
	return value
}

func randomToken(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}
