# Screen Specifications

This document contains early screen wireframes and interaction notes from the implementation phase.
Treat absolute paths, exact tab sets, and sidebar examples here as illustrative unless they match the current app and newer API or ops docs.

## 1. Login

Route: `/login`

Purpose: Single-user authentication.

```
┌──────────────────────────────────────────────────────────┐
│                                                          │
│                                                          │
│                      STACKLAB                            │
│                                                          │
│               ┌──────────────────────┐                   │
│               │ Password             │                   │
│               └──────────────────────┘                   │
│               [        Log in        ]                   │
│                                                          │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

Notes:

- single password field (single-user system, no username needed)
- on first launch, show "Set password" instead of "Log in"
- session persists via HTTP-only cookie
- failed attempts show inline error, no lockout in v1 (LAN-only)

## 2. Stack List (Dashboard)

Route: `/stacks`

Purpose: Overview of all stacks, quick actions, entry point to detail views.

### Desktop (>= 1280px)

```
┌──────────────────────────────────────────────────────────────────────┐
│  STACKLAB                              🔍 Search...    [user ▾]     │
├────────┬─────────────────────────────────────────────────────────────┤
│        │  Stacks (12)                              [+ New stack]    │
│ Stacks │─────────────────────────────────────────────────────────────│
│ Audit  │                                                            │
│ Settin │  ● traefik        Running           3/3   [↻] [⏹] [⬆]   │
│        │  ● nextcloud       Running  ✎ Drifted 2/2  [↻] [⏹] [⬆]  │
│        │  ◐ monitoring      Partial          2/3   [↻] [⏹] [⬆]   │
│        │  ! home-assistant  Error            1/2   [↻] [⏹] [⬆]   │
│        │  ○ backup-nightly  Stopped          0/2   [▶] [⬆]        │
│        │  ◌ new-project     Defined          —     [▶]             │
│        │                                                            │
│        │  ─── Quick stats ──────────────────────────────────────    │
│        │  Stacks: 12 running, 3 stopped, 1 error                   │
│        │  Containers: 28 running / 35 total                         │
│        │                                                            │
└────────┴─────────────────────────────────────────────────────────────┘
```

### Stack row anatomy

Each row contains:

| Element | Description |
|---|---|
| Runtime badge | Color circle indicating runtime state |
| Stack name | Clickable, navigates to stack detail |
| Runtime label | Text label from `display_state`: Running, Stopped, Partial, Error, Defined, Orphaned |
| Config indicator | Secondary indicator from `config_state`, shown only when `drifted` or `invalid` |
| Service count | `running/total` format |
| Quick actions | Contextual: restart, stop, start, pull. Disabled during operations. |

### Tablet (768px - 1279px)

- sidebar collapses to icon bar
- quick actions collapse to a single "..." menu per row
- service count still visible

### Search

- filters stack list by name (client-side, instant)
- no server round-trip needed for v1

### Sorting

Default: alphabetical by name. Optional sort by state (errors first) or last action time.

## 3. Stack Overview

Route: `/stacks/:stackId`

Purpose: Detailed view of a single stack with service breakdown.

```
┌──────────────────────────────────────────────────────────────────────┐
│  STACKLAB                              🔍 Search...    [user ▾]     │
├────────┬─────────────────────────────────────────────────────────────┤
│        │  ← Stacks / nextcloud                                      │
│ Stacks │                                                            │
│ Audit  │  ● Running (2/2)                     ✎ Drifted             │
│ Settin │                                                            │
│        │  [Overview] [Editor] [Logs] [Stats] [Terminal] [History]   │
│        │─────────────────────────────────────────────────────────────│
│        │                                                            │
│        │  Actions: [▶ Deploy] [↻ Restart] [⏹ Stop] [⬇ Down] [⬆ Pull]
│        │                                                            │
│        │  ┌─ Services ──────────────────────────────────────────┐   │
│        │  │                                                     │   │
│        │  │  ● app                                              │   │
│        │  │    Image: nextcloud:29       Mode: pull             │   │
│        │  │    Ports: 8080:80                                   │   │
│        │  │    Status: Up 3 days         CPU: 2.1%  RAM: 245MB │   │
│        │  │    Mounts: config/nextcloud → /config               │   │
│        │  │            data/nextcloud → /data                   │   │
│        │  │    [Shell] [Logs] [Restart]                         │   │
│        │  │                                                     │   │
│        │  │  ● db                                               │   │
│        │  │    Image: postgres:16        Mode: pull             │   │
│        │  │    Status: Up 3 days (healthy)  CPU: 0.3%  RAM: 64MB│  │
│        │  │    Mounts: data/nextcloud/db → /var/lib/postgresql  │   │
│        │  │    [Shell] [Logs] [Restart]                         │   │
│        │  │                                                     │   │
│        │  └─────────────────────────────────────────────────────┘   │
│        │                                                            │
└────────┴─────────────────────────────────────────────────────────────┘
```

### Service card anatomy

| Element | Description |
|---|---|
| State badge | Same colors as container states |
| Service name | From compose.yaml service key |
| Image / Build | Image tag or build context path. Labeled with domain `mode`: `image`, `build`, or `hybrid`. |
| Ports | Published ports mapping |
| Status | Docker status string + uptime |
| Inline stats | CPU % and RAM usage (mini, from stats stream) |
| Mounts | Key volume mounts, showing relative paths under the managed Stacklab roots |
| Per-service actions | Shell, Logs (navigate to filtered log view), Restart |

### Stack-level actions bar

Buttons are contextual to `display_state` (from `runtime_state`). `activity_state = locked` disables all mutating buttons as an overlay.

| `display_state` | Available actions |
|---|---|
| `running` | Deploy (Up), Restart, Stop, Down, Pull |
| `stopped` | Deploy (Up), Pull, Remove |
| `partial` | Deploy (Up), Restart, Stop, Down, Pull |
| `error` | Deploy (Up), Restart, Stop, Down, Pull |
| `defined` | Deploy (Up), Edit |
| `orphaned` | Down, Remove |

### Orphaned stack — tab and navigation behavior

When `display_state = orphaned`, the stack has runtime containers but no canonical `compose.yaml`. Tabs that depend on stack definition are disabled; tabs that work with runtime remain available.

| Tab | State | Reason |
|---|---|---|
| Overview | Available | Shows runtime containers, ports, states. Displays a warning banner: "Stack definition missing — runtime containers exist without compose.yaml." |
| Editor | **Disabled** | Tooltip: "No compose.yaml found for this stack." |
| Logs | Available | Runtime containers produce logs. |
| Stats | Available | Runtime containers produce stats. |
| Terminal | Available | Container exec works on running containers. |
| History | Available | Audit log exists independently of definition files. |

Disabled tabs remain visible in the tab bar (not hidden) to preserve consistent layout. They are grayed out with a tooltip explaining why they are unavailable.

When `activity_state = locked`: all buttons disabled, spinner shown with current job action name.

"Down" and "Remove" require confirmation dialog (see states-and-empty-cases.md).

Action names in the UI map to domain operations (see `docs/domain/operation-model.md`):

| UI button | Domain action |
|---|---|
| Deploy | `up` |
| Restart | `restart` |
| Stop | `stop` |
| Down | `down` |
| Pull | `pull` |
| Build | `build` |
| Remove | `remove_stack_definition` |
| Save | `save_definition` |

## 4. Compose Editor

Route: `/stacks/:stackId/editor`

Purpose: Edit `compose.yaml` and `.env`, validate, preview resolved config, deploy.

```
┌──────────────────────────────────────────────────────────────────────┐
│  ← Stacks / nextcloud                                               │
│  [Overview] [Editor] [Logs] [Stats] [Terminal] [History]            │
├─────────────────────────────────┬────────────────────────────────────┤
│                                 │                                    │
│  [compose.yaml ▾] [.env]       │  Resolved config                   │
│                                 │                                    │
│  services:                      │  name: nextcloud                   │
│    app:                         │  services:                         │
│      image: nextcloud:29        │    app:                            │
│      ports:                     │      image: nextcloud:29           │
│        - "${PORT}:80"           │      ports:                        │
│      volumes:                   │        - "8080:80"                 │
│        - ../../config/next...   │      environment:                  │
│      environment:               │        DB_HOST: db                 │
│        DB_HOST: db              │        DB_NAME: nextcloud          │
│        DB_NAME: ${DB_NAME}      │      ...                           │
│    db:                          │    db:                              │
│      image: postgres:16         │      image: postgres:16            │
│      ...                        │      ...                           │
│                                 │                                    │
│                                 │                                    │
│                                 │                                    │
├─────────────────────────────────┴────────────────────────────────────┤
│  ✓ Config valid                    [Discard] [Save] [Save & Deploy] │
└──────────────────────────────────────────────────────────────────────┘
```

### Layout

- **Left panel**: CodeMirror 6 editor. Tab selector for `compose.yaml` and `.env`.
- **Right panel**: Read-only resolved config output from `docker compose config`. Auto-refreshes on save or on-demand.
- **Bottom bar**: Validation status, action buttons.

### Desktop vs tablet

- Desktop (>= 1280px): side-by-side panels
- Tablet (768-1279px): stacked vertically (editor on top, resolved below) or toggle between editor and preview
- Below 768px: editor only with a "Preview" toggle button. Hint: "Full editor experience on desktop."

### Validation

- triggered on save (not on every keystroke — YAML parsing is expensive)
- runs `docker compose config` via backend API
- result shown in bottom bar: green checkmark or red error with line number
- invalid config blocks "Save & Deploy" but allows "Save" (user may want to save work in progress)

### File tabs

- `compose.yaml` — primary, always present
- `.env` — shown if file exists or user creates one
- future: additional compose override files

## 7. Config Workspace

Route: `/config`

Purpose: browse, edit, diff, commit, and push managed config files, while clearly surfacing blocked files caused by ownership or mode drift.

### Files mode blocked-file variant

When `blocked_reason != null` for the selected file:

- keep the normal header visible
- replace the editor pane with a blocked-file card
- show owner, group, mode, and effective read/write state
- do not render editable controls

### Changes mode blocked-file variant

When a changed file has:

- `commit_allowed = false`
  - row checkbox is disabled
- `diff_available = false`
  - right pane shows blocked-file card instead of diff text

The row should still stay visible in the normal stack grouping so the operator understands that Git sees the change even though Stacklab cannot safely act on it.

## 5. Log Viewer

Route: `/stacks/:stackId/logs`

Purpose: Live-streamed logs from stack services.

```
┌──────────────────────────────────────────────────────────────────────┐
│  ← Stacks / nextcloud                                               │
│  [Overview] [Editor] [Logs] [Stats] [Terminal] [History]            │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Services: [All ▾]  [app] [db]        🔍 Filter...    [⏸ Pause]    │
│                                                                      │
│  12:03:01  app  | Nextcloud is ready                                │
│  12:03:02  db   | LOG: checkpoint complete                          │
│  12:04:15  app  | GET /status 200 OK                                │
│  12:04:15  app  | GET /apps/dashboard 200 OK                        │
│  12:05:00  db   | LOG: automatic vacuum of table "oc_filecache"     │
│  12:05:01  app  | WARN: session timeout for user admin              │
│  ...                                                                 │
│  ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ (live)     │
│                                                                      │
│  [↓ Scroll to bottom]                     Lines: 1,247   [Download] │
└──────────────────────────────────────────────────────────────────────┘
```

### Features

- **Service filter**: toggle which services are visible. "All" is default.
- **Text filter**: client-side search within visible lines. Highlights matches.
- **Pause/Resume**: pauses auto-scroll and incoming lines buffer. Resume appends buffered lines.
- **Auto-scroll**: follows new output. Disabled when user scrolls up. "Scroll to bottom" button appears.
- **Download**: exports visible (filtered) logs as text file.
- **Color coding**: each service gets a consistent color for its name prefix.
- **Timestamps**: shown in local time, toggleable between local and UTC.

### Performance

- virtual scrolling for large log buffers (10k+ lines)
- configurable buffer limit (default: 5000 lines, older lines dropped)
- WebSocket stream with backpressure handling

## 6. Stats Dashboard

Route: `/stacks/:stackId/stats`

Purpose: Real-time resource usage per container and aggregated per stack.

```
┌──────────────────────────────────────────────────────────────────────┐
│  ← Stacks / nextcloud                                               │
│  [Overview] [Editor] [Logs] [Stats] [Terminal] [History]            │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Session history: last ~5 min, collected while this view is open     │
│                                                                      │
│  ┌─ Stack CPU ──────┐ ┌─ Stack RAM ──────┐ ┌─ Stack Net ──────────┐ │
│  │ 2.4%             │ │ 309 MB / 1 GB    │ │ ↓12 KB/s · ↑3 KB/s   │ │
│  │ [trend chart]    │ │ [trend chart]    │ │ [trend chart]        │ │
│  └──────────────────┘ └──────────────────┘ └──────────────────────┘ │
│                                                                      │
│  ┌─ app ────────────────────────────────────────────────────────┐   │
│  │  CPU ████████░░░░░░░░░░ 2.1%    RAM ██████░░░░░░ 245/512 MB │   │
│  │  Net ↓ 10.2 KB/s  ↑ 2.8 KB/s                                │   │
│  │  [cpu sparkline ~~~~~~~~~~~]  [ram sparkline ~~~~~~~~~~~]    │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─ db ─────────────────────────────────────────────────────────┐   │
│  │  CPU █░░░░░░░░░░░░░░░░░ 0.3%    RAM ██░░░░░░░░░░  64/256 MB │   │
│  │  Net ↓ 1.5 KB/s  ↑ 0.4 KB/s                                 │   │
│  │  [cpu sparkline ~~~~~~~~~~~]  [ram sparkline ~~~~~~~~~~~]    │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

