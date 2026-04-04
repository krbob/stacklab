# Git Workspace Contract Draft

This document defines the proposed contract for Milestone 3:

- read-only Git visibility for the local Stacklab workspace
- changed/untracked file listing
- per-file diff against `HEAD`

It is intentionally narrower than full GitOps or a generic Git client.

## Goals

- expose local Git state for `/opt/stacklab/stacks` and `/opt/stacklab/config`
- make UI edits auditable in a Git-aware workflow
- prepare the ground for later `commit + push`
- preserve enough metadata for selective per-file commit flows later

## Non-Goals

- clone / pull / fetch / merge / rebase
- branch switching
- conflict resolution UI
- generic Git browser for arbitrary host repositories

## Workspace Boundary

Git visibility is limited to the Stacklab workspace root:

```text
/opt/stacklab
```

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
    },
    {
      "path": "stacks/demo/compose.yaml",
      "scope": "stacks",
      "stack_id": "demo",
      "status": "modified",
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

- `items` should include only changed files under managed roots
- unchanged files are excluded
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
  "is_binary": true,
  "diff": null,
  "truncated": false
}
```

Notes:

- untracked files should diff against an empty file
- deleted files should still return metadata and diff text when practical
- binary files should not return inline content
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

## Later Compatibility

This contract is intentionally compatible with Milestone 4:

- `commit + push`

Meaning:

- `status` response should already include enough metadata for file selection UX later
- the primary write model later should be per-file selection
- stack-aware grouping should support quick selection of one stack without forcing stack-only commits
- but Milestone 3 itself remains read-only
