# States, Badges, and Empty Cases

## Stack State Model

Stack state is composed of two independent dimensions following the architect's decision to separate runtime from config state.

### Runtime State

Derived from Docker Engine. Describes what is actually running.

| State | Meaning | Badge color | Icon |
|---|---|---|---|
| `running` | All defined services have running containers | Green | Filled circle |
| `stopped` | All containers are stopped or removed | Gray | Empty circle |
| `partial` | Some services running, some not | Yellow | Half circle |
| `error` | At least one container in error, restarting, or dead state | Red | Exclamation |
| `unknown` | Cannot determine state (Docker unreachable, race condition) | Gray dashed | Question mark |

### Config State

Derived from filesystem and deploy history. Describes relationship between files on disk and last deployed state.

| State | Meaning | Badge | Icon |
|---|---|---|---|
| `synced` | `compose.yaml` matches last deployed version | None (default) | — |
| `modified` | `compose.yaml` changed since last deploy | Yellow dot | Pencil |
| `new` | Stack directory exists with `compose.yaml` but was never deployed through Stacklab | Blue dot | Sparkle |
| `invalid` | `compose.yaml` fails `docker compose config` validation | Red dot | Warning triangle |

### Combined Display

The stack list shows both states together:

```
┌──────────────────────────────────────────────────┐
│  ● traefik          Running              3/3     │
│  ● nextcloud        Running   ✎ Modified  2/2    │
│  ◐ monitoring       Partial              2/3     │
│  ○ backup           Stopped              0/2     │
│  ✦ new-stack        New                  —       │
│  ○ broken           Stopped   ⚠ Invalid  0/1     │
└──────────────────────────────────────────────────┘
```

Runtime state is the primary badge (left). Config state is shown as a secondary indicator only when not `synced`.

## Service State

Individual services within a stack detail view:

| State | Meaning | Color |
|---|---|---|
| `running` | Container running, health check passing or no healthcheck | Green |
| `healthy` | Container running, health check explicitly passing | Green with heart |
| `unhealthy` | Container running, health check failing | Red |
| `starting` | Container starting, health check not yet passed | Yellow |
| `stopped` | Container exited with code 0 or manually stopped | Gray |
| `exited` | Container exited with non-zero code | Red |
| `restarting` | Container in restart loop | Yellow pulsing |
| `dead` | Container in dead state | Red |
| `not_created` | Service defined in compose but no container exists | Gray dashed |

## Operation State

When a mutating operation is in progress on a stack:

| State | Meaning | UI Treatment |
|---|---|---|
| `idle` | No operation running | Normal display |
| `in_progress` | Operation executing (pull, build, up, down, restart) | Blue spinner on stack badge. Action buttons disabled. Progress panel visible. |
| `completed` | Last operation succeeded | Brief green flash, then return to idle |
| `failed` | Last operation failed | Red indicator with "View log" link. Persists until dismissed or next operation. |

When a stack is `in_progress`:

- all action buttons for that stack are disabled
- the progress panel shows real-time output streamed via WebSocket
- other stacks remain fully interactive
- navigation away from the stack is allowed (operation continues in background)

## Empty States

### Stack List — no stacks

```
┌──────────────────────────────────────────────┐
│                                              │
│         No stacks found                      │
│                                              │
│   No compose.yaml files detected in          │
│   /opt/stacklab/stacks/                      │
│                                              │
│   [Create your first stack]                  │
│                                              │
│   Or create a directory manually:            │
│   mkdir /opt/stacklab/stacks/my-app          │
│   and add a compose.yaml file.               │
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

Auto-reconnect with backoff. Terminal preserves scrollback buffer on reconnect.

### Operation failed

Inline error in the operation progress panel:

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
☐ Delete config directory (/opt/stacklab/config/nextcloud/)
☐ Delete data directory (/opt/stacklab/data/nextcloud/)

⚠ Deleting data is irreversible.

[Cancel]    [Remove selected]
```

Only "runtime" is checked by default. Each additional checkbox adds a stronger visual warning.
