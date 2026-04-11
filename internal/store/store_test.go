package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestListAuditEntriesPaginatesAndFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	baseTime := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-1",
		StackID:     stringPtr("alpha"),
		Action:      "up",
		RequestedBy: "local",
		Result:      "succeeded",
		RequestedAt: baseTime.Add(1 * time.Minute),
		TargetType:  "stack",
		TargetID:    stringPtr("alpha"),
	})
	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-2",
		StackID:     stringPtr("beta"),
		Action:      "pull",
		RequestedBy: "local",
		Result:      "failed",
		RequestedAt: baseTime.Add(2 * time.Minute),
		TargetType:  "stack",
		TargetID:    stringPtr("beta"),
	})
	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-3",
		StackID:     stringPtr("alpha"),
		Action:      "restart",
		RequestedBy: "local",
		Result:      "succeeded",
		RequestedAt: baseTime.Add(3 * time.Minute),
		TargetType:  "stack",
		TargetID:    stringPtr("alpha"),
	})

	firstPage, err := testStore.ListAuditEntries(ctx, AuditQuery{Limit: 2})
	if err != nil {
		t.Fatalf("ListAuditEntries(first page) error = %v", err)
	}
	if len(firstPage.Items) != 2 {
		t.Fatalf("ListAuditEntries(first page) len = %d, want 2", len(firstPage.Items))
	}
	if firstPage.Items[0].ID != "audit-3" || firstPage.Items[1].ID != "audit-2" {
		t.Fatalf("unexpected first page order: got %q, %q", firstPage.Items[0].ID, firstPage.Items[1].ID)
	}
	if firstPage.NextCursor == nil || *firstPage.NextCursor == "" {
		t.Fatalf("expected non-empty next cursor")
	}

	secondPage, err := testStore.ListAuditEntries(ctx, AuditQuery{Limit: 2, Cursor: *firstPage.NextCursor})
	if err != nil {
		t.Fatalf("ListAuditEntries(second page) error = %v", err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID != "audit-1" {
		t.Fatalf("unexpected second page items: %#v", secondPage.Items)
	}

	alphaOnly, err := testStore.ListAuditEntries(ctx, AuditQuery{StackID: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("ListAuditEntries(alpha) error = %v", err)
	}
	if len(alphaOnly.Items) != 2 {
		t.Fatalf("ListAuditEntries(alpha) len = %d, want 2", len(alphaOnly.Items))
	}
	for _, item := range alphaOnly.Items {
		if item.StackID == nil || *item.StackID != "alpha" {
			t.Fatalf("expected alpha-only filter, got %#v", item.StackID)
		}
	}
}

func TestLatestAuditEntriesByStackIDsReturnsNewestPerStack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	baseTime := time.Date(2026, 4, 3, 13, 0, 0, 0, time.UTC)

	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-alpha-old",
		StackID:     stringPtr("alpha"),
		Action:      "pull",
		RequestedBy: "local",
		Result:      "failed",
		RequestedAt: baseTime,
		TargetType:  "stack",
		TargetID:    stringPtr("alpha"),
	})
	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-alpha-new",
		StackID:     stringPtr("alpha"),
		Action:      "up",
		RequestedBy: "local",
		Result:      "succeeded",
		RequestedAt: baseTime.Add(2 * time.Minute),
		TargetType:  "stack",
		TargetID:    stringPtr("alpha"),
	})
	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-beta",
		StackID:     stringPtr("beta"),
		Action:      "restart",
		RequestedBy: "local",
		Result:      "succeeded",
		RequestedAt: baseTime.Add(1 * time.Minute),
		TargetType:  "stack",
		TargetID:    stringPtr("beta"),
	})

	lastEntries, err := testStore.LatestAuditEntriesByStackIDs(ctx, []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("LatestAuditEntriesByStackIDs() error = %v", err)
	}
	if len(lastEntries) != 2 {
		t.Fatalf("LatestAuditEntriesByStackIDs() len = %d, want 2", len(lastEntries))
	}
	if entry := lastEntries["alpha"]; entry.ID != "audit-alpha-new" {
		t.Fatalf("latest alpha audit id = %q, want %q", entry.ID, "audit-alpha-new")
	}
	if entry := lastEntries["beta"]; entry.ID != "audit-beta" {
		t.Fatalf("latest beta audit id = %q, want %q", entry.ID, "audit-beta")
	}
}

