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
- browser E2E, Docker-backed integration tests, and staging validation on Linux `amd64` and `arm64`

## Near-Term Product Goals

### 1. Operator Trust And Self-Observability

- show Stacklab version and build metadata clearly in the UI
- add a host overview page with CPU, memory, disk, uptime, OS, Docker version, and Compose version
- expose Stacklab service logs from `journalctl -u stacklab` in the browser
- improve visibility of why actions succeeded, warned, or failed
- add optional job and maintenance notifications

### 2. Safe Maintenance Workflows

- manual prune workflows with explicit scope and preview
- image inventory and selective image maintenance
- read-only visibility into Docker networks and volumes that affect Compose stacks
- targeted create/delete actions for external networks or volumes when directly useful to stacks
- scheduled maintenance jobs only as explicit opt-in policies

### 3. Git-Aware Compose Operations

- read-only Git workspace status for `/opt/stacklab/stacks` and `/opt/stacklab/config`
- diff against working tree / HEAD for stack definitions and config files
- simple commit and push workflow for the local Stacklab workspace
- config browser/editor for `/opt/stacklab/config`
- avoid turning Stacklab into an always-reconciling GitOps controller

## Mid-Term Product Goals

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
- deep GitOps reconciliation from remote repositories
- enterprise auth, MFA, and RBAC as a near-term priority
- registry management platform replacement