### Per-container card

| Element | Description |
|---|---|
| CPU bar | Percentage bar + numeric value |
| RAM bar | Usage / limit bar + numeric values |
| Network | Download and upload rates |
| Sparklines | Rolling 5-minute mini charts for CPU and RAM |

### Stack aggregate

Top cards show stack-wide CPU, memory, and network trends.

History rules:

- history is frontend-only
- history starts when the stats view is open in the browser
- history is not persisted in SQLite
- refresh or navigation resets the local history buffer

### Data source

- WebSocket stats stream from Docker Engine API
- update interval: ~2 seconds
- trend charts store roughly the last 5 minutes client-side, capped at 150 frames

### No running containers

Show empty state (see states-and-empty-cases.md).

## 7. Terminal

Route: `/stacks/:stackId/terminal`

Purpose: Container shell sessions via `docker exec`.

```
┌──────────────────────────────────────────────────────────────────────┐
│  ← Stacks / nextcloud                                               │
│  [Overview] [Editor] [Logs] [Stats] [Terminal] [History]            │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Container: [app ▾]    Shell: [/bin/sh ▾]    [+ New session]        │
│                                                                      │
│  Sessions: [app #1] [app #2] [db #1]                        [×]    │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │ root@abc123:/app# ls -la                                     │   │
│  │ total 48                                                     │   │
│  │ drwxr-xr-x  1 root root 4096 Apr  1 10:00 .                │   │
│  │ drwxr-xr-x  1 root root 4096 Apr  1 10:00 ..               │   │
│  │ -rw-r--r--  1 root root  123 Apr  1 10:00 config.php       │   │
│  │ root@abc123:/app# _                                          │   │
│  │                                                              │   │
│  │                                                              │   │
│  │                                                              │   │
│  │                                                              │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  Connected ●                                         Resize: auto   │
└──────────────────────────────────────────────────────────────────────┘
```

