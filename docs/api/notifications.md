# Notifications API

Purpose:

- add first-class outgoing notifications without requiring the operator to stay in the UI
- keep the first iteration narrow and predictable before layering on richer mobile alerts

Scope:

- outgoing channels only
- settings stored in SQLite `app_settings`
- delivery on selected terminal job states
- explicit test notification endpoint

Current event toggles:

- `job_failed`
- `job_succeeded_with_warnings`
- `maintenance_succeeded`
- `post_update_recovery_failed`

Supported terminal events:

- `job_failed`
- `job_succeeded_with_warnings`
- `maintenance_succeeded`
- `post_update_recovery_failed`
- `test_notification`

Current channels:

- `webhook`
- `telegram`

Compatibility note:

- the REST contract still exposes legacy top-level webhook fields:
  - `enabled`
  - `configured`
  - `webhook_url`
- newer clients should prefer the nested `channels` shape

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

Webhook headers:

- `Content-Type: application/json`
- `User-Agent: Stacklab-Notifications/1`
- `X-Stacklab-Event: <event>`

Telegram delivery:

- uses the official `sendMessage` Bot API endpoint with plain text messages  
  Source: https://core.telegram.org/bots/api#sendmessage

Delivery semantics:

- best-effort only
- no retry queue in v1
- notification delivery must not block the originating job
- job completion remains the source of truth even if webhook delivery fails
- if multiple channels are enabled, Stacklab attempts delivery to each enabled channel independently

Storage key:

- `app_settings.key = notifications_v2`

Validation:

- `webhook_url` must be an absolute `http` or `https` URL
- `enabled = true` requires `webhook_url`
- `test` may use a valid URL even if notifications are not yet enabled
- Telegram requires:
  - `bot_token`
  - `chat_id`

Post-update recovery failure:

- applies only to `update_stacks`
- after a successful maintenance workflow, Stacklab inspects each targeted stack
- if any targeted stack does not return to a healthy `running` state, Stacklab emits:
  - `post_update_recovery_failed`
- this is intended to catch:
  - partial/error runtime state after update
  - unhealthy containers after update

Payload extension for post-update failures:

```json
{
  "event": "post_update_recovery_failed",
  "summary": "Stacklab post-update recovery failed: update_stacks",
  "post_update": {
    "failed_stacks": [
      {
        "stack_id": "demo",
        "runtime_state": "error",
        "display_state": "error",
        "unhealthy_container_count": 1,
        "running_container_count": 0,
        "total_container_count": 1,
        "reason": "stack_not_healthy_after_update"
      }
    ]
  }
}
```

Non-goals for the current slice:

- multiple channels or multiple webhook targets
- WhatsApp native integration
- log anomaly alerting
- retry queues
- batching or digests
- notification inbox inside Stacklab
- rich templating
