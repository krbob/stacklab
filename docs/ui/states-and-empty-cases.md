# States, Badges, and Empty Cases

## Stack State Model

Stack state is composed of two independent dimensions following the architect's decision to separate runtime from config state.

### Runtime State

Derived from Docker Engine. Describes what is actually running. Maps directly to the domain `runtime_state` (see `docs/domain/stack-model.md`).

| Domain value | UI label | Badge color | Icon |
|---|---|---|---|
| `running` | Running | Green | Filled circle |
| `stopped` | Stopped | Gray | Empty circle |
| `partial` | Partial | Yellow | Half circle |
| `error` | Error | Red | Exclamation |
| `defined` | Defined | Gray muted | Dotted circle |
| `orphaned` | Orphaned | Red/warning | Warning triangle |

Notes:

- `display_state` from the API equals `runtime_state` — UI uses it directly as the primary badge
- `defined` replaces the previous UI-only concept of "new" — it means the stack exists on disk but has no runtime containers and no known deployment history
- `orphaned` means runtime containers claim a stack identity but the canonical `compose.yaml` is missing on disk

### Config State

Derived from filesystem and deploy history. Describes relationship between files on disk and last deployed state. Maps directly to the domain `config_state` (see `docs/domain/stack-model.md`).

| Domain value | UI label | Badge | Icon |
|---|---|---|---|
| `in_sync` | — | None (default, not shown) | — |
| `drifted` | Drifted | Yellow dot | Pencil |
| `unknown` | — | None (not shown, applies to stacks never deployed) | — |
| `invalid` | Invalid | Red dot | Warning triangle |

Notes:

- `in_sync` and `unknown` produce no secondary indicator — they are the "quiet" states
- `drifted` means the compose definition changed since the last known deploy; it is informational, not an error
- `invalid` blocks deploy actions but allows save (user may want to save work in progress)

### Combined Display

The stack list shows both states together:

```
┌──────────────────────────────────────────────────┐
│  ● traefik          Running              3/3     │
│  ● nextcloud        Running   ✎ Drifted   2/2    │
│  ◐ monitoring       Partial              2/3     │
│  ○ backup           Stopped              0/2     │
│  ◌ new-stack        Defined              —       │
│  ○ broken           Stopped   ⚠ Invalid  0/1     │
│  ⚠ ghost            Orphaned             2/?     │
└──────────────────────────────────────────────────┘
```

- Primary badge (left icon + label) = `display_state` derived from `runtime_state`
- Secondary indicator (right) = `config_state`, shown only when `drifted` or `invalid`
- `activity_state = locked` is an overlay on top of both (see Activity State below)

## Service / Container State

Individual containers within a stack detail view. Maps to the normalized Docker `status` values from the domain Container entity (see `docs/domain/stack-model.md`).

| Domain `status` | UI label | Color | Notes |
|---|---|---|---|
| `created` | Created | Gray | Container exists but never started |
| `running` | Running | Green | Health shown separately if healthcheck present |
| `restarting` | Restarting | Yellow pulsing | Container in restart loop |
| `paused` | Paused | Yellow | Container paused |
| `exited` | Exited | Gray (code 0) or Red (non-zero) | Show exit code |
| `dead` | Dead | Red | Unrecoverable state |

Health status (shown as supplementary badge when `healthcheck_present = true`):

| Health | UI indicator | Color |
|---|---|---|
| `healthy` | Heart icon | Green |
| `unhealthy` | Broken heart icon | Red |
| `starting` | Pulse icon | Yellow |
| none | — | — |

When a service has no container at all (defined in compose but not created), the row shows "Not created" in gray dashed style.

## Activity State (Operation Overlay)

The domain `activity_state` is an overlay on top of the primary badge, not a replacement for it. It maps to the stack lock (see `docs/domain/stack-model.md` and `docs/domain/operation-model.md`).

| Domain value | UI treatment |
|---|---|
| `idle` | Normal display, no overlay |
| `locked` | Blue spinner overlaid on primary badge. Action buttons disabled. Progress panel visible. |

`locked` does not replace `display_state`. A stack can be `running` + `locked` (e.g. pull in progress while containers still run).

### Job State Mapping

Jobs streamed via WebSocket use domain job states. UI maps them as follows:

| Domain job state | UI presentation |
|---|---|
| `queued` | "Queued..." in progress panel (v1 may skip queuing and reject instead) |
| `running` | Active progress panel with streaming output |
| `succeeded` | Brief green flash on badge, progress panel auto-collapses after 3s |
| `failed` | Red indicator with "View log" link. Persists until dismissed or next operation. |
| `cancel_requested` | "Cancelling..." in progress panel |
| `cancelled` | Gray "Cancelled" status, progress panel stays open |
| `timed_out` | Red "Timed out" status, same treatment as `failed` |

### Behavior when `activity_state = locked`

- all mutating action buttons for that stack are disabled
- the progress panel shows real-time job output streamed via WebSocket
- other stacks remain fully interactive
- navigation away from the stack is allowed (job continues in background)
- read operations and streaming diagnostics (logs, stats) remain available

## Empty States

### Stack List — no stacks

```
┌──────────────────────────────────────────────┐
│                                              │
│         No stacks found                      │
│                                              │
│   No compose.yaml files detected in the      │
│   managed stacks root.                       │
│                                              │
│   [Create your first stack]                  │
│                                              │
│   Or create a stack from the UI and add      │
│   a compose.yaml definition.                 │
│                                              │
└──────────────────────────────────────────────┘
```

