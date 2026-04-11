package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

type Session struct {
	ID         string
	UserID     string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  string
	IPAddress  string
	RevokedAt  *time.Time
}

type Job struct {
	ID           string       `json:"id"`
	StackID      string       `json:"stack_id"`
	Action       string       `json:"action"`
	State        string       `json:"state"`
	RequestedBy  string       `json:"-"`
	RequestedAt  time.Time    `json:"requested_at"`
	StartedAt    *time.Time   `json:"started_at"`
	FinishedAt   *time.Time   `json:"finished_at"`
	Workflow     *JobWorkflow `json:"workflow,omitempty"`
	ErrorCode    string       `json:"-"`
	ErrorMessage string       `json:"-"`
}

func (j Job) MarshalJSON() ([]byte, error) {
	type workflowJSON struct {
		Steps []JobWorkflowStep `json:"steps"`
	}
	type jobJSON struct {
		ID          string        `json:"id"`
		StackID     *string       `json:"stack_id"`
		Action      string        `json:"action"`
		State       string        `json:"state"`
		RequestedAt time.Time     `json:"requested_at"`
		StartedAt   *time.Time    `json:"started_at,omitempty"`
		FinishedAt  *time.Time    `json:"finished_at,omitempty"`
		Workflow    *workflowJSON `json:"workflow,omitempty"`
	}

	var stackID *string
	if j.StackID != "" {
		stackID = &j.StackID
	}

	var workflow *workflowJSON
	if j.Workflow != nil {
		workflow = &workflowJSON{Steps: j.Workflow.Steps}
	}

	return json.Marshal(jobJSON{
		ID:          j.ID,
		StackID:     stackID,
		Action:      j.Action,
		State:       j.State,
		RequestedAt: j.RequestedAt,
		StartedAt:   j.StartedAt,
		FinishedAt:  j.FinishedAt,
		Workflow:    workflow,
	})
}

type JobWorkflow struct {
	Steps []JobWorkflowStep `json:"steps"`
}

type JobWorkflowStep struct {
	Action        string `json:"action"`
	State         string `json:"state"`
	TargetStackID string `json:"target_stack_id,omitempty"`
}

type JobEvent struct {
	JobID     string        `json:"job_id"`
	Sequence  int           `json:"sequence"`
	Event     string        `json:"event"`
	State     string        `json:"state"`
	Message   string        `json:"message,omitempty"`
	Data      string        `json:"data,omitempty"`
	Step      *JobEventStep `json:"step,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

type JobEventStep struct {
	Index         int    `json:"index"`
	Total         int    `json:"total"`
	Action        string `json:"action"`
	TargetStackID string `json:"target_stack_id,omitempty"`
}

type AuditEntry struct {
	ID          string     `json:"id"`
	StackID     *string    `json:"stack_id"`
	JobID       *string    `json:"job_id"`
	Action      string     `json:"action"`
	RequestedBy string     `json:"requested_by"`
	Result      string     `json:"result"`
	RequestedAt time.Time  `json:"requested_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	DurationMS  *int       `json:"duration_ms"`
	TargetType  string     `json:"-"`
	TargetID    *string    `json:"-"`
	DetailJSON  *string    `json:"-"`
}

type AuditQuery struct {
	StackID string
	Cursor  string
	Limit   int
}

