package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"stacklab/internal/retention"
	"stacklab/internal/store"
)

func main() {
	var (
		dbPath   string
		runPrune bool
	)

	flag.StringVar(&dbPath, "db", defaultDatabasePath(), "path to stacklab.db")
	flag.BoolVar(&runPrune, "run-prune", false, "apply operational retention cleanup after seeding")
	flag.Parse()

	ctx := context.Background()

	if err := resetSeedData(ctx, dbPath); err != nil {
		log.Fatalf("reset seed data: %v", err)
	}

	appStore, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := appStore.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	now := time.Now().UTC()
	if err := seedFixtures(ctx, appStore, now); err != nil {
		log.Fatalf("seed fixtures: %v", err)
	}

	fmt.Printf("Seeded retention fixtures into %s\n", dbPath)
	fmt.Println("Fixture stacks:")
	fmt.Println("- seed-retention-recent: recent audit row with retained detailed output")
	fmt.Println("- seed-retention-detail-gone: older audit row that should keep the job summary but lose detailed job events after cleanup")
	fmt.Println("- seed-retention-pruned: very old audit/job pair that should disappear after cleanup")

	if runPrune {
		summary, err := appStore.PruneOperationalData(ctx, now, retention.DefaultPolicy())
		if err != nil {
			log.Fatalf("run retention prune: %v", err)
		}
		fmt.Println()
		fmt.Println("Applied operational retention cleanup:")
		fmt.Printf("- audit_entries deleted: %d\n", summary.AuditEntriesDeleted)
		fmt.Printf("- jobs deleted: %d\n", summary.JobsDeleted)
		fmt.Printf("- job_events deleted: %d\n", summary.JobEventsDeleted)
		fmt.Printf("- auth_sessions deleted: %d\n", summary.SessionsDeleted)
		fmt.Println()
		fmt.Println("Expected UI checks:")
		fmt.Println("- Audit shows seed-retention-recent with full job detail")
		fmt.Println("- Audit shows seed-retention-detail-gone and its drawer says detailed output is no longer retained")
		fmt.Println("- seed-retention-pruned no longer appears in Audit")
	} else {
		fmt.Println()
		fmt.Println("Next step:")
		fmt.Println("- restart Stacklab to let the background retention service prune old detail, or rerun with --run-prune")
	}
}

func defaultDatabasePath() string {
	if value := os.Getenv("STACKLAB_DATABASE_PATH"); value != "" {
		return value
	}
	return filepath.Join(".local", "var", "lib", "stacklab", "stacklab.db")
}

