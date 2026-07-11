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
- a Prometheus-compatible public scrape endpoint;
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

- `http.requests_total` counts completed HTTP handlers. A WebSocket upgrade is
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
