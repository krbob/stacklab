package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCreatesPrivateDatabaseFiles(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	databasePath := filepath.Join(dataDir, "stacklab.db")
	testStore, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })

	assertPathMode(t, dataDir, 0o700)
	assertPathMode(t, databasePath, 0o600)
	for _, suffix := range []string{"-wal", "-shm"} {
		path := databasePath + suffix
		if _, err := os.Stat(path); err == nil {
			assertPathMode(t, path, 0o600)
		} else if !os.IsNotExist(err) {
			t.Fatalf("Stat(%s) error = %v", path, err)
		}
	}
}

func TestEnsureDataDirectoryCreatesAndMigratesPrivateMode(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(dataDir) error = %v", err)
	}
	if err := os.Chmod(dataDir, 0o755); err != nil {
		t.Fatalf("Chmod(dataDir) error = %v", err)
	}

	if err := EnsureDataDirectory(dataDir); err != nil {
		t.Fatalf("EnsureDataDirectory() error = %v", err)
	}
	assertPathMode(t, dataDir, 0o700)
}

func TestSecureDatabaseFileModesMigratesExistingFiles(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "stacklab.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := databasePath + suffix
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("Chmod(%s) error = %v", path, err)
		}
	}

	if err := secureDatabaseFileModes(databasePath); err != nil {
		t.Fatalf("secureDatabaseFileModes() error = %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		assertPathMode(t, databasePath+suffix, 0o600)
	}
}

func TestCreateJobWithInitialEventPersistsWorkflowAtomically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	job := Job{
		ID:          "job_atomic_start",
		StackID:     "demo",
		Action:      "up",
		State:       "running",
		RequestedBy: "local",
		RequestedAt: now,
		StartedAt:   &now,
		Workflow: &JobWorkflow{Steps: []JobWorkflowStep{
			{Action: "pull", State: "running", TargetStackID: "demo"},
			{Action: "up", State: "queued", TargetStackID: "demo"},
		}},
	}
	event := JobEvent{
		JobID:     job.ID,
		Sequence:  1,
		Event:     "job_started",
		State:     job.State,
		Message:   "Job started.",
		Timestamp: now,
	}

	if err := testStore.CreateJobWithInitialEvent(ctx, job, event); err != nil {
		t.Fatalf("CreateJobWithInitialEvent() error = %v", err)
	}
	stored, err := testStore.JobByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("JobByID() error = %v", err)
	}
	if stored.Workflow == nil || len(stored.Workflow.Steps) != 2 || stored.Workflow.Steps[0].State != "running" {
		t.Fatalf("stored workflow = %#v", stored.Workflow)
	}
	events, err := testStore.ListJobEvents(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListJobEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Sequence != 1 || events[0].Event != "job_started" {
		t.Fatalf("stored events = %#v", events)
	}
}

func TestCreateJobWithInitialEventRollsBackOnInjectedEventFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	if _, err := testStore.db.ExecContext(ctx, `
		CREATE TRIGGER fail_initial_job_event
		BEFORE INSERT ON job_events
		WHEN NEW.job_id = 'job_injected_failure'
		BEGIN
			SELECT RAISE(FAIL, 'injected initial event failure');
		END;
	`); err != nil {
		t.Fatalf("create fault-injection trigger error = %v", err)
	}
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	job := Job{
		ID:          "job_injected_failure",
		StackID:     "demo",
		Action:      "up",
		State:       "running",
		RequestedBy: "local",
		RequestedAt: now,
		StartedAt:   &now,
		Workflow:    &JobWorkflow{Steps: []JobWorkflowStep{{Action: "up", State: "running"}}},
	}
	event := JobEvent{JobID: job.ID, Sequence: 1, Event: "job_started", State: "running", Timestamp: now}

	if err := testStore.CreateJobWithInitialEvent(ctx, job, event); err == nil {
		t.Fatal("CreateJobWithInitialEvent() error = nil, want injected error")
	}
	if _, err := testStore.JobByID(ctx, job.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("JobByID(after rollback) error = %v, want ErrNotFound", err)
	}
	events, err := testStore.ListJobEvents(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListJobEvents(after rollback) error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events after rollback = %#v, want none", events)
	}
}

