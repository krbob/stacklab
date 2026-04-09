# Notifications API

Purpose:

- add first-class outgoing notifications without requiring the operator to stay in the UI
- keep v1 intentionally narrow: one webhook target, one delivery format, no inbox

Scope:

- outgoing webhook only
- settings stored in SQLite `app_settings`
- delivery on selected terminal job states
- explicit test notification endpoint

V1 event toggles:

- `job_failed`
- `job_succeeded_with_warnings`
- `maintenance_succeeded`

Supported terminal events:

- `job_failed`
- `job_succeeded_with_warnings`
- `maintenance_succeeded`
- `test_notification`

Payload shape:

```json
{
  "event": "job_failed",
  "sent_at": "2026-04-09T19:00:00Z",
  "source": "stacklab",
  "summary": "Stacklab job failed: up · demo",
  "warning_count": 0,
  "job": {
    "id": "job_01hr...",
    "action": "up",
    "state": "failed",
    "stack_id": "demo",
    "requested_at": "2026-04-09T18:59:00Z",
    "started_at": "2026-04-09T18:59:01Z",
    "finished_at": "2026-04-09T19:00:00Z",
    "error_code": "stack_action_failed",
    "error_message": "docker compose up failed",
    "duration_ms": 59000
  }
}
```

Headers:

- `Content-Type: application/json`
- `User-Agent: Stacklab-Notifications/1`
- `X-Stacklab-Event: <event>`

Delivery semantics:

- best-effort only
- no retry queue in v1
- notification delivery must not block the originating job
- job completion remains the source of truth even if webhook delivery fails

Storage key:

- `app_settings.key = notifications_webhook_v1`

Validation:

- `webhook_url` must be an absolute `http` or `https` URL
- `enabled = true` requires `webhook_url`
- `test` may use a valid URL even if notifications are not yet enabled

Non-goals for v1:

- multiple channels or multiple webhook targets
- message templating
- notification inbox inside Stacklab
- batching or digests
- retries / dead-letter queues