func TestPruneOperationalData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	now := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	policy := OperationalRetentionPolicy{
		AuditEntryRetention:     180 * 24 * time.Hour,
		JobRetention:            180 * 24 * time.Hour,
		JobEventRetention:       14 * 24 * time.Hour,
		ExpiredSessionRetention: 7 * 24 * time.Hour,
	}

	oldExpiredSession := Session{
		ID:         "session-old-expired",
		UserID:     "local",
		CreatedAt:  now.Add(-10 * 24 * time.Hour),
		LastSeenAt: now.Add(-10 * 24 * time.Hour),
		ExpiresAt:  now.Add(-8 * 24 * time.Hour),
	}
	recentExpiredSession := Session{
		ID:         "session-recent-expired",
		UserID:     "local",
		CreatedAt:  now.Add(-5 * 24 * time.Hour),
		LastSeenAt: now.Add(-5 * 24 * time.Hour),
		ExpiresAt:  now.Add(-3 * 24 * time.Hour),
	}
	oldRevokedSession := Session{
		ID:         "session-old-revoked",
		UserID:     "local",
		CreatedAt:  now.Add(-10 * 24 * time.Hour),
		LastSeenAt: now.Add(-10 * 24 * time.Hour),
		ExpiresAt:  now.Add(10 * 24 * time.Hour),
	}
	activeSession := Session{
		ID:         "session-active",
		UserID:     "local",
		CreatedAt:  now.Add(-1 * time.Hour),
		LastSeenAt: now.Add(-1 * time.Hour),
		ExpiresAt:  now.Add(12 * time.Hour),
	}
	for _, session := range []Session{oldExpiredSession, recentExpiredSession, oldRevokedSession, activeSession} {
		if err := testStore.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession(%s) error = %v", session.ID, err)
		}
	}
	if err := testStore.RevokeSession(ctx, oldRevokedSession.ID, now.Add(-8*24*time.Hour)); err != nil {
		t.Fatalf("RevokeSession(old) error = %v", err)
	}

	oldDetailJob := insertJob(t, testStore, Job{
		ID:          "job-old-detail",
		StackID:     "demo",
		Action:      "pull",
		State:       "succeeded",
		RequestedBy: "local",
		RequestedAt: now.Add(-20 * 24 * time.Hour),
		FinishedAt:  timePtr(now.Add(-20 * 24 * time.Hour)),
	})
	activeOldJob := insertJob(t, testStore, Job{
		ID:          "job-active-old",
		StackID:     "demo",
		Action:      "up",
		State:       "running",
		RequestedBy: "local",
		RequestedAt: now.Add(-20 * 24 * time.Hour),
		StartedAt:   timePtr(now.Add(-20 * 24 * time.Hour)),
	})
	staleJob := insertJob(t, testStore, Job{
		ID:          "job-stale",
		StackID:     "demo",
		Action:      "restart",
		State:       "failed",
		RequestedBy: "local",
		RequestedAt: now.Add(-181 * 24 * time.Hour),
		FinishedAt:  timePtr(now.Add(-181 * 24 * time.Hour)),
	})
	linkedJob := insertJob(t, testStore, Job{
		ID:          "job-linked",
		StackID:     "demo",
		Action:      "update",
		State:       "succeeded",
		RequestedBy: "local",
		RequestedAt: now.Add(-181 * 24 * time.Hour),
		FinishedAt:  timePtr(now.Add(-181 * 24 * time.Hour)),
	})

	insertJobEvent(t, testStore, JobEvent{JobID: oldDetailJob.ID, Sequence: 1, Event: "job_log", State: "succeeded", Timestamp: now.Add(-15 * 24 * time.Hour)})
	insertJobEvent(t, testStore, JobEvent{JobID: activeOldJob.ID, Sequence: 1, Event: "job_log", State: "running", Timestamp: now.Add(-15 * 24 * time.Hour)})
	insertJobEvent(t, testStore, JobEvent{JobID: staleJob.ID, Sequence: 1, Event: "job_log", State: "failed", Timestamp: now.Add(-181 * 24 * time.Hour)})
	insertJobEvent(t, testStore, JobEvent{JobID: linkedJob.ID, Sequence: 1, Event: "job_log", State: "succeeded", Timestamp: now.Add(-181 * 24 * time.Hour)})

	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-old",
		StackID:     stringPtr("demo"),
		JobID:       stringPtr(staleJob.ID),
		Action:      "restart",
		RequestedBy: "local",
		Result:      "failed",
		RequestedAt: now.Add(-181 * 24 * time.Hour),
		TargetType:  "stack",
		TargetID:    stringPtr("demo"),
	})
	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-recent-linked",
		StackID:     stringPtr("demo"),
		JobID:       stringPtr(linkedJob.ID),
		Action:      "update",
		RequestedBy: "local",
		Result:      "succeeded",
		RequestedAt: now.Add(-1 * 24 * time.Hour),
		TargetType:  "stack",
		TargetID:    stringPtr("demo"),
	})

	summary, err := testStore.PruneOperationalData(ctx, now, policy)
	if err != nil {
		t.Fatalf("PruneOperationalData() error = %v", err)
	}
	if summary.SessionsDeleted != 2 {
		t.Fatalf("SessionsDeleted = %d, want 2", summary.SessionsDeleted)
	}
	if summary.JobEventsDeleted != 3 {
		t.Fatalf("JobEventsDeleted = %d, want 3", summary.JobEventsDeleted)
	}
	if summary.JobsDeleted != 1 {
		t.Fatalf("JobsDeleted = %d, want 1", summary.JobsDeleted)
	}
	if summary.AuditEntriesDeleted != 1 {
		t.Fatalf("AuditEntriesDeleted = %d, want 1", summary.AuditEntriesDeleted)
	}

	assertSessionMissing(t, testStore, oldExpiredSession.ID)
	assertSessionExists(t, testStore, recentExpiredSession.ID)
	assertSessionMissing(t, testStore, oldRevokedSession.ID)
	assertSessionExists(t, testStore, activeSession.ID)

	assertJobExists(t, testStore, oldDetailJob.ID)
	assertJobExists(t, testStore, activeOldJob.ID)
	assertJobMissing(t, testStore, staleJob.ID)
	assertJobExists(t, testStore, linkedJob.ID)

	assertJobEventsLen(t, testStore, oldDetailJob.ID, 0)
	assertJobEventsLen(t, testStore, activeOldJob.ID, 1)
	assertJobEventsLen(t, testStore, staleJob.ID, 0)
	assertJobEventsLen(t, testStore, linkedJob.ID, 0)

	auditEntries, err := testStore.ListAuditEntries(ctx, AuditQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListAuditEntries() error = %v", err)
	}
	if len(auditEntries.Items) != 1 || auditEntries.Items[0].ID != "audit-recent-linked" {
		t.Fatalf("unexpected audit entries after prune: %#v", auditEntries.Items)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "stacklab.db")
	testStore, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := testStore.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	return testStore
}

