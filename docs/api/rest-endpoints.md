# REST Endpoints

## Purpose

This document defines the HTTP REST contract for Stacklab v1.

It is intentionally focused on:

- stable resource shapes for the UI
- clear separation between read models and mutating jobs
- compatibility with the domain model defined in `docs/domain/`

## API Conventions

### Base Path

All endpoints are served under:

```text
/api
```

### Authentication

Authenticated requests use a session cookie.

Rules:

- session cookie is HTTP-only
- unauthenticated requests return `401 Unauthorized`
- authenticated but forbidden operations return `403 Forbidden`

### Content Type

- requests: `application/json` unless otherwise stated
- responses: `application/json`

### Time Format

All timestamps use ISO 8601 in UTC.

Example:

```text
2026-04-03T18:42:00Z
```

### Error Envelope

Errors use a consistent envelope:

```json
{
  "error": {
    "code": "stack_locked",
    "message": "A mutating job is already running for this stack.",
    "details": {
      "stack_id": "nextcloud",
      "job_id": "job_01hr..."
    }
  }
}
```

Suggested common error codes:

- `unauthorized`
- `forbidden`
- `not_found`
- `validation_failed`
- `stack_locked`
- `invalid_state`
- `conflict`
- `docker_unavailable`
- `internal_error`

## Read Resources

## `GET /api/health`

Purpose:

- basic liveness probe for frontend bootstrap and operations checks

Response:

```json
{
  "status": "ok",
  "version": "0.1.0"
}
```

## `GET /api/session`

Purpose:

- determine whether the current browser session is authenticated
- bootstrap minimal session-aware UI state

Response:

```json
{
  "authenticated": true,
  "user": {
    "id": "local",
    "display_name": "Local Operator"
  },
  "features": {
    "host_shell": false
  }
}
```

## `GET /api/meta`

Purpose:

- fetch application-wide metadata needed by the UI

Response:

```json
{
  "app": {
    "name": "Stacklab",
    "version": "0.1.0"
  },
  "environment": {
    "stack_root": "/opt/stacklab",
    "platform": "linux-amd64"
  },
  "docker": {
    "engine_version": "27.0.1",
    "compose_version": "2.29.0"
  },
  "features": {
    "host_shell": false
  }
}
```

## `GET /api/host/overview`

Purpose:

- fetch host-level runtime metadata for the dedicated host page

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

## `GET /api/host/stacklab-logs`

Purpose:

- fetch recent Stacklab service logs from `journalctl -u stacklab`

Query parameters:

- `limit` optional, default `200`, max `1000`
- `cursor` optional opaque follow cursor
- `level` optional: `debug`, `info`, `warn`, `error`
- `q` optional text filter

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

## `GET /api/docker/admin/overview`

Purpose:

- fetch Docker daemon administration metadata for the dedicated Docker page

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

- returns `200` even in degraded environments
- unsupported `systemd` or unavailable Docker Engine should be represented in the payload, not as a generic hard failure

## `GET /api/docker/admin/daemon-config`

Purpose:

- fetch the current Docker `daemon.json` file in a read-only browser-safe shape

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

Notes:

- if `daemon.json` is missing, this still returns `200` with `exists = false`
- if the file contains invalid JSON, the response should include:
  - `valid_json = false`
  - `parse_error`
  - raw `content`
- `write_capability` describes the planned privileged apply path and the currently managed keys

## `POST /api/docker/admin/daemon-config/validate`

Purpose:

- validate managed Docker daemon setting changes and return a merged preview without writing the file

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

- this is a managed settings preview, not a raw `daemon.json` editor
- unknown existing keys are preserved in the preview
- unsupported `remove_keys` values are rejected with `400 validation_failed`
- invalid current JSON returns `409 invalid_state`
- unreadable current config returns `409 permission_denied`

## `POST /api/docker/admin/daemon-config/apply`

Purpose:

- start the global Docker daemon apply workflow using the same managed settings payload as validate

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

- this is a global job because restarting Docker affects the whole host
- Stacklab locks managed stacks during the apply workflow
- if the helper is not configured, this returns `501 not_implemented`
- helper-backed failures may emit warnings about rollback attempts into the job event stream

## `GET /api/config/workspace/tree`

Purpose:

- browse one directory level of the Stacklab config workspace

Query parameters:

- `path` optional relative directory path, default root

Response:

```json
{
  "workspace_root": "/opt/stacklab/config",
  "current_path": "nextcloud",
  "parent_path": "",
  "items": [
    {
      "name": "dynamic",
      "path": "nextcloud/dynamic",
      "type": "directory",
      "size_bytes": 0,
      "modified_at": "2026-04-04T11:55:00Z",
      "stack_id": "nextcloud",
      "permissions": {
        "owner_uid": 1000,
        "owner_name": "bob",
        "group_gid": 1000,
        "group_name": "bob",
        "mode": "0755",
        "readable": true,
        "writable": true
      }
    },
    {
      "name": "nginx.conf",
      "path": "nextcloud/nginx.conf",
      "type": "text_file",
      "size_bytes": 1782,
      "modified_at": "2026-04-04T12:00:00Z",
      "stack_id": "nextcloud",
      "permissions": {
        "owner_uid": 1000,
        "owner_name": "bob",
        "group_gid": 1000,
        "group_name": "bob",
        "mode": "0644",
        "readable": true,
        "writable": true
      }
    }
  ]
}
```

Notes:

- workspace is limited to `/opt/stacklab/config`
- sorting is deterministic:
  - directories first
  - then files
  - alphabetical within each group
- each tree item includes `permissions` with current owner/group/mode plus effective `readable` / `writable`

## `GET /api/config/workspace/file`

Purpose:

- fetch file metadata and content for one config workspace file

Query parameters:

- `path` required relative file path

Response for text file:

```json
{
  "path": "nextcloud/nginx.conf",
  "name": "nginx.conf",
  "type": "text_file",
  "stack_id": "nextcloud",
  "content": "server {\\n  listen 80;\\n}\\n",
  "encoding": "utf-8",
  "size_bytes": 1782,
  "modified_at": "2026-04-04T12:00:00Z",
  "readable": true,
  "writable": true,
  "blocked_reason": null,
  "permissions": {
    "owner_uid": 1000,
    "owner_name": "bob",
    "group_gid": 1000,
    "group_name": "bob",
    "mode": "0644",
    "readable": true,
    "writable": true
  }
}
```

Response for non-text file:

```json
{
  "path": "nextcloud/certificate.p12",
  "name": "certificate.p12",
  "type": "binary_file",
  "stack_id": "nextcloud",
  "content": null,
  "encoding": null,
  "size_bytes": 4096,
  "modified_at": "2026-04-04T12:00:00Z",
  "readable": true,
  "writable": false,
  "blocked_reason": null,
  "permissions": {
    "owner_uid": 1000,
    "owner_name": "bob",
    "group_gid": 1000,
    "group_name": "bob",
    "mode": "0644",
    "readable": true,
    "writable": false
  }
}
```

Response for blocked file:

```json
{
  "path": "nextcloud/secret.env",
  "name": "secret.env",
  "type": "unknown_file",
  "stack_id": "nextcloud",
  "content": null,
  "encoding": null,
  "size_bytes": 128,
  "modified_at": "2026-04-04T12:00:00Z",
  "readable": false,
  "writable": false,
  "blocked_reason": "not_readable",
  "permissions": {
    "owner_uid": 0,
    "owner_name": "root",
    "group_gid": 0,
    "group_name": "root",
    "mode": "0600",
    "readable": false,
    "writable": false
  }
}
```

Notes:

- `permissions` describe the current file inode, not assumed ACL inheritance
- unreadable files return metadata only and set `blocked_reason`
- blocked reasons currently include:
  - `not_readable`
  - `not_writable`

## `PUT /api/config/workspace/file`

Purpose:

- save content for an existing or new text file under the config workspace

Request:

```json
{
  "path": "nextcloud/nginx.conf",
  "content": "server {\\n  listen 8080;\\n}\\n",
  "create_parent_directories": false
}
```

Response:

```json
{
  "saved": true,
  "path": "nextcloud/nginx.conf",
  "modified_at": "2026-04-04T12:05:00Z",
  "audit_action": "save_config_file"
}
```

Rules:

- all paths are relative to `/opt/stacklab/config`
- path traversal outside the workspace is rejected
- binary files are not editable through this endpoint
- successful saves create audit entries with action `save_config_file`

## `GET /api/git/workspace/status`

Purpose:

- fetch current Git status for the managed Stacklab workspace

Response when Git workspace is available:

```json
{
  "available": true,
  "repo_root": "/opt/stacklab",
  "managed_roots": ["stacks", "config"],
  "branch": "main",
  "head_commit": "abc1234def5678",
  "has_upstream": true,
  "upstream_name": "origin/main",
  "ahead_count": 1,
  "behind_count": 0,
  "clean": false,
  "items": [
    {
      "path": "config/demo/app.conf",
      "scope": "config",
      "stack_id": "demo",
      "status": "modified",
      "old_path": null,
      "permissions": {
        "owner_uid": 1000,
        "owner_name": "bob",
        "group_gid": 1000,
        "group_name": "bob",
        "mode": "0644",
        "readable": true,
        "writable": true
      },
      "diff_available": true,
      "commit_allowed": true,
      "blocked_reason": null
    },
    {
      "path": "config/demo/new.env",
      "scope": "config",
      "stack_id": "demo",
      "status": "untracked",
      "old_path": null,
      "permissions": {
        "owner_uid": 1000,
        "owner_name": "bob",
        "group_gid": 1000,
        "group_name": "bob",
        "mode": "0644",
        "readable": true,
        "writable": true
      },
      "diff_available": true,
      "commit_allowed": true,
      "blocked_reason": null
    }
  ]
}
```

