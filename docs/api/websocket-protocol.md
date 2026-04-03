# WebSocket Protocol

## Purpose

This document defines the WebSocket protocol used by Stacklab v1 for:

- live logs
- live stats
- job progress streaming
- terminal sessions

## Transport Decision

Stacklab v1 uses:

- one native WebSocket endpoint
- one WebSocket connection per browser tab or app instance
- multiple logical streams multiplexed over that one connection

This avoids opening separate sockets for logs, stats, jobs, and terminal while still allowing independent subscriptions.

## Endpoint

```text
GET /api/ws
```

Authentication:

- the browser connects with the authenticated session cookie
- unauthenticated upgrade attempts are rejected

## Protocol Principles

- all application frames are JSON
- subscriptions are correlated by `stream_id`
- command/ack pairs are correlated by `request_id`
- terminal sessions are correlated by server-issued `session_id`
- logs, stats, and job streams are ephemeral to the current socket connection
- terminal sessions may survive reconnect only if the backend PTY is still alive

## Common Frame Shapes

### Client Command

```json
{
  "type": "logs.subscribe",
  "request_id": "req_01",
  "stream_id": "stream_logs_nextcloud",
  "payload": {
    "stack_id": "nextcloud"
  }
}
```

### Server Ack

```json
{
  "type": "ack",
  "request_id": "req_01",
  "stream_id": "stream_logs_nextcloud",
  "payload": {
    "status": "subscribed"
  }
}
```

### Server Error

```json
{
  "type": "error",
  "request_id": "req_01",
  "stream_id": "stream_logs_nextcloud",
  "error": {
    "code": "not_found",
    "message": "Stack not found."
  }
}
```

### Server Event

```json
{
  "type": "logs.event",
  "stream_id": "stream_logs_nextcloud",
  "payload": {
    "entries": []
  }
}
```

## Connection Lifecycle

### 1. Open

Client opens `GET /api/ws`.

### 2. Hello

Server immediately sends:

```json
{
  "type": "hello",
  "payload": {
    "connection_id": "conn_01hr...",
    "protocol_version": 1,
    "heartbeat_interval_ms": 20000,
    "features": {
      "host_shell": false
    }
  }
}
```

### 3. Subscribe Or Open Sessions

Client sends one or more commands:

- `logs.subscribe`
- `stats.subscribe`
- `jobs.subscribe`
- `terminal.open`
- `terminal.attach`

### 4. Heartbeat

Server sends app-level heartbeat frames:

```json
{
  "type": "ping",
  "payload": {
    "ts": "2026-04-03T18:42:00Z"
  }
}
```

Client responds:

```json
{
  "type": "pong",
  "payload": {
    "ts": "2026-04-03T18:42:00Z"
  }
}
```

Rules:

- server heartbeat interval comes from `hello.payload.heartbeat_interval_ms`
- if heartbeat is missed repeatedly, the server may close the connection

### 5. Reconnect

Recommended client reconnect backoff:

- 1s
- 2s
- 5s
- 10s
- 20s
- 30s max

Rules after reconnect:

- logs, stats, and job subscriptions must be re-sent by the client
- terminal sessions are not assumed to resume automatically
- client may attempt `terminal.attach` with a previously known `session_id`

## Stream Types

## Logs

### Subscribe

Client:

```json
{
  "type": "logs.subscribe",
  "request_id": "req_logs_01",
  "stream_id": "logs_nextcloud_all",
  "payload": {
    "stack_id": "nextcloud",
    "service_names": [],
    "tail": 200,
    "timestamps": true
  }
}
```

Rules:

- `service_names = []` means all services in the stack
- `tail` is optional; default is implementation-defined

### Unsubscribe

Client:

```json
{
  "type": "logs.unsubscribe",
  "request_id": "req_logs_02",
  "stream_id": "logs_nextcloud_all",
  "payload": {}
}
```

### Event

Server:

```json
{
  "type": "logs.event",
  "stream_id": "logs_nextcloud_all",
  "payload": {
    "entries": [
      {
        "timestamp": "2026-04-03T18:42:01Z",
        "service_name": "app",
        "container_id": "2f4b...",
        "stream": "stdout",
        "line": "Nextcloud is ready"
      }
    ]
  }
}
```

Notes:

- logs may be batched into `entries[]`
- UI should preserve order within a batch

## Stats

### Subscribe

Client:

```json
{
  "type": "stats.subscribe",
  "request_id": "req_stats_01",
  "stream_id": "stats_nextcloud",
  "payload": {
    "stack_id": "nextcloud"
  }
}
```

