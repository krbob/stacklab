# Maintenance Workflows Contract Draft

This document defines the current contract for maintenance update workflows.

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
  - `up -d --remove-orphans` for active stacks and explicitly selected stacks
- preserve the inactive state of stopped and never-started stacks during
  all-stacks updates
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

## Current Progress Model vs. Rich Docker Pull Progress

The current Stacklab maintenance workflow is intentionally:

- workflow-oriented
- step-oriented
- log-oriented

That means the UI can reliably show:

- overall job state
- current step index and total
- current `target_stack_id`
- chronological step transitions
- raw output emitted by the underlying Compose commands

This is enough to answer:

- which stack is being processed
- which action is currently running
- where the workflow failed
- what raw command output led to the result

It does **not** yet provide Dockge-style structured progress such as:

- per-image or per-layer pull progress bars
- precise elapsed time per pull/build step
- live percentage updates for image transfer

That richer experience remains a planned enhancement rather than a v1 contract requirement.
It would require extending the current job event model with structured maintenance progress events instead of relying only on:

- `job_step_started`
- `job_log`
- `job_step_finished`

Recommended later enrichment:

1. elapsed time on each workflow step
2. live step duration while a step is running
3. richer rendering of `pull`, `build`, and `up` output
4. optional structured image progress events for Docker pull/build operations

## Richer Maintenance Progress v1.1

The next UX slice should still stay within the current event model.

Meaning:

- no ANSI parsing requirement
- no image layer progress bars yet
- no XTerm-style terminal renderer inside maintenance

The immediate richer progress goal is:

- render one card per workflow step
- show elapsed time derived from:
  - `job_step_started.timestamp`
  - `job_step_finished.timestamp`
  - `Date.now()` while running
- render raw `job_log` output inside the corresponding step card
- keep raw output collapsed by default, expandable on demand

Important existing contract detail:

- maintenance and prune `job_log` events already include `step`
- therefore the UI can assign log output to the active workflow step without backend changes

Recommended v1.1 UI grouping rule:

- `job_step_started` creates or activates a step card
- `job_log` with `step` appends output to that step card
- `job_step_finished` marks the card terminal and freezes elapsed time

This preserves the current backend contract while making the progress feel much more alive.

Broader product implication:

- this should feed a later global background activity UX, not only the `/maintenance` page
- operators should still see meaningful progress after the triggering button becomes idle again

## Job Model Implication

The maintenance update workflow is the first clear case of a workspace-scoped mutating job.

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
   - record service readiness before changing images if deployment is planned
   - run the stack's required backup and prepare its original images when those options are enabled
   - pull images
   - build when needed
   - run `up -d --remove-orphans` when the stack was already active
   - for manual selected-stack updates, also deploy explicitly selected stopped stacks
   - for all-stacks and scheduled updates, skip deployment when the stack was
     inactive so operator intent is preserved
   - check readiness again after `up` for the services included in the update
   - attempt image recovery once when an eligible service regresses or deployment partially fails
4. optionally run prune after all selected stacks succeed

### Readiness during updates

The job history records a `verify_before` step and a `verify_after` step.
Docker healthchecks declared in Compose or inherited from an image supply the
native readiness signal. If a service is running without a probe, the step
records a warning naming the services whose readiness remains unverified.
Verification waits up to three minutes per stack. It requires two passing
observations of the same instances, two seconds apart, so containers may
finish starting before the step completes.

To check a service from the Stacklab host, add these labels to its Compose
definition. The port is the service's internal port.

```yaml
services:
  app:
    labels:
      io.stacklab.health.http: /readyz
      io.stacklab.health.port: "8080"
```

The endpoint must accept an unauthenticated HTTP GET and return a 2xx status
when the service is ready. Stacklab connects directly to the inspected
container address. The host must be able to reach it. Redirects count as a
failed check. Use an application-local test that can run during startup.

If readiness verification fails, the job records the affected service and
skips the optional prune. Independent stacks can continue.

### Per-stack image rollback and backup

Open the stack's **Backups → Maintenance update protection** panel. Both options
default to off. They apply independently to manual and scheduled maintenance
when the workflow plans to deploy that stack.

- **Automatic image rollback** captures the exact local image ID and running
  replica count of each included, previously healthy service. A Docker or HTTP
  probe must have passed and all replicas must use the same image. The running
  containers must also match the service configuration for the deployment.
  Pending changes such as a different database mount make that service ineligible.
  Services using Compose `configs` or `secrets` are skipped because their
  deployed content cannot be verified from the service hash. One-shot services
  and services without a confirmed readiness signal are also skipped. Preparation logs the eligible
  services and any omissions before the pull or build begins.