func insertJob(t *testing.T, testStore *Store, job Job) Job {
	t.Helper()

	if err := testStore.CreateJob(context.Background(), job); err != nil {
		t.Fatalf("CreateJob(%s) error = %v", job.ID, err)
	}
	return job
}

func insertJobEvent(t *testing.T, testStore *Store, event JobEvent) {
	t.Helper()

	if err := testStore.CreateJobEvent(context.Background(), event); err != nil {
		t.Fatalf("CreateJobEvent(%s/%d) error = %v", event.JobID, event.Sequence, err)
	}
}

func insertAuditEntry(t *testing.T, testStore *Store, entry AuditEntry) {
	t.Helper()

	if err := testStore.CreateAuditEntry(context.Background(), entry); err != nil {
		t.Fatalf("CreateAuditEntry() error = %v", err)
	}
}

func assertSessionExists(t *testing.T, testStore *Store, id string) {
	t.Helper()

	if _, err := testStore.SessionByID(context.Background(), id); err != nil {
		t.Fatalf("SessionByID(%s) error = %v", id, err)
	}
}

func assertSessionMissing(t *testing.T, testStore *Store, id string) {
	t.Helper()

	_, err := testStore.SessionByID(context.Background(), id)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionByID(%s) error = %v, want ErrNotFound", id, err)
	}
}

func assertJobExists(t *testing.T, testStore *Store, id string) {
	t.Helper()

	if _, err := testStore.JobByID(context.Background(), id); err != nil {
		t.Fatalf("JobByID(%s) error = %v", id, err)
	}
}

func assertJobMissing(t *testing.T, testStore *Store, id string) {
	t.Helper()

	_, err := testStore.JobByID(context.Background(), id)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("JobByID(%s) error = %v, want ErrNotFound", id, err)
	}
}

func assertJobEventsLen(t *testing.T, testStore *Store, jobID string, want int) {
	t.Helper()

	events, err := testStore.ListJobEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("ListJobEvents(%s) error = %v", jobID, err)
	}
	if len(events) != want {
		t.Fatalf("ListJobEvents(%s) len = %d, want %d", jobID, len(events), want)
	}
}

func stringPtr(value string) *string {
	return &value
}

func timePtr(value time.Time) *time.Time {
	return &value
}