type AuditListResult struct {
	Items      []AuditEntry `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}

type OperationalRetentionPolicy struct {
	AuditEntryRetention     time.Duration
	JobRetention            time.Duration
	JobEventRetention       time.Duration
	ExpiredSessionRetention time.Duration
}

type OperationalRetentionSummary struct {
	AuditEntriesDeleted int64
	JobsDeleted         int64
	JobEventsDeleted    int64
	SessionsDeleted     int64
}

func (s OperationalRetentionSummary) TotalDeleted() int64 {
	return s.AuditEntriesDeleted + s.JobsDeleted + s.JobEventsDeleted + s.SessionsDeleted
}

func Open(databasePath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA busy_timeout = 5000;`,
		`CREATE TABLE IF NOT EXISTS auth_password (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			password_hash TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			password_version INTEGER NOT NULL DEFAULT 1
		);`,
		`CREATE TABLE IF NOT EXISTS auth_sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			user_agent TEXT,
			ip_address TEXT,
			revoked_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires_at ON auth_sessions (expires_at);`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_revoked_at ON auth_sessions (revoked_at);`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			stack_id TEXT NOT NULL,
			action TEXT NOT NULL,
			state TEXT NOT NULL,
			requested_by TEXT NOT NULL,
			requested_at TEXT NOT NULL,
			started_at TEXT,
			finished_at TEXT,
			workflow_json TEXT,
			error_code TEXT,
			error_message TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_stack_requested_at ON jobs (stack_id, requested_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_state_requested_at ON jobs (state, requested_at DESC);`,
		`CREATE TABLE IF NOT EXISTS job_events (
			job_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			event TEXT NOT NULL,
			state TEXT NOT NULL,
			message TEXT,
			data TEXT,
			step_json TEXT,
			timestamp TEXT NOT NULL,
			PRIMARY KEY (job_id, sequence)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_job_events_job_sequence ON job_events (job_id, sequence ASC);`,
		`CREATE INDEX IF NOT EXISTS idx_job_events_timestamp ON job_events (timestamp);`,
		`CREATE TABLE IF NOT EXISTS audit_entries (
			id TEXT PRIMARY KEY,
			stack_id TEXT,
			job_id TEXT,
			action TEXT NOT NULL,
			requested_by TEXT NOT NULL,
			requested_at TEXT NOT NULL,
			finished_at TEXT,
			result TEXT NOT NULL,
			duration_ms INTEGER,
			target_type TEXT NOT NULL,
			target_id TEXT,
			detail_json TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_entries_stack_requested_at ON audit_entries (stack_id, requested_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_entries_requested_at ON audit_entries (requested_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_entries_action_requested_at ON audit_entries (action, requested_at DESC);`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate sqlite store: %w", err)
		}
	}

	return nil
}

func (s *Store) PasswordHash(ctx context.Context) (string, bool, error) {
	var passwordHash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM auth_password WHERE id = 1`).Scan(&passwordHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("load password hash: %w", err)
	default:
		return passwordHash, true, nil
	}
}

func (s *Store) SetPasswordHash(ctx context.Context, passwordHash string, updatedAt time.Time) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO auth_password (id, password_hash, updated_at, password_version)
		 VALUES (1, ?, ?, 1)
		 ON CONFLICT(id) DO UPDATE SET password_hash = excluded.password_hash, updated_at = excluded.updated_at`,
		passwordHash,
		updatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("store password hash: %w", err)
	}
	return nil
}

func (s *Store) AppSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM app_settings WHERE key = ?`, key).Scan(&value)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("load app setting %q: %w", key, err)
	default:
		return value, true, nil
	}
}

func (s *Store) SetAppSetting(ctx context.Context, key, valueJSON string, updatedAt time.Time) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO app_settings (key, value_json, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		key,
		valueJSON,
		updatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("store app setting %q: %w", key, err)
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, session Session) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO auth_sessions (id, user_id, created_at, last_seen_at, expires_at, user_agent, ip_address, revoked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
		session.ID,
		session.UserID,
		session.CreatedAt.UTC().Format(time.RFC3339Nano),
		session.LastSeenAt.UTC().Format(time.RFC3339Nano),
		session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		session.UserAgent,
		session.IPAddress,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) SessionByID(ctx context.Context, id string) (Session, error) {
	var rawCreatedAt string
	var rawLastSeenAt string
	var rawExpiresAt string
	var rawRevokedAt sql.NullString

	session := Session{}
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, user_id, created_at, last_seen_at, expires_at, user_agent, ip_address, revoked_at
		 FROM auth_sessions
		 WHERE id = ?`,
		id,
	).Scan(
		&session.ID,
		&session.UserID,
		&rawCreatedAt,
		&rawLastSeenAt,
		&rawExpiresAt,
		&session.UserAgent,
		&session.IPAddress,
		&rawRevokedAt,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Session{}, ErrNotFound
	case err != nil:
		return Session{}, fmt.Errorf("load session: %w", err)
	}

	session.CreatedAt, err = time.Parse(time.RFC3339Nano, rawCreatedAt)
	if err != nil {
		return Session{}, fmt.Errorf("parse created_at: %w", err)
	}
	session.LastSeenAt, err = time.Parse(time.RFC3339Nano, rawLastSeenAt)
	if err != nil {
		return Session{}, fmt.Errorf("parse last_seen_at: %w", err)
	}
	session.ExpiresAt, err = time.Parse(time.RFC3339Nano, rawExpiresAt)
	if err != nil {
		return Session{}, fmt.Errorf("parse expires_at: %w", err)
	}
	if rawRevokedAt.Valid {
		revokedAt, err := time.Parse(time.RFC3339Nano, rawRevokedAt.String)
		if err != nil {
			return Session{}, fmt.Errorf("parse revoked_at: %w", err)
		}
		session.RevokedAt = &revokedAt
	}

	return session, nil
}