### Features

- **Container selector**: dropdown listing running containers in the stack
- **Shell selector**: defaults to `/bin/sh`, option for `/bin/bash` if available
- **Multiple sessions**: tab bar for parallel sessions to different (or same) containers
- **Close session**: per-tab close button. Confirmation if session is active.
- **Connection indicator**: green dot when connected, red when disconnected. On disconnect: UI attempts WebSocket reconnect with backoff. If the backend PTY session is still alive, the stream resumes. If the PTY was terminated (idle timeout, cleanup), UI shows "Session ended. Start a new session?" — it does not silently pretend the old session continues. Scrollback buffer is preserved client-side in both cases.
- **Auto-resize**: XTerm.js fit addon syncs terminal size with browser viewport

### Terminal component architecture

The terminal component is designed for reuse across two modes:

| Mode | MVP | Source |
|---|---|---|
| Container exec | Yes | `docker exec -it <container> <shell>` |
| Host shell | Post-MVP | Direct PTY on host |

Both modes use the same XTerm.js wrapper and WebSocket transport. The mode selector is hidden in v1 but the plumbing supports both.

### Security (pending architect's security-model.md)

- terminal WebSocket requires authenticated session
- idle timeout: TBD by architect (suggested: 30 minutes)
- max concurrent sessions: TBD by architect (suggested: 5)
- session activity logged in audit

