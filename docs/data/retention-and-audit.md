# Retention And Audit

## Purpose

This document defines retention strategy for operational records in Stacklab v1.

The goal is to preserve useful operator history without turning SQLite into an unbounded event sink.

## Principles

- audit summaries live longer than detailed job output
- progress replay is useful for recent work, not for indefinite archival
- retention must preserve enough context for troubleshooting common failures
- cleanup must be predictable and safe

## Data Classes

## Durable Audit Summaries

Primary table:

- `audit_entries`

Purpose:

- power global history and per-stack history
- answer "what happened, when, and with what outcome?"

Characteristics:

- compact
- filterable
- low write volume

## Recent Job Detail

Primary tables:

- `jobs`
- `job_events`

Purpose:

- support live progress
- support short-term replay after navigation or refresh
- support failed job diagnosis from history links

Characteristics:

- potentially noisy
- more write-heavy
- not intended for long-term archival

## Auth And Session State

Primary table:

- `auth_sessions`

Purpose:

- enforce active authenticated sessions

Characteristics:

- ephemeral
- should be cleaned aggressively after expiry

## Deploy Baselines

Primary table:

- `stack_deploy_baselines`

Purpose:

- support drift detection

Characteristics:

- tiny
- retained indefinitely until superseded or stack removal

## Recommended Retention Windows

## `audit_entries`

Recommended v1 retention:

- keep for `180 days`

Rationale:

- enough for meaningful homelab troubleshooting history
- compact enough for SQLite

Alternative acceptable range:

- `90` to `365 days`

## `jobs`

Recommended v1 retention:

- keep for `180 days`

Rules:

- terminal job summaries are retained for the same window as `audit_entries`
- detailed output is not retained for the full job summary window
- jobs referenced by retained audit entries should not be pruned earlier than their audit entry

Rationale:

- audit links stay useful even after detailed progress output is purged
- job rows are compact compared with streamed `job_events`
- old audit rows can still open a job detail drawer with metadata and a clear retained-output message

## `job_events`

Recommended v1 retention:

- keep for `14 days`

Rationale:

- enough for recent failure diagnosis and progress replay
- prevents raw streamed output from growing without bound

Important UX consequence:

- old audit entries may remain after their detailed job events are purged
- in that case, "View log" should gracefully report that detailed output is no longer retained

## `auth_sessions`

Recommended v1 retention:

- delete expired or revoked sessions after `7 days`

Rationale:

- short forensic window without clutter

## `stack_deploy_baselines`

Retention:

- keep indefinitely while the stack exists
- remove when stack definition is explicitly deleted and no runtime remains

## Cleanup Strategy

Cleanup should run as a lightweight periodic maintenance job.

Recommended frequency:

- once per day

Tasks:

1. delete expired/revoked sessions older than retention window
2. delete old `job_events`
3. delete old terminal job summaries beyond retention window when they are no longer referenced by retained audit entries
4. delete old audit entries beyond retention window
5. run `VACUUM` sparingly, not on every cleanup pass

Recommended `VACUUM` cadence:

- manual, or
- automatic at low frequency such as weekly or after large cleanup thresholds

## Audit Linking Rules

## Stack Operation Audit

For mutating stack operations:

- `audit_entries.job_id` should reference the related job
- UI may use that link for "View log" or "Open job details"

Expected flow:

1. user opens audit screen
2. UI reads `audit_entries`
3. user clicks "View log" on a failed stack operation
4. UI fetches `GET /api/jobs/{jobId}`
5. UI attaches to job history or receives a retention message

## Terminal Metadata Audit

For terminal session metadata events:

- `audit_entries.target_type = terminal_session`
- `audit_entries.job_id = NULL`

These events are not linked to `jobs` because terminal sessions are not modeled as mutating stack jobs.

## Retention UX Rules

Frontend should distinguish:

- audit summary exists
- detailed job output exists

Example outcomes:

- audit entry exists and `job_events` still exist → show detailed progress log
- audit entry exists but `job_events` were purged → show "Detailed output is no longer retained"
- audit entry exists with `job_id = NULL` → no "View log" link

## Recommended API Behavior For Missing Retained Detail

If the UI asks for old job detail and detailed events are gone:

- `GET /api/jobs/{jobId}` may still return the job summary if retained
- job event fetch or stream replay should return a clear empty-state response

Recommended message:

```text
Detailed output for this job is no longer retained.
```

This should not be treated as an error condition in the UI.

## Secret-Safe Retention

Retention rules must account for possible secret exposure in:

- job output
- validation errors
- resolved config previews

Rules:

- do not retain raw output longer than necessary
- avoid persisting full resolved config snapshots in SQLite in v1
- audit detail should remain compact and non-secret by default

## Stack Removal Interaction

When a stack is removed:

- filesystem definition and runtime handling follow explicit user choices
- audit history may be retained even after stack deletion
- deploy baselines for that stack should be removed when the stack definition is gone

This preserves historical operator context without keeping obsolete drift metadata.

## Future Extensions

Possible later features:

- configurable retention windows in settings
- export/archive of audit entries before purge
- compressed persisted job output for selected failed jobs
- explicit job detail endpoint beyond current summary model
