# SQLite Schema

## Purpose and source of truth

SQLite stores Stacklab's operational state and metadata. It does not replace
the filesystem as the source of truth for stack definitions.

The authoritative schema is the migration registry in
[`internal/store/migrations.go`](../../internal/store/migrations.go). This
document describes the current schema at version 5 and its application-level
contract. When this document and executable migrations disagree, migrations
win and this document must be corrected in the same change.

The persistence models and queries in
[`internal/store/store.go`](../../internal/store/store.go) define how the
schema is used after migration.

## Design principles

- stack definitions stay on disk under the managed stacks root;
- SQLite stores settings, credentials metadata, sessions, jobs, audit entries,
  deploy baselines, and small operational read models;
- database loss must be survivable without losing stack definitions;
- writes remain moderate and predictable on a single host;
- references between operational tables are logical rather than enforced by
  foreign keys, which keeps retention and partial recovery simple.

## Runtime configuration

The store configures SQLite on startup with:

- `PRAGMA journal_mode = WAL`;
- `PRAGMA busy_timeout = 5000`.

The data directory is restricted to mode `0700` and the database, WAL, and SHM
files to mode `0600`. Timestamps written by the store are UTC RFC 3339 values
with nanosecond precision.

## Current tables

| Table | Responsibility |
|---|---|
| `schema_migrations` | Applied migration history |
| `app_settings` | Versioned JSON settings and small runtime state |
| `auth_password` | Single-operator password hash and credential generation |
| `auth_sessions` | Server-side authenticated sessions |
| `jobs` | Current state of mutating operations |
| `job_events` | Retained, ordered job event stream |
| `audit_entries` | Durable summaries of operations |
| `image_update_status` | Cached image digest comparison results |
| `stack_deploy_baselines` | Last successful deployment snapshots for drift detection |

There are no `job_locks`, `terminal_audit_events`, or `metrics_rollups` tables
in schema version 5. Job locks are process-local, terminal metadata uses
`audit_entries`, and charts keep a frontend-session buffer.

## Table specifications

### `schema_migrations`

Records the authoritative, append-only migration history.

| Column | Type | Contract |
|---|---|---|
| `version` | `INTEGER PRIMARY KEY` | Contiguous schema version |
| `name` | `TEXT NOT NULL` | Stable registry name |
| `applied_at` | `TEXT NOT NULL` | UTC RFC 3339 migration commit time |

Rules:

- each numbered migration runs in its own transaction;
- its schema changes, data backfill, and history row commit or roll back
  together;
- recorded versions must be contiguous and names must match the binary's
  registry;
- a binary refuses to open a database containing a newer schema version;
- migrations are forward-only and additive where practical.

Current registry:

| Version | Name | Change |
|---|---|---|
| 1 | `initial_schema` | Operational tables and indexes |
| 2 | `job_event_progress` | Adds `job_events.progress_json` |
| 3 | `password_version` | Adds credential generations to password and session rows |
| 4 | `job_event_sequence` | Adds and backfills `jobs.event_sequence` |
| 5 | `job_request_id` | Adds optional `jobs.request_id` correlation |

Before rolling application code back across a schema-changing release, restore
the corresponding database backup or use a binary that supports the recorded
schema version.

### `app_settings`

Stores JSON settings and small persisted runtime state, keyed by subsystem.

| Column | Type | Contract |
|---|---|---|
| `key` | `TEXT PRIMARY KEY` | Stable, usually versioned setting name |
| `value_json` | `TEXT NOT NULL` | JSON-encoded value |
| `updated_at` | `TEXT NOT NULL` | UTC RFC 3339 update time |

Current consumers include notifications, maintenance schedules, host
observability, and self-update state. Values are replaced atomically with an
upsert; their JSON shape is owned by the corresponding subsystem.

### `auth_password`

Stores password hash metadata for the single local operator.

| Column | Type | Contract |
|---|---|---|
| `id` | `INTEGER PRIMARY KEY CHECK (id = 1)` | Singleton row |
| `password_hash` | `TEXT NOT NULL` | One-way password hash |
| `updated_at` | `TEXT NOT NULL` | UTC RFC 3339 update time |
| `password_version` | `INTEGER NOT NULL DEFAULT 1` | Credential generation |

Rules:

- there is at most one logical row and its ID is always `1`;
- plaintext passwords and reversible secrets are never stored;
- changing a password increments `password_version` and revokes all active
  sessions in the same transaction.

### `auth_sessions`

Stores authenticated server-side sessions.

| Column | Type | Contract |
|---|---|---|
| `id` | `TEXT PRIMARY KEY` | Session ID |
| `user_id` | `TEXT NOT NULL` | Local operator ID |
| `created_at` | `TEXT NOT NULL` | UTC RFC 3339 creation time |
| `last_seen_at` | `TEXT NOT NULL` | Last successful activity time |
| `expires_at` | `TEXT NOT NULL` | Current expiry time |
| `user_agent` | `TEXT` | Optional forensic context |
| `ip_address` | `TEXT` | Optional forensic context |
| `revoked_at` | `TEXT` | Null while active |
| `password_version` | `INTEGER NOT NULL DEFAULT 1` | Credential generation verified at login |

