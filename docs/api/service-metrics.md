# Service Metrics

This document defines the lightweight, process-local observability contract for
Stacklab itself. Host CPU, memory, filesystem, network, and process-list samples
remain under `GET /api/host/metrics`.

## Goals

- expose enough service activity to diagnose a single-host installation;
- report HTTP requests, jobs, WebSocket connections, durations, errors, and
  current readiness;
- keep collection in memory with fixed-size aggregates;
- avoid identifiers or labels whose cardinality grows with paths, stacks, jobs,
  sessions, or clients.

## Non-Goals

- long-term metrics retention;
- per-route, per-job-action, per-stack, or per-user analytics;
- unauthenticated public access to service metrics;
- distributed tracing or cross-host aggregation.

## `GET /api/service/metrics`

The endpoint requires the normal Stacklab session cookie and returns `200` with
the current snapshot. It performs the same bounded readiness checks as
`GET /api/ready` before taking the snapshot. An unavailable component is
therefore represented by `readiness.status = unavailable`; it does not change
the metrics response to `503`.

Example response:

```json
{
  "collected_at": "2026-07-11T12:30:00Z",
  "process": {
    "started_at": "2026-07-11T12:00:00Z",
    "uptime_seconds": 1800
  },
  "http": {
    "requests_total": 1420,
    "requests_in_flight": 2,
    "errors_total": 3,
    "duration_seconds_total": 18.625,
    "duration_seconds_max": 1.42
  },
  "jobs": {
    "started_total": 24,
    "active": 1,
    "completed_total": 23,
    "errors_total": 2,
    "duration_seconds_total": 284.4,
    "duration_seconds_max": 62.1
  },
  "websockets": {
    "connections_total": 9,
    "connections_active": 1,
    "errors_total": 1,
    "connection_duration_seconds_total": 7200.5,
    "connection_duration_seconds_max": 1800.2
  },
  "readiness": {
    "status": "ok",
    "checked_at": "2026-07-11T12:30:00Z",
    "checks": {
      "database": "ok",
      "frontend": "ok",
      "runtime": "ok"
    }
  }
}
```

## Metric Semantics

All values are process-local and reset when the Stacklab process restarts. The
collector stores only counters, gauges, cumulative durations, and maximum
durations; it does not retain individual observations.

- Prometheus scrapes at `/metrics` are excluded from HTTP activity and access logs.
- `http.requests_total` counts other completed HTTP handlers. A WebSocket upgrade is
  completed when that connection handler exits.
- `http.requests_in_flight` includes the current metrics request while its
  snapshot is being produced.
- `http.errors_total` counts completed responses with status `5xx`. Validation,
  authentication, and other `4xx` responses are not service errors.
- HTTP duration values cover completed handlers only.
- `jobs.started_total` increments only after a job and its initial event commit
  successfully. `jobs.completed_total` increments only after a terminal state
  transition commits.
- `jobs.errors_total` counts terminal `failed` and `timed_out` jobs. Successful
  and explicitly cancelled jobs are completed but are not errors.
- Job duration runs from `started_at` (falling back to `requested_at`) through
  `finished_at`.
- `websockets.connections_total` counts accepted, registered connections.
- `websockets.errors_total` counts failed upgrades and unexpected connection
  I/O. Repeated I/O failures on one accepted connection are coalesced into one
  error.
- WebSocket duration values cover closed, previously accepted connections.
- `readiness` contains only stable component names and `ok`/`error` states. The
  public diagnostic message remains in `/api/ready`; internal error details are
  logged and are never included in this response.

Because the snapshot is intentionally cumulative and in-memory, consumers that
need rates should calculate deltas between successive samples and treat a lower
counter or newer `process.started_at` as a process restart.

## `GET /metrics`

The optional Prometheus endpoint uses the main HTTP listener and is disabled
(`404`) unless `STACKLAB_METRICS_TOKEN_FILE` points to a readable token file.
The file must contain a randomly generated 32–256 character token without
whitespace; a trailing newline is allowed. Invalid configured files fail startup
rather than silently exposing an unauthenticated endpoint. The token is read
once at startup; rotation requires restarting Stacklab and refreshing the
scraper's credential file.

Requests require `Authorization: Bearer <token>`. Browser sessions do not grant
access, and this token cannot access any other Stacklab API. Failed authentication
returns `401` before readiness probes run. Responses use `Cache-Control: no-store`.
Keep the endpoint on a trusted internal network, or use HTTPS when crossing a
network boundary. Do not put the token in a URL or the managed Git workspace.

The endpoint evaluates readiness with the same two-second bound as `/api/ready`,
then returns Prometheus metrics. Component failures produce `stacklab_ready 0`
with HTTP `200`; Prometheus `up` describes scrape success, not application readiness.

| Metric | Meaning |
| --- | --- |
| `stacklab_build_info{version,commit}` | Current build, value 1 |
| `stacklab_uptime_seconds` | Process uptime |
| `stacklab_http_requests_total`, `stacklab_http_errors_total` | Completed requests and 5xx responses |
| `stacklab_http_requests_in_flight` | Active HTTP and WebSocket handlers |
| `stacklab_http_request_duration_seconds` | Completed-handler duration histogram |
| `stacklab_jobs_started_total`, `stacklab_jobs_completed_total`, `stacklab_jobs_errors_total` | Job activity and failures/timeouts |
| `stacklab_jobs_active`, `stacklab_job_duration_seconds` | Active jobs and completed-job duration histogram |
| `stacklab_websocket_connections_total`, `stacklab_websocket_connections_active`, `stacklab_websocket_errors_total` | WebSocket activity |
| `stacklab_websocket_connection_duration_seconds_total` | Cumulative duration of closed WebSockets |
| `stacklab_ready`, `stacklab_readiness_check{component}` | Overall and database/frontend/runtime readiness (0 or 1) |
| `stacklab_readiness_checked_timestamp_seconds` | Time of the latest readiness evaluation |
| `go_*`, `process_*` | Standard Go runtime and process metrics; process collector support depends on the OS |

HTTP histogram boundaries are 5, 10, 25, 50, 100, 250, and 500 ms, then 1, 2.5,
5, and 10 seconds. Job boundaries are 0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600,
1800, and 3600 seconds. Both also expose the implicit `+Inf` bucket. HTTP durations
include WebSocket handlers when those connections close, so long-lived sessions
can affect HTTP quantiles. No request paths, stack IDs, job IDs, session data,
credentials, or diagnostic messages are exported.

Collection remains in memory with fixed-size aggregates. The Prometheus adapter
mirrors the same snapshot used by the JSON endpoint; counters and histogram
buckets are copied under one lock. Each application instance has its own
Prometheus registry.

See [Prometheus and Grafana setup](../ops/monitoring.md) for the exporter, scrape,
and dashboard configuration.
