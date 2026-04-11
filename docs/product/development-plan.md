# Development Plan

This document turns the current product direction into an executable sequence of milestones.

This document is now partly historical. For the current product state and next
priorities, use `docs/roadmap.md` first.

## Guiding Principle

Keep product work and release hygiene moving together.

Reason:

- the daily operator loop is now broad enough that install/update reliability matters
- Stacklab has package-managed installs, APT channels, and self-update in active use
- new product surfaces should not outpace the release path that operators use to test them

Current near-term sequence:

1. keep release hygiene healthy:
   - APT package retention
   - nightly prerelease cleanup
   - post-publish smoke
2. add a focused template library / starter catalog

## Completed Foundations

The following milestones are already materially in place:

- Host Observability
  - Stacklab version/build metadata in the UI
  - host overview page
  - Stacklab service log viewer
- Config Workspace
  - safe browse/read/write under `/opt/stacklab/config`
  - audit integration
- Local Git Workspace Visibility
  - read-only Git status and diff backend
  - UI integration in `/config`
- Local Git Commit And Push
  - per-file commit selection
  - upstream-aware push flow
  - audit integration
- Safe Bulk Maintenance
  - selected/all stack update workflow
  - optional prune
  - progress and audit integration
- Operational Data Retention
  - bounded SQLite retention for audit, job summaries, job events, and sessions
  - detailed job output purged earlier than audit/job summaries
- Frontend-Only Stats History
  - stack-level session history charts
  - no backend metric retention in v1

For the fuller current baseline, including APT release automation,
self-update, Docker administration, notifications, scheduled maintenance,
stack auxiliary files, and maintenance inventory, see `docs/roadmap.md`.

## Recommended Sequence

## Milestone 4: Safe Bulk Maintenance

Goal:

- replace ad-hoc host scripts such as `update_stacks.sh` with a first-class Stacklab workflow

Scope:

- update selected stacks or all stacks in one workflow
- explicit step model:
  - pull
  - build when needed
  - `up -d --remove-orphans`
- optional prune step, never implicit by default
- result visibility per step, including warnings and partial failures

Backend work:

- maintenance workflow endpoint(s)
- step-oriented job model for bulk stack updates
- target selection for one, many, or all stacks
- tests for multi-stack pull/build/up flows

UI work:

- maintenance entry point
- stack selection UX
- progress and result breakdown
- optional prune confirmation

UI developer input needed:

- early
- selection UX and bulk progress presentation should not be guessed by backend

## Milestone 5: Local Git Commit And Push

Goal:

- let operators persist local workspace changes to GitHub without leaving Stacklab

Scope:

- commit message flow
- per-file selection as the primary write model
- stack-scoped quick selection as a convenience:
  - `stacks/<stack_id>/**`
  - `config/<stack_id>/**`
- push to configured remote
- clear error reporting for auth / remote failures

Not in this milestone:

- pull, merge, rebase
- branch switching
- conflict resolution

Backend work:

- commit endpoint(s)
- push endpoint
- selection model for files and stack-scoped presets
- clear Git error mapping
- audit integration
- tests using temporary local/remote Git repos

UI work:

- changed-files selection
- stack-level quick actions
- commit modal
- push result and error states

UI developer input needed:

- early in the milestone
- commit/push UX and selection affordances should be designed intentionally

## Milestone 6: Workspace Permission Diagnostics And Repair

Goal:

- make container-created ownership and permission problems visible and recoverable without running Stacklab as `root`

Scope:

- surface unreadable and unwritable files in config and Git views
- show owner, group, and mode where possible
- base diagnostics on the current file inode and effective access, not on assumed ACL inheritance
- explain why some files cannot be diffed, edited, or committed
- add explicit helper-backed repair workflows restricted to managed roots

Non-goal:

- running the main Stacklab service as `root` by default

Backend work:

- enrich workspace and Git models with permission diagnostics
- detect blocked reads and blocked writes cleanly
- expose helper-backed repair endpoints for config and stack workspaces
- keep repair constrained to managed roots and explicit target paths

UI work:

- blocked-file states
- ownership/mode messaging
- repair entry points in blocked file states after backend contract is available

UI developer input needed:

- after backend exposes concrete repair capability and response semantics

## Milestone 7: Maintenance Inventory And Cleanup

Goal:

- add maintenance-oriented visibility beyond stack lifecycle without becoming a generic Docker console

Scope:

- image inventory first
- manual prune workflows with explicit scope and preview
- read-only networks and volumes inventory later
- limited actions only where they directly support Compose operations

Backend work:

- image inventory endpoint(s)
- prune preview/execution model
- global prune job model
- audit integration for cleanup jobs
- destructive action guardrails
- tests around Docker-dependent maintenance flows

UI work:

- `Images` and `Cleanup` sections inside `/maintenance`
- destructive confirmation UX
- inventory filters and relationships

UI developer input needed:

- early for `Images` vs `Cleanup` IA inside `/maintenance`
- again after prune preview shape stabilizes

## Milestone 8: Docker Administration Surface

Goal:

- remove routine SSH usage for common Docker daemon administration without turning Stacklab into a generic host shell