Indexes:

- `idx_auth_sessions_expires_at` on `(expires_at)`;
- `idx_auth_sessions_revoked_at` on `(revoked_at)`.

Rules:

- expired or revoked sessions are invalid;
- a session is invalid when its `password_version` differs from the current
  `auth_password.password_version`;
- session creation can be conditioned on the verified credential generation,
  closing the login-versus-password-change race;
- session activity updates use optimistic concurrency on `last_seen_at` and the
  credential generation.

### `jobs`

Stores the current summary state of mutating operations.

| Column | Type | Contract |
|---|---|---|
| `id` | `TEXT PRIMARY KEY` | Stable job ID |
| `stack_id` | `TEXT NOT NULL` | Stack ID, or an empty string for a global job |
| `action` | `TEXT NOT NULL` | Domain action name |
| `state` | `TEXT NOT NULL` | Current application-level job state |
| `requested_by` | `TEXT NOT NULL` | Initiating operator |
| `requested_at` | `TEXT NOT NULL` | UTC RFC 3339 request time |
| `started_at` | `TEXT` | Null until started |
| `finished_at` | `TEXT` | Null until terminal |
| `workflow_json` | `TEXT` | Optional `JobWorkflow` payload |
| `error_code` | `TEXT` | Optional terminal failure classification |
| `error_message` | `TEXT` | Optional short failure summary |
| `event_sequence` | `INTEGER NOT NULL DEFAULT 0` | Last sequence reserved for this job |
| `request_id` | `TEXT` | Optional originating HTTP request ID |

Indexes:

- `idx_jobs_stack_requested_at` on `(stack_id, requested_at DESC)`;
- `idx_jobs_state_requested_at` on `(state, requested_at DESC)`.

Application states are `queued`, `running`, `cancel_requested`, `succeeded`,
`failed`, `cancelled`, and `timed_out`. They are intentionally not constrained
with a database `CHECK`, so transitions remain an application contract.

Rules:

- `jobs` is the current summary; detailed retained output belongs in
  `job_events`;
- initial workflow metadata, the job row, and sequence-1 `job_started` event
  are committed together;
- transitions reserve event sequences and persist their events in the same
  transaction;
- active-job queries include `queued`, `running`, and `cancel_requested`.

`workflow_json` has the shape represented by `JobWorkflow`: a `steps` array in
which each item has `action`, `state`, optional `target_stack_id`, and optional
`target_service_names`.

### `job_events`

Stores the retained, append-only event stream for jobs.

| Column | Type | Contract |
|---|---|---|
| `job_id` | `TEXT NOT NULL` | Logical reference to `jobs.id` |
| `sequence` | `INTEGER NOT NULL` | Per-job monotonically increasing sequence |
| `event` | `TEXT NOT NULL` | Application event name |
| `state` | `TEXT NOT NULL` | Persisted job state at append time |
| `message` | `TEXT` | Optional human-readable summary |
| `data` | `TEXT` | Optional output chunk or event data |
| `step_json` | `TEXT` | Optional `JobEventStep` payload |
| `timestamp` | `TEXT NOT NULL` | UTC RFC 3339 event time |
| `progress_json` | `TEXT` | Optional `JobProgress` payload |

Primary key and indexes:

- composite primary key `(job_id, sequence)`;
- `idx_job_events_job_sequence` on `(job_id, sequence ASC)`;
- `idx_job_events_timestamp` on `(timestamp)`.

There is no synthetic event ID, `sequence_no`, `event_type`, or `created_at`
column. Consumers replay rows ordered by `sequence`.

Current application event names include `job_started`, `job_step_started`,
`job_step_finished`, `job_progress`, `job_log`, `job_warning`, `job_error`,
`job_cancel_requested`, and `job_finished`. Event names are not database-
constrained.

Rules:

- event sequences are allocated by atomically incrementing
  `jobs.event_sequence`, then reading the job's persisted state;
- appending and advancing the owning job sequence happen in one transaction;
- terminal state transitions and their final events are atomic;
- rows are intended for UI replay within retention limits, not as an unlimited
  log sink.

`step_json` represents `JobEventStep`: `index`, `total`, `action`, optional
`target_stack_id`, and optional `target_service_names`.

`progress_json` represents `JobProgress`: `phase`, `completed`, `total`,
`unit`, and optional `detail`.

### `audit_entries`

Stores durable summaries of mutating operations and terminal metadata events.

