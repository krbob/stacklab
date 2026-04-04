# Config Workspace Handoff

This handoff covers the UI work for Milestone 2:

- browsing `/opt/stacklab/config`
- editing supported text files
- preparing the workspace for later Git-aware features

Backend contract draft:

- `docs/api/config-workspace.md`

## Product Intent

This is not a generic file manager.

The UI should communicate that this is:

- the Stacklab config workspace
- limited to `/opt/stacklab/config`
- focused on common text-based config workflows

## Recommended Information Architecture

Two reasonable options exist:

### Option A: Dedicated Global Route

- `/config`

Pros:

- clean mental model
- obvious future place for Git workspace visibility
- works well with tree + editor layouts

Cons:

- adds another top-level sidebar item

### Option B: Config Workspace Nested Under Host Or Settings

Not recommended.

Reason:

- config editing is neither host diagnostics nor app settings
- it deserves its own operational surface

## Current Architectural Recommendation

Recommend a dedicated global route:

- `/config`

Sidebar order suggestion:

- `Stacks`
- `Host`
- `Config`
- `Audit`
- `Settings`

## Screen Shape

Recommended desktop layout:

```text
┌──────────────────────────────────────────────────────────────────────┐
│  Config Workspace                                                   │
├───────────────────────┬──────────────────────────────────────────────┤
│  tree                 │  file header                                │
│  nextcloud/           │  nextcloud/nginx.conf                       │
│    nginx.conf         │  [text_file] [Save] [Discard]               │
│    redis.conf         │──────────────────────────────────────────────│
│  traefik/             │                                              │
│    dynamic/           │  editor or read-only preview                │
│      routers.yml      │                                              │
│                       │                                              │
└───────────────────────┴──────────────────────────────────────────────┘
```

Recommended layout behavior:

- left pane: directory tree / file list
- right pane:
  - text editor for `text_file`
  - metadata + read-only notice for `binary_file` / `unknown_file`

Tablet fallback:

- collapsible tree drawer
- editor remains primary

## Required UI States

- root empty state
- directory empty state
- file loading state
- file save success state
- file save error state
- non-text file read-only state
- blocked file state with ownership/mode details
- path not found state
- path outside workspace should never be reachable through normal UI, but error screen still needed

Blocked-file semantics are defined in:

- `docs/ui/workspace-permissions-handoff.md`

## Questions For UI Developer

Please propose:

1. exact IA for `/config`
2. whether the tree is:
   - always visible on desktop
   - collapsible on tablet
3. whether stack-oriented grouping should be explicit in the UI when the first path segment matches a stack name
4. whether the first implementation should expose:
   - only existing files
   - or also “new file” affordances in existing directories

## Expected Backend/UI Sequence

1. architecture confirms route placement and interaction model
2. backend implements browse/read/write endpoints
3. UI implements tree + editor + non-text fallback
4. backend and UI add audit visibility for config saves
5. Milestone 3 later overlays Git status/diff on the same workspace
