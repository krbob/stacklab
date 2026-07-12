# Screen Specifications

This document defines the current presentation and interaction contract for
implemented screens. Route ownership and responsive navigation are defined in
[Information Architecture](information-architecture.md); payloads and status
codes remain owned by `docs/api/openapi.yaml` and the focused API guides.

## Shared Application Shell

Authenticated screens use the same desktop sidebar, mobile header and bottom
navigation, command palette, global activity affordance, and system status.

Every route:

- renders one logical `h1` and a route-specific browser title;
- has distinct loading, successful-empty, stale-data, and error states;
- keeps the last successful data visible when a refresh fails where it remains
  safe and useful;
- exposes a local retry for the failed resource instead of forcing a page
  reload;
- preserves keyboard focus and respects reduced-motion settings;
- asks before navigation or unload would discard a dirty editor or form.

The shared asynchronous and accessibility rules are specified in
[States, Badges, and Empty Cases](states-and-empty-cases.md) and
[Dynamic Interface Accessibility](accessibility-dynamic-status.md).

## Login

Route: `/login`

The login screen contains one password field and no username because Stacklab
has one logical local operator. Bootstrap password creation is an installation
task, not a browser registration flow.

- Empty submission is disabled.
- Invalid credentials, rate limiting, backend failures, and unreachable service
  errors are distinguishable without revealing password details.
- Successful login enters `/stacks`; an already authenticated session does not
  remain on `/login`.
- A password change revokes existing sessions and returns the current browser
  to login with an explanatory status.

## Stack Dashboard

Route: `/stacks`

The dashboard is a responsive tile grid. Each stack tile links to its overview
and shows:

- display state, activity state, service count, drift/invalid state, and
  unhealthy-container count;
- the latest available CPU and memory snapshot;
- image-update availability and last action when present;
- validated stack metadata links as separate external actions.

The toolbar provides instant name filtering and All, Problems, and Updates
views. `/` focuses the filter unless focus is already in an editable control.
`Check updates` starts a background job, shows bounded progress, and refreshes
the list at completion. Normal dashboard refreshes run only while the document
is visible and retain the last successful response on a later failure.

A successful empty response offers `Create your first stack`. A failed request
must never be presented as an empty installation.

## Stack Context And Overview

Routes:

- `/stacks/:stackId`
- `/stacks/:stackId/editor`
- `/stacks/:stackId/files`
- `/stacks/:stackId/logs`
- `/stacks/:stackId/stats`
- `/stacks/:stackId/terminal`
- `/stacks/:stackId/audit`

The shared stack shell owns the stack heading, status, metadata links, and tab
bar. It refetches when the URL changes to another stack ID and never presents
the previous stack under a new URL. Definition-dependent tabs are unavailable
for an orphaned stack; runtime diagnostics remain available when supported by
the backend capabilities.

The Overview renders service cards from the detail response. Each card shows
the service mode, image/build source, ports, container state and health, mounts,
and contextual Logs or Shell links. Mutating controls come from
`available_actions`; the frontend does not infer them from labels alone.

Actions are grouped into deployment, image, and disruptive sets. Stop, Down,
and definition/data removal require explicit review. A locked stack disables
its mutations but leaves read-only diagnostics and unrelated stacks usable.
Operation output remains visible after completion.

## Compose Editor

Route: `/stacks/:stackId/editor`

The editor manages canonical `compose.yaml` and root `.env` content, with a
resolved configuration pane.

- Compose and `.env` drafts form one save operation.
- Draft preview validates the current unsaved content without writing it.
- A failed definition or preview load does not replace previously loaded
  content with an empty editor.
- A stale revision conflict preserves the local draft and requires an explicit
  reload or retry decision.
- Save may succeed with a validation warning; present `Saved` separately from
  `Saved, but resolved config is invalid`.
- A successful save response supplies the job ID used to open the shared
  progress surface. A failed HTTP save is authoritative and may not expose that
  already-failed job ID, so the UI reports the request error without waiting
  for a subscription it cannot identify.
- Save & Deploy validates the draft, saves it, waits for save completion, and
  starts deployment only after a successful write.
