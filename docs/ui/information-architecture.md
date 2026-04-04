# Information Architecture

## Navigation Model

Stacklab uses a flat two-level navigation:

- top level: global sections accessible from a persistent sidebar or top bar
- second level: contextual views within a stack

There is no deeper nesting. Every screen is reachable in at most two clicks from any other screen.

## Global Navigation

```
┌─────────────────────────────────────────────┐
│  STACKLAB    [activity] [search]   [user ▾] │
├──────┬──────────────────────────────────────┤
│      │                                      │
│  ◉ Stacks    Main content area              │
│  ◎ Host                                     │
│  ◎ Audit                                    │
│  ◎ Settings                                 │
│      │                                      │
└──────┴──────────────────────────────────────┘
```

### Sidebar sections

| Section | Route | Purpose |
|---|---|---|
| **Stacks** | `/stacks` | Stack list dashboard. Default landing page. |
| **Host** | `/host` | Host overview, Stacklab version, and Stacklab service logs |
| **Audit** | `/audit` | Global audit log of all mutating actions |
| **Settings** | `/settings` | Application settings, auth, preferences, and later update schedules |

The sidebar is collapsible on tablet widths (below 1024px) to a narrow icon bar.

Future global chrome note:

- long-running jobs should later surface in a persistent global activity affordance in the app chrome
- this is intended for background job visibility, not as a replacement for audit or page-local progress panels

## Stack Context Navigation

Entering a stack opens a detail view with tabbed sub-navigation:

```
/stacks/:stackId
/stacks/:stackId/editor
/stacks/:stackId/logs
/stacks/:stackId/stats
/stacks/:stackId/terminal
/stacks/:stackId/audit
```

### Stack tabs

| Tab | Route suffix | Purpose |
|---|---|---|
| **Overview** | `/stacks/:id` | Services, containers, ports, state, health |
| **Editor** | `/stacks/:id/editor` | Edit `compose.yaml` and `.env` with validation |
| **Logs** | `/stacks/:id/logs` | Live log streaming, filterable by service |
| **Stats** | `/stacks/:id/stats` | CPU, memory, network per container and aggregated |
| **Terminal** | `/stacks/:id/terminal` | Container shell sessions (host shell post-MVP) |
| **History** | `/stacks/:id/audit` | Per-stack audit log |

## Screen Inventory

### Global screens

| Screen | Description | MVP |
|---|---|---|
| Stack List | Dashboard with all stacks, their states, quick actions | Yes |
| Host | Host overview, Stacklab version/build info, Stacklab service logs | Post-MVP / Next milestone |
| Global Audit | Chronological log of all mutating operations | Yes |
| Settings | App configuration, password change | Yes |
| Login | Authentication screen | Yes |

### Stack-scoped screens

| Screen | Description | MVP |
|---|---|---|
| Stack Overview | Service list, container states, ports, mounts, image/build mode | Yes |
| Compose Editor | CodeMirror editor for `compose.yaml` and `.env`, validation, resolved preview | Yes |
| Log Viewer | Live log stream with service filter and search | Yes |
| Stats Dashboard | Real-time CPU/mem/net charts per container and aggregated | Yes |
| Terminal | Container exec shell sessions via XTerm.js | Yes |
| Stack History | Per-stack audit trail | Yes |
| Create Stack | Form/editor to create a new stack with directory scaffolding | Yes |

### Modal / overlay screens

| Screen | Description | MVP |
|---|---|---|
| Confirm Action | Confirmation dialog for destructive actions (down, remove) | Yes |
| Operation Progress | Overlay or inline panel showing live job output (pull, build, up) | Yes |
| Remove Stack | Multi-option removal dialog (runtime / definition / config / data) | Yes |

## Navigation Flows

### Primary flow: daily operations

```
Login → Stack List → Stack Overview → [action: restart/pull/up]
                                    → Logs (diagnose)
                                    → Terminal (debug)
```

### Edit flow

```
Stack List → Stack Overview → Editor → Validate → Deploy (up)
```

### Create flow

```
Stack List → Create Stack → Editor → Deploy
```

### Audit flow

```
Global Audit → filter by stack → Stack History
Stack Overview → History tab
```

### Host diagnostics flow

```
Stacks → Host → inspect host health → inspect Stacklab logs
```

## Responsive Breakpoints

| Breakpoint | Name | Behavior |
|---|---|---|
| >= 1280px | Desktop | Full layout: sidebar + content + optional side panel |
| 768px - 1279px | Tablet | Collapsed sidebar (icons only), full content area. Terminal and editor show a "best on desktop" hint but remain usable. |
| < 768px | Mobile | Not a target. Basic stack list readable, but editor and terminal are not formally supported. |

## URL Structure

All routes are client-side (SPA with history mode):

```
/login
/stacks
/host
/stacks/new
/stacks/:stackId
/stacks/:stackId/editor
/stacks/:stackId/logs
/stacks/:stackId/stats
/stacks/:stackId/terminal
/stacks/:stackId/audit
/audit
/settings
```

Stack IDs in URLs match the filesystem directory name (lowercase ASCII with dashes).
