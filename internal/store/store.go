package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

type JobWorkflow struct {
	Steps []JobWorkflowStep `json:"steps"`
}

type JobWorkflowStep struct {
	Action string `json:"action"`
	State  string `json:"state"`
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
	var rawRequestedAt string
	var rawStartedAt sql.NullString
	var rawFinishedAt sql.NullString
	var rawWorkflow sql.NullString
	var rawErrorCode sql.NullString
	var rawErrorMessage sql.NullString

	job := Job{}
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, stack_id, action, state, requested_by, requested_at, started_at, finished_at, workflow_json, error_code, error_message
		 FROM jobs
		 WHERE id = ?`,
		id,
	).Scan(
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
		return Job{}, fmt.Errorf("load job: %w", err)
	}

	job.RequestedAt, err = time.Parse(time.RFC3339Nano, rawRequestedAt)
	if err != nil {
		return Job{}, fmt.Errorf("parse requested_at: %w", err)
	}
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

func nullIfEmpty(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
