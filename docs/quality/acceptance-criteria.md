# Acceptance Criteria

## Purpose

This document defines the acceptance criteria for Stacklab MVP.

The criteria are written to support:

- implementation planning
- manual QA
- future automated verification

## MVP Acceptance Rule

Stacklab MVP is acceptable when:

- all critical acceptance criteria below are met
- no known blocker remains in authentication, stack discovery, stack actions, editor validation, logs, stats, terminal, or audit
- the product can manage real Compose stacks on one Linux host without taking ownership away from the filesystem

## Environment Preconditions

These criteria assume:

- one Linux host
- `amd64` as the primary deployment architecture
- `arm64` also supported
- Docker Engine installed and running
- Compose v2 available through either `docker compose` or standalone `docker-compose`
- Stacklab deployed host-natively via the documented model
- managed roots follow the active install mode:
  - package-managed installs use `/srv/stacklab`
  - tarball installs use `/opt/stacklab`

## A. Authentication And Session

- user can reach the login screen and authenticate with the configured password
- failed login shows a clear error without exposing sensitive details
- authenticated session persists across normal page navigation
- logout destroys the session
- expired session forces return to login
- unauthenticated access to REST and WebSocket protected resources is rejected

## B. Stack Discovery And Dashboard

- Stacklab discovers every directory under the managed stacks root containing canonical `compose.yaml`
- a valid new stack added manually to the filesystem appears in the dashboard without database seeding
- removing a stack definition from disk removes it from normal stack discovery
- stack list shows correct `display_state`, `config_state`, and `activity_state`
- stack list summary counts are correct for running, stopped, error, defined, and orphaned stacks
- stack search filters results by stack name

## C. Stack Detail

- opening a stack detail page shows services defined in `compose.yaml`
- runtime containers are mapped to services correctly
- per-service image/build mode is shown correctly
- ports and key mounts are visible
- health summary is displayed when health information exists
- `capabilities` and `available_actions` from the backend are sufficient to render tabs and action buttons without UI-side rule duplication

## D. Orphaned Behavior

- if runtime containers exist for a stack identity but canonical `compose.yaml` is missing, the stack is presented as `orphaned`
- orphaned overview remains readable
- orphaned stack disables the Editor tab
- orphaned stack keeps Logs, Stats, Terminal, and History available
- orphaned stack exposes only safe available actions consistent with documented behavior

## E. Compose Editor

- editor loads `compose.yaml` from the filesystem
- editor loads `.env` if present
- editor behaves correctly when `.env` is absent
- user can save updated definition content through the API
- invalid config does not prevent saving work in progress
- invalid config prevents deploy-oriented actions
- resolved config preview can be fetched from persisted files
- resolved config preview can be fetched from unsaved draft content without writing files
- validation feedback is clear enough to identify the failure cause

## F. Stack Actions

- `up`, `down`, `stop`, `restart`, `pull`, `build`, and `recreate` can be triggered through the UI when allowed
- every mutating action creates a job
- conflicting mutating actions on the same stack are rejected while the stack is locked
- mutating actions on different stacks can proceed independently
- successful deploy-oriented actions update drift baseline
- default remove flow does not delete config or data
- destructive remove options require explicit user choice

## G. Job Progress

- progress panel can render live job output for active jobs
- job state changes are reflected correctly: `queued`, `running`, `succeeded`, `failed`, `cancel_requested`, `cancelled`, `timed_out`
- workflow jobs expose step progress in a way the UI can render
- navigating away from a stack does not lose the job itself
- returning to an in-progress job allows the UI to recover current state and continue streaming
- old jobs remain visible in summary form within retention limits

## H. Logs

- log view can subscribe to all services in a stack
- log view can filter by selected services
- log lines display timestamps and service identity
- reconnect after socket loss restores log streaming after re-subscription
- log UI remains usable with large buffers through buffering or virtualization

## I. Stats

- stats view shows stack totals
- stats view shows frontend-only session history for stack totals
- stats view shows per-container CPU, memory, and network values
- stats update continuously while containers are running
- stats reconnect behavior is correct after WebSocket reconnect
- empty state is shown when no running containers are available

## J. Terminal

- user can open a container shell session for a running container
- terminal supports `/bin/sh` by default
- `/bin/bash` is offered only when available and allowed
- terminal input, output, and resize all work
- multiple concurrent terminal sessions work up to the configured limit
- opening a session beyond the configured limit is rejected with a clear UI error
- terminal idle timeout closes inactive sessions with the correct reason
- reconnect does not falsely imply PTY resume
- if PTY attach succeeds after reconnect, the session continues
- if PTY attach fails after reconnect, the UI shows a "Session ended" flow and preserves local scrollback

## K. Audit

- every mutating stack action produces an audit entry
- per-stack history and global history both load correctly
- audit rows show action, result, timestamps, and duration
- audit rows for stack jobs link to job detail through `job_id`
- if detailed job events are no longer retained, UI shows a clear retained-summary-only message
- terminal metadata events are auditable without logging command contents

## L. Security

- password is stored as a hash, not plaintext
- session cookie uses secure attributes according to deployment mode
- REST mutation endpoints require authentication
- WebSocket endpoint requires authentication
- Origin checks are enforced for WebSocket and sensitive REST flows
- terminal output is rendered through terminal emulation, not raw HTML
- path traversal or stack ID escape attempts are rejected
- generic shell execution from arbitrary UI input is not possible

## M. Recovery And Resilience

- loss of SQLite does not destroy stack definitions on disk
- after SQLite loss, Stacklab can still rediscover stacks from the filesystem
- after SQLite loss, drift state may fall back to unknown until new successful deploys happen
- reverse proxy failure does not inherently destroy Stacklab's host-native service
- application startup failure due to Docker or DB problems is observable in logs

## N. Deployment

- Stacklab can run as a `systemd` service under the documented host-native model
- service can be started, stopped, restarted, and inspected through standard systemd commands
- service keeps runtime state in `/var/lib/stacklab`
- service works behind a reverse proxy using the documented deployment posture

## O. Helper-Backed Operations

- if workspace repair is configured, blocked files can be repaired within managed roots only
- if Docker admin helper is configured, `daemon.json` validate and apply flows work on Linux
- if Stacklab is package-managed and self-update helper is configured, self-update overview and apply work through the documented flow

## Non-Acceptance Conditions

MVP is not acceptable if any of the following remain:

- stack definitions are silently rewritten into a hidden database source of truth
- editor deploys invalid config without explicit validation failure handling
- terminal output is rendered unsafely
- a failed or locked stack can still accept conflicting mutating actions
- audit history cannot identify what action occurred on what stack
- UI cannot recover gracefully from session expiry or WebSocket reconnect
