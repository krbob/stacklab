# Job Detail Handoff

## Goal

Add a shared job detail surface that can be opened from:

- global activity
- global audit
- stack history

The surface should stop duplicating inline "View log" implementations across pages.

## Available data

- `GET /api/jobs/{jobId}`
- `GET /api/jobs/{jobId}/events`

## Recommended scope for the first UI slice

Show:

- job action
- stack or global target
- terminal state
- requested / started / finished timestamps
- workflow step list
- retained event stream grouped or presented chronologically

Defer:

- cancel/retry
- retention settings
- export/download

## Important states

### Retained detail available

- render replayable events normally
- step-aware layouts may use `event.step`

### Detailed output no longer retained

- still render the job summary
- show the non-error note from the API
- do not treat this as a red failure state

### Job not found

- standard `404` empty/error state

## Open design decisions

The next UI slice needs explicit choices for:

1. route vs overlay
2. whether audit rows should navigate or expand inline
3. whether global activity rows should open the shared job detail or keep current page-local routing
