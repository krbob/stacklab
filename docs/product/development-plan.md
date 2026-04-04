# Development Plan

This document turns the current product direction into an executable sequence of milestones.

It is intentionally short-term and implementation-oriented.

## Guiding Principle

Work on product features before `.deb` packaging and APT distribution.

Reason:

- Stacklab already has a validated tarball release and upgrade path
- the biggest product value still sits in operator workflows
- package distribution is important, but currently lower leverage than the next feature set

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
- later add explicit repair workflows restricted to managed roots

Non-goal:

- running the main Stacklab service as `root` by default

Backend work:

- enrich workspace and Git models with permission diagnostics
- detect blocked reads and blocked writes cleanly
- define a future repair interface that stays scoped to managed roots

UI work:

- blocked-file states
- ownership/mode messaging
- repair entry point later, once backend model exists

UI developer input needed:

- after backend exposes concrete blocked-file semantics

## Milestone 7: Broader Maintenance Inventory

Goal:

- add maintenance-oriented visibility beyond stack lifecycle without becoming a generic Docker console

Scope:

- manual prune workflows with explicit scope and preview
- image inventory and selective image maintenance
- read-only networks and volumes inventory
- limited actions only where they directly support Compose operations

Backend work:

- inventory endpoints
- prune preview/execution model
- destructive action guardrails
- tests around Docker-dependent maintenance flows

UI work:

- maintenance dashboard/views
- destructive confirmation UX
- inventory filters and relationships

UI developer input needed:

- after backend model stabilizes
- especially for prune UX and destructive affordances

## Packaging Track

`.deb` packaging and later APT publication should start after Milestones 4-5 are substantially complete.

Reason:

- those milestones shape Git assumptions, maintenance workflows, and operator expectations
- packaging too early would lock operational details before the product shape settles

Suggested order:

1. tarball flow remains the primary install path
2. `.deb` artifact spike
3. Debian install/upgrade validation
4. signed APT repository later

## Recommended Immediate Next Step

Complete Milestone 6 UI blocked-file states on top of the stable backend semantics, then move to Milestone 7.