| Column | Type | Contract |
|---|---|---|
| `id` | `TEXT PRIMARY KEY` | Stable audit ID |
| `stack_id` | `TEXT` | Optional stack scope |
| `job_id` | `TEXT` | Optional logical reference to `jobs.id` |
| `action` | `TEXT NOT NULL` | Domain or metadata action |
| `requested_by` | `TEXT NOT NULL` | Initiating operator |
| `requested_at` | `TEXT NOT NULL` | UTC RFC 3339 request time |
| `finished_at` | `TEXT` | Optional completion time |
| `result` | `TEXT NOT NULL` | Application-level result |
| `duration_ms` | `INTEGER` | Optional duration |
| `target_type` | `TEXT NOT NULL` | Target category |
| `target_id` | `TEXT` | Optional target ID |
| `detail_json` | `TEXT` | Optional structured summary |

Indexes:

- `idx_audit_entries_stack_requested_at` on
  `(stack_id, requested_at DESC)`;
- `idx_audit_entries_requested_at` on `(requested_at DESC)`;
- `idx_audit_entries_action_requested_at` on
  `(action, requested_at DESC)`.

Rules:

- audit rows are summaries, not raw logs;
- stack operation rows should carry `job_id` so the UI can navigate to retained
  job details;
- `job_id` may be null for terminal metadata and system-level entries;
- list queries order by `(requested_at DESC, id DESC)` and use that pair for
  cursor pagination.

### `image_update_status`

Caches the latest comparison between local and remote image digests.

| Column | Type | Contract |
|---|---|---|
| `image_ref` | `TEXT PRIMARY KEY` | Normalized image reference |
| `local_digest` | `TEXT` | Optional locally present digest |
| `remote_digest` | `TEXT` | Optional registry digest |
| `state` | `TEXT NOT NULL` | Comparison result |
| `checked_at` | `TEXT NOT NULL` | UTC RFC 3339 check time |

Current states are `up_to_date`, `available`, and `unknown`. Rows are upserted
by image reference and loaded into the in-memory image-update read model at
startup. `unknown` covers cases where either digest cannot be resolved, such as
missing local images or inaccessible registry metadata.

These rows are a cache, not authoritative runtime inventory, and are not
removed by the operational retention job.

### `stack_deploy_baselines`

Stores the last successful deployment snapshot used for drift detection and
`resolved-config?source=last_valid`.

| Column | Type | Contract |
|---|---|---|
| `stack_id` | `TEXT PRIMARY KEY` | Stack ID |
| `compose_sha256` | `TEXT NOT NULL` | Hash of normalized saved Compose content |
| `env_sha256` | `TEXT NOT NULL` | Hash of saved `.env` content |
| `compose_yaml` | `TEXT NOT NULL` | Compose snapshot after successful deploy |
| `env` | `TEXT NOT NULL` | `.env` snapshot, empty when absent |
| `env_exists` | `INTEGER NOT NULL` | Boolean encoded as `0` or `1` |
| `last_deployed_at` | `TEXT NOT NULL` | UTC RFC 3339 deployment time |
| `last_job_id` | `TEXT` | Optional deployment job ID |

Rules:

- rows are upserted only for successful deployment-oriented actions;
- the hashes determine `config_state = in_sync` versus `drifted`;
- snapshots allow the editor to resolve the last deployed configuration;
- deleting a stack baseline returns drift state to `unknown`;
- stack-scoped files outside Compose and `.env` are not included in the drift
  hash.

`env_exists` has no database default in schema version 5; every insert must
supply it explicitly.

## Retention behavior

Default retention values are:

| Data | Default | Pruning behavior |
|---|---:|---|
| `audit_entries` | 180 days | Delete by `requested_at` |
| terminal `jobs` | 180 days | Keep active jobs and jobs referenced by retained audit rows |
| `job_events` | 14 days | Keep events for active jobs regardless of age |
| expired or revoked `auth_sessions` | 7 days | Delete after the corresponding expiry or revocation age |

Pruning is transactional. When deleting an old terminal job, its events are
removed first. Password metadata, settings, image update status, and deploy
baselines are not part of this operational pruning pass.

See [Retention and audit](./retention-and-audit.md) for the lifecycle and audit
policy.

## JSON storage

`*_json` columns store small structured payloads as JSON text. JSON validity is
enforced by application encoding and decoding, not by SQLite constraints.
This applies to settings, job workflow and event payloads, and audit details.

## Concurrency and atomicity

SQLite fits the single-host workload, but the store still uses short
transactions and avoids holding a transaction while streaming external
process output.

Important atomic boundaries are:

- password replacement, credential generation increment, and session
  revocation;
- job creation, workflow persistence, and the initial event;
- job state transitions, event-sequence reservation, and transition events;
- each schema migration and its migration-history row;
- a complete retention pruning pass.

## Derived data and recovery

Read models combine several sources:

- stack list state = filesystem + Docker + `jobs` +
  `stack_deploy_baselines` + `image_update_status`;
- audit screens = `audit_entries`;
- job recovery after navigation = `jobs` + `job_events`.

If SQLite is lost, stack definitions remain on disk and runtime discovery still
works. Settings, sessions, audit history, jobs, cached image status, and deploy
baselines are lost. Drift detection falls back to `unknown` until a new
successful deployment establishes a baseline.
