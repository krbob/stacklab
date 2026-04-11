# SQLite Schema

## Purpose

This document defines the SQLite persistence model for Stacklab v1.

The database stores operational state and metadata. It does **not** replace the filesystem as the source of truth for stack definitions.

## Design Principles

- stack definitions stay on disk under `/opt/stacklab/stacks`
- SQLite stores only metadata, settings, sessions, jobs, audit entries, and deploy baselines
- database loss must be survivable without losing stack definitions
- write volume should remain moderate and predictable on a single host

## What Belongs In SQLite

- application settings
- password hash metadata
- authenticated sessions
- mutating jobs
- audit entries
- deploy baseline metadata used for drift detection
- optional lightweight caches

## What Does Not Belong In SQLite

- full `compose.yaml` source of truth
- full `.env` source of truth
- container logs as a primary log store
- metrics history in v1; stats charts keep only a frontend session buffer
- arbitrary Docker runtime inventory as authoritative state

## Entity Overview

Proposed v1 tables:

- `app_settings`
- `auth_password`
- `auth_sessions`
- `jobs`
- `job_events`
- `audit_entries`
- `stack_deploy_baselines`

Optional later tables:

- `job_locks`
- `terminal_audit_events`
- `metrics_rollups`

## Table Specifications

## `app_settings`

Stores singleton-like application settings.

Suggested columns:

| Column | Type | Notes |
|---|---|---|
| `key` | `TEXT PRIMARY KEY` | Setting name |
| `value_json` | `TEXT NOT NULL` | JSON-encoded value |
| `updated_at` | `TEXT NOT NULL` | ISO 8601 UTC |

Examples:

- `feature_flags`
- `auth_policy`
- `ui_preferences`

## `auth_password`

Stores password hash metadata for the single local operator.

Suggested columns:

| Column | Type | Notes |
|---|---|---|
| `id` | `INTEGER PRIMARY KEY CHECK (id = 1)` | Singleton row |
| `password_hash` | `TEXT NOT NULL` | Argon2id hash |
| `updated_at` | `TEXT NOT NULL` | ISO 8601 UTC |
| `password_version` | `INTEGER NOT NULL DEFAULT 1` | Future migration support |

Rules:

- exactly one logical row
- no plaintext password or reversible secret storage

## `auth_sessions`

Stores authenticated application sessions.

Suggested columns:

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT PRIMARY KEY` | Server-side session ID |
| `user_id` | `TEXT NOT NULL` | `local` in v1 |
| `created_at` | `TEXT NOT NULL` | ISO 8601 UTC |
| `last_seen_at` | `TEXT NOT NULL` | ISO 8601 UTC |
| `expires_at` | `TEXT NOT NULL` | Idle or absolute expiry |
| `user_agent` | `TEXT` | Optional forensic context |
| `ip_address` | `TEXT` | Optional forensic context |
| `revoked_at` | `TEXT` | Null if active |

Indexes:

- index on `expires_at`
- index on `revoked_at`

Rules:

- expired or revoked sessions are invalid
- WebSocket upgrade and REST requests resolve through this table or an equivalent server-side session store

## `jobs`

Stores mutating stack jobs.

Suggested columns:

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT PRIMARY KEY` | Stable job ID |
| `stack_id` | `TEXT NOT NULL` | Stack directory name |
| `action` | `TEXT NOT NULL` | Domain action name |
| `state` | `TEXT NOT NULL` | Domain job state |
| `requested_by` | `TEXT NOT NULL` | `local` in v1 |
| `requested_at` | `TEXT NOT NULL` | ISO 8601 UTC |
| `started_at` | `TEXT` | Null until running |
| `finished_at` | `TEXT` | Null until terminal |
| `workflow_json` | `TEXT` | Optional workflow steps metadata |
| `error_code` | `TEXT` | Terminal failure classification |
| `error_message` | `TEXT` | Short failure summary |

Allowed `state` values:

- `queued`
- `running`
- `succeeded`
- `failed`
- `cancel_requested`
- `cancelled`
- `timed_out`

Indexes:

- index on `(stack_id, requested_at DESC)`
- index on `(state, requested_at DESC)`

Rules:

- this table stores the current summary view of a job
- detailed streamed output belongs in `job_events`

## `job_events`

Stores append-only event records for mutating jobs.

Suggested columns:

| Column | Type | Notes |
|---|---|---|
| `id` | `INTEGER PRIMARY KEY AUTOINCREMENT` | Monotonic DB-local ID |
| `job_id` | `TEXT NOT NULL` | FK-like reference to `jobs.id` |
| `sequence_no` | `INTEGER NOT NULL` | Per-job monotonic sequence |
| `event_type` | `TEXT NOT NULL` | `job_started`, `job_log`, etc. |
| `state` | `TEXT` | Job state at time of event |
| `message` | `TEXT` | Human-readable summary |
| `data` | `TEXT` | Raw output chunk when relevant |
| `step_json` | `TEXT` | Optional workflow step metadata |
| `created_at` | `TEXT NOT NULL` | ISO 8601 UTC |

Indexes:

- unique index on `(job_id, sequence_no)`
- index on `(job_id, created_at)`

Rules:

- append-only
- suitable for replaying progress panels within retention limits
- not intended as an infinite log sink

Allowed `event_type` values:

- `job_started`
- `job_step_started`
- `job_step_finished`
- `job_progress`
- `job_log`
- `job_warning`
- `job_error`
- `job_finished`

## `audit_entries`

Stores high-level audit records for mutating operations and terminal metadata events.

Suggested columns:

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT PRIMARY KEY` | Stable audit ID |
| `stack_id` | `TEXT` | Nullable for global/system events |
| `job_id` | `TEXT` | Nullable link to `jobs.id` for stack operation audit entries |
| `action` | `TEXT NOT NULL` | Domain action or terminal metadata action |
| `requested_by` | `TEXT NOT NULL` | `local` in v1 |
| `requested_at` | `TEXT NOT NULL` | ISO 8601 UTC |
| `finished_at` | `TEXT` | Null for instant metadata events if unused |
| `result` | `TEXT NOT NULL` | `succeeded`, `failed`, etc. |
| `duration_ms` | `INTEGER` | Nullable for non-duration events |
| `target_type` | `TEXT NOT NULL` | `stack`, `terminal_session`, `system` |
| `target_id` | `TEXT` | Stack ID or session ID |
| `detail_json` | `TEXT` | Small structured summary |

Indexes:

- index on `(stack_id, requested_at DESC)`
- index on `(requested_at DESC)`
- index on `(action, requested_at DESC)`

Rules:

- audit entries are durable summaries, not raw logs
- audit is optimized for read and filtering, not for streaming raw output
- `job_id` should be populated for stack operation audit entries so UI can jump from history to job details and replayable job events
- `job_id` may remain null for terminal metadata events and system-level audit entries

Examples:

- stack `pull`
- stack `save_definition`
- terminal session `opened`
- terminal session `closed`

## `stack_deploy_baselines`

Stores the last known successful deployment baseline for drift detection.

Suggested columns:

| Column | Type | Notes |
|---|---|---|
| `stack_id` | `TEXT PRIMARY KEY` | Stack directory name |
| `compose_sha256` | `TEXT NOT NULL` | Hash of normalized saved compose content |
| `env_sha256` | `TEXT NOT NULL` | Hash of saved `.env` content or empty string hash |
| `last_deployed_at` | `TEXT NOT NULL` | ISO 8601 UTC |
| `last_job_id` | `TEXT` | Reference to deployment job |

Rules:

- updated only on successful deployment-oriented actions
- basis for `config_state = in_sync` vs `drifted`

v1 scope:

- does not include `/opt/stacklab/config/<stack>/` in the drift hash

## Schema Notes

## Foreign Keys

SQLite foreign keys may be enabled, but Stacklab should not depend too heavily on rigid FK enforcement for operational tables if it complicates retention and cleanup.

Recommended posture:

- use logical references
- optionally enable foreign keys where beneficial
- keep retention jobs simple

## JSON Storage

`*_json` columns store small structured payloads as JSON text.

Use cases:

- workflow metadata
- audit detail summaries
- future-compatible structured settings

## Concurrency Expectations

SQLite is acceptable because:

- Stacklab is single-host
- write throughput is moderate
- most screens are read-heavy
- mutating operations are serialized per stack

Backend should still:

- use short transactions
- avoid long write transactions during streaming
- batch event persistence reasonably

## Derived Data Strategy

Read models should be assembled from:

- filesystem scan
- Docker runtime inspection
- SQLite metadata

Important examples:

- stack list state = filesystem + Docker + `jobs` + `stack_deploy_baselines`
- audit screens = `audit_entries`
- progress recovery after navigation = `jobs` + `job_events`

## Recovery Expectations

If SQLite is lost:

- stack definitions remain on disk
- stack discovery still works
- runtime state still works
- audit history and deploy baselines are lost
- drift detection falls back to `config_state = unknown` until new successful deploys happen

This is acceptable for v1.