### Tablet / responsive

- terminal is usable on tablet but shows a hint: "Best experience on desktop"
- minimum usable width: 768px (80 columns at standard font size)
- below 768px: terminal view shows "Open on desktop for terminal access"

## 8. Stack History (Per-Stack Audit)

Route: `/stacks/:stackId/audit`

Purpose: Chronological log of mutating operations on this stack.

```
┌──────────────────────────────────────────────────────────────────────┐
│  ← Stacks / nextcloud                                               │
│  [Overview] [Editor] [Logs] [Stats] [Terminal] [History]            │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Stack history                                          [Export]     │
│                                                                      │
│  2026-04-03 14:22  pull             ✓ succeeded   12s                │
│  2026-04-03 14:23  up               ✓ succeeded    4s                │
│  2026-04-02 09:15  restart          ✓ succeeded    6s                │
│  2026-04-01 18:00  save_definition  ✓ succeeded    —                 │
│  2026-04-01 18:01  up               ✗ failed       2s  [View log]   │
│  2026-04-01 18:05  save_definition  ✓ succeeded    —                 │
│  2026-04-01 18:05  up               ✓ succeeded    3s                │
│  ...                                                                 │
│                                                                      │
│                                          [Load more]                 │
└──────────────────────────────────────────────────────────────────────┘
```

