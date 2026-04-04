# Git Workspace Handoff

This handoff covers the UI work for Milestone 3:

- showing local Git changes for the Stacklab workspace
- diffing changed files
- integrating Git visibility into the existing `/config` workspace

Backend contract draft:

- `docs/api/git-workspace.md`

## Product Intent

This is not GitOps and not a general-purpose Git UI.

The product intent is narrower:

- edit files in Stacklab
- immediately see what changed locally
- later commit and push those changes to GitHub

The mental model is:

```text
Files -> Changes -> Commit -> Push
```

not:

```text
remote repo -> auto sync -> reconcile
```

## Current Architectural Recommendation

Git visibility should live inside `/config`, not as a separate top-level route.

Reason:

- the operator is already editing files in `/config`
- changes are about the same local workspace
- a dedicated `/git` page would create unnecessary context switching
- this also prepares a later commit flow where selection starts from changed files, not from a separate Git page

Recommended sidebar remains:

- `Stacks`
- `Host`
- `Config`
- `Audit`
- `Settings`

## Recommended Information Architecture

Inside `/config`, add a mode toggle in the left panel:

- `Files`
- `Changes`

Meaning:

- `Files` mode behaves like today's config workspace browser
- `Changes` mode shows only changed/untracked files from the Git workspace

## Recommended Screen Shape

Desktop recommendation:

```text
┌──────────────────────────────────────────────────────────────────────┐
│  Config Workspace                                                   │
├───────────────────────┬──────────────────────────────────────────────┤
│ [Files] [Changes]     │  file header / diff header                  │
│                       │                                              │
│ demo                  │  config/demo/app.conf                       │
│   M app.conf          │  [modified] [demo]                          │
│   A new.env           │──────────────────────────────────────────────│
│                       │                                              │
│ Other                 │  unified diff                               │
│   U config/global.yml │                                              │
│                       │                                              │
└───────────────────────┴──────────────────────────────────────────────┘
```

Recommended behavior:

- `Files` mode:
  - current tree + editor
- `Changes` mode:
  - grouped changed-files list
  - right panel shows unified diff
  - optional button to open the same path in the full editor
  - later commit flow should start from per-file selection on this same surface

Tablet fallback:

- keep the same conceptual split
- diff remains primary pane
- left pane may collapse behind a toggle/drawer

## Grouping Recommendation

Recommended default grouping:

- group by `stack_id` when available
- group non-stack files under `Other`

Why:

- this matches how operators think about homelab changes
- it aligns with both `stacks/` and `config/` structure
- it prepares the later commit flow naturally without forcing commits to be stack-only

## Diff Recommendation

Recommended first implementation:

- unified diff

Reason:

- simpler and denser than side-by-side on smaller screens
- easier to render consistently
- fits the operational workflow better than a code-review-style split diff

Nice-to-have later:

- optional side-by-side diff for large screens

## Required UI States

- Git unavailable state:
  - workspace is not a Git repo
- clean workspace state:
  - no local changes under `stacks/` and `config/`
- loading state:
  - status fetch
  - diff fetch
- binary diff state:
  - show metadata-only message
- truncated diff state:
  - show explicit truncation notice
- path no longer changed state:
  - diff target disappeared or became clean

## Confirmed UI Decisions

Confirmed by UI implementation planning:

1. `Files / Changes` uses pill-style buttons at the top of the tree panel
   - `Changes` shows a counter badge
   - when Git is unavailable, `Changes` is disabled with tooltip
2. changed files use compact colored status prefixes:
   - `M`, `A`, `D`, `R`, `U`, `C`
   - no separate per-row badges
3. diff header includes context action:
   - `Open in editor` for `config/*`
   - link to stack editor for `stacks/*`
   - disabled state for deleted files

## Expected Backend/UI Sequence

1. backend implements read-only Git status and diff endpoints
2. UI adds `Changes` mode inside `/config`
3. UI validates grouping and diff readability
4. later Milestone 4 adds per-file commit/push actions on top of the same surface, with stack-scoped quick selection as a convenience
