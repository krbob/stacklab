# Development Plan

This document turns the current product direction into an executable sequence of milestones.

It is intentionally short-term and implementation-oriented.

## Guiding Principle

Work on product features before `.deb` packaging and APT distribution.

Reason:

- Stacklab already has a validated tarball release and upgrade path
- the biggest product value still sits in operator workflows
- package distribution is important, but currently lower leverage than the next feature set

## Recommended Sequence

## Milestone 1: Host Observability

Goal:

- make Stacklab itself and the managed host observable from the UI

Scope:

- show Stacklab version and build metadata in the UI
- add host overview data:
  - hostname
  - OS / kernel
  - uptime
  - CPU
  - memory
  - disk
  - Docker version
  - Compose version
- add Stacklab service log viewer backed by `journalctl -u stacklab`

Backend work:

- extend host/system read model
- add REST endpoints for host overview and Stacklab service logs
- support polling or lightweight streaming for service logs
- add tests for host metadata and journald-backed reads

UI work:

- host overview page or dashboard section
- version display in settings / footer / host page
- Stacklab log viewer with basic filtering and refresh/stream mode

UI developer input needed:

- after backend contract draft exists
- before implementing the host overview layout and log viewer UX

## Milestone 2: Config Workspace

Goal:

- make `/opt/stacklab/config` a first-class, safe workspace inside Stacklab

Scope:

- config tree view limited to `/opt/stacklab/config`
- file browser for directories and files
- text editor for common text-based config files
- read-only fallback for unknown/binary files
- save + audit integration

Backend work:

- define safe filesystem boundary under `/opt/stacklab/config`
- expose browse/read/write endpoints
- content-type / binary detection
- file validation rules and path traversal hardening
- audit entries for config edits
- tests for browse/read/write and path safety

UI work:

- information architecture for config workspace
- tree + editor layout
- save/discard flow
- file-type handling states

UI developer input needed:

- early
- needed before finalizing IA and route structure

## Milestone 3: Local Git Workspace Visibility

Goal:

- expose what changed locally in `/opt/stacklab/stacks` and `/opt/stacklab/config`

Scope:

- workspace Git status
- changed/untracked files
- diff against working tree / HEAD
- stack-oriented and file-oriented views

Backend work:

- define Git workspace root and safety assumptions
- add read-only Git status and diff endpoints
- map changed files to stack/config contexts
- add tests for clean/dirty/untracked/diff scenarios

UI work:

- status badges and changed-files views
- diff viewer
- integration with stack detail and config workspace

UI developer input needed:

- after API and domain semantics are clear
- before finalizing diff UX

## Milestone 4: Local Git Commit And Push

Goal:

- let operators persist local workspace changes to GitHub without leaving Stacklab

Scope:

- commit message flow
- commit all or selected files
- push to configured remote
- clear error reporting for auth / remote failures

Not in this milestone:

- pull, merge, rebase
- branch switching
- conflict resolution

Backend work:

- commit endpoint(s)
- push endpoint
- clear Git error mapping
- audit integration
- tests using temporary local/remote Git repos

UI work:

- changed-files selection
- commit modal
- push result and error states

UI developer input needed:

- early in the milestone
- commit/push UX is not something backend should guess

## Milestone 5: Safe Maintenance

Goal:

- give operators safe maintenance tools without becoming a generic Docker console

Scope:

- image inventory
- manual prune workflows
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

`.deb` packaging and later APT publication should start after Milestones 1-3 are substantially complete.

Reason:

- those milestones shape runtime metadata, workspace assumptions, and config surface
- packaging too early would lock operational details before the product shape settles

Suggested order:

1. tarball flow remains the primary install path
2. `.deb` artifact spike
3. Debian install/upgrade validation
4. signed APT repository later

## Recommended Immediate Next Step

Start Milestone 3.

Concrete first deliverables:

1. backend contract draft for Git workspace status and diff
2. UI handoff for `Changes` mode inside `/config`
3. backend implementation and tests for read-only Git visibility