### Row anatomy

All field names and values use the domain vocabulary from `docs/domain/operation-model.md`.

| Field | Domain source | Description |
|---|---|---|
| Timestamp | `requested_at` | Local time |
| Action | `action` | Domain action name: `up`, `down`, `stop`, `restart`, `pull`, `build`, `recreate`, `save_definition`, `create_stack`, `remove_stack_definition`, `validate` |
| Result | job `state` | `succeeded`, `failed`, `cancelled`, `timed_out` |
| Duration | `finished_at - started_at` | Wall clock time, shown as `duration_ms` formatted |
| Detail link | — | "View log" for `failed` / `timed_out` — shows captured job output |

### Pagination

- newest first
- load 50 entries at a time
- "Load more" button (no infinite scroll — explicit control)

## 9. Global Audit

Route: `/audit`

Purpose: System-wide audit log across all stacks.

Same layout as stack history but with an additional "Stack" column and a stack filter dropdown.

```
│  2026-04-03 14:22  nextcloud   pull     ✓ succeeded    12s         │
│  2026-04-03 14:20  traefik     restart  ✓ succeeded     2s         │
│  2026-04-03 10:00  monitoring  up       ✗ failed        8s  [Log]  │
```

## 10. Create Stack

Route: `/stacks/new`

Purpose: Create a new stack with directory scaffolding.

