# Config Workspace Contract Draft

This document defines the current contract for the managed config workspace:

- safe browsing of the managed config root
- text editing of supported config files
- read-only fallback for non-text files

It is intentionally narrower than a generic file manager.

## Goals

- make the managed config root a first-class workspace inside Stacklab
- support common homelab config editing without leaving the product
- keep the filesystem boundary explicit and safe
- prepare the ground for later Git-aware workflows

## Non-Goals

- arbitrary host filesystem browsing
- binary editing
- full IDE-like file management
- broad host file CRUD outside the Stacklab workspace

## Workspace Boundary

Root:

```text
<managed-config-root>
```

Typical values:

- package-managed install: `/srv/stacklab/config`
- tarball install: `/opt/stacklab/config`

Rules:

- all API paths are relative to this root
- path traversal outside the root must be rejected
- symlinks that resolve outside the root must be rejected
- hidden files are allowed only if they still resolve under the root

## Core Model

## Entry Types

Each workspace entry is either:

- `directory`
- `text_file`
- `binary_file`
- `unknown_file`

The type is determined by backend inspection, not by file extension alone.

## Relative Paths

Paths in the API are workspace-relative.

Examples:

- `""` means the workspace root
- `nextcloud`
- `nextcloud/nginx.conf`
- `traefik/dynamic/routers.yml`

Paths always use forward slashes in the API.

## REST Endpoints

## `GET /api/config/workspace/tree`

Purpose:

- browse one directory level of the config workspace

Query parameters:

- `path` optional relative directory path, default root

Response:

```json
{
  "workspace_root": "/srv/stacklab/config",
  "current_path": "nextcloud",
  "parent_path": "",
  "items": [
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
    },
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
    }
  ]
}
```

Notes:

- the response is directory-scoped, not a full recursive tree
- `stack_id` is derived from the first path segment when it matches a stack directory convention
- each item includes `permissions` for the current file or directory inode
- sorting should be deterministic:
  - directories first
  - then files
  - alphabetical within each group

## `GET /api/config/workspace/file`

Purpose:

- fetch file content and metadata for one workspace file

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

- text files include content
- binary or unsupported files return metadata only
- unreadable files return metadata only with `blocked_reason`
- each file response includes `repair_capability`
- the first version does not need syntax-aware validation beyond text/binary detection

Example `repair_capability`:

```json
{
  "supported": false,
  "reason": "Workspace permission repair is not configured yet.",
  "recursive": true
}
```

## `PUT /api/config/workspace/file`

Purpose:

- save content for an existing or new text file under the workspace

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

- text files may be overwritten
- new files may be created
- parent directory creation is optional and explicit
- binary files must not be overwritten through this endpoint unless they are intentionally treated as text by backend policy

## `POST /api/config/workspace/repair-permissions`

Purpose:

- repair ownership and owner access bits for one existing path under the managed config workspace

Request:

```json
{
  "path": "nextcloud/secret.env",
  "recursive": false
}
```

Response:

```json
{
  "repaired": true,
  "path": "nextcloud/secret.env",
  "recursive": false,
  "changed_items": 1,
  "warnings": [],
  "target_permissions_before": {
    "owner_uid": 0,
    "owner_name": "root",
    "group_gid": 0,
    "group_name": "root",
    "mode": "0600",
    "readable": false,
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
  "audit_action": "repair_config_workspace_permissions",
  "repair_capability": {
    "supported": true,
    "recursive": true
  }
}
```

Rules:

- this is restricted to existing paths under the managed config workspace
- path traversal is rejected
- the first slice is helper-backed and opt-in
- without a configured privileged helper this endpoint returns `501 not_implemented`
- the helper must stay limited to managed Stacklab roots, not arbitrary host paths

## `POST /api/config/workspace/directory`

Status:

- optional for the first implementation

Purpose:

- create a new directory under the config workspace

This can be deferred if UI starts with editing existing files and creating new files only in existing directories.

## `DELETE /api/config/workspace/file`

Status:

- deferred

Reason:

- browsing and editing are higher-value than deletion for the first slice
- destructive config deletion should come with stronger UX review

## Error Handling

Suggested error codes:

- `validation_failed`
- `not_found`
- `conflict`
- `binary_not_editable`
- `permission_denied`
- `path_outside_workspace`
- `path_not_directory`
- `path_not_file`
- `internal_error`
- `not_implemented`

Examples:

- trying to read `../etc/passwd` → `400 path_outside_workspace`
- trying to open a directory through file endpoint → `400 path_not_file`
- trying to save a binary file through text editor → `409 binary_not_editable`
- trying to save an unreadable or unwritable file → `409 permission_denied`
- trying to repair permissions without a configured helper → `501 not_implemented`

## Audit Expectations

Mutating file saves should write audit entries.

Suggested action names:

- `save_config_file`
- `repair_config_workspace_permissions`
- later: `create_config_directory`, `delete_config_file`

Suggested audit details:

- relative path
- stack_id when derivable
- file type

## UI Expectations

Expected first UI surface:

- dedicated top-level route or global workspace route
- tree on the left
- file details/editor on the right
- read-only preview for non-text files

The contract intentionally supports both:

- a focused stack-config view later
- a broader config workspace view now

## Suggested Backend Rules

- maximum file size for text editing should be capped defensively
- text/binary detection should be content-aware, not extension-only
- save operations should use atomic write semantics where practical

## Tests

Recommended initial tests:

- browse root and nested directories
- reject path traversal
- read text file
- read binary file metadata without content
- save existing text file
- create new text file in existing directory
- reject binary file writes through text endpoint

Recommended later tests:

- symlink escape rejection
- directory creation
- Git-aware integration with the managed workspace change flow