- Save and preview failures keep both drafts intact.
- Successful save refreshes the definition, resolved preview, stack state, and
  capabilities while retaining job output.

Desktop uses editor and preview side by side. Narrow layouts stack the surfaces
without making the editor horizontally overflow the application shell.

## Managed Workspaces And Git

Routes:

- `/config`
- `/stacks/:stackId/files`

`/config` has Files and Changes modes. Files provides a managed-root tree and
text editor. Changes provides Git status grouped by stack where possible,
unified diff, per-file selection, local commit, and push status. Git operations
stay local-workspace-first; this is not a branch, merge, or reconciliation UI.

Rules shared by Config and Stack Files:

- non-text files show metadata and a read-only explanation;
- unreadable files remain visible with owner, group, mode, effective access,
  and a first-class blocked state;
- optimistic writes include the expected modification time and preserve the
  draft on a stale-file conflict;
- permission repair is explicit, limited to managed roots, and non-recursive
  by default;
- unsupported repair capability remains visible with its reason.

Stack Files applies the same model inside one stack root. Root `compose.yaml`
and root `.env` direct the operator to the Compose Editor; supporting text files
and nested `.env` files use the normal workspace editor.

Changes mode never hides an unreadable changed file. It disables diff or commit
selection only for that item, and group selection skips ineligible items. A
clean workspace, missing repository, absent upstream, rejected push, and
already-up-to-date push are separate states.

## Runtime Diagnostics

### Logs

Route: `/stacks/:stackId/logs`

Logs stream over WebSocket and support service selection, client-side text
filtering, pause/resume, clear, line wrapping, copy visible, and download
visible. A service deep link preselects that service. Pause uses a bounded
buffer; resume merges it without duplicating lines. The client retains at most
5,000 entries and distinguishes no output from no filter matches.

### Stats

Route: `/stacks/:stackId/stats`

Stats show stack totals and per-container CPU, memory, and network values with
short session-local trends. History exists only while the browser view is open,
is capped at 150 frames, and is not persisted in SQLite. Disconnect state is
visible while automatic reconnect runs; no running containers is a successful
empty state.

### Terminal

Route: `/stacks/:stackId/terminal`

Terminal opens an authenticated `docker exec -it` PTY for a selected running
container and an allowlisted shell. Container and shell controls are locked
while a session is active. Resize follows the terminal viewport and Disconnect
closes the PTY deliberately.

Transport reconnect and PTY lifetime are separate. Local scrollback survives a
transport interruption, but the UI only resumes when the backend session still
exists; otherwise it asks the operator to start a new session. Exit reasons
such as process exit, idle timeout, server cleanup, and connection replacement
remain distinguishable.

The backend enforces a 30-minute terminal idle timeout and at most five
concurrent PTYs per authenticated owner. Logout, session revocation, password
change, or server shutdown terminates owned PTYs. Host shell is not supported.

## Audit

Routes:

- `/audit`
- `/stacks/:stackId/audit`

Global Audit covers all operations; Stack History fixes the stack scope. Both
use URL-backed search, action, result, and date filters so a filtered view can
be refreshed or shared. Entries load newest first with an explicit Load more
control. An entry with a job ID opens the shared job detail; expired event
details leave the audit summary intact.

Loading and filter-empty results are different states. Refresh failures retain
already loaded entries and offer a retry.

## Create Stack

Route: `/stacks/new`

The screen creates a validated stack ID, canonical definition, and managed
config/data directories. The operator may start from a minimal blank Compose
file or a built-in template.

- Template loading failure does not block blank creation.
- Required template variables validate inline and render into an inspectable,
  editable Compose preview.
- Editing rendered Compose makes the resulting content—not the catalog
  template—the submitted definition.
- `Deploy immediately` chains deployment only after successful creation.
- Creation returns a job and follows the shared progress contract.

## Host And System Health

Route: `/host`

Host combines independent overview, metrics, system-health, and Stacklab-log
resources. A failure in one resource must not hide successful data from the
others.

