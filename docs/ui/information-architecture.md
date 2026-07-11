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
│  ◎ Config                                   │
│  ◎ Maintenance                              │
│  ◎ Docker                                   │
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
| **Config** | `/config` | Managed config workspace, Git changes, commit, and push |
| **Maintenance** | `/maintenance` | Bulk update, images inventory, and cleanup workflows |
| **Docker** | `/docker` | Docker daemon status, Engine metadata, and read-only `daemon.json` visibility |
| **Audit** | `/audit` | Global audit log of all mutating actions |
| **Settings** | `/settings` | Application settings, auth, notifications, preferences, and later update schedules |

The sidebar is collapsible on tablet widths (below 1024px) to a narrow icon bar.

Global activity is part of the app chrome:

- active and recently completed jobs are visible in the sidebar and mobile header
- the activity affordance opens the shared job drawer for live job detail
- page-local progress panels remain the primary context for a workflow that was just started

## Stack Context Navigation

Entering a stack opens a detail view with tabbed sub-navigation:

```
/stacks/:stackId
/stacks/:stackId/editor
/stacks/:stackId/files
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
| **Files** | `/stacks/:id/files` | Browse and edit stack-scoped workspace files |
| **Logs** | `/stacks/:id/logs` | Live log streaming, filterable by service |
| **Stats** | `/stacks/:id/stats` | CPU, memory, network per container and aggregated |
| **Terminal** | `/stacks/:id/terminal` | Container shell sessions (host shell post-MVP) |
| **History** | `/stacks/:id/audit` | Per-stack audit log |

## Document Metadata And Heading Hierarchy

Every routed screen has exactly one logical `h1`. Global screens receive it
from the shared page header, stack-scoped screens use the stack name, and
route-level loading or error states render a provisional `h1` instead of
leaving the previous screen's heading in place. Screen sections start at `h2`
and nested subsections use `h3`.

Browser titles are derived from the active route:

- global screen: `<Screen> | Stacklab`
- resolved stack view: `<View> — <Stack name> (<stack-id>) | Stacklab`
- stack loading/error fallback: `<View> — <stack-id> | Stacklab`
- unmatched route: `Page not found | Stacklab`

The route and stack ID update the title before asynchronous stack data is
available. A successful detail response enriches it with the display name.
Auth loading, lazy-route loading, API errors, and navigation between two stack
IDs must never retain the title of the previous screen.

The HTML metadata and PWA manifest describe Stacklab as a host-native control
panel for Docker Compose stacks, host health, updates, and maintenance.

## Screen Inventory

### Global screens

| Screen | Description | MVP |
|---|---|---|
| Stack List | Dashboard with all stacks, their states, quick actions | Yes |
| Host | Host overview, Stacklab version/build info, Stacklab service logs | Yes |
| Docker Admin | Docker daemon status, Engine metadata, `daemon.json` visibility, and managed daemon settings apply where configured | Yes |
| Config Workspace | Browse, edit, diff, commit, and push managed config files | Yes |
| Maintenance | Bulk stack update, image inventory, and cleanup | Yes |
| Global Audit | Chronological log of all mutating operations | Yes |
| Settings | App configuration, password change | Yes |
| Login | Authentication screen | Yes |

### Stack-scoped screens

| Screen | Description | MVP |
|---|---|---|
| Stack Overview | Service list, container states, ports, mounts, image/build mode | Yes |
| Compose Editor | CodeMirror editor for `compose.yaml` and `.env`, validation, resolved preview | Yes |
| Stack Files | Stack-scoped file browser/editor for non-definition files | Yes |
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

### Docker administration flow

```
Stacks → Docker → inspect daemon status → inspect daemon.json
```

## Responsive Breakpoints

| Breakpoint | Name | Behavior |
|---|---|---|
| >= 1280px | Desktop | Full layout: sidebar + content + optional side panel |
| 768px - 1279px | Tablet | Collapsed sidebar (icons only), full content area. Terminal and editor show a "best on desktop" hint but remain usable. |
| < 768px | Mobile | Fixed app shell with top header, bottom primary navigation, and a "More" drawer. Core read/operate flows are supported; dense tools such as editor and terminal remain usable but are optimized for larger screens. |

Mobile navigation rules:

- `/` canonicalizes to `/stacks`, so deep-link and refresh behavior use one
  landing URL;
- Stacks, Host, Maintenance, and Audit stay in the bottom navigation;
- Config, Docker, and Settings activate the `More` affordance and remain
  available in its drawer;
- stack views use a single-line, horizontally scrollable sticky tab bar;
- stack actions stay directly below that tab bar while scrolling and are
  separated into deployment, image, and disruptive groups.

## URL Structure

All routes are client-side (SPA with history mode):

```
/login
/stacks
/host
/docker
/config
/maintenance
/stacks/new
/stacks/:stackId
/stacks/:stackId/editor
/stacks/:stackId/files
/stacks/:stackId/logs
/stacks/:stackId/stats
/stacks/:stackId/terminal
/stacks/:stackId/audit
/audit
/settings
```

Stack IDs in URLs match the filesystem directory name (lowercase ASCII with dashes).
