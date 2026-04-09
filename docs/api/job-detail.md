# Job Detail

## Purpose

This contract defines the read-only data needed for a dedicated job detail screen and for replaying retained progress from audit/history links.

The goal is to give the UI one stable place to load:

- the current job snapshot
- retained `job_events`
- graceful empty-state messaging when detailed output was already purged

This slice does not change live WebSocket semantics. It only adds a REST read model for retained history.

## Endpoints

- `GET /api/jobs/{jobId}`
- `GET /api/jobs/{jobId}/events`

## `GET /api/jobs/{jobId}`

Use this for:

- the durable job summary
- current terminal state
- workflow step list
- stack/global scope detection

This endpoint remains the source of truth for:

- `action`
- `state`
- `stack_id`
- `requested_at`
- `started_at`
- `finished_at`
- `workflow`

## `GET /api/jobs/{jobId}/events`

Use this for:

- replayable progress detail
- recent failure diagnosis
- grouping logs by step in a dedicated job detail screen

Response shape:

```json
{
  "job_id": "job_01hr...",
  "retained": true,
  "items": [
    {
      "job_id": "job_01hr...",
      "sequence": 1,
      "event": "job_started",
      "state": "running",
      "message": "Job started.",
      "timestamp": "2026-04-03T18:40:01Z"
    }
  ]
}
```

If detailed output is gone, the endpoint still returns `200`:

```json
{
  "job_id": "job_01hr...",
  "retained": false,
  "message": "Detailed output for this job is no longer retained.",
  "items": []
}
```

## UX Intent

The UI should treat these states distinctly:

- job exists and retained events exist
- job exists but retained events are gone
- job does not exist

Only the last case is an actual error.

## Notes

- event ordering is by ascending `sequence`
- `job_events` are immutable after write
- step context is attached per event when available
- this contract does not add cancellation or retry behavior
