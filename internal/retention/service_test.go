package retention

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"stacklab/internal/store"
)

func TestRunOnceAppliesDefaultSessionRetention(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	retentionStore := openRetentionStore(t)
	service := NewService(retentionStore, nil)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	createRetentionSession(t, retentionStore, "expired-old", now.Add(-8*24*time.Hour))
	createRetentionSession(t, retentionStore, "expired-recent", now.Add(-6*24*time.Hour))
	createRetentionSession(t, retentionStore, "active", now.Add(time.Hour))

	summary, err := service.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if summary.SessionsDeleted != 1 || summary.TotalDeleted() != 1 {
		t.Fatalf("RunOnce() summary = %#v, want one expired session", summary)
	}
	if _, err := retentionStore.SessionByID(ctx, "expired-old"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SessionByID(expired-old) error = %v, want ErrNotFound", err)
	}
	for _, sessionID := range []string{"expired-recent", "active"} {
		if _, err := retentionStore.SessionByID(ctx, sessionID); err != nil {
			t.Fatalf("SessionByID(%s) error = %v", sessionID, err)
		}
	}
}

func TestRunAndLogReportsDeletionAndFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	retentionStore := openRetentionStore(t)
	handler := newRetentionLogHandler()
	service := NewService(retentionStore, slog.New(handler))
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	createRetentionSession(t, retentionStore, "expired", now.Add(-8*24*time.Hour))

	service.runAndLog(ctx)
	record := receiveRetentionLog(t, handler.records)
	if record.Level != slog.LevelInfo || record.Message != "operational data retention cleanup completed" {
		t.Fatalf("cleanup log = level %s message %q", record.Level, record.Message)
	}
	attributes := retentionLogAttributes(record)
	if got := attributes["sessions_deleted"].Int64(); got != 1 {
		t.Fatalf("sessions_deleted = %d, want 1", got)
	}
	for _, name := range []string{"audit_entries_deleted", "jobs_deleted", "job_events_deleted"} {
		if got := attributes[name].Int64(); got != 0 {
			t.Errorf("%s = %d, want 0", name, got)
		}
	}

	// A successful no-op cleanup is intentionally silent.
	service.runAndLog(ctx)
	select {
	case unexpected := <-handler.records:
		t.Fatalf("no-op cleanup logged %#v", unexpected)
	default:
	}

	if err := retentionStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	service.runAndLog(ctx)
	record = receiveRetentionLog(t, handler.records)
	if record.Level != slog.LevelWarn || record.Message != "operational data retention cleanup failed" {
		t.Fatalf("failure log = level %s message %q", record.Level, record.Message)
	}
	if value, ok := retentionLogAttributes(record)["err"]; !ok || value.String() == "" {
		t.Fatalf("failure log err = %#v", value)
	}
}

func TestBackgroundCleanupRunsImmediatelyAndOnInterval(t *testing.T) {
	t.Parallel()

	retentionStore := openRetentionStore(t)
	if err := retentionStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	handler := newRetentionLogHandler()
	service := NewService(retentionStore, slog.New(handler))
	service.cleanupInterval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.RunBackground(ctx)
		close(done)
	}()

	first := receiveRetentionLog(t, handler.records)
	second := receiveRetentionLog(t, handler.records)
	if first.Level != slog.LevelWarn || second.Level != slog.LevelWarn {
		t.Fatalf("background log levels = %s, %s; want warnings", first.Level, second.Level)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunBackground() did not stop after cancellation")
	}

	// Exercise the fire-and-forget entry point with an already-cancelled
	// context; the first cleanup still runs before the loop observes Done.
	cancelledCtx, cancelImmediately := context.WithCancel(context.Background())
	cancelImmediately()
	service.StartBackground(cancelledCtx)
	if record := receiveRetentionLog(t, handler.records); record.Level != slog.LevelWarn {
		t.Fatalf("StartBackground() log level = %s, want warning", record.Level)
	}
}

func TestDefaultPolicyAndRunOnceFailure(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	if policy.AuditEntryRetention != DefaultAuditEntryRetention ||
		policy.JobRetention != DefaultJobRetention ||
		policy.JobEventRetention != DefaultJobEventRetention ||
		policy.ExpiredSessionRetention != DefaultExpiredSessionRetention {
		t.Fatalf("DefaultPolicy() = %#v", policy)
	}

	retentionStore := openRetentionStore(t)
	if err := retentionStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	service := NewService(retentionStore, nil)
	if _, err := service.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce(closed store) returned nil error")
	}
	// A nil logger must keep the background cleanup failure path safe.
	service.runAndLog(context.Background())
}

type retentionLogHandler struct {
	records chan slog.Record
}

func newRetentionLogHandler() *retentionLogHandler {
	return &retentionLogHandler{records: make(chan slog.Record, 16)}
}

func (h *retentionLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *retentionLogHandler) Handle(_ context.Context, record slog.Record) error {
	h.records <- record.Clone()
	return nil
}

func (h *retentionLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *retentionLogHandler) WithGroup(string) slog.Handler { return h }

func receiveRetentionLog(t *testing.T, records <-chan slog.Record) slog.Record {
	t.Helper()
	select {
	case record := <-records:
		return record
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for retention log")
		return slog.Record{}
	}
}

func retentionLogAttributes(record slog.Record) map[string]slog.Value {
	attributes := make(map[string]slog.Value)
	record.Attrs(func(attribute slog.Attr) bool {
		attributes[attribute.Key] = attribute.Value
		return true
	})
	return attributes
}

func createRetentionSession(t *testing.T, retentionStore *store.Store, id string, expiresAt time.Time) {
	t.Helper()
	createdAt := expiresAt.Add(-time.Hour)
	if err := retentionStore.CreateSession(context.Background(), store.Session{
		ID: id, UserID: "local", CreatedAt: createdAt, LastSeenAt: createdAt, ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatalf("CreateSession(%s) error = %v", id, err)
	}
}

func openRetentionStore(t *testing.T) *store.Store {
	t.Helper()
	retentionStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = retentionStore.Close() })
	return retentionStore
}