### Unsubscribe

Client:

```json
{
  "type": "stats.unsubscribe",
  "request_id": "req_stats_02",
  "stream_id": "stats_nextcloud",
  "payload": {}
}
```

### Frame

Server:

```json
{
  "type": "stats.frame",
  "stream_id": "stats_nextcloud",
  "payload": {
    "timestamp": "2026-04-03T18:42:02Z",
    "stack_totals": {
      "cpu_percent": 2.4,
      "memory_bytes": 324009984,
      "memory_limit_bytes": 805306368,
      "network_rx_bytes_per_sec": 12288,
      "network_tx_bytes_per_sec": 3072
    },
    "containers": [
      {
        "container_id": "2f4b...",
        "service_name": "app",
        "cpu_percent": 2.1,
        "memory_bytes": 256901120,
        "memory_limit_bytes": 536870912,
        "network_rx_bytes_per_sec": 10444,
        "network_tx_bytes_per_sec": 2867
      }
    ]
  }
}
```

Rules:

- stats frames are snapshots, not deltas
- UI may store rolling history client-side for sparklines

## Jobs

### Subscribe

Client:

```json
{
  "type": "jobs.subscribe",
  "request_id": "req_job_01",
  "stream_id": "job_01hr_progress",
  "payload": {
    "job_id": "job_01hr..."
  }
}
```

### Unsubscribe

Client:

```json
{
  "type": "jobs.unsubscribe",
  "request_id": "req_job_02",
  "stream_id": "job_01hr_progress",
  "payload": {}
}
```

### Event

Server:

```json
{
  "type": "jobs.event",
  "stream_id": "job_01hr_progress",
  "payload": {
    "job_id": "job_01hr...",
    "stack_id": "nextcloud",
    "action": "pull",
    "state": "running",
    "event": "job_progress",
    "message": "Pulling image nextcloud:29",
    "step": null,
    "timestamp": "2026-04-03T18:42:03Z"
  }
}
```

### Workflow Step Event

For workflow jobs:

```json
{
  "type": "jobs.event",
  "stream_id": "job_create_stack_progress",
  "payload": {
    "job_id": "job_01hr...",
    "stack_id": "my-new-app",
    "action": "create_stack",
    "state": "running",
    "event": "job_step_started",
    "message": "Starting workflow step",
    "step": {
      "index": 2,
      "total": 2,
      "action": "up"
    },
    "timestamp": "2026-04-03T18:42:05Z"
  }
}
```

Rules:

- `event` values mirror the domain model:
  - `job_started`
  - `job_step_started`
  - `job_step_finished`
  - `job_progress`
  - `job_log`
  - `job_warning`
  - `job_error`
  - `job_finished`
- `state` uses the domain job state:
  - `queued`
  - `running`
  - `succeeded`
  - `failed`
  - `cancel_requested`
  - `cancelled`
  - `timed_out`
- `step` is `null` for non-workflow jobs

### Job Log Event Semantics

When `event = job_log`, the payload may include both:

- `message`: optional human-readable summary
- `data`: optional raw process output chunk

Example:

```json
{
  "type": "jobs.event",
  "stream_id": "job_01hr_progress",
  "payload": {
    "job_id": "job_01hr...",
    "stack_id": "nextcloud",
    "action": "pull",
    "state": "running",
    "event": "job_log",
    "message": "Pulling service app",
    "data": "29-apache: Downloading [=====>    ] 67%\r",
    "step": null,
    "timestamp": "2026-04-03T18:42:04Z"
  }
}
```

Rules:

- `message` is intended for human-readable summaries when available
- `data` is intended for raw stdout/stderr chunks and may contain carriage returns or ANSI sequences
- UI may prefer rendering `data` for progress panels that mimic CLI output
- either field may be omitted when not relevant

## Terminal

Terminal transport uses JSON frames carrying UTF-8 strings.

Rules:

- terminal bytes are transported in `payload.data`
- ANSI escape sequences are preserved as raw terminal text
- UI must render terminal output only through XTerm.js, not through HTML injection

### Open Session

Client:

```json
{
  "type": "terminal.open",
  "request_id": "req_term_open_01",
  "stream_id": "term_nextcloud_app_1",
  "payload": {
    "stack_id": "nextcloud",
    "container_id": "2f4b...",
    "shell": "/bin/sh",
    "cols": 120,
    "rows": 36
  }
}
```

Server:

```json
{
  "type": "terminal.opened",
  "request_id": "req_term_open_01",
  "stream_id": "term_nextcloud_app_1",
  "payload": {
    "session_id": "term_01hr...",
    "container_id": "2f4b...",
    "shell": "/bin/sh"
  }
}
```