```
┌──────────────────────────────────────────────────────────────────────┐
│  ← Stacks / New stack                                               │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Stack name:  [my-new-app          ]                                │
│                                                                      │
│  ℹ Will create:                                                     │
│    a new stack definition and optional                              │
│    stack-scoped config/data directories                              │
│                                                                      │
│  Initial compose.yaml:                                               │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │ services:                                                    │   │
│  │   app:                                                       │   │
│  │     image: _                                                 │   │
│  │                                                              │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  [Cancel]                                  [Create] [Create & Deploy]│
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

### Validation

- stack name: lowercase ASCII with dashes, no spaces, no special characters
- real-time validation as user types (inline error if invalid)
- check for name collision with existing stacks
- initial compose.yaml must pass `docker compose config` for "Create & Deploy"

## 11. Host

Route: `/host`

Purpose: Operational view of the managed host and Stacklab itself.

```
┌──────────────────────────────────────────────────────────────────────┐
│  Host                                                                │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─ Stacklab ───────┐ ┌─ System ─────────┐ ┌─ Docker ─────────────┐ │
│  │ v2026.04.0       │ │ debian-homelab   │ │ Engine 28.5.1        │ │
│  │ commit abc12345  │ │ Debian GNU/Linux │ │ Compose 2.39.2       │ │
│  │ started 14:10    │ │ kernel 6.12      │ │                      │ │
│  └──────────────────┘ │ uptime 3d 5h     │ └──────────────────────┘ │
│                       └───────────────────┘                          │
│                                                                      │
│  ┌─ Resources ────────────────────────────────────────────────────┐  │
│  │ CPU   12.4%  [████░░░░░░]  4 cores                            │  │
│  │ RAM   3.1 GB / 8.0 GB [█████░░░░░]                            │  │
│  │ Disk  83 GB / 274 GB  [███░░░░░░░]                            │  │
│  └────────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  Stacklab logs                                          [Refresh]   │
│  [All ▾] [follow on/off] [text filter...]                           │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ 14:13:22  info   HTTP server listening                        │  │
│  │ 14:13:24  warn   Stacklab logs unavailable                    │  │
│  │ 14:13:27  error  Failed to read journal                       │  │
│  └────────────────────────────────────────────────────────────────┘  │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

Notes:

- dedicated top-level page in the sidebar
- cards are optimized for quick host-health scanning, not dense reporting
- Stacklab logs are stacked under the overview, not a separate tab
- logs use polling follow mode in the first version

## 12. Settings

Route: `/settings`

Purpose: Application configuration.

```
┌──────────────────────────────────────────────────────────────────────┐
│  Settings                                                            │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Authentication                                                      │
│  ──────────────                                                      │
│  Change password:  [current] [new] [confirm]    [Update]            │
│                                                                      │
│  Notifications                                                       │
│  ─────────────                                                       │
│  Enable notifications  [toggle]                                     │
│  Webhook URL: https://hooks.example.test/stacklab                   │
│  [x] Failed jobs   [x] Jobs with warnings   [ ] Maintenance         │
│  [Send test]                                      [Save]            │
│                                                                      │
│  Appearance                                                          │
│  ──────────                                                          │
│  Theme: [Dark ▾]                                                    │
│                                                                      │
│  Stack defaults                                                      │
│  ──────────────                                                      │
│  Install mode and managed roots shown from backend metadata         │
│                                                                      │
│  About                                                               │
│  ─────                                                               │
│  Stacklab v1.0.0                                                    │
│  Docker Engine: 27.0.1                                               │
│  Docker Compose: v2.29.0                                             │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

## 13. Operation Progress Panel

Not a standalone screen but an inline/overlay panel shown during mutating operations.

```
┌──────────────────────────────────────────────────────────────────────┐
│  ⟳ Pulling nextcloud...                                             │
│                                                                      │
│  app: Pulling from library/nextcloud                                │
│  app: 29-apache: Pulling fs layer                                   │
│  app: 29-apache: Downloading  ████████░░░░ 67%                     │
│  db:  Pulling from library/postgres                                 │
│  db:  16: Already up to date                                        │
│                                                                      │
│  [Cancel]                                                            │
└──────────────────────────────────────────────────────────────────────┘
```

- appears below the action bar on stack overview
- streamed via WebSocket in real time
- cancellable where Docker supports it (SIGINT)
- on completion: auto-collapses after 3 seconds if success, stays open if failure
