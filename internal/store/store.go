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

var (
	ErrNotFound               = errors.New("not found")
	ErrPasswordVersionChanged = errors.New("password version changed")
	ErrSessionChanged         = errors.New("session changed")
)

const (
	databaseDirectoryMode os.FileMode = 0o700
	databaseFileMode      os.FileMode = 0o600
)

type Store struct {
	db *sql.DB
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type Session struct {
	ID              string
	UserID          string
	CreatedAt       time.Time
	LastSeenAt      time.Time
	ExpiresAt       time.Time
	UserAgent       string
	IPAddress       string
	RevokedAt       *time.Time
	PasswordVersion int
}

type PasswordCredentials struct {
	Hash    string
	Version int
}

type Job struct {
	ID           string       `json:"id"`
	StackID      string       `json:"stack_id"`
	Action       string       `json:"action"`
	State        string       `json:"state"`
	RequestedBy  string       `json:"-"`
	RequestID    string       `json:"request_id,omitempty"`
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
		RequestID   string        `json:"request_id,omitempty"`
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
		RequestID:   j.RequestID,
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
	Action             string   `json:"action"`
	State              string   `json:"state"`
	TargetStackID      string   `json:"target_stack_id,omitempty"`
	TargetServiceNames []string `json:"target_service_names,omitempty"`
}

type JobEvent struct {
	JobID     string        `json:"job_id"`
	Sequence  int           `json:"sequence"`
	Event     string        `json:"event"`
	State     string        `json:"state"`
	Message   string        `json:"message,omitempty"`
	Data      string        `json:"data,omitempty"`
	Step      *JobEventStep `json:"step,omitempty"`
	Progress  *JobProgress  `json:"progress,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// JobProgress is the structured progress payload for pull/build-heavy steps
// (dashboard read-model contract, Slice C). Additive: events without progress
// keep their existing shape.
type JobProgress struct {
	Phase     string `json:"phase"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
	Unit      string `json:"unit"`
	Detail    string `json:"detail,omitempty"`
}

type JobEventStep struct {
	Index              int      `json:"index"`
	Total              int      `json:"total"`
	Action             string   `json:"action"`
	TargetStackID      string   `json:"target_stack_id,omitempty"`
	TargetServiceNames []string `json:"target_service_names,omitempty"`
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
	StackID         string
	Cursor          string
	Search          string
	Results         []string
	RequestedFrom   *time.Time
	RequestedBefore *time.Time
	Limit           int
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

func EnsureDataDirectory(path string) error {
	if err := os.MkdirAll(path, databaseDirectoryMode); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(path, databaseDirectoryMode); err != nil {
		return fmt.Errorf("secure data directory: %w", err)
	}
	return nil
}

func Open(databasePath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(databasePath), databaseDirectoryMode); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	file, err := os.OpenFile(databasePath, os.O_CREATE|os.O_RDWR, databaseFileMode)
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close database file: %w", err)
	}
	if err := secureDatabaseFileModes(databasePath); err != nil {
		return nil, err
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
	if err := secureDatabaseFileModes(databasePath); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func secureDatabaseFileModes(databasePath string) error {
	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		if err := os.Chmod(path, databaseFileMode); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("secure database file %q: %w", path, err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite store: %w", err)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	return s.runMigrations(ctx)
}

func (s *Store) PasswordHash(ctx context.Context) (string, bool, error) {
	credentials, configured, err := s.PasswordCredentials(ctx)
	return credentials.Hash, configured, err
}

func (s *Store) PasswordCredentials(ctx context.Context) (PasswordCredentials, bool, error) {
	var credentials PasswordCredentials
	err := s.db.QueryRowContext(ctx, `SELECT password_hash, password_version FROM auth_password WHERE id = 1`).Scan(&credentials.Hash, &credentials.Version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return PasswordCredentials{}, false, nil
	case err != nil:
		return PasswordCredentials{}, false, fmt.Errorf("load password credentials: %w", err)
	default:
		return credentials, true, nil
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

// UpdatePasswordAndRevokeSessions changes the credential generation and revokes
// every session in one transaction. The expected version prevents two password
// changes that both verified the same old password from committing.
func (s *Store) UpdatePasswordAndRevokeSessions(ctx context.Context, expectedVersion int, passwordHash string, updatedAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	timestamp := updatedAt.UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(
		ctx,
		`UPDATE auth_password
		 SET password_hash = ?, updated_at = ?, password_version = password_version + 1
		 WHERE id = 1 AND password_version = ?`,
		passwordHash,
		timestamp,
		expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update password rows affected: %w", err)
	}
	if rowsAffected != 1 {
		return ErrPasswordVersionChanged
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE auth_sessions SET revoked_at = ? WHERE revoked_at IS NULL`,
		timestamp,
	); err != nil {
		return fmt.Errorf("revoke sessions after password update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit password update: %w", err)
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
	passwordVersion := session.PasswordVersion
	if passwordVersion <= 0 {
		passwordVersion = 1
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO auth_sessions (id, user_id, created_at, last_seen_at, expires_at, user_agent, ip_address, revoked_at, password_version)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?)`,
		session.ID,
		session.UserID,
		session.CreatedAt.UTC().Format(time.RFC3339Nano),
		session.LastSeenAt.UTC().Format(time.RFC3339Nano),
		session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		session.UserAgent,
		session.IPAddress,
		passwordVersion,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// CreateSessionAtPasswordVersion only creates a session if the verified
// password generation is still current. This closes the race between password
// verification and a concurrent password change.
func (s *Store) CreateSessionAtPasswordVersion(ctx context.Context, session Session, passwordVersion int) error {
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO auth_sessions (id, user_id, created_at, last_seen_at, expires_at, user_agent, ip_address, revoked_at, password_version)
		 SELECT ?, ?, ?, ?, ?, ?, ?, NULL, ?
		 FROM auth_password
		 WHERE id = 1 AND password_version = ?`,
		session.ID,
		session.UserID,
		session.CreatedAt.UTC().Format(time.RFC3339Nano),
		session.LastSeenAt.UTC().Format(time.RFC3339Nano),
		session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		session.UserAgent,
		session.IPAddress,
		passwordVersion,
		passwordVersion,
	)
	if err != nil {
		return fmt.Errorf("create versioned session: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("create versioned session rows affected: %w", err)
	}
	if rowsAffected != 1 {
		return ErrPasswordVersionChanged
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
		`SELECT id, user_id, created_at, last_seen_at, expires_at, user_agent, ip_address, revoked_at, password_version
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
		&session.PasswordVersion,
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

func (s *Store) SessionAtCurrentPasswordVersion(ctx context.Context, id string) (Session, error) {
	var rawCreatedAt string
	var rawLastSeenAt string
	var rawExpiresAt string
	var rawRevokedAt sql.NullString

	session := Session{}
	err := s.db.QueryRowContext(
		ctx,
		`SELECT s.id, s.user_id, s.created_at, s.last_seen_at, s.expires_at, s.user_agent, s.ip_address, s.revoked_at, s.password_version
		 FROM auth_sessions s
		 JOIN auth_password p ON p.id = 1 AND p.password_version = s.password_version
		 WHERE s.id = ?`,
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
		&session.PasswordVersion,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Session{}, ErrNotFound
	case err != nil:
		return Session{}, fmt.Errorf("load current-version session: %w", err)
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

// TouchSession atomically advances both timestamps only when the caller's
// authenticated snapshot is still current. A concurrent touch returns
// ErrSessionChanged so the caller can re-read instead of treating a valid
// session as revoked.
func (s *Store) TouchSession(ctx context.Context, id string, expectedLastSeenAt, lastSeenAt, expiresAt time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE auth_sessions
		 SET last_seen_at = ?, expires_at = ?
		 WHERE id = ?
		   AND revoked_at IS NULL
		   AND last_seen_at = ?
		   AND password_version = (SELECT password_version FROM auth_password WHERE id = 1)`,
		lastSeenAt.UTC().Format(time.RFC3339Nano),
		expiresAt.UTC().Format(time.RFC3339Nano),
		id,
		expectedLastSeenAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("touch session rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrSessionChanged
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
	return createJob(ctx, s.db, job)
}

// CreateJobWithInitialEvent persists the complete visible job initialization
// boundary. A caller never observes a running job without its workflow or
// first retained event.
func (s *Store) CreateJobWithInitialEvent(ctx context.Context, job Job, event JobEvent) error {
	if event.JobID != job.ID || event.Sequence != 1 {
		return errors.New("initial job event must use the job id and sequence 1")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin job initialization: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := createJob(ctx, tx, job); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET event_sequence = 1 WHERE id = ?`, job.ID); err != nil {
		return fmt.Errorf("initialize job event sequence: %w", err)
	}
	if err := createJobEvent(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit job initialization: %w", err)
	}
	return nil
}

func createJob(ctx context.Context, executor sqlExecutor, job Job) error {
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

	_, err := executor.ExecContext(
		ctx,
		`INSERT INTO jobs (id, stack_id, action, state, requested_by, request_id, requested_at, started_at, finished_at, workflow_json, error_code, error_message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID,
		job.StackID,
		job.Action,
		job.State,
		job.RequestedBy,
		nullIfEmpty(job.RequestID),
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
	updated, err := s.updateJob(ctx, job, "")
	if err != nil {
		return err
	}
	if !updated {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateJobIfStateIn(ctx context.Context, job Job, states []string) (bool, error) {
	if len(states) == 0 {
		return false, nil
	}
	placeholders := make([]string, len(states))
	args := make([]any, 0, 7+len(states))
	for i, state := range states {
		placeholders[i] = "?"
		args = append(args, state)
	}
	return s.updateJob(ctx, job, " AND state IN ("+strings.Join(placeholders, ",")+")", args...)
}

// UpdateJobWorkflow updates only workflow_json. In particular, a worker with a
// stale Job value cannot move cancel_requested back to running while reporting
// step progress.
func (s *Store) UpdateJobWorkflow(ctx context.Context, id string, workflow *JobWorkflow) error {
	workflowJSON, err := marshalJobWorkflow(workflow)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET workflow_json = ? WHERE id = ?`, workflowJSON, id)
	if err != nil {
		return fmt.Errorf("update job workflow: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update job workflow rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// TransitionJobWithEvents serializes an allowed state transition and all
// events describing it through the job row's sequence counter.
func (s *Store) TransitionJobWithEvents(ctx context.Context, job Job, allowedStates []string, events []JobEvent) (bool, []JobEvent, error) {
	if len(allowedStates) == 0 || len(events) == 0 {
		return false, nil, errors.New("job transition requires allowed states and events")
	}
	for _, event := range events {
		if event.JobID != job.ID || event.State != job.State {
			return false, nil, errors.New("job transition event must match job id and state")
		}
	}

	workflowJSON, err := marshalJobWorkflow(job.Workflow)
	if err != nil {
		return false, nil, err
	}
	var startedAt sql.NullString
	if job.StartedAt != nil {
		startedAt = sql.NullString{String: job.StartedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}
	var finishedAt sql.NullString
	if job.FinishedAt != nil {
		finishedAt = sql.NullString{String: job.FinishedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, nil, fmt.Errorf("begin job transition: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	placeholders := make([]string, len(allowedStates))
	args := []any{
		job.State,
		startedAt,
		finishedAt,
		workflowJSON,
		nullIfEmpty(job.ErrorCode),
		nullIfEmpty(job.ErrorMessage),
		len(events),
		job.ID,
	}
	for index, state := range allowedStates {
		placeholders[index] = "?"
		args = append(args, state)
	}
	result, err := tx.ExecContext(ctx, `UPDATE jobs
		SET state = ?, started_at = ?, finished_at = ?, workflow_json = ?, error_code = ?, error_message = ?,
			event_sequence = event_sequence + ?
		WHERE id = ? AND state IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return false, nil, fmt.Errorf("transition job: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, nil, fmt.Errorf("transition job rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return false, nil, nil
	}

	var lastSequence int
	if err := tx.QueryRowContext(ctx, `SELECT event_sequence FROM jobs WHERE id = ?`, job.ID).Scan(&lastSequence); err != nil {
		return false, nil, fmt.Errorf("load transitioned job event sequence: %w", err)
	}
	committedEvents := make([]JobEvent, len(events))
	firstSequence := lastSequence - len(events) + 1
	for index, event := range events {
		event.Sequence = firstSequence + index
		if err := createJobEvent(ctx, tx, event); err != nil {
			return false, nil, err
		}
		committedEvents[index] = event
	}
	if err := tx.Commit(); err != nil {
		return false, nil, fmt.Errorf("commit job transition: %w", err)
	}
	return true, committedEvents, nil
}

func (s *Store) updateJob(ctx context.Context, job Job, whereSuffix string, whereArgs ...any) (bool, error) {
	workflowJSON, err := marshalJobWorkflow(job.Workflow)
	if err != nil {
		return false, err
	}

	var startedAt sql.NullString
	if job.StartedAt != nil {
		startedAt = sql.NullString{String: job.StartedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}
	var finishedAt sql.NullString
	if job.FinishedAt != nil {
		finishedAt = sql.NullString{String: job.FinishedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	query := `UPDATE jobs
		 SET state = ?, started_at = ?, finished_at = ?, workflow_json = ?, error_code = ?, error_message = ?
		 WHERE id = ?` + whereSuffix
	args := []any{
		job.State,
		startedAt,
		finishedAt,
		workflowJSON,
		nullIfEmpty(job.ErrorCode),
		nullIfEmpty(job.ErrorMessage),
		job.ID,
	}
	args = append(args, whereArgs...)

	result, err := s.db.ExecContext(
		ctx,
		query,
		args...,
	)
	if err != nil {
		return false, fmt.Errorf("update job: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("update job rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

func marshalJobWorkflow(workflow *JobWorkflow) (sql.NullString, error) {
	if workflow == nil {
		return sql.NullString{}, nil
	}
	workflowBytes, err := json.Marshal(workflow)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("marshal workflow: %w", err)
	}
	return sql.NullString{String: string(workflowBytes), Valid: true}, nil
}

func (s *Store) JobByID(ctx context.Context, id string) (Job, error) {
	job, err := scanJob(s.db.QueryRowContext(
		ctx,
		`SELECT id, stack_id, action, state, requested_by, request_id, requested_at, started_at, finished_at, workflow_json, error_code, error_message
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
		`SELECT id, stack_id, action, state, requested_by, request_id, requested_at, started_at, finished_at, workflow_json, error_code, error_message
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

// AppendJobEvent assigns the next sequence and captures the current persisted
// state in one transaction. Concurrent publishers serialize on the job row
// counter, so an event appended after cancellation cannot claim running state.
func (s *Store) AppendJobEvent(ctx context.Context, event JobEvent) (JobEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return JobEvent{}, fmt.Errorf("begin append job event: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `UPDATE jobs SET event_sequence = event_sequence + 1 WHERE id = ?`, event.JobID)
	if err != nil {
		return JobEvent{}, fmt.Errorf("increment job event sequence: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return JobEvent{}, fmt.Errorf("increment job event sequence rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return JobEvent{}, ErrNotFound
	}
	if err := tx.QueryRowContext(ctx, `SELECT event_sequence, state FROM jobs WHERE id = ?`, event.JobID).Scan(&event.Sequence, &event.State); err != nil {
		return JobEvent{}, fmt.Errorf("load appended job event sequence: %w", err)
	}
	if err := createJobEvent(ctx, tx, event); err != nil {
		return JobEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return JobEvent{}, fmt.Errorf("commit appended job event: %w", err)
	}
	return event, nil
}

func (s *Store) CreateJobEvent(ctx context.Context, event JobEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create job event: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := createJobEvent(ctx, tx, event); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET event_sequence = MAX(event_sequence, ?)
		WHERE id = ?
	`, event.Sequence, event.JobID); err != nil {
		return fmt.Errorf("advance explicit job event sequence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit explicit job event: %w", err)
	}
	return nil
}

func createJobEvent(ctx context.Context, executor sqlExecutor, event JobEvent) error {
	var stepJSON sql.NullString
	if event.Step != nil {
		encoded, err := json.Marshal(event.Step)
		if err != nil {
			return fmt.Errorf("marshal job event step: %w", err)
		}
		stepJSON = sql.NullString{String: string(encoded), Valid: true}
	}

	progressJSON := sql.NullString{}
	if event.Progress != nil {
		encoded, err := json.Marshal(event.Progress)
		if err != nil {
			return fmt.Errorf("marshal job event progress: %w", err)
		}
		progressJSON = sql.NullString{String: string(encoded), Valid: true}
	}

	_, err := executor.ExecContext(
		ctx,
		`INSERT INTO job_events (job_id, sequence, event, state, message, data, step_json, progress_json, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.JobID,
		event.Sequence,
		event.Event,
		event.State,
		nullIfEmpty(event.Message),
		nullIfEmpty(event.Data),
		stepJSON,
		progressJSON,
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
		`SELECT job_id, sequence, event, state, message, data, step_json, progress_json, timestamp
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
		`SELECT job_id, sequence, event, state, message, data, step_json, progress_json, timestamp
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
	if query.Search != "" {
		pattern := "%" + escapeLikePattern(strings.ToLower(strings.TrimSpace(query.Search))) + "%"
		where = append(where, `(LOWER(action) LIKE ? ESCAPE '\' OR LOWER(COALESCE(stack_id, '')) LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if len(query.Results) > 0 {
		placeholders := make([]string, len(query.Results))
		for index, result := range query.Results {
			placeholders[index] = "?"
			args = append(args, result)
		}
		where = append(where, "result IN ("+strings.Join(placeholders, ",")+")")
	}
	if query.RequestedFrom != nil {
		where = append(where, "julianday(requested_at) >= julianday(?)")
		args = append(args, query.RequestedFrom.UTC().Format(time.RFC3339Nano))
	}
	if query.RequestedBefore != nil {
		where = append(where, "julianday(requested_at) < julianday(?)")
		args = append(args, query.RequestedBefore.UTC().Format(time.RFC3339Nano))
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

func escapeLikePattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
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
	var rawRequestID sql.NullString
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
		&rawRequestID,
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
	if rawRequestID.Valid {
		job.RequestID = rawRequestID.String
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

func scanJobEvent(scanner interface{ Scan(dest ...any) error }) (JobEvent, error) {
	var rawMessage sql.NullString
	var rawData sql.NullString
	var rawStepJSON sql.NullString
	var rawProgressJSON sql.NullString
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
		&rawProgressJSON,
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
	if rawProgressJSON.Valid {
		var progress JobProgress
		if err := json.Unmarshal([]byte(rawProgressJSON.String), &progress); err != nil {
			return JobEvent{}, fmt.Errorf("unmarshal job event progress: %w", err)
		}
		event.Progress = &progress
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

// --- Image update status (dashboard read-model contract, Slice B) ---

type ImageUpdateStatus struct {
	ImageRef     string    `json:"image_ref"`
	LocalDigest  string    `json:"local_digest,omitempty"`
	RemoteDigest string    `json:"remote_digest,omitempty"`
	State        string    `json:"state"`
	CheckedAt    time.Time `json:"checked_at"`
}

type StackDeployBaseline struct {
	StackID        string
	ComposeSHA256  string
	EnvSHA256      string
	ComposeYAML    string
	Env            string
	EnvExists      bool
	LastDeployedAt time.Time
	LastJobID      string
}

func (s *Store) UpsertImageUpdateStatus(ctx context.Context, status ImageUpdateStatus) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO image_update_status (image_ref, local_digest, remote_digest, state, checked_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(image_ref) DO UPDATE SET
		   local_digest = excluded.local_digest,
		   remote_digest = excluded.remote_digest,
		   state = excluded.state,
		   checked_at = excluded.checked_at`,
		status.ImageRef,
		nullIfEmpty(status.LocalDigest),
		nullIfEmpty(status.RemoteDigest),
		status.State,
		status.CheckedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert image update status: %w", err)
	}
	return nil
}

func (s *Store) ListImageUpdateStatus(ctx context.Context) ([]ImageUpdateStatus, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT image_ref, local_digest, remote_digest, state, checked_at FROM image_update_status ORDER BY image_ref ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list image update status: %w", err)
	}
	defer rows.Close()

	items := make([]ImageUpdateStatus, 0, 16)
	for rows.Next() {
		var item ImageUpdateStatus
		var localDigest, remoteDigest sql.NullString
		var rawCheckedAt string
		if err := rows.Scan(&item.ImageRef, &localDigest, &remoteDigest, &item.State, &rawCheckedAt); err != nil {
			return nil, fmt.Errorf("scan image update status: %w", err)
		}
		item.LocalDigest = localDigest.String
		item.RemoteDigest = remoteDigest.String
		checkedAt, err := time.Parse(time.RFC3339Nano, rawCheckedAt)
		if err != nil {
			return nil, fmt.Errorf("parse image update checked_at: %w", err)
		}
		item.CheckedAt = checkedAt
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate image update status: %w", err)
	}
	return items, nil
}

// --- Stack deploy baselines ---

func (s *Store) UpsertStackDeployBaseline(ctx context.Context, baseline StackDeployBaseline) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO stack_deploy_baselines (
		   stack_id, compose_sha256, env_sha256, compose_yaml, env, env_exists, last_deployed_at, last_job_id
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(stack_id) DO UPDATE SET
		   compose_sha256 = excluded.compose_sha256,
		   env_sha256 = excluded.env_sha256,
		   compose_yaml = excluded.compose_yaml,
		   env = excluded.env,
		   env_exists = excluded.env_exists,
		   last_deployed_at = excluded.last_deployed_at,
		   last_job_id = excluded.last_job_id`,
		baseline.StackID,
		baseline.ComposeSHA256,
		baseline.EnvSHA256,
		baseline.ComposeYAML,
		baseline.Env,
		boolInt(baseline.EnvExists),
		baseline.LastDeployedAt.UTC().Format(time.RFC3339Nano),
		nullIfEmpty(baseline.LastJobID),
	)
	if err != nil {
		return fmt.Errorf("upsert stack deploy baseline: %w", err)
	}
	return nil
}

func (s *Store) StackDeployBaseline(ctx context.Context, stackID string) (StackDeployBaseline, bool, error) {
	var baseline StackDeployBaseline
	var rawLastDeployedAt string
	var envExists int
	var lastJobID sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT stack_id, compose_sha256, env_sha256, compose_yaml, env, env_exists, last_deployed_at, last_job_id
		   FROM stack_deploy_baselines
		  WHERE stack_id = ?`,
		stackID,
	).Scan(
		&baseline.StackID,
		&baseline.ComposeSHA256,
		&baseline.EnvSHA256,
		&baseline.ComposeYAML,
		&baseline.Env,
		&envExists,
		&rawLastDeployedAt,
		&lastJobID,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return StackDeployBaseline{}, false, nil
	case err != nil:
		return StackDeployBaseline{}, false, fmt.Errorf("load stack deploy baseline: %w", err)
	}
	lastDeployedAt, err := time.Parse(time.RFC3339Nano, rawLastDeployedAt)
	if err != nil {
		return StackDeployBaseline{}, false, fmt.Errorf("parse stack deploy baseline last_deployed_at: %w", err)
	}
	baseline.EnvExists = envExists != 0
	baseline.LastDeployedAt = lastDeployedAt
	baseline.LastJobID = lastJobID.String
	return baseline, true, nil
}

func (s *Store) ListStackDeployBaselines(ctx context.Context) ([]StackDeployBaseline, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT stack_id, compose_sha256, env_sha256, compose_yaml, env, env_exists, last_deployed_at, last_job_id
		   FROM stack_deploy_baselines
		  ORDER BY stack_id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list stack deploy baselines: %w", err)
	}
	defer rows.Close()

	items := make([]StackDeployBaseline, 0, 16)
	for rows.Next() {
		var item StackDeployBaseline
		var rawLastDeployedAt string
		var envExists int
		var lastJobID sql.NullString
		if err := rows.Scan(&item.StackID, &item.ComposeSHA256, &item.EnvSHA256, &item.ComposeYAML, &item.Env, &envExists, &rawLastDeployedAt, &lastJobID); err != nil {
			return nil, fmt.Errorf("scan stack deploy baseline: %w", err)
		}
		lastDeployedAt, err := time.Parse(time.RFC3339Nano, rawLastDeployedAt)
		if err != nil {
			return nil, fmt.Errorf("parse stack deploy baseline last_deployed_at: %w", err)
		}
		item.EnvExists = envExists != 0
		item.LastDeployedAt = lastDeployedAt
		item.LastJobID = lastJobID.String
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stack deploy baselines: %w", err)
	}
	return items, nil
}

func (s *Store) DeleteStackDeployBaseline(ctx context.Context, stackID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM stack_deploy_baselines WHERE stack_id = ?`, stackID); err != nil {
		return fmt.Errorf("delete stack deploy baseline: %w", err)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