Scope:

- read-only Docker service status and daemon configuration visibility first
- controlled editing of selected Docker daemon settings later
- explicit apply workflow:
  - validate config
  - write backup
  - save `daemon.json`
  - restart Docker
  - verify recovery
  - roll back automatically if restart fails

First concrete use case:

- managing Docker DNS settings such as:
  - `"dns": ["192.168.1.2"]`

Non-goals in this milestone:

- arbitrary host file editing outside the Docker admin surface
- general-purpose shell access
- editing every possible Docker daemon key in v1

Backend work:

- Docker service status endpoint(s)
- read-only `daemon.json` endpoint
- constrained write/apply endpoint(s) for daemon settings
- backup/rollback model
- privileged helper or tightly allowlisted `sudo` execution path
- tests for config validation and Docker restart recovery behavior

UI work:

- Docker Admin area under host/maintenance-oriented navigation
- read-only daemon config visibility
- guided edit/apply UX with strong warnings
- restart/rollback result surfacing

UI developer input needed:

- after the backend contract is sketched
- the information architecture should decide whether this lives under `/host`, `/maintenance`, or a later dedicated Docker admin surface

## Milestone 9: Global Background Activity UX

Goal:

- make long-running and background jobs visible across the whole application instead of only at the point where they were started

Scope:

- persistent global activity indicator while a job is running
- visible state after the triggering button returns to idle
- elapsed time for running jobs
- current step and current target where the backend exposes them
- clear completion, warning, and failure surfacing
- later optional expansion into notifications or an activity center

Initial product fit:

- maintenance update and cleanup
- stack actions such as `pull`, `build`, `up`, `restart`
- save/deploy flows
- Git push and other workspace-level jobs when they take longer than a quick request/response

Non-goals in the first version:

- full notification center
- release-management UX

## Milestone 10: Debian Package Build

Goal:

- produce native `.deb` artifacts for supported Debian architectures

Scope:

- package-managed runtime payload
- service account creation
- `systemd` unit installation
- dependency declaration:
  - `systemd`
  - Docker Engine
  - Compose
  - `git`
- install and upgrade validation on Debian `amd64` and `arm64`

Backend / release work:

- packaging layout under Debian-native paths
- maintainer scripts
- `.deb` build workflow
- Debian install/upgrade smoke

UI developer input needed:

- none initially

## Milestone 11: Stable Release And APT Publication

Goal:

- turn release artifacts into a repeatable operator-facing release channel

Scope:

- manual stable release workflow
- Git tags and GitHub Releases
- signed APT `stable` repository
- documented hotfix release path

Backend / release work:

- stable release workflow
- release notes generation
- APT metadata generation and signing
- release validation and rollback documentation

UI developer input needed:

- none

## Milestone 12: Nightly And Monthly Release Automation

Goal:

- automate publication once packaging and release validation are proven

Scope:

- nightly prereleases when `main` changed
- APT `nightly` publication
- automatic monthly stable release on the `1st`
- selective Renovate automerge for low-risk dependency classes only

## Milestone 13: Mobile Notifications And Post-Update Alerts

Goal:

- get important operator-facing failures onto the phone without requiring the UI to stay open

Scope:

- keep webhook notifications as the generic baseline
- add Telegram as the first native mobile delivery channel
- evaluate `ntfy` and `Gotify` later as homelab-friendly self-hosted channels
- add post-update recovery alerts when a maintenance workflow completes but a stack does not return to a healthy state

Recommended event order:

1. post-update stack recovery failed
2. job failed
3. job succeeded with warnings
4. Stacklab self-health alerts from `journald`
5. later runtime health degradation:
   - unhealthy containers
   - restart loops
   - stack transitions into degraded states
6. runtime log error bursts:
   - repeated new error-like log lines from managed containers
   - cooldown and baseline seeding to avoid spam on startup

Non-goals in the first mobile alert slice:

- WhatsApp native integration
- log-anomaly alerting
- notification inbox in the UI
- templating or batching

Follow-up slice after the first Telegram rollout:

- `stacklab_service_error` event sourced from the `stacklab` systemd unit logs
- thresholded and deduplicated delivery, for example:
  - `N` error or fatal entries in `M` minutes
  - suppress repeated identical messages for a cooldown window
- operator-facing copy focused on "Stacklab itself is unhealthy", not raw journald mechanics
- then add runtime log error bursts sourced from managed container logs
- keep the first version heuristic:
  - repeated `error` / `fatal` / `panic` style lines
  - no regex editor or per-service rules

Backend work:

- notification channel abstraction above the existing webhook sender
- Telegram channel support with test send
- post-update verification logic tied to maintenance completion
- Stacklab service error detector later, using journald access already present for Host Observability
- debounced runtime health alert model only after post-update and Stacklab self-health alerts are stable

UI developer input needed:

- after the Telegram/settings contract is ready
- the first UI slice still lives inside `/settings`, not on a dedicated notifications page

Important constraint:

- release day publishes the already-green state of `main`
- it does not merge a batch of risky updates

Backend / release work:

- nightly workflow
- scheduled stable workflow
- change detection for nightly builds
- release guardrails and fallback manual dispatch

UI developer input needed:

- none

- full historical job browser replacing audit
- desktop-style notification center
- exact Dockge-style pull telemetry for every image layer

Backend work:

- expose enough job metadata for elapsed time and richer status chips
- later consider structured progress events for pull/build-heavy flows

UI work:

- global activity affordance in app chrome
- reusable background job presentation model
- consistent post-start behavior so actions do not appear to "disappear" after button disable/enable transitions

UI developer input needed:

- early
- this is primarily a cross-app interaction and information-architecture problem, not just a component styling task

## Milestone 10: Richer Maintenance Progress

Goal:

- make long-running maintenance workflows feel active and legible without pretending to be a full Docker pull terminal

Scope:

- replace the current bare step list with step cards
- show elapsed time per workflow step
- attach raw `job_log` output to the corresponding step card
- keep output collapsed by default with expand-on-demand

Important constraint:

- use the current job event model first
- do not block on structured image-layer progress or ANSI parsing

Backend work:

- none required for the first slice if current `job_log` events continue carrying `step`
- only document and preserve the current contract semantics

UI work:

- step-card presentation for update and cleanup workflows
- elapsed time rendering from existing event timestamps
- step-local raw output rendering

UI developer input needed:

- early, but now already directionally decided:
  - step cards over timeline-only list
  - elapsed time per step
  - step-local collapsible output

## Milestone 14: Scheduled Maintenance Policies

Goal:

- let operators move routine update and cleanup windows into Stacklab without leaving the product for host cron or ad-hoc timers

Scope:

- one scheduled update policy
- one scheduled prune policy
- host-local time only
- cadence:
  - daily
  - weekly
- runtime status:
  - next run
  - last run
  - skipped/failed/succeeded result

Important constraint:

- do not introduce a generic cron editor
- do not auto-retry skipped runs in the first slice
- keep automatic prune explicit and separately configurable

Backend work:

- persistent schedule settings in SQLite
- background dispatcher inside Stacklab
- reuse existing `update_stacks` and `prune` workflows
- scheduled runs should feed:
  - jobs
  - audit
  - global activity
  - notifications

UI developer input needed:

- yes
- placement:
  - `/settings`
  - or `/maintenance`
- shape:
  - two fixed cards
  - or a lightweight policy list

## Packaging Track

`.deb` packaging and later APT publication should start after the next product-shaping operator milestones are substantially complete.

Reason:

- those milestones shape Git assumptions, maintenance workflows, and operator expectations
- Docker administration may influence package dependencies, service privileges, and documentation
- packaging too early would lock operational details before the product shape settles

Suggested order:

1. `.deb` and APT remain the primary install path on Debian-family hosts
2. tarball remains the secondary manual path for other Linux distributions
3. migration between install modes stays out of scope
4. release hygiene should validate both install modes, with `.deb` as the stable release gate

## Recommended Immediate Next Step

Implement the first Milestone 9 backend slice:

1. `GET /api/jobs/active`
2. return the latest event and current step for each active job
3. hand the concrete active-job model to the UI developer for the global chrome activity affordance

Then decide the final UI shape:

- compact pill vs slim bar
- popover vs tray vs drawer
- collapsed single-job summary vs aggregate count-first summary

## Milestone 11: Stack Auxiliary Files

Goal:

- cover stack-local helper files such as `Dockerfile` and nested config without turning the stack area into a generic file manager

Scope:

- browse auxiliary files under `stacks/<stack_id>/`
- text editing for supported files
- blocked-file diagnostics reuse from existing workspace flows
- keep `compose.yaml` and root `.env` in the dedicated stack editor

Backend work:

- stack-scoped workspace endpoint(s)
- reserved canonical file handling
- atomic save path for auxiliary text files
- audit integration with `save_stack_file`
- tests for path safety, reserved files, binary detection, and permissions

UI work:

- place the feature inside the stack surface
- file tree + editor/read-only preview
- clear affordance back to the main Compose editor

UI developer input needed:

- early
- the main decision is whether this is a new stack tab or a mode inside the existing editor

## Milestone 16: Stacklab Self-Update

Goal:

- let operators upgrade Stacklab itself from the UI on APT-managed installs

Scope:

- current version and package channel visibility
- candidate version visibility
- helper-backed `apt` upgrade workflow for the `stacklab` package only
- restart verification after upgrade
- result visibility through jobs, global activity, and job detail

Non-goals:

- tarball self-update
- host-wide package management
- reboot management
- package rollback UI

Backend work:

- self-update overview endpoint
- self-update apply endpoint
- detached helper that survives process restart and writes workflow state back to SQLite
- reconciliation on restart for audit and notifications
- packaging and docs for the helper and sudoers example

UI work:

- Stacklab update card in application settings
- unsupported/degraded states for tarball installs or missing helper capability
- explicit `Update Stacklab` action and runtime/result visibility

UI developer input needed:

- yes
- final placement and affordances inside `/settings`
- how much advanced control to expose in v1 versus keeping update as one primary action