- **Required backup before deployment** uses an enabled native backup plan
  dedicated to this stack. Review its selected mounts, stack definition or
  managed configuration, exclusions, consistency mode and enabled destinations.
  Maintenance requires a freshly verified copy at every enabled destination.
  Changes to the approved source selection, consistency or destination identity
  require a new review in the panel. A failed copy or a changed plan blocks
  deployment for this stack. Other stacks can continue.

When the updated service fails its readiness check, Stacklab checks that the
regression still exists. A partial `compose up` failure can also trigger recovery.
Stacklab recreates the changed eligible services together with a local Compose
override and checks their original image IDs, replica counts and readiness.
Choose this option for applications whose previous version can safely open the
data left by the new version. Data migrations and writes made during the update
require their own recovery decision.

The job keeps its failed update status even after successful image recovery.
Its `rollback_images` step records the recovery outcome. The stack definition
and its usual image tag keep the requested update. A subsequent ordinary
`compose up` can therefore deploy that image again.

On the dashboard, **Problems** includes services that still run the previous
image after a confirmed rollback or an unsuccessful attempt to apply a different
image. Both stack views show the affected containers and link to the job while
its history is retained.

The `backup_before` step records the plan, source IDs, backup run, repository
and snapshot. **Review pre-update backup** opens that exact snapshot. Recover
the desired files or sources into a separate directory, then preview and
approve any application to live data. Shared storage is included only when its
source belongs to the approved plan. A private **Stacklab recovery** backup
includes the per-stack policies and backup plans in the SQLite snapshot.

Image preparation can use up to 16 GiB per stack under
`<STACKLAB_DATA_DIR>/maintenance-rollback/<job_id>/<stack_id>/` and requires
512 MiB of free space in reserve. Failure to prepare eligible images blocks the
deployment. A successful update or verified rollback releases the temporary
images and archive. If deployment is interrupted, recovery fails, or its outcome
is uncertain, inspect the job and the retained directory. Its `manifest.json`
identifies the original images, private tags, replica counts and SHA-256 of
`images.tar`. For manual recovery, verify the archive checksum, load it with
`docker image load -i images.tar`, then review `compose.rollback.yaml` against
the live stack and the manifest before recreating any services. Retained archives
require manual cleanup. After resolving the job and confirming the running
images, remove only its private tags that still match the recorded image IDs
and its per-stack archive directory. These local image archives are excluded
from the native backup of Stacklab state.

Recommended target order:

- alphabetical by `stack_id`

Failure semantics:

- transient registry failures during `pull` and `build` are retried twice,
  after 5 and 20 seconds; retryable failures include network/TLS timeouts,
  connection resets, temporary DNS failures, and registry rate limiting
- authentication, missing-manifest, cancellation, and configuration failures
  are not retried
- after a stack step exhausts its retries, dependent steps for that stack are
  marked `skipped`, but independent later stacks continue in deterministic order
- prune-after is marked `skipped` when any preceding stack update failed
- the top-level job finishes as `failed` when any requested stack step failed,
  even when other stacks completed successfully
- `job_warning` records retries, step-scoped `job_error` records exhausted
  failures, and `step.state` distinguishes the step outcome from the still
  running aggregate job
- cancellation, timeout, and durable state-write errors still stop the workflow
  immediately

## REST Endpoints

## `GET` and `PUT /api/stacks/{stackId}/update-protection`

`GET` returns the saved policy and its `revision`. Include this revision in
`PUT` to prevent a stale edit from overwriting a newer policy. On HTTP 409,
reload the settings and review the change. `PUT` replaces the policy and
requires the boolean `auto_rollback`. To require a backup, send the selected plan's
`backup_plan_id`, complete `backup_source_ids` and current
`backup_plan_fingerprint`. The stack coverage API exposes the fingerprint as
`update_backup_fingerprint` on each eligible plan. To clear the backup
requirement, omit the three backup fields. The admitted maintenance job uses
the policy saved when it was created. Removing the stack definition resets its
policy so a new stack created with the same ID starts with protection disabled.

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
- `options.preserve_inactive`:
  - default `false` for manually selected stacks
  - when `true`, stopped and never-started targets may pull or build images but
    are not deployed; all-stack and scheduled updates always preserve this state
  - the dashboard's `Update all` uses this option with the explicit set of
    stacks that had updates at review time
- `options.build_images`:
  - default `true`
- `options.remove_orphans`:
  - default `true`, except manual requests that include service exclusions
    default it to `false`
  - updates with service exclusions deploy the included services with `up --no-deps` so Compose keeps excluded dependencies in their current state
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
          "action": "verify_before",
          "target_stack_id": "nextcloud",
          "state": "running"
        },
        {
          "action": "pull",
          "target_stack_id": "nextcloud",
          "state": "queued"
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
          "action": "verify_after",
          "target_stack_id": "nextcloud",
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

The maintenance workflow needs a small extension to the existing jobs event model.

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
