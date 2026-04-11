# Git Workspace Contract Draft

This document defines the current contract for Git visibility and selective write flows inside the local Stacklab workspace.

It is intentionally narrower than full GitOps or a generic Git client.

## Goals

- expose local Git state for the managed `stacks/` and `config/` subtrees
- make UI edits auditable in a Git-aware workflow
- support selective per-file commits and later push without leaving Stacklab
- preserve enough metadata for stack-scoped quick selection in UI later

## Non-Goals

- clone / pull / fetch / merge / rebase
- branch switching
- conflict resolution UI
- generic Git browser for arbitrary host repositories

## Workspace Boundary

Git visibility is limited to the Stacklab workspace root:

```text
<managed-stacklab-root>
```

Typical values:

- package-managed install: `/srv/stacklab`
- tarball install: `/opt/stacklab`

Only these managed subtrees are exposed:

- `stacks/`
- `config/`

Files outside those roots must not appear in the API, even if the repository contains them.

## Repository Assumptions

The backend should treat Git as optional.

Meaning:

- if the Stacklab workspace is a Git repository, return read-only Git metadata
- if not, return a healthy `available: false` response, not a hard error

This allows Stacklab to run both:

- with Git-backed operator workflows
- without Git at all

## Core Model

## Managed Scopes

Each changed file belongs to one of:

- `stacks`
- `config`

## File Status

Suggested normalized statuses:

- `modified`
- `added`
- `deleted`
- `renamed`
- `untracked`
- `conflicted`

Notes:

- backend may derive these from Git porcelain status
- UI should not need to parse raw `XY` Git codes

## Stack Context

When the first path segment under the managed root matches a stack ID, backend should include:

- `stack_id`

Examples:

- `config/demo/app.conf` → `stack_id: "demo"`
- `stacks/demo/compose.yaml` → `stack_id: "demo"`
- `config/shared/traefik.yml` → `stack_id: null`

## REST Endpoints

## `GET /api/git/workspace/status`

Purpose:

- fetch current Git status for the managed Stacklab workspace

Response when Git workspace is available:

```json
{
  "available": true,
  "repo_root": "/srv/stacklab",
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
    },
    {
      "path": "stacks/demo/compose.yaml",
      "scope": "stacks",
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
    }
  ]
}
```

Response when Git is not available for the workspace:

```json
{
  "available": false,
  "repo_root": "/srv/stacklab",
  "managed_roots": ["stacks", "config"],
  "reason": "not_a_git_repository"
}
```

Notes:

- `items` should include only changed files under managed roots
- unchanged files are excluded
- missing Git repositories must return a degraded `available: false` response, not `500`
- each item should expose:
  - `permissions`
  - `diff_available`
  - `commit_allowed`
  - `blocked_reason`
- sort should be deterministic:
  - by `scope`
  - then by `stack_id` / `path`

## `GET /api/git/workspace/diff`

Purpose:

- fetch unified diff for one changed file

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
  "diff": "@@ -1,2 +1,2 @@\n-server_name old.local;\n+server_name demo.local;\n",
  "truncated": false
}
```

For binary files:

```json
{
  "available": true,
  "path": "config/demo/certificate.p12",
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

Notes:

- untracked files should diff against an empty file
- deleted files should still return metadata and diff text when practical
- binary files should not return inline content
- unreadable files should return metadata with `diff_available: false`
- large diffs may be truncated, but truncation must be explicit

## Error Handling

Suggested error codes:

- `not_found`
- `validation_failed`
- `path_outside_workspace`
- `git_unavailable`
- `internal_error`

Examples:

- asking for `path=../etc/passwd` → `400 path_outside_workspace`
- asking for a path outside `stacks/` or `config/` → `400 validation_failed`
- asking for a path with no current change entry → `404 not_found`
- trying to commit an unreadable changed file → `409 permission_denied`

## UI Expectations

Expected first UI surface:

- Git visibility lives inside `/config`, not a separate `/git` route
- tree panel gains a mode toggle:
  - `Files`
  - `Changes`
- `Changes` mode shows only changed files
- clicking a changed file opens unified diff in the right panel

Recommended grouping:

- by `stack_id`
- with an `Other` group for paths outside stack-specific directories

## `POST /api/git/workspace/commit`

Purpose:

- create one local Git commit from a selected set of changed managed files

Request body:

```json
{
  "message": "Update demo stack settings",
  "paths": [
    "config/demo/app.conf",
    "stacks/demo/compose.yaml"
  ]
}
```

Notes:

- `paths` is the primary write model
- UI may offer quick-select per stack, but backend still receives explicit file paths
- each selected path must currently be changed under `stacks/` or `config/`
- conflicted files must be rejected

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
  "remaining_changes": 3
}
```

Suggested error codes:

- `validation_failed`
- `path_outside_workspace`
- `not_found`
- `conflicted_files_selected`
- `permission_denied`
- `nothing_to_commit`
- `git_unavailable`

## `POST /api/git/workspace/push`

Purpose:

- push current branch `HEAD` to its configured upstream

Request body:

- none

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

Notes:

- the current version only supports push to the current upstream
- no branch switching, force push, pull, merge, or rebase
- if branch has no upstream, backend should return a clear non-500 error
- if there is nothing ahead to push, backend may return `pushed: false`

Suggested error codes:

- `git_unavailable`
- `upstream_not_configured`
- `git_auth_failed`
- `push_rejected`

## UI Expectations For Commit And Push

- commit/push live on top of the existing `/config` `Changes` surface
- primary selection stays per-file
- stack quick-select is a convenience action that expands to file paths
- push should be available only when workspace has an upstream and local commits ahead of remote
