# Workspace Permissions Handoff

This note describes the backend semantics for blocked files and the new helper-backed repair slice in:

- `/config`
- `/stacks/:id/files`
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

`POST /api/config/workspace/repair-permissions`

- takes:
  - `path`
  - `recursive`
- returns:
  - `changed_items`
  - `target_permissions_before`
  - `target_permissions_after`
  - `audit_action`
  - `repair_capability`
- returns `501 not_implemented` when the privileged helper is not configured yet

`GET /api/stacks/{stackId}/workspace/file`

- mirrors the same blocked-file semantics as `/config`
- also includes `repair_capability`

`POST /api/stacks/{stackId}/workspace/repair-permissions`

- mirrors the config repair shape
- is scoped to one stack root

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
- keep the normal file header visible so operator still sees:
  - path
  - stack link when available
  - modified time
  - file type
- if `repair_capability.supported` is true, a later slice may offer explicit repair from this state
- if `repair_capability.supported` is false, do not imply Stacklab can fix the file automatically

For Changes mode:

- show blocked changed files in the list normally
- disable commit selection for files with `commit_allowed: false`
- allow opening blocked diff entries, but render metadata/blocked state instead of diff text
- keep status grouping and stack grouping unchanged; blocked files are not hidden
- do not silently auto-select blocked files in group-level quick select

## Concrete Repair Slice

The first repair UI slice should stay narrow.

### Files mode

When a selected file has `blocked_reason != null`:

- replace editor/read-only preview with a blocked-file card
- show:
  - filename
  - path
  - owner
  - group
  - mode
  - whether the service user can read or write
- show a short explanation based on `blocked_reason`
- hide `Save`
- keep `Discard` hidden as well because there is no editable draft
- when `repair_capability.supported` is true:
  - show one explicit repair affordance
  - default to repairing only the selected path
  - do not default to recursive repair unless the operator opts into it
- when `repair_capability.supported` is false:
  - show preview-only / diagnostics-only messaging

Recommended blocked card title:

- `File access blocked`

Recommended detail rows:

- `Owner`
- `Group`
- `Mode`
- `Readable by Stacklab`
- `Writable by Stacklab`

### Changes mode

For `commit_allowed: false`:

- render the row normally
- disable the row checkbox
- keep the diff row clickable
- show a small blocked indicator in the row subtitle or tooltip

For `diff_available: false`:

- right panel should render a blocked diff state, not an error
- show the same permission metadata as in Files mode
- `Open in editor` stays disabled for blocked config files
- `Open stack editor` remains available for stack-scoped files only when that action still makes sense

### Group-level selection behavior

When a group checkbox is used:

- select only items with `commit_allowed: true`
- do not fail the whole interaction because one file is blocked
- if useful, show lightweight helper text such as:
  - `2 files selected, 1 blocked`

## Copy Guidance

Preferred user-facing explanations:

- `This file is currently not readable by the Stacklab service user.`
- `This file cannot be included in a Git commit until permissions are fixed.`
- `The container may have recreated this file with different ownership or mode.`
- `Prefer aligning container UID:GID or PUID/PGID with the Stacklab workspace owner.`

Avoid:

- vague messages like `Permission error`
- implying Stacklab can repair this automatically when `repair_capability.supported` is false
- telling the user ACLs should have solved it

## Repair Copy Guidance

When repair is available:

- `Repair access for this file`
- `Repair access recursively`
- `This uses the configured Stacklab workspace helper and is limited to managed roots.`
- `The file will be reassigned to the workspace owner and owner access bits will be restored.`

Recommended operator messaging:

- “This file is currently not readable by the Stacklab service user.”
- “Parent ACLs are not enough if the container recreated the file with different ownership.”
- “Prefer aligning container UID:GID or PUID/PGID with the Stacklab workspace owner.”

## Deferred Work

Not in this slice:

- generic host chmod/chown tools
- running Stacklab as `root`
- repair actions in Git Changes mode
- generic recursive subtree repair from arbitrary workspace directories