### Stack Detail — no containers

Stack exists in filesystem but has never been deployed:

```
┌──────────────────────────────────────────────┐
│                                              │
│   Stack "my-app" is defined but not running  │
│                                              │
│   compose.yaml found. No containers exist.   │
│                                              │
│   [Deploy]    [Edit compose.yaml]            │
│                                              │
└──────────────────────────────────────────────┘
```

### Logs — no log output

```
┌──────────────────────────────────────────────┐
│                                              │
│   No logs available                          │
│                                              │
│   The selected service has no log output     │
│   or is not running.                         │
│                                              │
│   [Start stack]                              │
│                                              │
└──────────────────────────────────────────────┘
```

### Stats — no running containers

```
┌──────────────────────────────────────────────┐
│                                              │
│   No stats available                         │
│                                              │
│   Stats require at least one running         │
│   container in this stack.                   │
│                                              │
└──────────────────────────────────────────────┘
```

### Terminal — no running containers

```
┌──────────────────────────────────────────────┐
│                                              │
│   No containers available for shell access   │
│                                              │
│   Start the stack to open a shell session.   │
│                                              │
└──────────────────────────────────────────────┘
```

### Audit — no entries

```
┌──────────────────────────────────────────────┐
│                                              │
│   No operations recorded yet                 │
│                                              │
│   Actions like deploy, stop, pull, and       │
│   remove will appear here.                   │
│                                              │
└──────────────────────────────────────────────┘
```

### Global Audit — no entries

Same as stack audit but scoped to the whole system.

## Loading States

Every data-fetching view shows a skeleton loader, not a spinner. Skeletons preserve layout stability and reduce perceived load time.

| View | Skeleton |
|---|---|
| Stack list | 3-5 placeholder stack rows with gray animated bars |
| Stack detail | Service cards with gray placeholders |
| Logs | Empty terminal area with "Connecting..." status |
| Stats | Chart areas with gray placeholder rectangles |
| Editor | CodeMirror container with gray block |

## Error States

### API unreachable

Persistent top banner (red):

```
┌──────────────────────────────────────────────┐
│ ⚠ Connection to Stacklab backend lost.       │
│   Retrying...                        [Retry] │
└──────────────────────────────────────────────┘
```

Shown above content. Content remains visible but grayed out. Auto-retry with exponential backoff.

### WebSocket disconnected

Inline indicator on affected view (logs, stats, terminal):

```
Stream disconnected. Reconnecting...   [Reconnect]
```

Auto-reconnect with backoff for logs and stats streams.

For terminal: WebSocket reconnect restores the transport connection, but does **not** automatically resume the same PTY session. Three distinct concerns:

1. **Scrollback preservation** — XTerm.js maintains the local scrollback buffer client-side across reconnects. This is purely a UI concern.
2. **WebSocket reconnect** — re-establishes the transport to the backend. Automatic with backoff.
3. **PTY session resume** — the original PTY process may have been terminated by the backend during disconnect (idle timeout, cleanup). If the PTY is gone, the UI shows "Session ended. Start a new session?" instead of silently reconnecting to a dead stream. If the PTY is still alive (brief network blip), the backend may reattach — but this is a backend capability, not a UI guarantee.

### Operation failed

Inline error in the operation progress panel:

## Config Workspace Blocked File

When a file is inside the workspace boundary but current permissions prevent Stacklab from reading or editing it, this is not a generic error screen. It is a first-class blocked-file state.

Expected trigger examples:

- container recreated a config file owned by `root`
- file mode changed to `0600`
- Git sees a changed file but Stacklab cannot read it

### Files mode

```text
┌──────────────────────────────────────────────┐
│  File access blocked                         │
│                                              │
│  Stacklab cannot read this file with the     │
│  current service user.                       │
│                                              │
│  Owner: root                                 │
│  Group: root                                 │
│  Mode: 0600                                  │
│  Readable by Stacklab: No                    │
│  Writable by Stacklab: No                    │
│                                              │
│  The container may have recreated this file  │
│  with different ownership or permissions.    │
│                                              │
└──────────────────────────────────────────────┘
```

Rules:

- this is not an API error banner
- file header remains visible
- editor is not rendered
- save/discard actions are hidden

### Changes mode

Blocked changed files remain visible in the list.

Rules:

- diff entry remains clickable
- diff panel shows blocked-file state instead of unified diff
- commit checkbox is disabled when `commit_allowed = false`
- group selection skips blocked files rather than failing entirely

```
┌──────────────────────────────────────────────┐
│ ✗ pull failed for service "app"              │
│                                              │
│   Error pulling image "registry/app:latest": │
│   timeout exceeded                           │
│                                              │
│   [View full log]    [Retry]    [Dismiss]    │
└──────────────────────────────────────────────┘
```

## Confirmation Dialogs

Destructive actions require explicit confirmation:

### Stop stack

```
Stop stack "nextcloud"?

This will stop all 3 running containers.
Data volumes will not be affected.

[Cancel]    [Stop]
```

### Remove stack

```
Remove stack "nextcloud"?

What to remove:
☑ Stop and remove containers (runtime)
☐ Delete stack definition (compose.yaml, .env)
☐ Delete this stack's config directory
☐ Delete this stack's data directory

⚠ Deleting data is irreversible.

[Cancel]    [Remove selected]
```

Only "runtime" is checked by default. Each additional checkbox adds a stronger visual warning.
