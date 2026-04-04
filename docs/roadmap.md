# Product Roadmap

This roadmap tracks product direction after the MVP baseline became functional.

For the reasoning behind the priorities, feature borrowing, and scope boundaries, see:

- `docs/product/feature-strategy.md`

## Current Baseline

Implemented and already exercised on Linux staging hosts:

- single-user authentication and sessions
- stack discovery from filesystem + Docker runtime
- stack detail views for services and containers
- Compose editor with `.env` support and resolved-config preview
- lifecycle actions with progress streaming and audit log
- live logs, stats, and container terminal
- stack create/delete flows
- host overview and Stacklab service log viewer
- config workspace for `/opt/stacklab/config`
- browser E2E, Docker-backed integration tests, and staging validation on Linux `amd64` and `arm64`

## Near-Term Product Goals

### 1. Safe Maintenance That Replaces Ad-Hoc Scripts

- selected/all stack update workflow that replaces ad-hoc scripts such as `update_stacks.sh`
- explicit step model:
  - pull
  - build when needed
  - `up -d --remove-orphans`
- optional prune step, never implicit by default
- improve visibility of why maintenance actions succeeded, warned, or failed
- improve global visibility of background jobs across the whole app:
  - persistent activity indicator
  - current step / current target
  - elapsed time
  - completion and failure states that remain visible after the triggering button stops being active
- add optional job and maintenance notifications

### 2. Git-Aware Workspace Writes

- read-only Git workspace status for `/opt/stacklab/stacks` and `/opt/stacklab/config`
- diff against working tree / HEAD for stack definitions and config files
- simple commit and push workflow for the local Stacklab workspace
- per-file selection as the primary write model
- stack-scoped quick selection as a convenience, not the only option

### 3. Workspace Permission Diagnostics

- surface unreadable or unwritable files created by containers
- show ownership and mode details where access is blocked
- add explicit repair workflows later, restricted to managed roots
- keep Stacklab itself on a non-root service model by default

## Mid-Term Product Goals

- manual prune workflows with explicit scope and preview
- image inventory and selective image maintenance
- Debian-native `.deb` packaging for `amd64` and `arm64`
- stable release workflow with GitHub Releases and signed APT `stable`
- nightly prereleases and signed APT `nightly`
- automatic monthly stable publication only after packaging and release validation are proven
- Docker administration surface for daemon-level settings:
  - read-only Docker service status and daemon config visibility first
  - managed edits for selected `daemon.json` keys later
  - explicit backup, restart, and rollback workflow
- read-only visibility into Docker networks and volumes that affect Compose stacks
- targeted create/delete actions for external networks or volumes when directly useful to stacks
- scheduled maintenance jobs only as explicit opt-in policies
- richer live stats visualization beyond the current lightweight sparklines
- theme toggle with system preference support
- compose template library and starter catalog
- custom project metadata such as icon and useful external links
- host-level system information widgets on dashboard views
- light internationalization groundwork, then selected translations once UI copy stabilizes
- richer maintenance notifications and scheduled update policies
- optional repo bootstrap workflows only if they complement the local Git workspace model

## Later / Conditional

- vulnerability scanning for images as an optional maintenance module
- read-only Docker object inventory beyond stacks where it improves troubleshooting
- CLI or API-key-based automation surface
- limited file/template tooling that supports Compose-first operations

## Explicitly De-Prioritized

- multi-host control plane
- remote agents
- full Portainer-style management of all Docker objects
- generic root-equivalent host administration through the browser
- deep GitOps reconciliation from remote repositories
- enterprise auth, MFA, and RBAC as a near-term priority
- registry management platform replacement