func TestUpdatePasswordAndRevokeSessionsIsVersionedAndAtomic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openStoreForTest(t)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	if err := testStore.SetPasswordHash(ctx, "old-hash", now); err != nil {
		t.Fatalf("SetPasswordHash() error = %v", err)
	}
	credentials, configured, err := testStore.PasswordCredentials(ctx)
	if err != nil || !configured {
		t.Fatalf("PasswordCredentials() = %#v, %t, %v", credentials, configured, err)
	}

	session := Session{
		ID:              "session-before-password-change",
		UserID:          "local",
		CreatedAt:       now,
		LastSeenAt:      now,
		ExpiresAt:       now.Add(time.Hour),
		PasswordVersion: credentials.Version,
	}
	if err := testStore.CreateSessionAtPasswordVersion(ctx, session, credentials.Version); err != nil {
		t.Fatalf("CreateSessionAtPasswordVersion() error = %v", err)
	}

	if err := testStore.UpdatePasswordAndRevokeSessions(ctx, credentials.Version, "new-hash", now.Add(time.Minute)); err != nil {
		t.Fatalf("UpdatePasswordAndRevokeSessions() error = %v", err)
	}
	updated, configured, err := testStore.PasswordCredentials(ctx)
	if err != nil || !configured {
		t.Fatalf("PasswordCredentials(updated) = %#v, %t, %v", updated, configured, err)
	}
	if updated.Hash != "new-hash" || updated.Version != credentials.Version+1 {
		t.Fatalf("updated credentials = %#v, want new hash and version %d", updated, credentials.Version+1)
	}
	record, err := testStore.SessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionByID() error = %v", err)
	}
	if record.RevokedAt == nil {
		t.Fatal("session was not revoked in password update transaction")
	}
	if _, err := testStore.SessionAtCurrentPasswordVersion(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionAtCurrentPasswordVersion(old session) error = %v, want ErrNotFound", err)
	}

	if err := testStore.UpdatePasswordAndRevokeSessions(ctx, credentials.Version, "stale-write", now.Add(2*time.Minute)); !errors.Is(err, ErrPasswordVersionChanged) {
		t.Fatalf("UpdatePasswordAndRevokeSessions(stale) error = %v, want ErrPasswordVersionChanged", err)
	}
	afterStaleWrite, _, err := testStore.PasswordCredentials(ctx)
	if err != nil {
		t.Fatalf("PasswordCredentials(after stale write) error = %v", err)
	}
	if afterStaleWrite != updated {
		t.Fatalf("credentials changed after rejected stale write: got %#v, want %#v", afterStaleWrite, updated)
	}
	if err := testStore.CreateSessionAtPasswordVersion(ctx, Session{
		ID:         "stale-session",
		UserID:     "local",
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(time.Hour),
	}, credentials.Version); !errors.Is(err, ErrPasswordVersionChanged) {
		t.Fatalf("CreateSessionAtPasswordVersion(stale) error = %v, want ErrPasswordVersionChanged", err)
	}
}

func TestOpenMigratesSessionPasswordVersion(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "stacklab.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE auth_password (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			password_hash TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			password_version INTEGER NOT NULL DEFAULT 1
		);
		CREATE TABLE auth_sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			user_agent TEXT,
			ip_address TEXT,
			revoked_at TEXT
		);
		INSERT INTO auth_password (id, password_hash, updated_at, password_version)
		VALUES (1, 'hash', '2026-07-11T12:00:00Z', 1);
		INSERT INTO auth_sessions (id, user_id, created_at, last_seen_at, expires_at, user_agent, ip_address)
		VALUES ('legacy-session', 'local', '2026-07-11T12:00:00Z', '2026-07-11T12:00:00Z', '2026-07-11T13:00:00Z', '', '');
	`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("create legacy schema error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database error = %v", err)
	}

	testStore, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open(legacy database) error = %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })
	record, err := testStore.SessionAtCurrentPasswordVersion(context.Background(), "legacy-session")
	if err != nil {
		t.Fatalf("SessionAtCurrentPasswordVersion(legacy session) error = %v", err)
	}
	if record.PasswordVersion != 1 {
		t.Fatalf("legacy session password version = %d, want 1", record.PasswordVersion)
	}
}

func assertPathMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %04o, want %04o", path, got, want)
	}
}

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

func TestStackDeployBaselineRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	deployedAt := time.Date(2026, 7, 6, 3, 17, 0, 0, time.UTC)

	baseline := StackDeployBaseline{
		StackID:        "demo",
		ComposeSHA256:  "compose-hash",
		EnvSHA256:      "env-hash",
		ComposeYAML:    "services:\n  app:\n    image: nginx\n",
		Env:            "TAG=latest\n",
		EnvExists:      true,
		LastDeployedAt: deployedAt,
		LastJobID:      "job_123",
	}
	if err := testStore.UpsertStackDeployBaseline(ctx, baseline); err != nil {
		t.Fatalf("UpsertStackDeployBaseline() error = %v", err)
	}

	got, ok, err := testStore.StackDeployBaseline(ctx, "demo")
	if err != nil {
		t.Fatalf("StackDeployBaseline() error = %v", err)
	}
	if !ok {
		t.Fatalf("StackDeployBaseline() ok = false, want true")
	}
	if got.StackID != baseline.StackID || got.ComposeSHA256 != baseline.ComposeSHA256 || got.EnvSHA256 != baseline.EnvSHA256 || got.ComposeYAML != baseline.ComposeYAML || got.Env != baseline.Env || !got.EnvExists || got.LastJobID != baseline.LastJobID || !got.LastDeployedAt.Equal(deployedAt) {
		t.Fatalf("StackDeployBaseline() = %#v, want %#v", got, baseline)
	}

	items, err := testStore.ListStackDeployBaselines(ctx)
	if err != nil {
		t.Fatalf("ListStackDeployBaselines() error = %v", err)
	}
	if len(items) != 1 || items[0].StackID != "demo" {
		t.Fatalf("ListStackDeployBaselines() = %#v", items)
	}

	if err := testStore.DeleteStackDeployBaseline(ctx, "demo"); err != nil {
		t.Fatalf("DeleteStackDeployBaseline() error = %v", err)
	}
	if _, ok, err := testStore.StackDeployBaseline(ctx, "demo"); err != nil || ok {
		t.Fatalf("StackDeployBaseline(after delete) ok=%v err=%v, want false nil", ok, err)
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

func openStoreForTest(t *testing.T) *Store {
	t.Helper()

	testStore, err := Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })
	return testStore
}
