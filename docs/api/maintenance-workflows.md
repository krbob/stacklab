# Maintenance Workflows Contract Draft

This document defines the proposed contract for Milestone 4:

- safe bulk maintenance workflows that replace ad-hoc host scripts
- selected/all stack update execution
- UI-visible step progress for multi-stack operations

It is intentionally narrower than a full generic Docker maintenance API.

## Goals

- replace scripts such as `update_stacks.sh` with a first-class Stacklab workflow
- update one, many, or all stacks without leaving the browser
- keep the workflow explicit and predictable:
  - pull
  - build when needed
  - `up -d --remove-orphans`
- make optional prune an explicit operator choice
- provide progress data that lets the UI show both:
  - overall workflow progress
  - which stack is currently being processed

## Non-Goals

- a generic scheduler in this milestone
- automatic update policies
- broad Docker object cleanup UI
- registry management
- system package maintenance

## Product Fit

This milestone exists primarily to replace a common homelab pattern:

```text
for each stack:
  docker compose pull
  docker compose up --build -d --remove-orphans

optional:
  docker system prune ...
```

Stacklab should own this workflow directly instead of requiring a sidecar shell script.

## Job Model Implication

Milestone 4 is the first clear case of a workspace-scoped mutating job.

Implication:

- `job.stack_id` should be allowed to be `null` for global maintenance jobs
- individual workflow steps should carry:
  - `action`
  - `target_stack_id`
  - `index`
  - `total`

This keeps the top-level job global while still giving the UI stack-aware progress.

## Execution Model

Recommended execution order:

1. determine target stacks
2. process targets in deterministic order
3. for each target stack:
   - pull images
   - build when needed
   - run `up -d --remove-orphans`
4. optionally run prune after all selected stacks succeed

Recommended target order:

- alphabetical by `stack_id`

Recommended failure semantics for v1:

- fail-fast by default
- stop processing remaining stacks after the first failed stack step
- keep all completed step history in the job events

Later extensions may add best-effort continue-on-error behavior, but not in the first version.

## REST Endpoints

## `POST /api/maintenance/update-stacks`

Purpose:

- trigger a bulk update workflow for selected or all stacks

Request body:

```json
{
  "target": {
    "mode": "selected",
    "stack_ids": ["nextcloud", "traefik"]
  },
  "options": {
    "pull_images": true,
    "build_images": true,
    "remove_orphans": true,
    "prune_after": {
      "enabled": false,
      "include_volumes": false
    }
  }
}
```

Field rules:

- `target.mode`:
  - `selected`
  - `all`
- `target.stack_ids`:
  - required and non-empty when `mode = selected`
  - omitted when `mode = all`
- `options.pull_images`:
  - default `true`
- `options.build_images`:
  - default `true`
- `options.remove_orphans`:
  - default `true`
- `options.prune_after.enabled`:
  - default `false`
- `options.prune_after.include_volumes`:
  - default `false`
  - should stay conservative in the first implementation

Response:

```json
{
  "job": {
    "id": "job_01hr...",
    "stack_id": null,
    "action": "update_stacks",
    "state": "running",
    "workflow": {
      "steps": [
        {
          "action": "pull",
          "target_stack_id": "nextcloud",
          "state": "running"
        },
        {
          "action": "build",
          "target_stack_id": "nextcloud",
          "state": "queued"
        },
        {
          "action": "up",
          "target_stack_id": "nextcloud",
          "state": "queued"
        },
        {
          "action": "pull",
          "target_stack_id": "traefik",
          "state": "queued"
        }
      ]
    }
  }
}
```

Notes:

- this extends the current job model with `target_stack_id` on workflow steps
- the job itself is global to the workspace, not to one stack

## Errors

Suggested error codes:

- `validation_failed`
- `not_found`
- `conflict`
- `docker_unavailable`
- `internal_error`

Examples:

- unknown stack ID in `target.stack_ids` -> `404 not_found`
- empty `stack_ids` for `selected` mode -> `400 validation_failed`
- prune requested with unsupported options -> `400 validation_failed`

## WebSocket / Job Event Extension

Milestone 4 needs a small extension to the existing jobs event model.

Current step shape:

```json
{
  "index": 2,
  "total": 6,
  "action": "pull"
}
```

Recommended step shape for maintenance workflows:

```json
{
  "index": 2,
  "total": 6,
  "action": "pull",
  "target_stack_id": "nextcloud"
}
```

Example event:

```json
{
  "type": "jobs.event",
  "stream_id": "job_update_stacks_progress",
  "payload": {
    "job_id": "job_01hr...",
    "stack_id": null,
    "action": "update_stacks",
    "state": "running",
    "event": "job_step_started",
    "message": "Pulling images for nextcloud.",
    "step": {
      "index": 1,
      "total": 6,
      "action": "pull",
      "target_stack_id": "nextcloud"
    },
    "timestamp": "2026-04-04T18:20:00Z"
  }
}
```

## Audit Expectations

The bulk maintenance job should produce:

- one global audit entry for `update_stacks`
- optional detail JSON listing:
  - target mode
  - selected stack IDs
  - whether prune was requested

Per-stack lifecycle actions do not need separate audit rows in the first implementation as long as job history remains detailed enough.

## UI Expectations

Expected first UI surface:

- one global page dedicated to maintenance
- primary workflow card: `Update stacks`
- selection modes:
  - `All stacks`
  - `Selected stacks`
- progress view that can answer:
  - what overall step is running
  - which stack is currently affected
  - which stacks already completed

## Later Compatibility

This contract is intentionally compatible with later follow-ups:

- scheduled maintenance
- image inventory
- safer/manual prune expansion
- notifications

The first milestone should stay focused on replacing the existing update script safely.