The overview shows Stacklab build/runtime, OS/kernel/uptime, Docker/Compose, and
managed environment context. Metrics cover CPU/load, memory/swap, filesystems,
network, temperatures, disk I/O, and a bounded process view when available.
Unsupported host collectors degrade by section rather than failing the page.

Public IP is masked by default and may be disabled in Security settings. The
operator must explicitly reveal it in the current view. Stacklab logs support
severity/access filtering, text search, refresh/follow behavior, and clear
unavailable states on non-systemd hosts.

System Health summarizes backend readiness, Docker access, and this browser's
WebSocket connection. Each dependency has its own last-success context, retry,
and diagnostic link; `Check all` does not collapse distinct errors into one
generic status.

## Maintenance

Route: `/maintenance`

Maintenance contains Update, Images, Networks, Volumes, and Cleanup tabs. Tabs
mount on first activation and retain filters or running job state after being
visited. Inactive inventory tabs do not poll. Inventory search is debounced;
usage and origin filters update immediately.

Update and Cleanup use one deliberate sequence:

1. select targets and scope;
2. review impact and recovery guidance;
3. confirm;
4. follow step-aware progress and raw output;
5. retain the result and a link from recent runs.

Volumes are never selected for cleanup by default and require stronger review.
Built-in, stack-managed, and in-use networks or volumes explain why removal is
blocked. External object creation stays narrow; arbitrary Docker CRUD is not
part of Maintenance.

## Docker Admin

Route: `/docker`

Docker Admin shows service-manager status, Engine metadata, daemon
configuration, the managed settings form, and registry authentication.

- Service, Engine, daemon config, and registry status load independently.
- Missing, unreadable, and invalid `daemon.json` are distinct states.
- Raw daemon JSON is view-only; managed settings are the only edit surface.
- Apply requires a successful preview and an operation review covering changed
  keys, Docker restart impact, backup, rollback, and manual recovery risk.
- Unsupported privileged capability stays visible with its reason.
- Registry status exposes registry names and effective config location but no
  passwords, tokens, or decoded auth values.
- Login result is followed through the shared job UI; logout always confirms
  the named registry.

## Settings

Routes:

- `/settings/security`
- `/settings/notifications`
- `/settings/automation`
- `/settings/updates`
- `/settings/about`

`/settings` redirects to Security. The shared task navigation remains visible
while only the active task loads its resource. Dirty tasks guard navigation.

- **Security:** change the operator password and manage host-observability
  privacy preferences.
- **Notifications:** configure Webhook and Telegram separately, test each
  channel using current draft values, and group job, maintenance, runtime, and
  Stacklab self-health events. Feedback is inline; an empty secret field keeps
  an already configured Telegram token.
- **Automation:** edit fixed update and cleanup schedules with host-local time,
  daily/weekly cadence, scope/exclusions, next/last status, and job links.
  Scheduled volume deletion requires high-impact review.
- **Updates:** show installed/candidate version, channel, install mode,
  capability, active job, and last result. Self-update is reviewed before it
  starts; unsupported non-APT installs receive a direct explanation.
- **About:** present application, build, Docker, Compose, and managed-environment
  information without edit affordances.

## Operation Progress And Job Detail

Page-local progress stays with the workflow that started a mutation. Global
activity keeps active and recently completed work visible after navigation and
opens the shared job-detail drawer.

- Open progress as soon as a mutation returns a job ID.
- Use the REST snapshot for current state and WebSocket subscription for replay
  and live events.
- Show action, target, step, elapsed time, warnings, raw output, and terminal
  state without announcing every appended log line.
- Allow navigation while work continues and keep unrelated resources usable.
- Offer cancellation only for cancellable queued/running work and expose the
  intermediate Cancelling state.
- Audit, stack history, schedules, Maintenance, and global activity open the
  same detail surface.
- If retained events have expired, keep the summary and show a calm retention
  note instead of a failure.
- Reconnect and REST fallback must not duplicate events or overwrite a newer
  terminal state with an older snapshot.

Destructive and high-impact actions use the shared operation review pattern:
scope, targets, effect, snapshot/backup where available, recovery path, and
residual risk appear before the final confirmation.
