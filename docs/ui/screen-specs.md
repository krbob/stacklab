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

The dashboard defaults to a responsive grid of cards. A `Cards` / `List`
switch selects a compact table, with the choice remembered in this browser.
Both views share filtering, sorting, and resource history. Each stack links to
its overview and shows:

- display and activity state, running/defined services, drift/invalid state,
  and healthy, unhealthy, and unchecked container counts;
- the latest CPU and memory snapshot, including real zero values and an explicit
  missing-sample state;
- image-update availability and last action result with a relative timestamp;
- validated stack metadata links as separate external actions.

Cards include separate CPU and RAM trends for the last 60 seconds. The table
includes a CPU trend and sortable Stack, CPU, and RAM column headers. On narrow
screens the table scrolls within a labeled, keyboard-focusable region.
History accumulates from observed samples while this dashboard is mounted,
keeps at most 60 points per stack, and is not persisted. Duplicate or older
server samples do not add points; polling gaps remain visible. An initial
sample is labeled as collecting history rather than drawing an invented trend.

The toolbar provides instant name filtering and All, Problems, and Updates
views, plus sorting by name (A–Z), CPU, or RAM (highest usage first). Resource
ties use name order; stacks without samples follow stacks with measured usage.
The selected order applies to new samples and filtered results.
`/` focuses the filter unless focus is already in an editable control.
`Check updates` starts a background job, shows bounded progress, and refreshes
the list at completion. Normal dashboard refreshes run only while the document
is visible and retain the last successful response on a later failure. CPU and
RAM refresh every second from the cached stats endpoint, independently of the
full inventory refresh every 10 seconds. Slow stats requests never overlap,
and returning to a visible tab requests a fresh sample immediately.
Background inventory refreshes are silent: no `Refreshing…` row is inserted
above the cards or table. Initial loading and refresh errors remain visible.

When updates are available, `Update all (N)` reviews every stack with a detected
update, including stacks hidden by the current filters. Confirmation keeps that
reviewed target fixed and starts one tracked maintenance job: pull images and
redeploy active stacks, preserving stopped and never-started stacks. It does
not request builds, orphan removal, or pruning. The job details open immediately;
the dashboard shows progress and the terminal result, then refreshes its badges.
Start failures keep the review open for retry.

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
- `/stacks/:stackId/backups`
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
  starts deployment only after a successful write. For an existing runtime,
  including stopped containers, it uses `recreate` so changes to inline
  `configs.content` are applied even when Compose's ordinary `up` considers
  the containers unchanged. Mounted volumes are preserved. A stack with no
  containers uses `up` for its first deployment.
- Save and preview failures keep both drafts intact.
- Successful save refreshes the definition, resolved preview, stack state, and
  capabilities while retaining job output.

Desktop uses editor and preview side by side. Narrow layouts stack the surfaces
without making the editor horizontally overflow the application shell.

## Managed Workspaces And Git

Routes:

- `/config`
- `/config?path=:workspacePath`
- `/stacks/:stackId/files`

`/config` has Files and Changes modes. Files provides a managed-root tree and
text editor. Changes provides Git status grouped by stack where possible,
unified diff, per-file selection, local commit, and push status. Git operations
stay local-workspace-first; this is not a branch, merge, or reconciliation UI.

Rules shared by Config and Stack files:

- non-text files show metadata and a read-only explanation;
- unreadable files remain visible with owner, group, mode, effective access,
  and a first-class blocked state;
- optimistic writes include the expected modification time and preserve the
  draft on a stale-file conflict;
- permission repair is explicit, limited to managed roots, and non-recursive
  by default;
- unsupported repair capability remains visible with its reason.

Stack files applies the same model inside one stack root. It displays the
stack-local and managed-config roots explicitly. Root `compose.yaml` and root
`.env` direct the operator to the Compose Editor; supporting text files and
nested `.env` files use the normal workspace editor. Managed configuration
remains a separate workspace, reached through an `Open managed config` deep
link to `/config?path=<stack-id>`. Config directory navigation keeps the
current path in the URL and displays its resolved filesystem location. A
missing deep-linked directory offers a return to the config root.

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

