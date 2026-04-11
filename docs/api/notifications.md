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
- `stacklab_service_error`
- `runtime_health_degraded`
- `runtime_log_error_burst`

Supported event types:

- `job_failed`
- `job_succeeded_with_warnings`
- `maintenance_succeeded`
- `post_update_recovery_failed`
- `stacklab_service_error`
- `runtime_health_degraded`
- `runtime_log_error_burst`
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

Stacklab self-health errors:

- sourced from `journalctl -u stacklab`
- detected in the background with a persisted cursor
- older journal entries are ignored on first startup so Stacklab does not page historical errors immediately
- delivery is deduplicated for a cooldown window so repeated identical bursts do not spam the operator

Payload extension for Stacklab self-health errors:

```json
{
  "event": "stacklab_service_error",
  "summary": "Stacklab service logged 2 new errors",
  "stacklab_service": {
    "entry_count": 2,
    "first_timestamp": "2026-04-10T08:14:00Z",
    "last_timestamp": "2026-04-10T08:14:05Z",
    "sample_messages": [
      "failed to bind socket (x2)"
    ],
    "latest_cursor": "s=cursor-4",
    "cooldown_seconds": 900
  }
}
```

Runtime health degradation:

- sourced from the current runtime state of managed stacks
- detected in the background with a persisted baseline fingerprint
- the first observed degraded state is treated as baseline and does not immediately page the operator
- repeated identical degradations are deduplicated for a cooldown window

Current triggers:

- one or more containers report `unhealthy`
- one or more containers are in `restarting`

Payload extension for runtime health degradation:

```json
{
  "event": "runtime_health_degraded",
  "summary": "2 stack(s) became unhealthy",
  "runtime_health": {
    "affected_stacks": [
      {
        "stack_id": "demo",
        "runtime_state": "error",
        "display_state": "error",
        "unhealthy_container_count": 1,
        "restarting_container_count": 0,
        "running_container_count": 1,
        "total_container_count": 1,
        "reasons": ["unhealthy_containers"]
      },
      {
        "stack_id": "worker",
        "runtime_state": "error",
        "display_state": "error",
        "unhealthy_container_count": 0,
        "restarting_container_count": 1,
        "running_container_count": 0,
        "total_container_count": 1,
        "reasons": ["restart_loop"]
      }
    ]
  }
}
```

Runtime log error bursts:

- sourced from recent `docker logs` output for managed stack containers
- detected in the background with a persisted `last_checked_at` baseline
- the first poll only seeds the baseline and does not immediately page historical log noise
- repeated identical bursts are deduplicated for a cooldown window

Current trigger:

- a managed stack emits at least `3` new error-like log lines since the previous poll

Current matching is heuristic and intentionally narrow:

- matches message text containing terms like:
  - `error`
  - `fatal`
  - `panic`
  - `exception`
  - `traceback`
  - `critical`

Payload extension for runtime log error bursts:

```json
{
  "event": "runtime_log_error_burst",
  "summary": "1 stack(s) started logging repeated errors",
  "runtime_log": {
    "window_start": "2026-04-10T09:09:30Z",
    "window_end": "2026-04-10T09:10:00Z",
    "affected_stacks": [
      {
        "stack_id": "demo",
        "matching_entry_count": 3,
        "container_count": 1,
        "containers": ["demo-app"],
        "sample_messages": [
          "ERROR connection refused",
          "panic in worker",
          "fatal: task crashed"
        ]
      }
    ]
  }
}
```

Non-goals for the current slice:

- multiple channels or multiple webhook targets
- WhatsApp native integration
- retry queues
- batching or digests
- notification inbox inside Stacklab
- rich templating
- per-stack regex rules or operator-defined log alert policies