func (s *Store) TouchSession(ctx context.Context, id string, lastSeenAt, expiresAt time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE auth_sessions
		 SET last_seen_at = ?, expires_at = ?
		 WHERE id = ? AND revoked_at IS NULL`,
		lastSeenAt.UTC().Format(time.RFC3339Nano),
		expiresAt.UTC().Format(time.RFC3339Nano),
		id,
	)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("touch session rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RevokeSession(ctx context.Context, id string, revokedAt time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE auth_sessions
		 SET revoked_at = ?
		 WHERE id = ? AND revoked_at IS NULL`,
		revokedAt.UTC().Format(time.RFC3339Nano),
		id,
	)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke session rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateJob(ctx context.Context, job Job) error {
	var workflowJSON sql.NullString
	if job.Workflow != nil {
		workflowBytes, err := json.Marshal(job.Workflow)
		if err != nil {
			return fmt.Errorf("marshal workflow: %w", err)
		}
		workflowJSON = sql.NullString{String: string(workflowBytes), Valid: true}
	}

	var startedAt sql.NullString
	if job.StartedAt != nil {
		startedAt = sql.NullString{String: job.StartedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}
	var finishedAt sql.NullString
	if job.FinishedAt != nil {
		finishedAt = sql.NullString{String: job.FinishedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO jobs (id, stack_id, action, state, requested_by, requested_at, started_at, finished_at, workflow_json, error_code, error_message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID,
		job.StackID,
		job.Action,
		job.State,
		job.RequestedBy,
		job.RequestedAt.UTC().Format(time.RFC3339Nano),
		startedAt,
		finishedAt,
		workflowJSON,
		nullIfEmpty(job.ErrorCode),
		nullIfEmpty(job.ErrorMessage),
	)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

func (s *Store) UpdateJob(ctx context.Context, job Job) error {
	var workflowJSON sql.NullString
	if job.Workflow != nil {
		workflowBytes, err := json.Marshal(job.Workflow)
		if err != nil {
			return fmt.Errorf("marshal workflow: %w", err)
		}
		workflowJSON = sql.NullString{String: string(workflowBytes), Valid: true}
	}

	var startedAt sql.NullString
	if job.StartedAt != nil {
		startedAt = sql.NullString{String: job.StartedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}
	var finishedAt sql.NullString
	if job.FinishedAt != nil {
		finishedAt = sql.NullString{String: job.FinishedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs
		 SET state = ?, started_at = ?, finished_at = ?, workflow_json = ?, error_code = ?, error_message = ?
		 WHERE id = ?`,
		job.State,
		startedAt,
		finishedAt,
		workflowJSON,
		nullIfEmpty(job.ErrorCode),
		nullIfEmpty(job.ErrorMessage),
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update job rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) JobByID(ctx context.Context, id string) (Job, error) {
	job, err := scanJob(s.db.QueryRowContext(
		ctx,
		`SELECT id, stack_id, action, state, requested_by, requested_at, started_at, finished_at, workflow_json, error_code, error_message
		 FROM jobs
		 WHERE id = ?`,
		id,
	))
	switch {
	case errors.Is(err, ErrNotFound):
		return Job{}, ErrNotFound
	case err != nil:
		return Job{}, fmt.Errorf("load job: %w", err)
	}

	return job, nil
}

func (s *Store) ListActiveJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, stack_id, action, state, requested_by, requested_at, started_at, finished_at, workflow_json, error_code, error_message
		 FROM jobs
		 WHERE state IN ('queued', 'running', 'cancel_requested')
		 ORDER BY COALESCE(started_at, requested_at) DESC, requested_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list active jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]Job, 0, 8)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active jobs: %w", err)
	}

	return jobs, nil
}

func (s *Store) NextJobEventSequence(ctx context.Context, jobID string) (int, error) {
	var nextSequence int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1
		 FROM job_events
		 WHERE job_id = ?`,
		jobID,
	).Scan(&nextSequence)
	if err != nil {
		return 0, fmt.Errorf("next job event sequence: %w", err)
	}

	return nextSequence, nil
}

func (s *Store) CreateJobEvent(ctx context.Context, event JobEvent) error {
	var stepJSON sql.NullString
	if event.Step != nil {
		encoded, err := json.Marshal(event.Step)
		if err != nil {
			return fmt.Errorf("marshal job event step: %w", err)
		}
		stepJSON = sql.NullString{String: string(encoded), Valid: true}
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO job_events (job_id, sequence, event, state, message, data, step_json, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.JobID,
		event.Sequence,
		event.Event,
		event.State,
		nullIfEmpty(event.Message),
		nullIfEmpty(event.Data),
		stepJSON,
		event.Timestamp.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("create job event: %w", err)
	}

	return nil
}

func (s *Store) ListJobEvents(ctx context.Context, jobID string) ([]JobEvent, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT job_id, sequence, event, state, message, data, step_json, timestamp
		 FROM job_events
		 WHERE job_id = ?
		 ORDER BY sequence ASC`,
		jobID,
	)
	if err != nil {
		return nil, fmt.Errorf("list job events: %w", err)
	}
	defer rows.Close()

	events := make([]JobEvent, 0, 16)
	for rows.Next() {
		event, err := scanJobEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job events: %w", err)
	}

	return events, nil
}

func (s *Store) LatestJobEvent(ctx context.Context, jobID string) (JobEvent, bool, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT job_id, sequence, event, state, message, data, step_json, timestamp
		 FROM job_events
		 WHERE job_id = ?
		 ORDER BY sequence DESC
		 LIMIT 1`,
		jobID,
	)

	event, err := scanJobEvent(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return JobEvent{}, false, nil
	case err != nil:
		return JobEvent{}, false, fmt.Errorf("load latest job event: %w", err)
	default:
		return event, true, nil
	}
}

func (s *Store) CreateAuditEntry(ctx context.Context, entry AuditEntry) error {
	var finishedAt sql.NullString
	if entry.FinishedAt != nil {
		finishedAt = sql.NullString{String: entry.FinishedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	var durationMS sql.NullInt64
	if entry.DurationMS != nil {
		durationMS = sql.NullInt64{Int64: int64(*entry.DurationMS), Valid: true}
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO audit_entries (id, stack_id, job_id, action, requested_by, requested_at, finished_at, result, duration_ms, target_type, target_id, detail_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID,
		nullIfPtr(entry.StackID),
		nullIfPtr(entry.JobID),
		entry.Action,
		entry.RequestedBy,
		entry.RequestedAt.UTC().Format(time.RFC3339Nano),
		finishedAt,
		entry.Result,
		durationMS,
		entry.TargetType,
		nullIfPtr(entry.TargetID),
		nullIfPtr(entry.DetailJSON),
	)
	if err != nil {
		return fmt.Errorf("create audit entry: %w", err)
	}
	return nil
}

func (s *Store) ListAuditEntries(ctx context.Context, query AuditQuery) (AuditListResult, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	var where []string
	var args []any
	if query.StackID != "" {
		where = append(where, "stack_id = ?")
		args = append(args, query.StackID)
	}
	if query.Cursor != "" {
		cursorTime, cursorID, err := decodeAuditCursor(query.Cursor)
		if err != nil {
			return AuditListResult{}, fmt.Errorf("decode audit cursor: %w", err)
		}
		where = append(where, "(requested_at < ? OR (requested_at = ? AND id < ?))")
		args = append(args, cursorTime.UTC().Format(time.RFC3339Nano), cursorTime.UTC().Format(time.RFC3339Nano), cursorID)
	}

	statement := `SELECT id, stack_id, job_id, action, requested_by, requested_at, finished_at, result, duration_ms, target_type, target_id, detail_json
		FROM audit_entries`
	if len(where) > 0 {
		statement += " WHERE " + strings.Join(where, " AND ")
	}
	statement += " ORDER BY requested_at DESC, id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return AuditListResult{}, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()

	items := make([]AuditEntry, 0, limit+1)
	for rows.Next() {
		entry, err := scanAuditEntry(rows)
		if err != nil {
			return AuditListResult{}, err
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return AuditListResult{}, fmt.Errorf("iterate audit entries: %w", err)
	}

	result := AuditListResult{}
	if len(items) > limit {
		last := items[limit-1]
		cursor := encodeAuditCursor(last.RequestedAt, last.ID)
		result.NextCursor = &cursor
		items = items[:limit]
	}
	result.Items = items

	return result, nil
}

func (s *Store) LatestAuditEntriesByStackIDs(ctx context.Context, stackIDs []string) (map[string]AuditEntry, error) {
	result := make(map[string]AuditEntry, len(stackIDs))
	if len(stackIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, 0, len(stackIDs))
	args := make([]any, 0, len(stackIDs))
	for _, stackID := range stackIDs {
		placeholders = append(placeholders, "?")
		args = append(args, stackID)
	}

	rows, err := s.db.QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT id, stack_id, job_id, action, requested_by, requested_at, finished_at, result, duration_ms, target_type, target_id, detail_json
			 FROM audit_entries
			 WHERE stack_id IN (%s)
			 ORDER BY requested_at DESC, id DESC`,
			strings.Join(placeholders, ", "),
		),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list latest audit entries by stack: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		entry, err := scanAuditEntry(rows)
		if err != nil {
			return nil, err
		}
		if entry.StackID == nil {
			continue
		}
		if _, exists := result[*entry.StackID]; exists {
			continue
		}
		result[*entry.StackID] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest audit entries: %w", err)
	}

	return result, nil
}

func (s *Store) PruneOperationalData(ctx context.Context, now time.Time, policy OperationalRetentionPolicy) (OperationalRetentionSummary, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OperationalRetentionSummary{}, fmt.Errorf("begin operational retention prune: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	now = now.UTC()
	auditCutoff := now.Add(-policy.AuditEntryRetention).Format(time.RFC3339Nano)
	jobCutoff := now.Add(-policy.JobRetention).Format(time.RFC3339Nano)
	jobEventCutoff := now.Add(-policy.JobEventRetention).Format(time.RFC3339Nano)
	sessionCutoff := now.Add(-policy.ExpiredSessionRetention).Format(time.RFC3339Nano)

	summary := OperationalRetentionSummary{}

	summary.SessionsDeleted, err = execPrune(
		ctx,
		tx,
		`DELETE FROM auth_sessions
		 WHERE expires_at < ?
		    OR (revoked_at IS NOT NULL AND revoked_at < ?)`,
		sessionCutoff,
		sessionCutoff,
	)
	if err != nil {
		return OperationalRetentionSummary{}, fmt.Errorf("prune auth sessions: %w", err)
	}

	deletedOldEvents, err := execPrune(
		ctx,
		tx,
		`DELETE FROM job_events
		 WHERE timestamp < ?
		   AND job_id NOT IN (
			 SELECT id FROM jobs WHERE state IN ('queued', 'running', 'cancel_requested')
		   )`,
		jobEventCutoff,
	)
	if err != nil {
		return OperationalRetentionSummary{}, fmt.Errorf("prune old job events: %w", err)
	}
	summary.JobEventsDeleted += deletedOldEvents

	deletedPrunedJobEvents, err := execPrune(
		ctx,
		tx,
		`DELETE FROM job_events
		 WHERE job_id IN (
			 SELECT id
			 FROM jobs
			 WHERE state NOT IN ('queued', 'running', 'cancel_requested')
			   AND COALESCE(finished_at, requested_at) < ?
			   AND NOT EXISTS (
				   SELECT 1
				   FROM audit_entries
				   WHERE audit_entries.job_id = jobs.id
				     AND audit_entries.requested_at >= ?
			   )
		 )`,
		jobCutoff,
		auditCutoff,
	)
	if err != nil {
		return OperationalRetentionSummary{}, fmt.Errorf("prune job events for old jobs: %w", err)
	}
	summary.JobEventsDeleted += deletedPrunedJobEvents

	summary.JobsDeleted, err = execPrune(
		ctx,
		tx,
		`DELETE FROM jobs
		 WHERE state NOT IN ('queued', 'running', 'cancel_requested')
		   AND COALESCE(finished_at, requested_at) < ?
		   AND NOT EXISTS (
			   SELECT 1
			   FROM audit_entries
			   WHERE audit_entries.job_id = jobs.id
			     AND audit_entries.requested_at >= ?
		   )`,
		jobCutoff,
		auditCutoff,
	)
	if err != nil {
		return OperationalRetentionSummary{}, fmt.Errorf("prune jobs: %w", err)
	}

	summary.AuditEntriesDeleted, err = execPrune(
		ctx,
		tx,
		`DELETE FROM audit_entries
		 WHERE requested_at < ?`,
		auditCutoff,
	)
	if err != nil {
		return OperationalRetentionSummary{}, fmt.Errorf("prune audit entries: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return OperationalRetentionSummary{}, fmt.Errorf("commit operational retention prune: %w", err)
	}

	return summary, nil
}

func execPrune(ctx context.Context, tx *sql.Tx, statement string, args ...any) (int64, error) {
	result, err := tx.ExecContext(ctx, statement, args...)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rowsAffected, nil
}

func nullIfEmpty(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullIfPtr(value *string) sql.NullString {
	if value == nil || *value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func scanJob(scanner interface{ Scan(dest ...any) error }) (Job, error) {
	var rawRequestedAt string
	var rawStartedAt sql.NullString
	var rawFinishedAt sql.NullString
	var rawWorkflow sql.NullString
	var rawErrorCode sql.NullString
	var rawErrorMessage sql.NullString

	job := Job{}
	err := scanner.Scan(
		&job.ID,
		&job.StackID,
		&job.Action,
		&job.State,
		&job.RequestedBy,
		&rawRequestedAt,
		&rawStartedAt,
		&rawFinishedAt,
		&rawWorkflow,
		&rawErrorCode,
		&rawErrorMessage,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Job{}, ErrNotFound
	case err != nil:
		return Job{}, fmt.Errorf("scan job: %w", err)
	}

	parsedRequestedAt, err := time.Parse(time.RFC3339Nano, rawRequestedAt)
	if err != nil {
		return Job{}, fmt.Errorf("parse requested_at: %w", err)
	}
	job.RequestedAt = parsedRequestedAt

	if rawStartedAt.Valid {
		startedAt, err := time.Parse(time.RFC3339Nano, rawStartedAt.String)
		if err != nil {
			return Job{}, fmt.Errorf("parse started_at: %w", err)
		}
		job.StartedAt = &startedAt
	}
	if rawFinishedAt.Valid {
		finishedAt, err := time.Parse(time.RFC3339Nano, rawFinishedAt.String)
		if err != nil {
			return Job{}, fmt.Errorf("parse finished_at: %w", err)
		}
		job.FinishedAt = &finishedAt
	}
	if rawWorkflow.Valid {
		var workflow JobWorkflow
		if err := json.Unmarshal([]byte(rawWorkflow.String), &workflow); err != nil {
			return Job{}, fmt.Errorf("unmarshal workflow: %w", err)
		}
		job.Workflow = &workflow
	}
	if rawErrorCode.Valid {
		job.ErrorCode = rawErrorCode.String
	}
	if rawErrorMessage.Valid {
		job.ErrorMessage = rawErrorMessage.String
	}

	return job, nil
}

func scanJobEvent(scanner interface{ Scan(dest ...any) error }) (JobEvent, error) {
	var rawMessage sql.NullString
	var rawData sql.NullString
	var rawStepJSON sql.NullString
	var rawTimestamp string

	event := JobEvent{}
	err := scanner.Scan(
		&event.JobID,
		&event.Sequence,
		&event.Event,
		&event.State,
		&rawMessage,
		&rawData,
		&rawStepJSON,
		&rawTimestamp,
	)
	if err != nil {
		return JobEvent{}, fmt.Errorf("scan job event: %w", err)
	}

	if rawMessage.Valid {
		event.Message = rawMessage.String
	}
	if rawData.Valid {
		event.Data = rawData.String
	}
	if rawStepJSON.Valid {
		var step JobEventStep
		if err := json.Unmarshal([]byte(rawStepJSON.String), &step); err != nil {
			return JobEvent{}, fmt.Errorf("unmarshal job event step: %w", err)
		}
		event.Step = &step
	}

	timestamp, err := time.Parse(time.RFC3339Nano, rawTimestamp)
	if err != nil {
		return JobEvent{}, fmt.Errorf("parse job event timestamp: %w", err)
	}
	event.Timestamp = timestamp

	return event, nil
}

func scanAuditEntry(scanner interface{ Scan(dest ...any) error }) (AuditEntry, error) {
	var rawStackID sql.NullString
	var rawJobID sql.NullString
	var rawRequestedAt string
	var rawFinishedAt sql.NullString
	var rawDurationMS sql.NullInt64
	var rawTargetID sql.NullString
	var rawDetailJSON sql.NullString

	entry := AuditEntry{}
	err := scanner.Scan(
		&entry.ID,
		&rawStackID,
		&rawJobID,
		&entry.Action,
		&entry.RequestedBy,
		&rawRequestedAt,
		&rawFinishedAt,
		&entry.Result,
		&rawDurationMS,
		&entry.TargetType,
		&rawTargetID,
		&rawDetailJSON,
	)
	if err != nil {
		return AuditEntry{}, fmt.Errorf("scan audit entry: %w", err)
	}

	requestedAt, err := time.Parse(time.RFC3339Nano, rawRequestedAt)
	if err != nil {
		return AuditEntry{}, fmt.Errorf("parse audit requested_at: %w", err)
	}
	entry.RequestedAt = requestedAt

	if rawFinishedAt.Valid {
		finishedAt, err := time.Parse(time.RFC3339Nano, rawFinishedAt.String)
		if err != nil {
			return AuditEntry{}, fmt.Errorf("parse audit finished_at: %w", err)
		}
		entry.FinishedAt = &finishedAt
	}
	if rawStackID.Valid {
		entry.StackID = &rawStackID.String
	}
	if rawJobID.Valid {
		entry.JobID = &rawJobID.String
	}
	if rawDurationMS.Valid {
		durationMS := int(rawDurationMS.Int64)
		entry.DurationMS = &durationMS
	}
	if rawTargetID.Valid {
		entry.TargetID = &rawTargetID.String
	}
	if rawDetailJSON.Valid {
		entry.DetailJSON = &rawDetailJSON.String
	}

	return entry, nil
}

func encodeAuditCursor(requestedAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(requestedAt.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeAuditCursor(value string) (time.Time, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, "", err
	}

	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}

	requestedAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", err
	}

	return requestedAt, parts[1], nil
}