Response when Git is not available for the workspace:

```json
{
  "available": false,
  "repo_root": "/opt/stacklab",
  "managed_roots": ["stacks", "config"],
  "reason": "not_a_git_repository"
}
```

Notes:

- only changes under `stacks/` and `config/` are included
- `clean` is scoped to those managed roots, not the whole repository
- this endpoint is read-only and safe to poll on navigation
- each changed item includes:
  - `permissions`
  - `diff_available`
  - `commit_allowed`
  - `blocked_reason`

## `GET /api/git/workspace/diff`

Purpose:

- fetch unified diff for one changed managed file

Query parameters:

- `path` required managed path such as `config/demo/app.conf`

Response:

```json
{
  "available": true,
  "path": "config/demo/app.conf",
  "scope": "config",
  "stack_id": "demo",
  "status": "modified",
  "old_path": null,
  "permissions": {
    "owner_uid": 1000,
    "owner_name": "bob",
    "group_gid": 1000,
    "group_name": "bob",
    "mode": "0644",
    "readable": true,
    "writable": true
  },
  "diff_available": true,
  "blocked_reason": null,
  "is_binary": false,
  "diff": "diff --git a/config/demo/app.conf b/config/demo/app.conf\n@@ -1 +1 @@\n-server_name old.local;\n+server_name demo.local;\n",
  "truncated": false
}
```

Binary response:

```json
{
  "available": true,
  "path": "config/demo/blob.bin",
  "scope": "config",
  "stack_id": "demo",
  "status": "untracked",
  "old_path": null,
  "permissions": {
    "owner_uid": 1000,
    "owner_name": "bob",
    "group_gid": 1000,
    "group_name": "bob",
    "mode": "0644",
    "readable": true,
    "writable": true
  },
  "diff_available": false,
  "blocked_reason": null,
  "is_binary": true,
  "diff": null,
  "truncated": false
}
```

Blocked response:

```json
{
  "available": true,
  "path": "config/demo/secret.env",
  "scope": "config",
  "stack_id": "demo",
  "status": "modified",
  "old_path": null,
  "permissions": {
    "owner_uid": 0,
    "owner_name": "root",
    "group_gid": 0,
    "group_name": "root",
    "mode": "0600",
    "readable": false,
    "writable": false
  },
  "diff_available": false,
  "blocked_reason": "not_readable",
  "is_binary": false,
  "diff": null,
  "truncated": false
}
```

Rules:

- only files under `stacks/` and `config/` are allowed
- path traversal is rejected
- if the workspace is not a Git repository, this endpoint returns `git_unavailable`
- untracked files diff against an empty file
- large diffs may be truncated, but truncation is explicit

## `POST /api/git/workspace/commit`

Purpose:

- create one local Git commit from an explicit set of changed managed files

Request:

```json
{
  "message": "Update demo stack settings",
  "paths": [
    "config/demo/app.conf",
    "stacks/demo/compose.yaml"
  ]
}
```

Response:

```json
{
  "committed": true,
  "commit": "abc1234def5678",
  "summary": "Update demo stack settings",
  "paths": [
    "config/demo/app.conf",
    "stacks/demo/compose.yaml"
  ],
  "remaining_changes": 2
}
```

Rules:

- selection is always explicit per-file
- stack-level quick selection is a UI convenience only
- selected files must currently be changed under `stacks/` or `config/`
- conflicted files are rejected
- unreadable selected files are rejected with `409 permission_denied`

## `POST /api/git/workspace/push`

Purpose:

- push current branch `HEAD` to its configured upstream

Response:

```json
{
  "pushed": true,
  "remote": "origin",
  "branch": "main",
  "upstream_name": "origin/main",
  "head_commit": "abc1234def5678",
  "ahead_count": 0,
  "behind_count": 0
}
```

Rules:

- this milestone supports only push to the current upstream
- no pull, merge, rebase, branch switching, or force push
- when there is nothing ahead to push, backend may return `pushed: false`

## `POST /api/maintenance/update-stacks`

Purpose:

- replace ad-hoc host update scripts with one explicit browser-driven workflow
- update selected or all managed stacks in deterministic order

Request:

```json
{
  "target": {
    "mode": "selected",
    "stack_ids": ["demo", "traefik"]
  },
  "options": {
    "pull_images": true,
    "build_images": true,
    "remove_orphans": true,
    "prune_after": {
      "enabled": false,
      "include_volumes": false
    }
  }
}
```

Success response:

```json
{
  "job": {
    "id": "job_01hr...",
    "stack_id": null,
    "action": "update_stacks",
    "state": "running",
    "workflow": {
      "steps": [
        {
          "action": "pull",
          "state": "running",
          "target_stack_id": "demo"
        },
        {
          "action": "build",
          "state": "queued",
          "target_stack_id": "demo"
        },
        {
          "action": "up",
          "state": "queued",
          "target_stack_id": "demo"
        },
        {
          "action": "prune",
          "state": "queued"
        }
      ]
    }
  }
}
```

Rules:

- `target.mode`:
  - `selected`
  - `all`
- `target.stack_ids` must be non-empty when `mode = selected`
- update order is alphabetical by `stack_id`
- v1 is fail-fast: the first failed stack step stops the remaining workflow
- `prune_after.include_volumes = true` requires `prune_after.enabled = true`
- returns `409 stack_locked` if another mutating job already owns one of the selected stacks

## `GET /api/maintenance/images`

Purpose:

- list host images in a maintenance-oriented shape
- show which ones are currently mapped to managed stacks

Query parameters:

- `q` optional text filter
- `usage` optional: `all`, `used`, `unused`
- `origin` optional: `all`, `stack_managed`, `external`

Response:

```json
{
  "items": [
    {
      "id": "sha256:abc123...",
      "repository": "ghcr.io/example/app",
      "tag": "latest",
      "reference": "ghcr.io/example/app:latest",
      "size_bytes": 483183820,
      "created_at": "2026-04-04T12:11:00Z",
      "containers_using": 2,
      "stacks_using": [
        {
          "stack_id": "demo",
          "service_names": ["app"]
        }
      ],
      "is_dangling": false,
      "is_unused": false,
      "source": "stack_managed"
    }
  ]
}
```

Rules:

- `source = stack_managed` means Stacklab could map the image back to at least one managed stack
- `source = external` means the image exists on the host but is not mapped back to a managed stack
- `is_unused = true` means no container currently uses the image
- inventory is read-only in this slice

## `GET /api/maintenance/networks`

Purpose:

- list host Docker networks in a maintenance-oriented shape
- show which ones are currently used by managed stacks, including external networks

Query parameters:

- `q` optional text filter
- `usage` optional: `all`, `used`, `unused`
- `origin` optional: `all`, `stack_managed`, `external`

Response:

```json
{
  "items": [
    {
      "id": "network-demo",
      "name": "demo_default",
      "driver": "bridge",
      "scope": "local",
      "internal": false,
      "attachable": false,
      "ingress": false,
      "containers_using": 1,
      "stacks_using": [
        {
          "stack_id": "demo",
          "service_names": ["app"]
        }
      ],
      "is_unused": false,
      "source": "stack_managed"
    }
  ]
}
```

Rules:

- `source = stack_managed` means Stacklab could map the network back to at least one managed stack
- external networks used by managed stack containers still count as `stack_managed`
- `is_unused = true` means no container currently uses the network
- inventory is read-only in this slice

## `POST /api/maintenance/networks`

Purpose:

- create a plain external Docker network by name

Request:

```json
{
  "name": "homelab_proxy"
}
```

Response:

```json
{
  "created": true,
  "name": "homelab_proxy"
}
```

Rules:

- only name-based creation is supported in this slice
- advanced driver/options editing is out of scope

## `DELETE /api/maintenance/networks/{name}`

Purpose:

- remove an unused external Docker network deliberately

Response:

```json
{
  "deleted": true,
  "name": "old_shared_network"
}
```

Rules:

- built-in networks like `bridge`, `host`, `none`, and `ingress` are protected
- stack-managed networks are protected
- in-use networks are protected

## `GET /api/maintenance/volumes`

Purpose:

- list host Docker volumes in a maintenance-oriented shape
- show which ones are currently used by managed stacks, including external named volumes

Query parameters:

- `q` optional text filter
- `usage` optional: `all`, `used`, `unused`
- `origin` optional: `all`, `stack_managed`, `external`

Response:

```json
{
  "items": [
    {
      "name": "demo_data",
      "driver": "local",
      "mountpoint": "/var/lib/docker/volumes/demo_data/_data",
      "scope": "local",
      "options_count": 0,
      "containers_using": 1,
      "stacks_using": [
        {
          "stack_id": "demo",
          "service_names": ["app"]
        }
      ],
      "is_unused": false,
      "source": "stack_managed"
    }
  ]
}
```

Rules:

- `source = stack_managed` means Stacklab could map the volume back to at least one managed stack
- external named volumes used by managed stack containers still count as `stack_managed`
- `is_unused = true` means no container currently uses the volume
- inventory is read-only in this slice

## `POST /api/maintenance/volumes`

Purpose:

- create a plain external named Docker volume by name

Request:

```json
{
  "name": "media_cache"
}
```

Response:

```json
{
  "created": true,
  "name": "media_cache"
}
```