Creation failures keep the draft editable and display the failed job's retained
output when the response includes a job ID. Permission errors in the managed
`stacks`, `config`, or `data` directories must be diagnosable from this output.

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

## Backups And Recovery

Routes: `/backups` and `/stacks/:stackId/backups`.

The global screen opens on **Protection**. Its keyboard-operated tabs are
Protection, Recovery, History, Plans, and Destinations. The URL retains the tab;
old `view=runs` links open History. Refresh errors preserve the previous response
and missing native tools are reported explicitly. Visited panels remain mounted,
preserving Recovery selection/progress and History filters. Background refreshes
do not insert a layout-shifting message, and overview polling pauses in hidden
browser tabs. Errors belong to the action that produced them; another review
starts without that action's error.

Adjacent summary and workflow panels use a consistent vertical gap, including
both sides of Protection status and the two destination recovery steps. Warnings,
copy metadata and action groups have their own spacing. On narrow screens, the
summary uses two columns, tabs wrap so every view remains visible, and panel
actions sit below the heading. Plan, destination and history card titles share
one text style; long names wrap in cards and dialogs. Backup form selects retain
native keyboard behavior while matching text-field height and padding in Chromium
and WebKit.

- **Protection:** show verified copies and their schedule compliance separately
  from configured source selection. Each plan lists destination-specific last
  capture, next occurrence, errors, snapshot presence and its observation time,
  repository availability/integrity, and a recovery test of that exact snapshot.
  A destination-wide exercise is not evidence for an unrelated plan or snapshot.
  Missing snapshots, failed integrity checks, unavailable repositories, clock
  anomalies and incomplete verification cannot appear as healthy protection.
  Manual copies have a separate count, a neutral badge and no automatic freshness
  claim. Changed selections require a
  fresh capture; a failed newer copy remains visible beside an older usable copy.
  Fleet source discovery identifies missing, disabled, narrowed and changed
  selections. Discovery failures make that inventory incomplete; they never
  produce an all-covered message. Refresh and configuration changes invalidate
  the coverage inventory together with the overview.
- **Destinations:** the new-destination flow has Location, Credentials & policy,
  and Verify steps. Typed S3/SFTP/rclone fields assemble the repository address;
  an advanced address input remains available. Local filesystem detection checks
  the intended mount. SFTP fields preserve absolute versus home-relative paths,
  nondefault ports and IPv6. Existing addresses are never rewritten. Passwords
  can be generated only for new destinations, then shown and copied;
  recovery credentials must also be saved independently of this host. Secret
  fields are write-only, and empty edit fields retain credentials. Repository
  type and address are immutable during editing. Password replacement requires an
  explicit choice and a value matching the existing repository; it does not rotate
  encryption keys. Saving a replacement leads to access testing.
  Save, test existing access, and
  initialize a new repository remain separate actions. Verification retains its
  job result in the wizard; successful verification leads into plan creation.
- **Plans:** select sources explicitly by stack, service and mount, with source
  search and an explicit select-visible action. Review exclusions across scopes.
  Stop-and-capture is the initial consistency choice; live capture explains
  active-database limits. Each destination has independent retention and a
  manual, interval, daily or weekly schedule. Interval cadence is fixed in UTC;
  wall-clock schedules show their timezone and authoritative next occurrences.
  New or changed schedules start at the next future occurrence; existing missed
  schedules catch up once. Preview dates use the displayed timezone independently
  of the browser; an unresolved host-zone name uses a labelled UTC preview.
  Retention 0/0/0 keeps every snapshot. The capture downtime limit includes stopping
  writers and reading their data. Unknown destination assignments remain visible
  and removable. Plan and destination editors confirm discarding unsaved changes,
  including Escape and backdrop dismissal.
  New plan creation leads into the first-run review. Manual runs load concrete
  running writers, including shared data, before enabling confirmation. Failed
  preflight blocks the action; execution resolves service state again.
- **History:** search retained history and filter by plan, result, destination and
  UTC date range. Ten runs per page expose capture/upload/verification/retention
  stages, capture time, service downtime, processed bytes, added data and
  repository-item counts. Repository items are staged artifacts, not original
  application file counts. Successful copies remain visible after another copy
  fails; retry targets failed copies until the staging deadline.
