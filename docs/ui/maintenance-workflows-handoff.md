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

## Recommended First Version

To keep scope tight:

- checkbox list for selected stacks
- chronological step list on the right
- current run only
- no historical summary card yet

That is enough to replace `update_stacks.sh` safely without over-designing the page.