Open failure behavior:

- if the backend cannot establish the exec/PTTY session, it responds with a standard `error` frame correlated by `request_id`
- no terminal session is considered open in that case

Example:

```json
{
  "type": "error",
  "request_id": "req_term_open_01",
  "stream_id": "term_nextcloud_app_1",
  "error": {
    "code": "invalid_state",
    "message": "Container is not running."
  }
}
```

### Attach Session

Used after reconnect when the UI wants to reattach to an existing server-side PTY if it still exists.

Client:

```json
{
  "type": "terminal.attach",
  "request_id": "req_term_attach_01",
  "stream_id": "term_nextcloud_app_1",
  "payload": {
    "session_id": "term_01hr...",
    "cols": 120,
    "rows": 36
  }
}
```

Success server response:

```json
{
  "type": "terminal.opened",
  "request_id": "req_term_attach_01",
  "stream_id": "term_nextcloud_app_1",
  "payload": {
    "session_id": "term_01hr...",
    "container_id": "2f4b...",
    "shell": "/bin/sh"
  }
}
```

Failure behavior:

- server returns `error` with code `terminal_session_not_found`
- UI should show `Session ended. Start a new session?`

### Input

Client:

```json
{
  "type": "terminal.input",
  "stream_id": "term_nextcloud_app_1",
  "payload": {
    "session_id": "term_01hr...",
    "data": "ls -la\r"
  }
}
```

### Output

Server:

```json
{
  "type": "terminal.output",
  "stream_id": "term_nextcloud_app_1",
  "payload": {
    "session_id": "term_01hr...",
    "data": "total 48\r\n-rw-r--r-- 1 root root 123 config.php\r\n"
  }
}
```

### Resize

Client:

```json
{
  "type": "terminal.resize",
  "stream_id": "term_nextcloud_app_1",
  "payload": {
    "session_id": "term_01hr...",
    "cols": 140,
    "rows": 40
  }
}
```

### Close

Client:

```json
{
  "type": "terminal.close",
  "request_id": "req_term_close_01",
  "stream_id": "term_nextcloud_app_1",
  "payload": {
    "session_id": "term_01hr..."
  }
}
```

Server terminal exit event:

```json
{
  "type": "terminal.exited",
  "stream_id": "term_nextcloud_app_1",
  "payload": {
    "session_id": "term_01hr...",
    "exit_code": 0,
    "reason": "process_exit"
  }
}
```

Possible `reason` values:

- `process_exit`
- `idle_timeout`
- `server_cleanup`
- `connection_replaced`

Rules:

- `terminal.exited` is only sent for a session that was successfully opened or attached
- failures before a session is established use the generic `error` frame, not `terminal.exited`
- if the client later sends `terminal.input` or `terminal.resize` for a dead session, the server may respond with `error` using `terminal_session_not_found`

## Multiplexing Rules

The same connection may carry multiple concurrent streams, for example:

- one logs stream
- one stats stream
- one job progress stream
- multiple terminal sessions

Rules:

- `stream_id` must be unique within the current connection
- UI should generate stable `stream_id` values per active view or session
- server may reject duplicate active `stream_id` values with `409 conflict`-style WebSocket error semantics

## Reconnect Semantics

### Logs

- client reconnects socket
- client re-sends `logs.subscribe`
- server starts a fresh log stream
- client preserves already rendered lines locally

### Stats

- client reconnects socket
- client re-sends `stats.subscribe`
- server resumes sending fresh snapshots
- no server-side replay is required

### Jobs

- client reconnects socket
- client re-sends `jobs.subscribe` for still-relevant jobs
- server sends current job events from that point onward
- UI may also call `GET /api/jobs/{jobId}` to recover current status

### Terminal

- client reconnects socket
- client decides whether to attempt `terminal.attach`
- if attach succeeds, stream continues
- if attach fails, UI keeps local scrollback and prompts for a new terminal session

## Error Codes Important To UI

Suggested WebSocket error codes:

- `unauthorized`
- `forbidden`
- `not_found`
- `validation_failed`
- `invalid_state`
- `stack_locked`
- `terminal_session_not_found`
- `terminal_not_supported`
- `stream_conflict`
- `internal_error`

## UI Guidance

- maintain one socket per tab
- store active subscriptions in local state for reconnect replay
- keep terminal `session_id` in memory per terminal tab
- treat job events as streaming progress, then reconcile final truth through REST
- never assume PTY resume unless `terminal.attach` succeeds
