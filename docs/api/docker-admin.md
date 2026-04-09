# Docker Admin Contract Draft

This document defines the proposed contract for Docker Admin:

- read-only Docker service status
- read-only Docker Engine and Compose metadata
- read-only `daemon.json` visibility
- managed validation preview for selected `daemon.json` keys

It is intentionally narrower than a generic host administration API or arbitrary file editor.

## Goals

- remove routine SSH usage for common Docker daemon diagnostics
- make Docker daemon status visible without exposing arbitrary host control
- support the first concrete use case of inspecting Docker DNS configuration

## Non-Goals

- arbitrary shell access
- arbitrary host file editing
- applying Docker daemon changes without an explicit privileged helper path
- managing every Docker daemon key in the first writable version

## Confirmed Route Shape

Recommended global route:

- `/docker`

Rationale:

- `/host` is for host and Stacklab observability
- `/maintenance` is for operational workflows such as update and cleanup
- Docker daemon administration will grow into its own product surface

## Managed Keys In The First Writable Slice

The first writable slice is intentionally constrained to:

- `dns`
- `registry_mirrors`
- `insecure_registries`
- `live_restore`

These are chosen because they cover real homelab needs such as custom DNS and registry reachability without opening up high-risk keys like `data-root`.

Current status:

- validation preview is supported
- actual privileged apply requires a configured helper path and remains disabled by default

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
    },
    "write_capability": {
      "supported": false,
      "reason": "Managed Docker daemon apply is not configured yet.",
      "managed_keys": ["dns", "registry_mirrors", "insecure_registries", "live_restore"]
    }
  },
  "write_capability": {
    "supported": false,
    "reason": "Managed Docker daemon apply is not configured yet.",
    "managed_keys": ["dns", "registry_mirrors", "insecure_registries", "live_restore"]
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
  "write_capability": {
    "supported": false,
    "reason": "Managed Docker daemon apply is not configured yet.",
    "managed_keys": ["dns", "registry_mirrors", "insecure_registries", "live_restore"]
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

- the first managed write slice is not a raw editor
- write/apply is represented by `write_capability`, which remains disabled until a privileged helper path is configured
- unreadable files should return `200` with `permissions.readable = false` instead of a generic hard failure
- the UI should treat missing `daemon.json` as "Docker is using defaults", not as an error by itself

## `POST /api/docker/admin/daemon-config/validate`

Purpose:

- validate and preview changes to the managed Docker daemon settings without writing the file yet

Request:

```json
{
  "settings": {
    "dns": ["192.168.1.2"],
    "registry_mirrors": ["https://mirror.local"],
    "live_restore": true
  },
  "remove_keys": ["insecure_registries"]
}
```

Response:

```json
{
  "write_capability": {
    "supported": false,
    "reason": "Managed Docker daemon apply is not configured yet.",
    "managed_keys": ["dns", "registry_mirrors", "insecure_registries", "live_restore"]
  },
  "changed_keys": ["dns", "live_restore"],
  "requires_restart": true,
  "warnings": [
    "Applying Docker daemon settings requires a Docker restart."
  ],
  "preview": {
    "path": "/etc/docker/daemon.json",
    "content": "{\n  \"dns\": [\n    \"192.168.1.2\"\n  ],\n  \"live-restore\": true,\n  \"log-driver\": \"json-file\"\n}\n",
    "configured_keys": ["dns", "live-restore", "log-driver"],
    "summary": {
      "dns": ["192.168.1.2"],
      "registry_mirrors": [],
      "insecure_registries": [],
      "log_driver": "json-file",
      "data_root": "",
      "live_restore": true
    }
  }
}
```

Notes:

- unknown keys already present in `daemon.json` must be preserved in the preview
- unsupported keys in `remove_keys` are rejected with `400 validation_failed`
- if the current file contains invalid JSON, the endpoint returns `409 invalid_state`
- if the current file is unreadable by the Stacklab service user, the endpoint returns `409 permission_denied`
- this endpoint is safe to use as the form-level preview step before a later apply flow

## `POST /api/docker/admin/daemon-config/apply`

Purpose:

- start the privileged apply workflow for the managed Docker daemon settings

Request:

- same payload as `POST /api/docker/admin/daemon-config/validate`

Response:

```json
{
  "job": {
    "id": "job_xxx",
    "stack_id": null,
    "action": "apply_docker_daemon_config",
    "state": "succeeded",
    "requested_at": "2026-04-09T12:00:00Z",
    "started_at": "2026-04-09T12:00:00Z",
    "finished_at": "2026-04-09T12:00:03Z",
    "workflow": {
      "steps": [
        { "action": "validate_config", "state": "succeeded" },
        { "action": "apply_and_restart", "state": "succeeded" },
        { "action": "verify_recovery", "state": "succeeded" }
      ]
    }
  }
}
```

Notes:

- this is a global job because Docker restart affects all managed stacks
- the helper must:
  - create a backup
  - write the new `daemon.json`
  - restart Docker
  - roll back automatically if restart fails
- if the helper path is not configured, this endpoint returns `501 not_implemented`
- job events should carry backup and rollback warnings where relevant

## Platform Caveat

This surface is designed primarily for Debian-family Linux hosts running Docker under `systemd`.

Expected degraded states:

- on macOS, `systemctl` status is unavailable
- on hosts without readable Docker daemon metadata, the engine block may be partially empty
- on hosts where `daemon.json` is unreadable by the Stacklab service user, content should remain unavailable while metadata stays visible
- on hosts without a privileged helper, `write_capability.supported` should remain `false` even though preview validation is available
- even when the helper binary is installed, operators should explicitly enable it through environment configuration and a narrow `sudoers` allowlist

## Suggested Backend Sources

- `systemctl show docker.service`
- `docker version --format '{{json .Server}}'`
- `docker info --format '{{json .}}'`
- `docker compose version --short`
- `/etc/docker/daemon.json`
- `stacklab-docker-admin-helper`

## Tests

Recommended initial tests:

- parse and expose `systemctl show` fields
- degrade cleanly when `systemctl` is unavailable
- degrade cleanly when Docker Engine is unavailable
- valid `daemon.json` summary extraction
- invalid `daemon.json` parse reporting
- managed settings validation preview while preserving unknown keys
- rejection when the current `daemon.json` is invalid or unreadable
- helper-backed apply flow with backup, restart, and rollback reporting
