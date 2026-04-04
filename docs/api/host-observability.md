# Host Observability Contract Draft

This document defines the proposed contract for Milestone 1:

- Stacklab version visibility
- host overview
- Stacklab service logs backed by `journald`

It is intentionally narrower than a full host monitoring API.

## Goals

- help the operator distinguish host problems from stack problems
- expose Stacklab's own version and runtime context clearly
- make Stacklab service logs available in the browser without exposing arbitrary host logs

## Non-Goals

- full host monitoring platform
- long-term metrics storage
- generic system log browser
- process manager UI

## REST Endpoints

## `GET /api/host/overview`

Purpose:

- return current host and Stacklab runtime metadata for a host overview screen

Response:

```json
{
  "host": {
    "hostname": "debian-homelab",
    "os_name": "Debian GNU/Linux 13",
    "kernel_version": "6.12.19-amd64",
    "architecture": "linux-amd64",
    "uptime_seconds": 86400
  },
  "stacklab": {
    "version": "2026.04.0",
    "commit": "abc1234",
    "started_at": "2026-04-04T14:10:00Z"
  },
  "docker": {
    "engine_version": "28.5.1",
    "compose_version": "2.39.2"
  },
  "resources": {
    "cpu": {
      "core_count": 4,
      "load_average": [0.31, 0.22, 0.18],
      "usage_percent": 12.4
    },
    "memory": {
      "total_bytes": 8589934592,
      "used_bytes": 3145728000,
      "available_bytes": 5444206592,
      "usage_percent": 36.6
    },
    "disk": {
      "path": "/opt/stacklab",
      "total_bytes": 274877906944,
      "used_bytes": 83437182976,
      "available_bytes": 191440723968,
      "usage_percent": 30.4
    }
  }
}
```

Notes:

- this endpoint is read-only and inexpensive enough for periodic refresh
- refresh target: every `15s` to `30s`
- `disk.path` should reflect the Stacklab root filesystem, not every mounted filesystem
- `stacklab.started_at` is process-start metadata, not install time

## `GET /api/host/stacklab-logs`

Purpose:

- fetch recent Stacklab service logs from `journalctl -u stacklab`

Query parameters:

- `limit` optional, default `200`, max `1000`
- `cursor` optional pagination/follow cursor
- `level` optional: `debug`, `info`, `warn`, `error`
- `q` optional text filter applied server-side when practical, otherwise client-side only

Response:

```json
{
  "items": [
    {
      "timestamp": "2026-04-04T14:13:22Z",
      "level": "info",
      "message": "HTTP server listening",
      "cursor": "s=8f2..."
    }
  ],
  "next_cursor": "s=8f3...",
  "has_more": true
}
```

Notes:

- this endpoint should support polling-based follow mode
- it does not need WebSocket support in the first implementation
- `cursor` should be opaque to the UI

## `GET /api/host/stacklab-logs/stream`

Status:

- optional, not required for first implementation

Reason:

- the first version can be built with polling against `GET /api/host/stacklab-logs`
- only add streaming if polling is not good enough in practice

## UI Expectations

## Host Overview

Expected UI uses:

- dedicated host page
- optional compact dashboard widget later
- version info in settings and/or footer should consume the same backend data model

## Stacklab Logs

Expected UI uses:

- recent log list
- refresh / follow mode
- severity filter
- text search

Not required initially:

- ANSI formatting
- multiline grouping sophistication
- arbitrary journal unit switching

## Suggested Backend Sources

- `/proc/uptime`
- `/proc/loadavg`
- `/proc/meminfo`
- `statfs` for disk usage
- `os.Hostname`
- OS-release parsing under `/etc/os-release`
- existing Docker / Compose version discovery code
- `journalctl -u stacklab --output=json`

## Tests

Recommended initial tests:

- host overview shape and non-empty required fields
- journald reader parser against fixture output
- limit and cursor behavior
- level filtering

Recommended later tests:

- integration test on real Linux host with `systemd`
