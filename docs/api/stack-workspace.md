# Stack Workspace Contract Draft

This document defines the current contract for auxiliary stack files under:

```text
<managed-stacks-root>/<stack_id>
```

It is intentionally narrower than a generic per-stack file manager.

## Goals

- expose text-based helper files that live next to a stack definition
- cover real homelab cases such as:
  - `Dockerfile`
  - app-specific config under subdirectories
  - helper `.env` or template files that are not the canonical root `.env`
- keep `compose.yaml` and the canonical root `.env` in the dedicated stack editor
- preserve filesystem-first ownership and Git friendliness

## Non-Goals

- editing arbitrary host paths
- replacing the dedicated `compose.yaml` / root `.env` editor
- binary editing
- broad stack directory CRUD

## Workspace Boundary

Root:

```text
<managed-stacks-root>/<stack_id>
```

Typical values:

- package-managed install: `/srv/stacklab/stacks/<stack_id>`
- tarball install: `/opt/stacklab/stacks/<stack_id>`

Rules:

- all API paths are relative to this stack root
- path traversal outside the root must be rejected
- symlinks that resolve outside the root must be rejected
- canonical root files are reserved:
  - `compose.yaml`
  - `.env`

Reserved root files stay on:

- `GET /api/stacks/{stackId}/definition`
- `PUT /api/stacks/{stackId}/definition`

## Entry Types

- `directory`
- `text_file`
- `binary_file`
- `unknown_file`

## REST Endpoints

## `GET /api/stacks/{stackId}/workspace/tree`

Browse one directory level of the stack workspace.

Notes:

- root-level `compose.yaml` and `.env` may be returned so the UI can show them as disabled entries that redirect to the dedicated Editor tab
- sorting is deterministic:
  - directories first
  - then files
  - alphabetical within each group

Response:

```json
{
  "stack_id": "jellyfin",
  "workspace_root": "/srv/stacklab/stacks/jellyfin",
  "current_path": "",
  "parent_path": null,
  "items": [
    {
      "name": "config",
      "path": "config",
      "type": "directory",
      "size_bytes": 0,
      "modified_at": "2026-04-09T10:00:00Z",
      "permissions": {
        "owner_uid": 1000,
        "owner_name": "stacklab",
        "group_gid": 1000,
        "group_name": "stacklab",
        "mode": "0755",
        "readable": true,
        "writable": true
      }
    },
    {
      "name": "Dockerfile",
      "path": "Dockerfile",
      "type": "text_file",
      "size_bytes": 184,
      "modified_at": "2026-04-09T10:00:00Z",
      "permissions": {
        "owner_uid": 1000,
        "owner_name": "stacklab",
        "group_gid": 1000,
        "group_name": "stacklab",
        "mode": "0644",
        "readable": true,
        "writable": true
      }
    }
  ]
}
```

## `GET /api/stacks/{stackId}/workspace/file`

Fetch one auxiliary file and its metadata.

Reserved root files:

- `compose.yaml`
- `.env`

must return:

- `409 invalid_state`

Response for text file:

```json
{
  "stack_id": "jellyfin",
  "path": "Dockerfile",
  "name": "Dockerfile",
  "type": "text_file",
  "content": "FROM jellyfin/jellyfin:latest\n",
  "encoding": "utf-8",
  "size_bytes": 184,
  "modified_at": "2026-04-09T10:00:00Z",
  "readable": true,
  "writable": true,
  "blocked_reason": null,
  "repair_capability": {
    "supported": false,
    "reason": "Workspace permission repair is not configured yet.",
    "recursive": true
  },
  "permissions": {
    "owner_uid": 1000,
    "owner_name": "stacklab",
    "group_gid": 1000,
    "group_name": "stacklab",
    "mode": "0644",
    "readable": true,
    "writable": true
  }
}
```

Response for blocked file:

```json
{
  "stack_id": "jellyfin",
  "path": "config/secret.key",
  "name": "secret.key",
  "type": "unknown_file",
  "content": null,
  "encoding": null,
  "size_bytes": 64,
  "modified_at": "2026-04-09T10:00:00Z",
  "readable": false,
  "writable": false,
  "blocked_reason": "not_readable",
  "repair_capability": {
    "supported": false,
    "reason": "Workspace permission repair is not configured yet.",
    "recursive": true
  },
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

## `PUT /api/stacks/{stackId}/workspace/file`

Save an auxiliary text file under the stack root.

Request:

```json
{
  "path": "config/custom.conf",
  "content": "port=8080\n",
  "create_parent_directories": false
}
```

Response:

```json
{
  "saved": true,
  "stack_id": "jellyfin",
  "path": "config/custom.conf",
  "modified_at": "2026-04-09T10:05:00Z",
  "audit_action": "save_stack_file"
}
```

## `POST /api/stacks/{stackId}/workspace/repair-permissions`

Repair ownership and owner access bits for one existing path under the stack root.

Request:

```json
{
  "path": "Dockerfile",
  "recursive": false
}
```

Response:

```json
{
  "repaired": true,
  "stack_id": "jellyfin",
  "path": "Dockerfile",
  "recursive": false,
  "changed_items": 1,
  "warnings": [],
  "target_permissions_before": {
    "owner_uid": 0,
    "owner_name": "root",
    "group_gid": 0,
    "group_name": "root",
    "mode": "0400",
    "readable": true,
    "writable": false
  },
  "target_permissions_after": {
    "owner_uid": 1000,
    "owner_name": "stacklab",
    "group_gid": 1000,
    "group_name": "stacklab",
    "mode": "0600",
    "readable": true,
    "writable": true
  },
  "audit_action": "repair_stack_workspace_permissions",
  "repair_capability": {
    "supported": true,
    "recursive": true
  }
}
```

Rules:

- this is restricted to existing paths under the managed stack workspace
- unlike `workspace/file`, reserved root paths may still be repaired
- path traversal is rejected
- helper-backed repair is opt-in and returns `501 not_implemented` until configured

## Error Semantics

- `400 path_outside_workspace`
- `400 path_not_directory`
- `400 path_not_file`
- `404 not_found`
- `409 invalid_state`
  - reserved canonical root files
- `409 binary_not_editable`
- `409 permission_denied`
- `501 not_implemented`