- **Repository maintenance:** checks choose metadata, a data subset or all stored
  data and have their own schedule. Retention cleanup can run independently on a
  schedule. Access, integrity-check and last-operation results remain distinct.
  Repository checking never implies a successful application recovery test.
  Manual cleanup has a cost/locking review. Missing native tools disable commands
  requiring them while configuration and local recovery inspection remain usable.
  An unmeasured repository size is shown as unknown, not zero bytes.
- **Recovery:** one workspace connects destination and snapshot selection to
  source browsing, recursive search, paged file metadata and snapshot comparison.
  Comparison distinguishes content and metadata changes. File indexes are inside
  encrypted snapshots. Search controls retain focus during loading; stale results
  cannot submit file selections for another source or query. Missing or oversized
  indexes explain the limitation without implying missing backup data. Local index
  generation after retrieval is also bounded; original archive downloads remain
  the offline inspection path when browsing is unavailable.
  Selected regular files (up to 100), a complete
  source, or the whole recovery set can be restored. Partial restores cannot be
  labelled complete tests. The interface explains that selected files require
  retrieval of complete source archives and displays estimated transfer size.
  Full offline retrieval is separate from a complete recovery exercise; restricted
  automatic-extraction metadata must not earn a completed-test label.
- **Completed restores:** registered results remain selectable after navigation.
  The result matching a just-completed job is selected automatically; directory
  paths never need to be copied from logs. Browse restored files and download
  regular files through registered identities. Missing directories remain visible
  as unavailable and cannot be applied. File-only restores are for download;
  applying to a stack requires a complete recovered source. Complete sources offer
  original archive downloads with offline-recovery restrictions visible. Deleting
  a managed result removes its local directory after confirmation. Forgetting a
  custom result removes only its registration and explicitly preserves every file.
  Neither action removes repository snapshots.
- **Apply recovered data:** the selected registration supplies its manifest.
  Choose only sources actually present in that restore and explicitly map
  directories or existing local volumes; no target or source is preselected.
  Manifest loading has visible progress and retry. Complete directory mappings
  default to replacement, removing entries absent from the archive. Explicit merge
  preserves extra files and warns against using it for exact database recovery.
  Single-file sources always merge into their parent so sibling files survive.
  Offline-only sources cannot be selected for automatic apply.
  Server preview supplies affected writers, overwrite/removal scope and warnings. A
  separate `RESTORE` confirmation sends a short-lived token. Changing mappings
  discards review. Focus moves to the confirmation field once review is ready.
  Writers remain stopped by default. Resuming previously running writers is explicit.
  Recovery of Stacklab's own database remains an offline runbook operation.
- **Backup settings and service recovery:** a global pause covers automatic copies
  and repository checks; recovery monitoring and safe local cleanup continue.
  Recovered state on a different host requires reviewing and
  acknowledging sources, repositories and service mappings before resuming.
  Settings expose overdue grace and a write-only external success-heartbeat URL;
  the independent monitor must alert when heartbeats stop. A degraded backup module
  retains the panel and exposes unresolved service recovery records. Retry uses
  recorded container identities. A held restore requires typed inspection
  acknowledgement, keeps services stopped and preserves rollback files. Corrupt
  records expose a diagnostic without an automatic resolution action.

The stack tab shows configured, unconfigured, disabled and changed-source
coverage, with exclusions and plan links. Configured selection uses a neutral
badge; verified protection is assessed in the global Protection view. New mounts
never become selected implicitly. Operations remain connected to the shared job
drawer and the Recovery workspace also keeps its local restore progress.

## Operation Progress And Job Detail

Page-local progress stays with the workflow that started a mutation. Global
activity keeps active and recently completed work visible after navigation and
opens the shared job-detail drawer.

- Open progress as soon as a mutation returns a job ID.
- Use the REST snapshot for current state and WebSocket subscription for replay
  and live events.
- Show action, target, step, elapsed time, warnings, raw output, and terminal
  state without announcing every appended log line.
- Preserve spaces in live progress details and spaces/newlines in raw output,
  so Docker's text progress bars keep a constant width as they fill.
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

Detailed backup completion, integrity, maintenance and recovery states are defined
in [states and empty cases](states-and-empty-cases.md#backup-protection-and-recovery).