func resetSeedData(ctx context.Context, dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000;`); err != nil {
		return fmt.Errorf("set sqlite busy_timeout: %w", err)
	}

	statements := []string{
		`DELETE FROM job_events WHERE job_id LIKE 'seed_retention_%'`,
		`DELETE FROM audit_entries WHERE id LIKE 'seed_retention_%' OR job_id LIKE 'seed_retention_%' OR stack_id LIKE 'seed-retention-%'`,
		`DELETE FROM jobs WHERE id LIKE 'seed_retention_%'`,
		`DELETE FROM auth_sessions WHERE id LIKE 'seed_retention_%'`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			if isMissingTable(err) {
				return nil
			}
			return err
		}
	}

	return nil
}

func seedFixtures(ctx context.Context, appStore *store.Store, now time.Time) error {
	recentTime := now.Add(-2 * time.Hour)
	detailGoneTime := now.Add(-20 * 24 * time.Hour)
	prunedTime := now.Add(-200 * 24 * time.Hour)

	if err := appStore.CreateSession(ctx, store.Session{
		ID:         "seed_retention_recent_session",
		UserID:     "local",
		CreatedAt:  now.Add(-30 * time.Minute),
		LastSeenAt: now.Add(-15 * time.Minute),
		ExpiresAt:  now.Add(12 * time.Hour),
		UserAgent:  "seed-retention-script",
		IPAddress:  "127.0.0.1",
	}); err != nil {
		return fmt.Errorf("create recent session: %w", err)
	}
	if err := appStore.CreateSession(ctx, store.Session{
		ID:         "seed_retention_expired_session",
		UserID:     "local",
		CreatedAt:  now.Add(-10 * 24 * time.Hour),
		LastSeenAt: now.Add(-10 * 24 * time.Hour),
		ExpiresAt:  now.Add(-8 * 24 * time.Hour),
		UserAgent:  "seed-retention-script",
		IPAddress:  "127.0.0.1",
	}); err != nil {
		return fmt.Errorf("create expired session: %w", err)
	}

	if err := createJobWithAudit(ctx, appStore, jobFixture{
		jobID:        "seed_retention_recent_job",
		auditID:      "seed_retention_recent_audit",
		stackID:      "seed-retention-recent",
		action:       "up",
		result:       "failed",
		when:         recentTime,
		errorCode:    "stack_action_failed",
		errorMsg:     "docker compose up failed",
		eventOffset:  5 * time.Minute,
		eventMessage: "Container failed healthcheck during deploy.",
	}); err != nil {
		return err
	}

	if err := createJobWithAudit(ctx, appStore, jobFixture{
		jobID:        "seed_retention_detail_gone_job",
		auditID:      "seed_retention_detail_gone_audit",
		stackID:      "seed-retention-detail-gone",
		action:       "pull",
		result:       "succeeded",
		when:         detailGoneTime,
		eventOffset:  2 * time.Minute,
		eventMessage: "Pulled newer images for this stack.",
	}); err != nil {
		return err
	}

	if err := createJobWithAudit(ctx, appStore, jobFixture{
		jobID:        "seed_retention_pruned_job",
		auditID:      "seed_retention_pruned_audit",
		stackID:      "seed-retention-pruned",
		action:       "restart",
		result:       "failed",
		when:         prunedTime,
		errorCode:    "stack_action_failed",
		errorMsg:     "container restart failed",
		eventOffset:  1 * time.Minute,
		eventMessage: "Restart attempt failed after timeout.",
	}); err != nil {
		return err
	}

	return nil
}

type jobFixture struct {
	jobID        string
	auditID      string
	stackID      string
	action       string
	result       string
	when         time.Time
	errorCode    string
	errorMsg     string
	eventOffset  time.Duration
	eventMessage string
}

func createJobWithAudit(ctx context.Context, appStore *store.Store, fixture jobFixture) error {
	startedAt := fixture.when
	finishedAt := fixture.when.Add(fixture.eventOffset)
	durationMS := int(fixture.eventOffset / time.Millisecond)
	job := store.Job{
		ID:           fixture.jobID,
		StackID:      fixture.stackID,
		Action:       fixture.action,
		State:        fixture.result,
		RequestedBy:  "seed",
		RequestedAt:  fixture.when,
		StartedAt:    &startedAt,
		FinishedAt:   &finishedAt,
		ErrorCode:    fixture.errorCode,
		ErrorMessage: fixture.errorMsg,
	}
	if err := appStore.CreateJob(ctx, job); err != nil {
		return fmt.Errorf("create job %s: %w", fixture.jobID, err)
	}

	if err := appStore.CreateJobEvent(ctx, store.JobEvent{
		JobID:     fixture.jobID,
		Sequence:  1,
		Event:     "job_started",
		State:     "running",
		Message:   "Job started.",
		Timestamp: fixture.when,
	}); err != nil {
		return fmt.Errorf("create start event %s: %w", fixture.jobID, err)
	}
	if err := appStore.CreateJobEvent(ctx, store.JobEvent{
		JobID:     fixture.jobID,
		Sequence:  2,
		Event:     "job_log",
		State:     fixture.result,
		Message:   fixture.eventMessage,
		Timestamp: finishedAt,
	}); err != nil {
		return fmt.Errorf("create log event %s: %w", fixture.jobID, err)
	}

	finishedAtCopy := finishedAt
	if err := appStore.CreateAuditEntry(ctx, store.AuditEntry{
		ID:          fixture.auditID,
		StackID:     stringPtr(fixture.stackID),
		JobID:       stringPtr(fixture.jobID),
		Action:      fixture.action,
		RequestedBy: "seed",
		Result:      fixture.result,
		RequestedAt: fixture.when,
		FinishedAt:  &finishedAtCopy,
		DurationMS:  &durationMS,
		TargetType:  "stack",
		TargetID:    stringPtr(fixture.stackID),
	}); err != nil {
		return fmt.Errorf("create audit entry %s: %w", fixture.auditID, err)
	}

	return nil
}

func stringPtr(value string) *string {
	return &value
}

func isMissingTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}
