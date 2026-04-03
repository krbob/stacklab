# Operation Model

## Purpose

This document defines Stacklab job execution, action semantics, locking rules, and audit expectations.

## Core Principle

Every mutating stack action is executed as a tracked job.

Jobs provide:

- serialization
- progress reporting
- auditability
- recoverable failure reporting

## Operation Categories

### Read Operations

Read operations do not mutate stack state and do not require a stack lock.

Examples:

- list stacks
- get stack detail
- inspect resolved config
- fetch current status snapshot

### Streaming Diagnostic Operations

Streaming diagnostics are session-based and do not acquire a mutating stack lock.

Examples:

- follow logs
- stream stats
- open container shell

Rules:

- they may coexist with read operations
- they may remain available during some mutating jobs when safe
- backend may reject them temporarily during disruptive operations

### Mutating Stack Operations

Mutating operations always create a job and acquire a per-stack lock.

MVP actions:

- `validate`
- `up`
- `down`
- `stop`
- `restart`
- `pull`
- `build`
- `recreate`
- `save_definition`
- `create_stack`
- `remove_stack_definition`

Notes:

- `validate` may be implemented as a non-locking job initially, but the API should still model it as a job-capable action for consistency
- `recreate` is a higher-level semantic action that may map to a sequence such as `pull/build` followed by `up`

## Job Model

### Job Identity

Each job has:

- `id`
- `stack_id`
- `action`
- `requested_by`
- `requested_at`
- `started_at`
- `finished_at`

### Job State

Allowed values:

- `queued`
- `running`
- `succeeded`
- `failed`
- `cancel_requested`
- `cancelled`
- `timed_out`

Rules:

- only one mutating job may be `running` per stack
- queued jobs for the same stack are optional in v1; backend may reject instead of queueing
- v1 may reject cancellation for actions that cannot be safely interrupted

### Workflow Jobs

Some jobs may internally consist of ordered steps while still remaining a single externally visible job.

Examples:

- `recreate`
- `create_stack` when `deploy_after_create = true`

Rules:

- the UI should still treat the workflow as one job for progress and audit purposes
- backend may expose current step and step events in streaming protocols
- the top-level job `action` remains the user-requested action

### Job Output

Each job may emit structured events:

- `job_started`
- `job_step_started`
- `job_step_finished`
- `job_progress`
- `job_log`
- `job_warning`
- `job_error`
- `job_finished`

UI should treat job events as append-only stream messages, not as the source of truth for final stack state.

## Locking Model

### Lock Scope

Primary lock scope is `per stack`.

Meaning:

- stack `A` and stack `B` may run mutating jobs concurrently
- stack `A` may not run conflicting mutating jobs concurrently

### Lock Behavior

When a mutating request arrives for a locked stack, backend behavior in v1 should be:

- default: reject with a conflict response
- optional later: queue after explicit policy is added

This keeps the initial implementation simple and explicit.

## Action Semantics

### Validate

Purpose:

- check if current stack definition is structurally deployable

Expected implementation:

- `docker compose config` or equivalent validation flow

Effects:

- updates `config_state`
- does not mutate runtime resources

### Up

Purpose:

- reconcile stack definition into running runtime state

Effects:

- may create, start, or replace containers
- updates deployment baseline on success

### Down

Purpose:

- remove stack runtime resources from Docker

Effects:

- does not remove definition, config, or data directories by default

### Stop

Purpose:

- stop current runtime containers without removing definition

### Restart

Purpose:

- restart runtime containers while preserving stack definition

### Pull

Purpose:

- update image-based services from remote registries

Rules:

- relevant mainly to services with `mode = image` or `mode = hybrid`

### Build

Purpose:

- rebuild build-based services from local contexts

Rules:

- relevant mainly to services with `mode = build` or `mode = hybrid`

### Recreate

Purpose:

- refresh runtime from current definition after image updates or rebuilds

Rules:

- API should expose it as a first-class user action
- backend may implement it as an orchestrated action sequence

### Save Definition

Purpose:

- persist updated `compose.yaml` and optionally `.env`

Rules:

- must run validation before enabling deploy-oriented follow-up actions
- does not automatically deploy unless an explicit combined action is invoked later

### Create Stack

Purpose:

- create canonical directories and initial files for a new stack

Default files:

- `/opt/stacklab/stacks/<stack>/compose.yaml`
- optional `.env`
- optional `/opt/stacklab/config/<stack>/`
- optional `/opt/stacklab/data/<stack>/`

### Remove Stack Definition

Purpose:

- remove stack definition files and optionally related directories

Rules:

- destructive options must be explicit and separately declared
- deleting data is never the default path

## Safety Rules

- backend must validate stack ownership and path safety before file operations
- mutating operations must not escape the configured root
- mutating operations must write audit entries
- failed jobs must preserve enough detail for operator diagnosis

## Audit Model

Every mutating job should produce an audit record with at least:

- `id`
- `stack_id`
- `action`
- `requested_by`
- `requested_at`
- `result`
- `duration_ms`

Optional later fields:

- request payload summary
- affected services
- output excerpt

Audit views should be available:

- globally
- per stack

## Terminal Session Model

### Container Shell

Container shell sessions are diagnostic sessions, not stack jobs.

Session fields:

- `session_id`
- `stack_id`
- `container_id`
- `created_by`
- `created_at`
- `last_activity_at`

Container shell is in MVP.

### Host Shell

Host shell is intentionally outside MVP.

The terminal subsystem should be designed so this capability can be added later without redefining the frontend terminal model.
