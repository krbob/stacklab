# Global Activity Handoff

This handoff covers the first cross-application activity milestone.

## Goal

Make background operations visible after the triggering button returns to idle.

Examples:

- bulk stack update started from `/maintenance`
- prune workflow started from `/maintenance`
- stack create/delete/save flows
- Git push or later Docker admin apply flows

The operator should not have to stay on the originating page to know that work is still happening.

## Backend Contract

Version 1 uses:

- `GET /api/jobs/active`

See:

- [../api/global-activity.md](../api/global-activity.md)

Important backend semantics:

- only active jobs are returned
- workspace-level jobs may have `stack_id = null`
- `current_step.target_stack_id` is the better label for bulk workflows
- `latest_event` is intended for compact human-readable context in the chrome

## UI Scope For Version 1

Required:

- a persistent global activity affordance in app chrome
- visible non-idle state while one or more jobs are active
- elapsed time for the most recent running job
- current action and target when available
- a way to expand into a compact list of active jobs
- clear terminal state for recently finished jobs can wait

Not required yet:

- desktop-style notification center
- toast storm for every job event
- full historical job browser
- per-image pull layer progress

## Open IA Questions For UI

These decisions are intentionally left to the UI developer:

1. What is the primary chrome affordance?
   - compact top bar pill
   - slim bottom activity bar
   - sidebar footer module

2. What is the expanded surface?
   - popover
   - tray
   - drawer

3. Should the first version show only the most recent active job in collapsed state, or an aggregate like `3 jobs running`?

4. How long, if at all, should completed jobs stay visible in the activity chrome before disappearing?

## Data Mapping Guidance

Collapsed state should usually use:

- `summary.active_count`
- the first item in `items`
- `current_step.action`
- `current_step.target_stack_id`
- `started_at` or `requested_at`

Expanded list should show per item:

- state
- action
- stack or target stack
- elapsed time
- latest event message if present

## Interaction Guidance

The activity affordance should feel global, not page-local.

Meaning:

- do not tie it to `/maintenance` only
- do not hide it because the originating page unmounted
- opening a stack page should not make the running maintenance workflow disappear

## Recommended V1 Shape

My backend recommendation is:

- collapsed chrome summary
- click opens a compact list of active jobs
- each list row links to the most relevant existing detail surface when one exists:
  - stack pages for stack-local jobs
  - `/maintenance` for maintenance and cleanup jobs
  - later `/docker` for Docker admin apply jobs

That is the preferred default, but the exact presentation needs UI developer input.
