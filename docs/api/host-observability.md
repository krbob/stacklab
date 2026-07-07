# Host Observability Contract Draft

This document defines the current host observability contract:

- Stacklab version visibility
- host overview
- short-window host metrics for the native dashboard
- Stacklab service logs backed by `journald`

It is intentionally narrower than a full host monitoring API.

## Goals

- help the operator distinguish host problems from stack problems
- expose Stacklab's own version and runtime context clearly
- provide an operator dashboard for CPU, CPU temperature, memory, filesystems,
  disk I/O, network throughput, and top process visibility
- make Stacklab service logs available in the browser without exposing arbitrary host logs

## Platform Caveat

The host observability surface is designed primarily for Linux hosts running Stacklab under `systemd`.

Practical implication:

- the most complete and representative `/host` view is on Linux
- local development on macOS is useful for UI work, but it is a degraded environment for host observability

Typical macOS limitations during development:

- Stacklab service logs may be unavailable because there is no `journald` + `journalctl -u stacklab` model
- some host metrics may be partial or less representative than on Linux
- Docker Desktop / local virtualization can make resource numbers look unlike a real target host

Linux deployment note:

- on Linux hosts with `systemd`, readable Stacklab service logs also require the Stacklab service user to have access to `journald`, typically via membership in the `systemd-journal` group when that group exists

This is expected behavior, not a product bug by itself.

## Non-Goals

- full host monitoring platform
- long-term metrics storage
- speedtest checks
- GPU metrics
- userland sensor daemon or `lm-sensors` runtime dependency
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
      "path": "/srv/stacklab",
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
- refresh target: every `5s` to `15s`
- `disk.path` should reflect the Stacklab root filesystem, not every mounted filesystem
- on package-managed installs this is typically `/srv/stacklab`; on tarball installs it is typically `/opt/stacklab`
- `stacklab.started_at` is process-start metadata, not install time
- on non-Linux development hosts the response may be partial or degraded compared to production Linux hosts

## `GET /api/host/metrics`

Purpose:

- return live host resource metrics and a short in-memory history for the native Host dashboard

Query parameters:

- `since` optional RFC3339 timestamp; when present, `history` contains only
  samples newer than `since`, while `current` still returns the latest sample

Response:

```json
{
  "sample_interval_seconds": 1,
  "background_sample_interval_seconds": 30,
  "active_sample_interval_seconds": 1,
  "history_window_seconds": 1800,
  "current": {
    "sampled_at": "2026-04-04T14:13:22Z",
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
    "swap": {
      "total_bytes": 2147483648,
      "used_bytes": 536870912,
      "available_bytes": 1610612736,
      "usage_percent": 25.0
    },
    "temperatures": {
      "cpu_celsius": 42.5,
      "cpu_sensor": {
        "name": "coretemp",
        "label": "Package id 0",
        "temperature_celsius": 42.5
      },
      "sensors": [
        {
          "name": "coretemp",
          "label": "Package id 0",
          "temperature_celsius": 42.5
        }
      ]
    },
    "filesystems": [
      {
        "mount_point": "/srv/stacklab",
        "device": "/dev/nvme0n1p2",
        "fs_type": "ext4",
        "total_bytes": 274877906944,
        "used_bytes": 83437182976,
        "available_bytes": 191440723968,
        "usage_percent": 30.4,
        "primary": true
      }
    ],
    "disk_io": {
      "total_read_bytes_per_sec": 4096,
      "total_write_bytes_per_sec": 2048,
      "devices": [
        {
          "name": "nvme0n1",
          "read_bytes": 123456789,
          "write_bytes": 98765432,
          "read_bytes_per_sec": 4096,
          "write_bytes_per_sec": 2048
        }
      ]
    },
    "network": {
      "total_rx_bytes_per_sec": 2048,
      "total_tx_bytes_per_sec": 1024,
      "public_ip": "8.8.8.8",
      "interfaces": [
        {
          "name": "eth0",
          "rx_bytes": 123456789,
          "tx_bytes": 98765432,
          "rx_bytes_per_sec": 2048,
          "tx_bytes_per_sec": 1024
        }
      ]
    },
    "processes": {
      "total": 118,
      "items": [
        {
          "pid": 1234,
          "user": "stacklab",
          "state": "S",
          "cpu_percent": 12.5,
          "memory_bytes": 268435456,
          "memory_percent": 3.1,
          "command": "stacklab"
        }
      ]
    }
  },
  "history": []
}
```

