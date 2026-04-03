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
    "state": "running"
  }
}
```

Rules:

- invalid stack ID returns `422 validation_failed`
- existing stack ID returns `409 conflict`

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

