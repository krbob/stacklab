package retention

import (
	"context"
	"log/slog"
	"time"

	"stacklab/internal/store"
)

const (
	DefaultAuditEntryRetention     = 180 * 24 * time.Hour
	DefaultJobRetention            = 180 * 24 * time.Hour
	DefaultJobEventRetention       = 14 * 24 * time.Hour
	DefaultExpiredSessionRetention = 7 * 24 * time.Hour
	DefaultCleanupInterval         = 24 * time.Hour
)

type Service struct {
	store           *store.Store
	logger          *slog.Logger
	now             func() time.Time
	cleanupInterval time.Duration
}

func NewService(appStore *store.Store, logger *slog.Logger) *Service {
	return &Service{
		store:           appStore,
		logger:          logger,
		now:             func() time.Time { return time.Now().UTC() },
		cleanupInterval: DefaultCleanupInterval,
	}
}

func (s *Service) StartBackground(ctx context.Context) {
	go s.loop(ctx)
}

func (s *Service) RunOnce(ctx context.Context) (store.OperationalRetentionSummary, error) {
	summary, err := s.store.PruneOperationalData(ctx, s.now(), DefaultPolicy())
	if err != nil {
		return store.OperationalRetentionSummary{}, err
	}
	return summary, nil
}

func DefaultPolicy() store.OperationalRetentionPolicy {
	return store.OperationalRetentionPolicy{
		AuditEntryRetention:     DefaultAuditEntryRetention,
		JobRetention:            DefaultJobRetention,
		JobEventRetention:       DefaultJobEventRetention,
		ExpiredSessionRetention: DefaultExpiredSessionRetention,
	}
}

func (s *Service) loop(ctx context.Context) {
	s.runAndLog(ctx)

	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runAndLog(ctx)
		}
	}
}

func (s *Service) runAndLog(ctx context.Context) {
	summary, err := s.RunOnce(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("operational data retention cleanup failed", slog.String("err", err.Error()))
		}
		return
	}
	if s.logger == nil || summary.TotalDeleted() == 0 {
		return
	}
	s.logger.Info(
		"operational data retention cleanup completed",
		slog.Int64("audit_entries_deleted", summary.AuditEntriesDeleted),
		slog.Int64("jobs_deleted", summary.JobsDeleted),
		slog.Int64("job_events_deleted", summary.JobEventsDeleted),
		slog.Int64("sessions_deleted", summary.SessionsDeleted),
	)
}