Notes:

- metrics are sampled server-side and kept only in memory
- history is pruned by sample timestamp to a `30m` window; no SQLite persistence is part of v1
- first UI load fetches the current history window; subsequent active polling can
  use `since=<last_sampled_at>` to fetch only new samples
- idle/background sampling runs every `30s`
- calling this endpoint marks the dashboard as active; active sampling runs every `1s`
- the frontend polls this endpoint only while the `/host` page is visible
- after the dashboard stops polling, active mode expires and sampling returns to the background interval
- filesystem metrics come from Linux mount information plus `statfs`
- filesystem metrics are deduplicated by physical mount identity
  (`fs_type`, `major:minor`, and device/source) because package-managed
  `systemd` services can expose Stacklab `ReadWritePaths` as repeated bind
  mounts in `/proc/self/mountinfo`
- swap metrics come from `SwapTotal` and `SwapFree` in `/proc/meminfo`; hosts
  without swap report zero totals
- temperature metrics are read directly from Linux sysfs:
  `/sys/class/hwmon` first, with `/sys/class/thermal` as a fallback
- CPU temperature is selected from CPU-like sensors such as `coretemp`,
  `k10temp`, `Tctl`, `Tdie`, package/core labels, or CPU thermal zones; if no
  CPU-like sensor is exposed, `cpu_celsius` is `null`
- when multiple CPU-like sensors are exposed, package/Tctl/Tdie-style sensors
  are preferred over individual core sensors so the UI label remains stable
  instead of alternating between raw sysfs sensor names
- sensor collection has no `lm-sensors` or `sensors` command dependency; those
  tools remain optional host diagnostics only
- disk I/O throughput is derived from `/proc/diskstats` sector deltas and uses
  `512` bytes per sector
- loop/ram devices and likely partitions are filtered out to avoid double-counting
  whole disks and their partitions in the totals
- virtual filesystems and Docker/container runtime internals are filtered out
- network filesystems such as NFS/CIFS are skipped in v1 so a stalled remote mount cannot block dashboard sampling
- `statfs` still runs synchronously for accepted local filesystems; add timeout
  isolation only if real deployments expose a local or unclassified filesystem
  that can hang
- network throughput is derived from `/proc/net/dev` byte deltas
- public IP is discovered asynchronously through `https://api64.ipify.org` only
  while the dashboard is active, cached for `10m`, and omitted when the lookup
  fails or returns a private/non-global address; lookup failures never block host
  metric sampling
- public IP lookup is opt-in and requires
  `STACKLAB_HOST_PUBLIC_IP_LOOKUP_ENABLED=true`; the UI masks the value until
  the operator explicitly reveals it
- process metrics are read from `/proc/<pid>/stat`, `/proc/<pid>/comm`, and
  process directory ownership; Stacklab does not read or return full process
  command lines, so process arguments are not exposed in the dashboard
- process `cpu_percent` is based on deltas between samples; the first sample can
  report `0`, and a busy multi-threaded process can exceed `100%` on multi-core
  hosts
- process collection is throttled to a slower interval than the active host
  metrics loop to avoid a full `/proc` walk every second on busy hosts
- `processes` is returned only on `current`; history samples omit it to keep the
  30-minute metrics window small
- v1 does not run speedtest checks
- Docker bridge/veth-style virtual interfaces are filtered out of the dashboard totals
- GPU metrics remain a backlog candidate, not part of this contract

## `GET /api/host/stacklab-logs`

Purpose:

- fetch recent Stacklab service logs from `journalctl -u stacklab`

Query parameters:

- `limit` optional, default `200`, max `1000`
- `cursor` optional pagination/follow cursor
- `level` optional: `debug`, `info`, `warn`, `error`
- `q` optional text filter applied server-side when practical, otherwise client-side only
- `include_http` optional boolean, default `false`; when false, routine Stacklab
  HTTP access logs (`msg="http request"`) are hidden from this diagnostic view

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
- the default response filters out routine HTTP access logs to keep `/host`
  focused on actionable Stacklab events; the journal remains unchanged and the
  UI can opt into full access-log visibility with `include_http=true`
- on hosts without readable `journald` service logs, the UI should show an unavailable/degraded state instead of treating it as a generic failure

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
- `/proc/self/mountinfo`
- `/proc/net/dev`
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
