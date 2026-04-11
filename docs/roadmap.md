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
- local Git status, diff, per-file commit, and push for managed workspace files
- workspace permission diagnostics and helper-backed repair for managed roots
- safe bulk stack update workflow with richer step-card progress
- image, network, volume, and cleanup maintenance surfaces
- scheduled maintenance policies for stack updates and cleanup
- global activity indicator and retained job detail drawer
- Docker daemon administration for selected `daemon.json` keys with validate/apply workflow
- webhook and Telegram notifications for job, maintenance, runtime, and Stacklab self-health events
- stack-local auxiliary file browsing/editing for files such as `Dockerfile`
- browser E2E, Docker-backed integration tests, and staging validation on Linux `amd64` and `arm64`
- Debian package publication through signed APT `stable` and `nightly` channels
- APT-backed Stacklab self-update on package-managed installs
- release hygiene for APT package retention and nightly prerelease cleanup
- bounded SQLite retention for sessions, audit, job summaries, and detailed job events
- frontend-only stats session history with explicit no-backend-retention scope

## Near-Term Product Goals

### 1. Template Library And Starter Catalog

- create new stacks from curated local templates
- keep templates Compose-first and transparent
- avoid remote template catalogs until the local workflow is stable

### 2. Polish And Adoption

- theme toggle with system preference support
- host-level system information widgets on dashboard views
- custom project metadata such as icon and useful external links

## Mid-Term Product Goals

- light internationalization groundwork, then selected translations once UI copy stabilizes
- runtime health alert refinements:
  - per-stack exclusions
  - configurable thresholds and cooldowns
  - clearer remediation links from notifications
- optional repo bootstrap workflows only if they complement the local Git workspace model
- broader notification channels:
  - `ntfy` or `Gotify` as strong self-hosted candidates
  - email only if there is a clear operator need
- automatic monthly stable publication only after release validation keeps proving reliable

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