Rules:

- only name-based creation is supported in this slice
- driver/options editing is out of scope

## `DELETE /api/maintenance/volumes/{name}`

Purpose:

- remove an unused external Docker volume deliberately

Response:

```json
{
  "deleted": true,
  "name": "old_media_cache"
}
```

Rules:

- stack-managed volumes are protected
- in-use volumes are protected

## `GET /api/maintenance/prune-preview`

Purpose:

- preview likely cleanup impact before the operator starts prune

Query parameters:

- `images` optional boolean, default `true`
- `build_cache` optional boolean, default `true`
- `stopped_containers` optional boolean, default `true`
- `volumes` optional boolean, default `false`

Response:

```json
{
  "preview": {
    "images": {
      "count": 1,
      "reclaimable_bytes": 483183820,
      "items": [
        {
          "reference": "ghcr.io/example/old-web:1.0.0",
          "size_bytes": 483183820,
          "reason": "unused_image"
        }
      ]
    },
    "build_cache": {
      "count": 3,
      "reclaimable_bytes": 312475648
    },
    "stopped_containers": {
      "count": 2,
      "reclaimable_bytes": 0
    },
    "volumes": {
      "count": 0,
      "reclaimable_bytes": 0
    },
    "total_reclaimable_bytes": 795659468
  }
}
```

Rules:

- preview may be approximate for some Docker categories
- `volumes` remains opt-in and higher risk than the other cleanup scopes

## `POST /api/maintenance/prune`

Purpose:

- run a global cleanup workflow with explicit scope

Request:

```json
{
  "scope": {
    "images": true,
    "build_cache": true,
    "stopped_containers": true,
    "volumes": false
  }
}
```

Success response:

```json
{
  "job": {
    "id": "job_01hr...",
    "stack_id": null,
    "action": "prune",
    "state": "running",
    "workflow": {
      "steps": [
        {
          "action": "prune_images",
          "state": "running"
        },
        {
          "action": "prune_build_cache",
          "state": "queued"
        },
        {
          "action": "prune_stopped_containers",
          "state": "queued"
        }
      ]
    }
  }
}
```

Rules:

- at least one prune scope must be enabled
- prune is a global workspace-scoped job
- the job should reuse the existing progress stream model:
  - `job_step_started`
  - `job_log`
  - `job_step_finished`
- returns `409 conflict` if another global or stack maintenance workflow already holds the relevant locks

## `GET /api/stacks`

Purpose:

- fetch the stack list for the main dashboard

Query parameters:

- `q` optional search string
- `sort` optional: `name`, `state`, `last_action`

Response:

```json
{
  "items": [
    {
      "id": "nextcloud",
      "name": "nextcloud",
      "created_at": "2026-03-01T10:00:00Z",
      "updated_at": "2026-04-03T18:42:00Z",
      "display_state": "running",
      "runtime_state": "running",
      "config_state": "drifted",
      "activity_state": "idle",
      "health_summary": {
        "healthy_container_count": 2,
        "unhealthy_container_count": 0,
        "unknown_health_container_count": 0
      },
      "service_count": {
        "defined": 2,
        "running": 2
      },
      "last_action": {
        "action": "pull",
        "result": "succeeded",
        "finished_at": "2026-04-03T18:42:00Z"
      }
    }
  ],
  "summary": {
    "stack_count": 12,
    "running_count": 8,
    "stopped_count": 3,
    "error_count": 1,
    "defined_count": 0,
    "orphaned_count": 0,
    "container_count": {
      "running": 28,
      "total": 35
    }
  }
}
```

Notes:

- `display_state` is included for UI convenience even though it equals `runtime_state`
- the list response is intentionally compact compared to stack detail

## `GET /api/stacks/{stackId}`

Purpose:

- fetch full stack detail for overview screen and tab-level shared header state

Response:

```json
{
  "stack": {
    "id": "nextcloud",
    "name": "nextcloud",
    "created_at": "2026-03-01T10:00:00Z",
    "updated_at": "2026-04-03T18:42:00Z",
    "root_path": "/opt/stacklab/stacks/nextcloud",
    "compose_file_path": "/opt/stacklab/stacks/nextcloud/compose.yaml",
    "env_file_path": "/opt/stacklab/stacks/nextcloud/.env",
    "config_path": "/opt/stacklab/config/nextcloud",
    "data_path": "/opt/stacklab/data/nextcloud",
    "display_state": "running",
    "runtime_state": "running",
    "config_state": "drifted",
    "activity_state": "idle",
    "health_summary": {
      "healthy_container_count": 2,
      "unhealthy_container_count": 0,
      "unknown_health_container_count": 0
    },
    "capabilities": {
      "can_edit_definition": true,
      "can_view_logs": true,
      "can_view_stats": true,
      "can_open_terminal": true
    },
    "available_actions": [
      "up",
      "restart",
      "stop",
      "down",
      "pull",
      "build",
      "recreate",
      "save_definition",
      "remove_stack_definition"
    ],
    "services": [
      {
        "name": "app",
        "mode": "image",
        "image_ref": "nextcloud:29",
        "build_context": null,
        "dockerfile_path": null,
        "ports": [
          {
            "published": 8080,
            "target": 80,
            "protocol": "tcp"
          }
        ],
        "volumes": [
          {
            "source": "/opt/stacklab/config/nextcloud",
            "target": "/config"
          }
        ],
        "depends_on": ["db"],
        "healthcheck_present": true
      }
    ],
    "containers": [
      {
        "id": "2f4b...",
        "name": "nextcloud-app-1",
        "service_name": "app",
        "status": "running",
        "health_status": "healthy",
        "started_at": "2026-04-03T16:12:00Z",
        "image_id": "sha256:...",
        "image_ref": "nextcloud:29",
        "ports": [
          {
            "published": 8080,
            "target": 80,
            "protocol": "tcp"
          }
        ],
        "networks": ["nextcloud_default"]
      }
    ],
    "last_deployed_at": "2026-04-03T16:12:00Z",
    "last_action": {
      "action": "pull",
      "result": "succeeded",
      "finished_at": "2026-04-03T18:42:00Z"
    }
  }
}
```

Notes:

- this endpoint is the main source for stack header state and per-service rendering
- `capabilities` is included so the UI can disable tabs and actions without re-deriving every rule

## `GET /api/stacks/{stackId}/definition`

Purpose:

- fetch editable stack definition files for the editor screen

Response:

```json
{
  "stack_id": "nextcloud",
  "files": {
    "compose_yaml": {
      "path": "/opt/stacklab/stacks/nextcloud/compose.yaml",
      "content": "services:\n  app:\n    image: nextcloud:29\n"
    },
    "env": {
      "path": "/opt/stacklab/stacks/nextcloud/.env",
      "content": "PORT=8080\nDB_NAME=nextcloud\n",
      "exists": true
    }
  },
  "config_state": "drifted"
}
```

Rules:

- for `orphaned` stacks this endpoint returns `409 invalid_state`
- `.env` may not exist; UI should handle `exists = false`
- `env` is always present in the response shape
- when `.env` does not exist, `env.exists = false` and `env.content = ""`

## `GET /api/stacks/{stackId}/resolved-config`

Purpose:

- fetch the last successfully resolved config snapshot for display

Query parameters:

- `source=current` optional, default
- `source=last_valid` optional future extension

Success response:

```json
{
  "stack_id": "nextcloud",
  "valid": true,
  "content": "name: nextcloud\nservices:\n  app:\n    image: nextcloud:29\n"
}
```

Invalid response:

```json
{
  "stack_id": "nextcloud",
  "valid": false,
  "error": {
    "code": "validation_failed",
    "message": "services.app.volumes contains an invalid mount spec",
    "details": {
      "line": 14,
      "column": 9
    }
  }
}
```

## `POST /api/stacks/{stackId}/resolved-config`

Purpose:

- resolve a draft editor state without persisting it to disk

This is a read-only dry-run endpoint intended for editor preview workflows.

Request:

```json
{
  "compose_yaml": "services:\n  app:\n    image: nextcloud:29\n",
  "env": "PORT=8080\n"
}
```

Success response:

```json
{
  "stack_id": "nextcloud",
  "valid": true,
  "content": "name: nextcloud\nservices:\n  app:\n    image: nextcloud:29\n"
}
```

Invalid response shape matches `GET /api/stacks/{stackId}/resolved-config`.

Rules:

- does not write files
- does not create a job
- may be called repeatedly by the editor
- for `orphaned` stacks returns `409 invalid_state`

## `GET /api/stacks/{stackId}/audit`

Purpose:

- fetch per-stack audit entries

Query parameters:

- `cursor` optional pagination cursor
- `limit` optional, default `50`, max `100`

Response:

```json
{
  "items": [
    {
      "id": "audit_01hr...",
      "stack_id": "nextcloud",
      "action": "pull",
      "requested_by": "local",
      "result": "succeeded",
      "requested_at": "2026-04-03T18:40:00Z",
      "finished_at": "2026-04-03T18:42:00Z",
      "duration_ms": 12000
    }
  ],
  "next_cursor": null
}
```

## `GET /api/audit`

Purpose:

- fetch global audit entries

Query parameters:

- `stack_id` optional filter
- `cursor` optional pagination cursor
- `limit` optional, default `50`, max `100`

Response shape matches `GET /api/stacks/{stackId}/audit`.

## Mutating Resources

## `POST /api/auth/login`

Purpose:

- authenticate using the single-user password

Request:

```json
{
  "password": "secret"
}
```

