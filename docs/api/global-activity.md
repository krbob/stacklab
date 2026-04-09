# Global Activity API

This contract defines the first backend slice for cross-application background job visibility.

The goal is not a full notification center. The goal is a stable read model that lets the UI show:

- whether anything is still running in the background
- what the current job is doing
- which stack, if any, is being targeted right now
- how long the job has been running

Version 1 is intentionally REST-first.

Reason:

- the backend already exposes durable per-job history and per-job WebSocket replay
- the missing piece for global chrome is a shared list of currently active jobs
- polling this list is enough for the first persistent activity affordance

Live push for global activity can come later as a second slice.

## `GET /api/jobs/active`

Purpose:

- list all currently active jobs across the application
- power a global activity badge, bar, tray, or drawer

Definition of "active":

- `queued`
- `running`
- `cancel_requested`

Terminal states are excluded:

- `succeeded`
- `failed`
- `cancelled`
- `timed_out`

Response:

```json
{
  "items": [
    {
      "id": "job_01hr...",
      "stack_id": null,
      "action": "update_stacks",
      "state": "running",
      "requested_at": "2026-04-09T10:15:00Z",
      "started_at": "2026-04-09T10:15:01Z",
      "workflow": {
        "steps": [
          { "action": "pull", "state": "running", "target_stack_id": "demo" },
          { "action": "up", "state": "queued", "target_stack_id": "demo" }
        ]
      },
      "current_step": {
        "index": 1,
        "total": 2,
        "action": "pull",
        "target_stack_id": "demo"
      },
      "latest_event": {
        "event": "job_step_started",
        "message": "Starting pull for demo.",
        "timestamp": "2026-04-09T10:15:02Z",
        "step": {
          "index": 1,
          "total": 2,
          "action": "pull",
          "target_stack_id": "demo"
        }
      }
    }
  ],
  "summary": {
    "active_count": 1,
    "running_count": 1,
    "queued_count": 0,
    "cancel_requested_count": 0
  }
}
```

Behavior:

- jobs are ordered by most recently active first
- `stack_id = null` is valid for workspace-level jobs such as:
  - bulk maintenance
  - cleanup
  - future Docker admin apply workflows
- `workflow` is optional
- `current_step` is optional
- `latest_event` is optional when a job exists but no event has been recorded yet

UI guidance:

- use `summary.active_count` for the smallest chrome affordance
- use `started_at` if present, otherwise `requested_at`, to derive elapsed time in the client
- prefer `current_step.target_stack_id` when present over root `stack_id` for bulk workflows

## Why REST First

The initial milestone optimizes for clarity and recoverability:

- page reloads can reconcile immediately through REST
- global chrome does not need to subscribe to every job individually
- the UI can poll at a low rate without overcomplicating the transport model

Later enhancements can add:

- global WebSocket activity updates
- richer progress telemetry for pull/build-heavy flows
- sticky completion states and dismissible notifications
