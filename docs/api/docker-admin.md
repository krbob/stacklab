# Docker Admin Contract Draft

This document defines the proposed contract for Docker Admin v1:

- read-only Docker service status
- read-only Docker Engine and Compose metadata
- read-only `daemon.json` visibility

It is intentionally narrower than a generic host administration API.

## Goals

- remove routine SSH usage for common Docker daemon diagnostics
- make Docker daemon status visible without exposing arbitrary host control
- support the first concrete use case of inspecting Docker DNS configuration

## Non-Goals

- arbitrary shell access
- arbitrary host file editing
- applying Docker daemon changes in v1
- managing every Docker daemon key in the first version

## Confirmed Route Shape

Recommended global route:

- `/docker`

Rationale:

- `/host` is for host and Stacklab observability
- `/maintenance` is for operational workflows such as update and cleanup
- Docker daemon administration will grow into its own product surface

## REST Endpoints

## `GET /api/docker/admin/overview`

Purpose:

- return the operator-facing Docker daemon overview for the `/docker` page

Response:

```json
{
  "service": {
    "manager": "systemd",
    "supported": true,
    "unit_name": "docker.service",
    "load_state": "loaded",
    "active_state": "active",
    "sub_state": "running",
    "unit_file_state": "enabled",
    "fragment_path": "/lib/systemd/system/docker.service",
    "started_at": "2026-04-09T08:00:00Z"
  },
  "engine": {
    "available": true,
    "version": "28.5.1",
    "api_version": "1.51",
    "compose_version": "2.39.2",
    "root_dir": "/var/lib/docker",
    "driver": "overlay2",
    "logging_driver": "json-file",
    "cgroup_driver": "systemd"
  },
  "daemon_config": {
    "path": "/etc/docker/daemon.json",
    "exists": true,
    "permissions": {
      "owner_uid": 0,
      "owner_name": "root",
      "group_gid": 0,
      "group_name": "root",
      "mode": "0644",
      "readable": true,
      "writable": false
    },
    "size_bytes": 84,
    "modified_at": "2026-04-08T19:20:00Z",
    "valid_json": true,
    "configured_keys": ["dns", "log-driver"],
    "summary": {
      "dns": ["192.168.1.2"],
      "registry_mirrors": [],
      "insecure_registries": [],
      "log_driver": "json-file",
      "data_root": "",
      "live_restore": null
    }
  }
}
```

Notes:

- this endpoint should be safe to poll every `15s` to `30s`
- the service block is allowed to degrade on hosts without readable `systemd`
- the engine block is allowed to degrade if Docker is not reachable
- the daemon config summary reflects configured keys in the file, not a full "effective configuration" model

## `GET /api/docker/admin/daemon-config`

Purpose:

- fetch the current `daemon.json` file in a read-only browser-safe shape

Response:

```json
{
  "path": "/etc/docker/daemon.json",
  "exists": true,
  "permissions": {
    "owner_uid": 0,
    "owner_name": "root",
    "group_gid": 0,
    "group_name": "root",
    "mode": "0644",
    "readable": true,
    "writable": false
  },
  "size_bytes": 84,
  "modified_at": "2026-04-08T19:20:00Z",
  "valid_json": true,
  "configured_keys": ["dns", "log-driver"],
  "summary": {
    "dns": ["192.168.1.2"],
    "registry_mirrors": [],
    "insecure_registries": [],
    "log_driver": "json-file",
    "data_root": "",
    "live_restore": null
  },
  "content": "{\n  \"dns\": [\"192.168.1.2\"],\n  \"log-driver\": \"json-file\"\n}\n"
}
```

If the file exists but contains invalid JSON:

```json
{
  "path": "/etc/docker/daemon.json",
  "exists": true,
  "permissions": {
    "mode": "0644",
    "readable": true,
    "writable": false
  },
  "size_bytes": 14,
  "modified_at": "2026-04-08T19:20:00Z",
  "valid_json": false,
  "parse_error": "invalid character 'i' looking for beginning of object key string",
  "configured_keys": [],
  "summary": {
    "dns": [],
    "registry_mirrors": [],
    "insecure_registries": [],
    "log_driver": "",
    "data_root": "",
    "live_restore": null
  },
  "content": "{ invalid json"
}
```

If the file does not exist:

```json
{
  "path": "/etc/docker/daemon.json",
  "exists": false,
  "valid_json": true,
  "configured_keys": [],
  "summary": {
    "dns": [],
    "registry_mirrors": [],
    "insecure_registries": [],
    "log_driver": "",
    "data_root": "",
    "live_restore": null
  }
}
```

Notes:

- no write/apply operation exists in this milestone
- unreadable files should return `200` with `permissions.readable = false` instead of a generic hard failure
- the UI should treat missing `daemon.json` as "Docker is using defaults", not as an error by itself

## Platform Caveat

This surface is designed primarily for Debian-family Linux hosts running Docker under `systemd`.

Expected degraded states:

- on macOS, `systemctl` status is unavailable
- on hosts without readable Docker daemon metadata, the engine block may be partially empty
- on hosts where `daemon.json` is unreadable by the Stacklab service user, content should remain unavailable while metadata stays visible

## Suggested Backend Sources

- `systemctl show docker.service`
- `docker version --format '{{json .Server}}'`
- `docker info --format '{{json .}}'`
- `docker compose version --short`
- `/etc/docker/daemon.json`

## Tests

Recommended initial tests:

- parse and expose `systemctl show` fields
- degrade cleanly when `systemctl` is unavailable
- degrade cleanly when Docker Engine is unavailable
- valid `daemon.json` summary extraction
- invalid `daemon.json` parse reporting
