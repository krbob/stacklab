# Workspace Permissions Handoff

This note describes the backend semantics for blocked files in:

- `/config`
- `/config` `Changes` mode

It exists to keep UI behavior aligned with real filesystem access instead of assumptions about inherited ACLs.

## Core Principle

Permission diagnostics are based on the current file inode and the effective access of the running Stacklab service user.

That means UI should not assume:

- a file is editable because its parent directory has permissive ACLs
- a file is still writable because an older file at the same path was writable before

Containers may replace files atomically and change owner/group/mode in the process.

## Config Workspace Semantics

`GET /api/config/workspace/tree`

- each item includes `permissions`
- `permissions.readable` and `permissions.writable` describe current effective access

`GET /api/config/workspace/file`

- always returns metadata for files inside the workspace boundary
- text content is present only when the file is readable text
- blocked files return:
  - `readable: false`
  - `writable: false`
  - `blocked_reason: "not_readable"`
  - `content: null`
- non-text files may still be readable but not editable:
  - `writable: false`
  - `blocked_reason: null`

`PUT /api/config/workspace/file`

- returns `409 permission_denied` when the target file or its parent directory is not writable for the Stacklab service user

## Git Workspace Semantics

`GET /api/git/workspace/status`

- each changed item includes:
  - `permissions`
  - `diff_available`
  - `commit_allowed`
  - `blocked_reason`
- unreadable files may still appear as changed items
- a blocked changed file typically has:
  - `diff_available: false`
  - `commit_allowed: false`
  - `blocked_reason: "not_readable"`

`GET /api/git/workspace/diff`

- blocked files return metadata with:
  - `diff_available: false`
  - `diff: null`
  - `blocked_reason: "not_readable"`

`POST /api/git/workspace/commit`

- returns `409 permission_denied` if any selected file is not commit-eligible because of permissions

## Recommended UI Behavior

For Files mode:

- show read-only blocked state when `blocked_reason != null`
- show owner, group, mode, and a short explanation
- do not render save affordances when `writable` is false

For Changes mode:

- show blocked changed files in the list normally
- disable commit selection for files with `commit_allowed: false`
- allow opening blocked diff entries, but render metadata/blocked state instead of diff text

Recommended operator messaging:

- “This file is currently not readable by the Stacklab service user.”
- “Parent ACLs are not enough if the container recreated the file with different ownership.”
- “Prefer aligning container UID:GID or PUID/PGID with the Stacklab workspace owner.”

## Deferred Work

Not in this slice:

- automatic permission repair
- generic host chmod/chown tools
- running Stacklab as `root`
