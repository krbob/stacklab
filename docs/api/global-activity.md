# Global Activity API

Status: implemented. The primary delivery path is WebSocket push with a REST
snapshot for initial recovery, reconnect, and compatibility fallback.

Global activity gives shared application chrome one read model for currently
active background work. It is not a notification inbox and does not replace
durable job detail or audit history.

Contract ownership:

- [openapi.yaml](openapi.yaml) defines `GET /api/jobs/active` and the exact
  `ActiveJobsResponse` schemas;
- [websocket-protocol.md](websocket-protocol.md) defines socket commands,
  acknowledgements, frames, limits, and reconnect behavior;
- this document defines active-job semantics and how consumers reconcile the
  two transports.

## Active-job snapshot

`GET /api/jobs/active` returns all currently active jobs plus aggregate counts.
The active states are:

- `queued`;
- `running`;
- `cancel_requested`.

Terminal jobs are excluded. They remain available through job detail and audit
history according to retention policy.

Snapshot behavior:

- jobs are ordered by most recent activity first;
- `stack_id` may be `null` for workspace- or host-scoped work;
- `workflow`, `current_step`, and `latest_event` may be absent when the job has
  not produced that information;
- for bulk work, `current_step.target_stack_id` is a more precise current
  target than the root `stack_id`;
- elapsed time is derived from `started_at` when present and otherwise from
  `requested_at`;
- `summary.active_count` is the authoritative small-badge count.

The exact fields and required/optional distinctions are intentionally not
repeated here; use the generated `ActiveJobsResponse` frontend type.

## Live WebSocket stream

The frontend normally maintains one shared logical subscription on the
multiplexed `/api/ws` connection:

```json
{
  "type": "activity.subscribe",
  "request_id": "req_activity_sub",
  "stream_id": "activity_global",
  "payload": {}
}
```

The server then:

1. returns an `ack` with `status: subscribed`;
2. sends `activity.snapshot` with a full `ActiveJobsResponse`;
3. sends coalesced `activity.update` frames after job events.

Every snapshot and update carries the full active-jobs response. Updates are
not deltas and do not contain only the changed job. The server emits at most
one activity update every 500 ms and reads the latest durable state before
sending it. Intermediate signals may be coalesced without losing the final
state.

When a job becomes terminal, the next full update omits it from `items` and
refreshes the summary counts. Consumers must replace their activity snapshot
rather than merge items by ID.

Use `activity.unsubscribe` with the same `stream_id` when the shared consumer
is disposed. Multiple widgets must consume one application-level subscription;
they should not independently subscribe and unsubscribe the same logical
stream.

## Recovery and fallback

REST remains necessary even with push:

- a page can reconcile immediately before or after socket setup;
- reconnect creates a new socket and the subscription must be sent again;
- older or degraded backends may reject `activity.subscribe`;
- a socket error must not erase the last known state.

The current frontend falls back to `GET /api/jobs/active` every 3 seconds when
the socket is disconnected or activity push is unsupported. A successful
snapshot is marked fresh. If a later request fails, the UI keeps the last
snapshot as stale; without any snapshot, activity is unavailable.

## Relationship to job detail

Global activity is intentionally bounded to active work and a latest-event
summary. Opening a job uses:

- `GET /api/jobs/{jobId}` for the durable job snapshot;
- `GET /api/jobs/{jobId}/events` for retained replay;
- `jobs.subscribe` for live per-job events.

See [job-detail.md](job-detail.md) for retention-aware detail behavior. Sticky
completion notifications, historical browsing, and dismissible messages do
not belong in the global activity read model.
