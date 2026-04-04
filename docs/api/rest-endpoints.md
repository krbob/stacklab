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
      "stack_id": "nextcloud"
    },
    {
      "name": "nginx.conf",
      "path": "nextcloud/nginx.conf",
      "type": "text_file",
      "size_bytes": 1782,
      "modified_at": "2026-04-04T12:00:00Z",
      "stack_id": "nextcloud"
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
  "writable": true
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
  "writable": false
}
```

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
      "old_path": null
    },
    {
      "path": "config/demo/new.env",
      "scope": "config",
      "stack_id": "demo",
      "status": "untracked",
      "old_path": null
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
  "is_binary": true,
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
