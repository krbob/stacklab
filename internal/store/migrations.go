package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrSchemaTooNew = errors.New("sqlite schema is newer than this Stacklab version")

const currentSchemaVersion = 4

type schemaMigration struct {
	version int
	name    string
	up      func(context.Context, *sql.Tx) error
}

var schemaMigrations = []schemaMigration{
	{version: 1, name: "initial_schema", up: migrateInitialSchema},
	{version: 2, name: "job_event_progress", up: migrateJobEventProgress},
	{version: 3, name: "password_version", up: migratePasswordVersion},
	{version: 4, name: "job_event_sequence", up: migrateJobEventSequence},
}

func (s *Store) runMigrations(ctx context.Context) error {
	if len(schemaMigrations) == 0 || schemaMigrations[len(schemaMigrations)-1].version != currentSchemaVersion {
		return errors.New("sqlite migration registry does not match current schema version")
	}
	if err := configureSQLite(ctx, s.db); err != nil {
		return err
	}
	if err := ensureSchemaMigrationsTable(ctx, s.db); err != nil {
		return err
	}

	applied, err := loadAppliedMigrations(ctx, s.db)
	if err != nil {
		return err
	}
	for index, migration := range schemaMigrations {
		if index < len(applied) {
			if applied[index].version != migration.version || applied[index].name != migration.name {
				return fmt.Errorf("invalid sqlite migration history at version %d", migration.version)
			}
			continue
		}
		if err := applySchemaMigration(ctx, s.db, migration); err != nil {
			return err
		}
	}
	return nil
}

func configureSQLite(ctx context.Context, db *sql.DB) error {
	for _, statement := range []string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA busy_timeout = 5000;`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite store: %w", err)
		}
	}
	return nil
}

func ensureSchemaMigrationsTable(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite migration metadata transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create sqlite migration metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite migration metadata: %w", err)
	}
	return nil
}

type appliedMigration struct {
	version int
	name    string
}

func loadAppliedMigrations(ctx context.Context, db *sql.DB) ([]appliedMigration, error) {
	rows, err := db.QueryContext(ctx, `SELECT version, name FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return nil, fmt.Errorf("load sqlite migration history: %w", err)
	}
	defer rows.Close()

	result := make([]appliedMigration, 0, len(schemaMigrations))
	expectedVersion := 1
	for rows.Next() {
		var item appliedMigration
		if err := rows.Scan(&item.version, &item.name); err != nil {
			return nil, fmt.Errorf("scan sqlite migration history: %w", err)
		}
		if item.version > currentSchemaVersion {
			return nil, fmt.Errorf("%w: database=%d supported=%d", ErrSchemaTooNew, item.version, currentSchemaVersion)
		}
		if item.version != expectedVersion {
			return nil, fmt.Errorf("invalid sqlite migration history: expected version %d, found %d", expectedVersion, item.version)
		}
		result = append(result, item)
		expectedVersion++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite migration history: %w", err)
	}
	return result, nil
}

func applySchemaMigration(ctx context.Context, db *sql.DB, migration schemaMigration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite migration %d (%s): %w", migration.version, migration.name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := migration.up(ctx, tx); err != nil {
		return fmt.Errorf("apply sqlite migration %d (%s): %w", migration.version, migration.name, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		migration.version,
		migration.name,
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("record sqlite migration %d (%s): %w", migration.version, migration.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite migration %d (%s): %w", migration.version, migration.name, err)
	}
	return nil
}

func migrateInitialSchema(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS auth_password (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			password_hash TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS auth_sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			user_agent TEXT,
			ip_address TEXT,
			revoked_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires_at ON auth_sessions (expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_revoked_at ON auth_sessions (revoked_at)`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_stack_requested_at ON jobs (stack_id, requested_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_state_requested_at ON jobs (state, requested_at DESC)`,
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_job_events_job_sequence ON job_events (job_id, sequence ASC)`,
		`CREATE INDEX IF NOT EXISTS idx_job_events_timestamp ON job_events (timestamp)`,
		`CREATE TABLE IF NOT EXISTS image_update_status (
			image_ref TEXT PRIMARY KEY,
			local_digest TEXT,
			remote_digest TEXT,
			state TEXT NOT NULL,
			checked_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS stack_deploy_baselines (
			stack_id TEXT PRIMARY KEY,
			compose_sha256 TEXT NOT NULL,
			env_sha256 TEXT NOT NULL,
			compose_yaml TEXT NOT NULL,
			env TEXT NOT NULL,
			env_exists INTEGER NOT NULL,
			last_deployed_at TEXT NOT NULL,
			last_job_id TEXT
		)`,
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_entries_stack_requested_at ON audit_entries (stack_id, requested_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_entries_requested_at ON audit_entries (requested_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_entries_action_requested_at ON audit_entries (action, requested_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func migrateJobEventProgress(ctx context.Context, tx *sql.Tx) error {
	return addColumnIfMissing(ctx, tx, "job_events", "progress_json", `ALTER TABLE job_events ADD COLUMN progress_json TEXT`)
}

func migratePasswordVersion(ctx context.Context, tx *sql.Tx) error {
	if err := addColumnIfMissing(ctx, tx, "auth_password", "password_version", `ALTER TABLE auth_password ADD COLUMN password_version INTEGER NOT NULL DEFAULT 1`); err != nil {
		return err
	}
	return addColumnIfMissing(ctx, tx, "auth_sessions", "password_version", `ALTER TABLE auth_sessions ADD COLUMN password_version INTEGER NOT NULL DEFAULT 1`)
}

func migrateJobEventSequence(ctx context.Context, tx *sql.Tx) error {
	if err := addColumnIfMissing(ctx, tx, "jobs", "event_sequence", `ALTER TABLE jobs ADD COLUMN event_sequence INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET event_sequence = COALESCE((
			SELECT MAX(job_events.sequence)
			FROM job_events
			WHERE job_events.job_id = jobs.id
		), 0)
		WHERE event_sequence < COALESCE((
			SELECT MAX(job_events.sequence)
			FROM job_events
			WHERE job_events.job_id = jobs.id
		), 0)
	`)
	return err
}

func addColumnIfMissing(ctx context.Context, tx *sql.Tx, table, column, alterStatement string) error {
	var exists int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM pragma_table_info(?) WHERE name = ?)`,
		table,
		column,
	).Scan(&exists); err != nil {
		return fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	if exists != 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, alterStatement); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}