Response:

```json
{
  "authenticated": true
}
```

Behavior:

- sets session cookie on success
- returns `401` on invalid password

## `POST /api/auth/logout`

Purpose:

- destroy current session

Response:

```json
{
  "authenticated": false
}
```

## `POST /api/stacks`

Purpose:

- create a new stack definition and canonical directories

Request:

```json
{
  "stack_id": "my-new-app",
  "compose_yaml": "services:\n  app:\n    image: nginx:alpine\n",
  "env": "",
  "create_config_dir": true,
  "create_data_dir": true,
  "deploy_after_create": false
}
```

Success response:

```json
{
  "job": {
    "id": "job_01hr...",
    "stack_id": "my-new-app",
    "action": "create_stack",
    "state": "running",
    "workflow": {
      "steps": [
        {
          "action": "create_stack",
          "state": "running"
        }
      ]
    }
  }
}
```

Rules:

- invalid stack ID returns `422 validation_failed`
- existing stack ID returns `409 conflict`
- when `deploy_after_create = true`, backend still returns a single job with top-level `action = create_stack`
- that job becomes a workflow job whose steps are `create_stack` followed by `up`
- the UI should present this as one progress flow, not as two unrelated jobs
- workflow steps may optionally carry `target_stack_id` for global jobs such as bulk maintenance

## `PUT /api/stacks/{stackId}/definition`

Purpose:

- save `compose.yaml` and optional `.env`

Request:

```json
{
  "compose_yaml": "services:\n  app:\n    image: nextcloud:29\n",
  "env": "PORT=8080\n",
  "validate_after_save": true
}
```

Success response:

```json
{
  "job": {
    "id": "job_01hr...",
    "stack_id": "nextcloud",
    "action": "save_definition",
    "state": "running"
  }
}
```

Rules:

- allowed to persist invalid definitions if requested content can be safely written
- validation results are reported via the job and reflected in `config_state`
- for `orphaned` stacks returns `409 invalid_state`

## `POST /api/stacks/{stackId}/actions/{action}`

Purpose:

- invoke a mutating stack action

Allowed `{action}` values in v1:

- `validate`
- `up`
- `down`
- `stop`
- `restart`
- `pull`
- `build`
- `recreate`

Request body:

```json
{}
```

Future action-specific payloads may be added, but the v1 default body is empty.

Success response:

```json
{
  "job": {
    "id": "job_01hr...",
    "stack_id": "nextcloud",
    "action": "pull",
    "state": "running"
  }
}
```

Behavior:

- returns `409 stack_locked` if another mutating job already owns the stack
- returns `409 invalid_state` if action is not currently allowed for that stack

## `DELETE /api/stacks/{stackId}`

Purpose:

- remove stack resources according to explicit flags

Request:

```json
{
  "remove_runtime": true,
  "remove_definition": false,
  "remove_config": false,
  "remove_data": false
}
```

Success response:

```json
{
  "job": {
    "id": "job_01hr...",
    "stack_id": "nextcloud",
    "action": "remove_stack_definition",
    "state": "running"
  }
}
```

Rules:

- `remove_runtime` is the default safe path and should be required explicitly by the UI
- `remove_data = true` should require a stronger confirmation UX on the client

## `POST /api/settings/password`

Purpose:

- change the single-user password

Request:

```json
{
  "current_password": "old",
  "new_password": "new"
}
```

Response:

```json
{
  "updated": true
}
```

## `GET /api/settings/notifications`

Purpose:

- fetch current outgoing webhook notification settings
- power the notifications section in `Settings`

Response:

```json
{
  "enabled": false,
  "configured": false,
  "webhook_url": "",
  "events": {
    "job_failed": true,
    "job_succeeded_with_warnings": true,
    "maintenance_succeeded": false
  }
}
```

Notes:

- v1 supports a single outgoing webhook only
- default policy is:
  - notify on failed jobs
  - notify on successful jobs with warnings
  - do not notify on successful maintenance by default

## `PUT /api/settings/notifications`

Purpose:

- persist outgoing webhook notification settings in SQLite

Request:

```json
{
  "enabled": true,
  "webhook_url": "https://hooks.example.test/stacklab",
  "events": {
    "job_failed": true,
    "job_succeeded_with_warnings": true,
    "maintenance_succeeded": true
  }
}
```

Response:

```json
{
  "enabled": true,
  "configured": true,
  "webhook_url": "https://hooks.example.test/stacklab",
  "events": {
    "job_failed": true,
    "job_succeeded_with_warnings": true,
    "maintenance_succeeded": true
  }
}
```

Validation:

- `webhook_url` must be an absolute `http` or `https` URL when provided
- `enabled = true` requires a non-empty `webhook_url`

## `POST /api/settings/notifications/test`

Purpose:

- deliver a test webhook using the current form payload
- let the operator validate URL and receiver formatting before enabling notifications

Request:

```json
{
  "enabled": false,
  "webhook_url": "https://hooks.example.test/stacklab",
  "events": {
    "job_failed": true,
    "job_succeeded_with_warnings": true,
    "maintenance_succeeded": false
  }
}
```

Response:

```json
{
  "sent": true
}
```

Error behavior:

- invalid URL or malformed payload -> `400 validation_failed`
- upstream delivery failure -> `502 delivery_failed`
- test send does not persist settings

Webhook payload:

```json
{
  "event": "test_notification",
  "sent_at": "2026-04-09T19:00:00Z",
  "source": "stacklab",
  "summary": "Stacklab test notification"
}
```

## `GET /api/jobs/{jobId}`

Purpose:

- fetch a single job snapshot when the UI needs to refresh or recover after navigation

Response:

```json
{
  "job": {
    "id": "job_01hr...",
    "stack_id": "nextcloud",
    "action": "pull",
    "state": "running",
    "requested_at": "2026-04-03T18:40:00Z",
    "started_at": "2026-04-03T18:40:01Z",
    "finished_at": null
  }
}
```

## `GET /api/jobs/{jobId}/events`

Purpose:

- fetch retained `job_events` for a single job
- power a dedicated job detail screen or replayable progress panel from audit/history links

Response:

```json
{
  "job_id": "job_01hr...",
  "retained": true,
  "items": [
    {
      "job_id": "job_01hr...",
      "sequence": 1,
      "event": "job_started",
      "state": "running",
      "message": "Job started.",
      "timestamp": "2026-04-03T18:40:01Z"
    },
    {
      "job_id": "job_01hr...",
      "sequence": 2,
      "event": "job_step_started",
      "state": "running",
      "message": "Starting pull for nextcloud.",
      "step": {
        "index": 1,
        "total": 2,
        "action": "pull",
        "target_stack_id": "nextcloud"
      },
      "timestamp": "2026-04-03T18:40:02Z"
    }
  ]
}
```

If detailed output was already purged, the endpoint still returns `200` with:

```json
{
  "job_id": "job_01hr...",
  "retained": false,
  "message": "Detailed output for this job is no longer retained.",
  "items": []
}
```

## `GET /api/jobs/active`

Purpose:

- power a global activity affordance for currently running background work
- let the UI recover cross-page job visibility without subscribing to each job individually

Definition of "active":

- `queued`
- `running`
- `cancel_requested`

Response:

```json
{
  "items": [
    {
      "id": "job_01hr...",
      "stack_id": null,
      "action": "update_stacks",
      "state": "running",
      "requested_at": "2026-04-09T10:15:00Z",
      "started_at": "2026-04-09T10:15:01Z",
      "workflow": {
        "steps": [
          { "action": "pull", "state": "running", "target_stack_id": "demo" },
          { "action": "up", "state": "queued", "target_stack_id": "demo" }
        ]
      },
      "current_step": {
        "index": 1,
        "total": 2,
        "action": "pull",
        "target_stack_id": "demo"
      },
      "latest_event": {
        "event": "job_step_started",
        "message": "Starting pull for demo.",
        "timestamp": "2026-04-09T10:15:02Z",
        "step": {
          "index": 1,
          "total": 2,
          "action": "pull",
          "target_stack_id": "demo"
        }
      }
    }
  ],
  "summary": {
    "active_count": 1,
    "running_count": 1,
    "queued_count": 0,
    "cancel_requested_count": 0
  }
}
```

Behavior:

- ordered by most recently active first
- `stack_id` may be `null` for workspace-level jobs
- `current_step` and `latest_event` are optional
- UI should derive elapsed time from `started_at` when present, otherwise `requested_at`

## `POST /api/jobs/{jobId}/cancel`

Purpose:

- request cancellation of a running job when supported

Success response:

```json
{
  "job": {
    "id": "job_01hr...",
    "state": "cancel_requested"
  }
}
```

Behavior:

- backend may return `409 invalid_state` if cancellation is not supported for the current action or job state

## Capability Rules Important To UI

### Orphaned Stacks

For `runtime_state = orphaned`:

- stack detail remains readable
- logs, stats, terminal, and history remain available
- definition editing is unavailable
- `available_actions` should typically include only actions that do not require a valid definition baseline

Recommended v1 behavior:

- allow `down`
- allow destructive cleanup via delete endpoint
- do not allow `up`, `restart`, `pull`, `build`, or `recreate`

### Locked Stacks

When `activity_state = locked`:

- mutating actions should return `409 stack_locked`
- read endpoints remain available
- streaming endpoints may remain available if safe

### Invalid Config

When `config_state = invalid`:

- `validate` remains allowed
- `save_definition` remains allowed
- deploy-oriented actions may be rejected with `409 invalid_state`
