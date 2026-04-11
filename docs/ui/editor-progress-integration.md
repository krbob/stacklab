# Editor and Progress Integration

This document is the backend-facing handoff for the Compose editor, mutating actions, and shared progress panel. It reflects the current implementation, including edge cases that are not obvious from the idealized REST and WebSocket contracts alone.

## Scope

This document covers:

- `GET /api/stacks/{stackId}/definition`
- `PUT /api/stacks/{stackId}/definition`
- `GET /api/stacks/{stackId}/resolved-config`
- `POST /api/stacks/{stackId}/resolved-config`
- all mutating endpoints that return `{ "job": ... }`
- `GET /api/jobs/{jobId}`
- WebSocket `jobs.subscribe`

This document does not cover:

- logs, stats, or terminal streams
- audit screen behavior beyond linking to job details
- `source=last_valid` for resolved config, which is not implemented yet

## Current Backend Reality

The editor and progress panel are backed by a mix of synchronous REST responses and asynchronous job events.

Important facts:

- Every mutating stack operation creates a job before doing the work.
- The progress panel should treat WebSocket `jobs.subscribe` as the authoritative event stream.
- `GET /api/jobs/{jobId}` returns the current job snapshot only. It does not return `job_events`.
- WebSocket `jobs.subscribe` replays retained events for that job, then switches to live events.
- `PUT /definition` can return an HTTP error after a job was already created and marked failed. In that failure path the response body does not include the `job.id`.
- `PUT /definition` with `validate_after_save=true` can still return `200` with a succeeded job even when the saved config does not resolve cleanly. In that case the backend emits a `job_warning` before the terminal `job_finished` event.

## Editor Flow

### Initial load

Recommended screen bootstrap:

1. `GET /api/stacks/{stackId}`
2. `GET /api/stacks/{stackId}/definition`
3. `GET /api/stacks/{stackId}/resolved-config?source=current`

Expected behavior:

- `definition` is available for normal stacks with a canonical `compose.yaml`
- `definition` returns `409 invalid_state` for stack states that do not expose a definition, such as `orphaned`
- `resolved-config?source=current` returns the current resolved preview from files on disk
- `resolved-config?source=last_valid` currently returns `501 not_implemented`

### Draft preview

Use `POST /api/stacks/{stackId}/resolved-config` for previewing unsaved editor contents.

Recommended payload source:

- `compose_yaml`: current editor contents
- `env`: current `.env` editor contents, including empty string when `.env` does not exist

Recommended UI behavior:

- treat this as the only preview source while the editor is dirty
- allow the user to request preview repeatedly without saving
- show preview errors inline in the preview pane or status area
- do not interpret preview invalidity as a hard save blocker unless the user clicked `Save & Deploy`

### Save

Use `PUT /api/stacks/{stackId}/definition`.

Relevant payload fields:

- `compose_yaml`
- `env`
- `validate_after_save`

Recommended UI behavior:

1. Submit `PUT /definition`
2. On `200`, extract `job.id`
3. Open or focus the shared progress panel
4. Subscribe to WebSocket `jobs.subscribe` for that `job.id`
5. When the editor is no longer dirty, refresh:
   - `GET /api/stacks/{stackId}/definition`
   - `GET /api/stacks/{stackId}/resolved-config?source=current`
   - `GET /api/stacks/{stackId}`

### Save outcomes

#### Case A: save succeeds and resolved config is valid

Typical sequence:

1. HTTP `200` with `{ "job": { "state": "succeeded", ... } }`
2. WS replay/live events include:
   - `job_started`
   - `job_progress`
   - `job_finished`

#### Case B: save succeeds but resolved config is invalid

Typical sequence:

1. HTTP `200` with `{ "job": { "state": "succeeded", ... } }`
2. WS replay/live events include:
   - `job_started`
   - `job_progress`
   - `job_warning`
   - `job_finished`

Implication:

- `job.state = succeeded` means the files were saved
- it does not guarantee that `docker compose config` resolved successfully after save
- the editor should surface the warning separately from the save success state

Recommended UI copy:

- primary result: `Saved`
- secondary warning: `Saved, but resolved config is invalid`

#### Case C: save fails during write

Typical sequence:

1. Backend creates a job
2. Backend marks it failed and records audit
3. HTTP response is an error such as `404`, `409`, or `500`
4. Response body does not include `{ "job": ... }`

Implication:

- the UI cannot always rely on getting `job.id` in save-failure responses
- treat the HTTP error as authoritative for user feedback
- do not block on progress UI in this path

Recommended fallback behavior:

- show inline or toast error from the HTTP response
- keep editor contents intact
- optionally refetch `GET /api/stacks/{stackId}` if the error implies state drift

## Progress Panel Flow

The shared progress panel should be used for:

- `save_definition`
- `create_stack`
- `remove_stack_definition`
- `up`
- `down`
- `stop`
- `restart`
- `pull`
- `build`
- `recreate`
- `validate`

### Source of truth

Use both:

- `GET /api/jobs/{jobId}` for the current job snapshot
- WebSocket `jobs.subscribe` for event history and live updates

Important limitation:

- `GET /api/jobs/{jobId}` does not include replayable event history
- if the UI wants step-by-step output, it must subscribe through WebSocket

### Recommended open sequence

For a newly started mutation:

1. call the mutating REST endpoint
2. read `job.id` from the `200` response
3. open progress UI immediately
4. issue `jobs.subscribe` with that `job.id`
5. render replayed events, then live events

For reopening an existing job from audit/history:

1. `GET /api/jobs/{jobId}`
2. open progress UI with snapshot metadata
3. issue `jobs.subscribe` with that `job.id`
4. if retained events still exist, render replayed events
5. if only the snapshot remains, show summary state and a calm note that detailed output may no longer be retained

### Event types in current use

The current backend emits these job event types:

- `job_started`
- `job_progress`
- `job_step_started`
- `job_step_finished`
- `job_warning`
- `job_error`
- `job_finished`

Interpretation guidelines:

- `job_started`: create initial row or state
- `job_progress`: human-readable activity text
- `job_step_started`: workflow step entered
- `job_step_finished`: workflow step completed
- `job_warning`: non-fatal warning; for `save_definition` it can appear just before the final `job_finished`
- `job_error`: fatal error detail
- `job_finished`: terminal event for both success and failure

### Workflow-aware operations

Some operations expose workflow steps, not just a flat job status.

Known examples:

- `create_stack`:
  - step 1: create stack files
  - step 2: optional `up` when `deploy_after_create=true`
- `remove_stack_definition`:
  - variable steps based on selected deletion flags

Recommended UI:

- show step index and total when `job.workflow.steps` is present
- keep a simple fallback mode for single-step jobs

## Audit Linkage

Audit entries for stack jobs carry `job_id`.

Recommended `View log` behavior:

1. read `job_id` from the selected audit entry
2. open the shared progress panel
3. load `GET /api/jobs/{jobId}`
4. subscribe to `jobs.subscribe`

If retained events are gone:

- keep the audit entry visible
- show job summary from REST
- show a non-error note such as `Detailed output is no longer retained`

## Error Handling Notes

### HTTP errors to expect

Common editor/progress-relevant errors:

- `401 unauthorized`
- `403 forbidden`
- `404 not_found`
- `409 invalid_state`
- `409 stack_locked`
- `422 validation_failed`
- `500 internal_error`
- `501 not_implemented` for `source=last_valid`

### State-sensitive UI

The UI should not reimplement action rules when detail data already provides them.

Use:

- `stack.capabilities`
- `stack.available_actions`

This is especially important for:

- `orphaned` stacks
- `invalid` configs
- `locked` stacks

## Recommended UX Rules

- treat the editor as optimistic only after the save response succeeds
- keep unsaved editor content on all HTTP save failures
- show `job_warning` as a warning, not a failure
- let the progress panel survive route changes and stack detail tab switches
- prefer the WebSocket replay stream over local event reconstruction
- when reopening older jobs, degrade gracefully if only summary data remains

## Current Gaps

These are known gaps in the current implementation, not UI bugs:

- `GET /api/stacks/{stackId}/resolved-config?source=last_valid` is not implemented
- `PUT /definition` failure responses do not include `job.id`
- a save can be `succeeded` and still have emitted `job_warning` during the same job stream

If any of these behaviors change, this document should be updated together with the endpoint or job-stream behavior.
