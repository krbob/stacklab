# Maintenance Workflows Handoff

This handoff covers the UI work for Milestone 4:

- replacing ad-hoc stack update scripts with a first-class Stacklab screen
- selected/all stack bulk update workflow
- progress UX for multi-stack jobs

Backend contract draft:

- `docs/api/maintenance-workflows.md`

## Product Intent

This milestone is not a full maintenance center yet.

The first product intent is narrower:

- pick one, many, or all stacks
- run the safe update workflow
- watch progress without opening SSH or shell scripts

The mental model is:

```text
select stacks -> run update workflow -> watch steps -> inspect result
```

not:

```text
general Docker admin console
```

## Recommended Route

Recommended first route:

- `/maintenance`

Recommended sidebar draft:

- `Stacks`
- `Host`
- `Config`
- `Maintenance`
- `Audit`
- `Settings`

Reason:

- this is a global workflow, not a stack-local tab
- it replaces a host-level operator script
- it should be easy to find without looking inside one stack first

## Recommended Screen Shape

Desktop recommendation:

```text
┌──────────────────────────────────────────────────────────────────────┐
│  Maintenance                                                        │
├───────────────────────────────┬──────────────────────────────────────┤
│  Update stacks                │  Progress / result                   │
│                               │                                      │
│  ( ) All stacks               │  Step 2/6: build                     │
│  (•) Selected stacks          │  Current stack: nextcloud            │
│                               │                                      │
│  [x] nextcloud                │  nextcloud  pull   done              │
│  [x] traefik                  │  nextcloud  build  running           │
│  [ ] immich                   │  nextcloud  up     queued            │
│                               │  traefik    pull   queued            │
│  [ ] Run prune after update   │                                      │
│                               │  [job log output]                    │
│  [Start update]               │                                      │
└───────────────────────────────┴──────────────────────────────────────┘
```

## Recommended Page Sections

### Left side: workflow setup

- target selection mode:
  - `All stacks`
  - `Selected stacks`
- stack checklist when `Selected stacks` is active
- conservative options:
  - `Run prune after update`
  - `Include volumes in prune` only if backend exposes it and product wants it visible
- primary action button:
  - `Start update`

### Right side: progress and outcome

- active progress panel
- current overall step
- current target stack
- compact per-step status list
- raw job output panel beneath

## Progress UX Recommendation

The UI should treat this as one global job with stack-aware steps.

Meaning:

- top-level state:
  - `running`, `succeeded`, `failed`
- step rows:
  - action
  - target stack
  - state
- job log area:
  - append raw process output
  - keep the latest relevant context visible

Recommended labels:

- `Pull`
- `Build`
- `Up`
- `Prune`

Recommended stack row display:

- `nextcloud · Pull`
- `nextcloud · Build`
- `traefik · Up`

## Important Current Limitation

The current maintenance progress model is not meant to mimic the exact visual behavior of Dockge's image pull screen.

Today the UI should emphasize:

- clear workflow steps
- current target stack
- readable chronological history
- raw Compose output

It should **not** assume the backend provides:

- per-image progress bars
- per-layer transfer percentages
- precise elapsed time for every Docker pull/build operation

If the backend later starts emitting richer structured progress events, the page can evolve toward a more Dockge-like progress view.
For now the correct mental model is:

- reliable workflow visibility first
- rich transfer telemetry later

Longer-term UX direction:

- maintenance progress should eventually also connect to a global activity affordance shared across the app
- the page-local progress panel remains important, but it should not be the only place where a long-running background workflow is visible

## Richer Maintenance Progress Direction

Agreed v1.1 direction:

- use step cards, not a terminal emulator and not Dockge-style ANSI progress bars
- each step card should show:
  - action label
  - `target_stack_id`
  - status dot / label
  - elapsed time
  - collapsible raw output

Recommended interpretation:

- one workflow step = one card
- raw `job_log` output with matching `step` belongs inside that card
- cards should default to collapsed raw output with only a short preview visible

Why this shape:

- it is a natural evolution of the current chronological step list
- it works well on desktop and still degrades cleanly on tablet
- it avoids overcommitting to CLI-emulator UX before the backend emits structured Docker pull/build progress

## Elapsed Time Guidance

Use event timestamps already present in the contract:

- `job_step_started.timestamp`
- `job_step_finished.timestamp`

While a step is running:

- derive elapsed time from `job_step_started.timestamp` to `Date.now()`

After a step finishes:

- freeze elapsed time using the finished timestamp

## Raw Output Guidance

Raw Docker/Compose output should be rendered as:

- monospace
- append-only
- step-local
- optionally line-clamped when collapsed

Do not try to parse:

- ANSI colors
- carriage-return progress bars
- image-layer percentages

That is a later milestone.

## Required UI States

- no stacks selected state
- update running state
- partial history visible while the job is still running
- failure state with:
  - failed step
  - affected stack
  - raw output available
- success state
- Git or config changes elsewhere in the app should not block this page

## Open Design Questions For UI

These need intentional UI input before implementation is locked:

1. Should the left side use:
   - checkbox list
   - searchable multi-select
   - or grouped list with stack states visible inline?
2. Should the progress view emphasize:
   - one chronological step list
   - or per-stack grouped progress rows?
3. Should the first version show:
   - only the current run
   - or also a lightweight "last maintenance run" summary?

## Current Decision Snapshot

Current direction after product review:

1. step cards
2. elapsed time per step from existing event timestamps
3. raw `job_log` output rendered inside the step card
4. no ANSI parsing or pseudo-terminal rendering in this milestone

## Recommended First Version

To keep scope tight:

- checkbox list for selected stacks
- step cards on the right
- current run only
- no historical summary card yet
- raw output collapsed by default

That is enough to replace `update_stacks.sh` safely without over-designing the page.
