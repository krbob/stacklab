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
- config workspace for the managed Stacklab config root
- local Git status, diff, per-file commit, and push for managed workspace files
- workspace permission diagnostics and helper-backed repair for managed roots
- safe bulk stack update workflow with richer step-card progress
- image, network, volume, and cleanup maintenance surfaces
- scheduled maintenance policies for stack updates and cleanup
- global activity indicator and retained job detail drawer
- Docker daemon administration for selected `daemon.json` keys with validate/apply workflow
- Docker registry auth for private images with login/logout in `/docker`
- webhook and Telegram notifications for job, maintenance, runtime, and Stacklab self-health events
- stack-local auxiliary file browsing/editing for files such as `Dockerfile`
- expanded built-in stack template starter catalog with server-rendered variables
- browser E2E, Docker-backed integration tests, and staging validation on Linux `amd64` and `arm64`
- Debian package publication through signed APT `stable` and `nightly` channels
- APT-backed Stacklab self-update on package-managed installs
- release hygiene for APT package retention and nightly prerelease cleanup
- bounded SQLite retention for sessions, audit, job summaries, and detailed job events
- frontend-only stats session history with explicit no-backend-retention scope

## Near-Term Product Goals

### 1. Template Library And Starter Catalog

- collect real operator feedback on the built-in starter catalog
- keep templates Compose-first and transparent, with simple `${VAR}` variables
- avoid remote template catalogs until the local workflow is stable

### 2. Polish And Adoption

- richer operation progress for stack actions, pull/build-heavy flows, and background work visibility
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
- optional Docker credential helper integration only after the basic registry auth flow is stable

## Later / Conditional

- vulnerability scanning for images as an optional maintenance module
- read-only Docker object inventory beyond stacks where it improves troubleshooting
- CLI or API-key-based automation surface
- limited file/template tooling that supports Compose-first operations

## Backlog Candidates

Engineering backlog:

- generate frontend API types from `docs/api/openapi.yaml` and add a CI check that prevents REST contract/type drift
- join detached job runner goroutines during graceful shutdown so jobs can reliably land as `cancelled` instead of falling back to startup reconciliation as interrupted
- evaluate tag-triggered release workflows and GoReleaser/nfpm only if they reduce the current release script surface without weakening APT channel validation
- turn the existing README screenshot capture script into a GitHub workflow that boots a deterministic demo harness, refreshes screenshots, uploads review artifacts, and optionally opens a docs-only update when tracked screenshots drift
- smoke test terminal and job WebSocket streams under the CSP policy in Safari; if needed, make the `connect-src` directive explicitly cover same-host `ws:` and `wss:` connections

Product backlog:

- add optional scheduled-update config snapshot commits: when a scheduled stack update starts from a clean Git workspace, updates at least one container, succeeds, and leaves controlled config files changed, Stacklab can create an automatic Git commit for those generated config changes instead of mixing them with later operator edits
- add stack backup orchestration for managed config, data directories, and named volumes: discover per-stack backup targets, schedule backup jobs, surface retention and restore metadata, emit audit/notifications, and provide a deliberate replacement path for sidecar tools such as Backrest without becoming a generic whole-host backup product
- expand Compose and Docker diagnostics with operator-facing checks for restart policies, healthchecks, startup ordering, resource limits, log growth risk, and daemon settings that affect homelab reliability
- add image update checks that can notify when a registry tag resolves to a new digest
- upgrade global activity from low-rate polling to WebSocket push once background activity volume justifies the extra transport complexity
- evaluate additional narrow Docker admin settings after the current managed-key model proves safe, especially log driver/log retention, stable `host-gateway` IPs, and read-only advisories for `default-ulimits`, `userns-remap`, CDI/runtimes, and cgroup driver choices
- add stack definition backup/export workflows for operator recovery and migration

## Explicitly De-Prioritized

- multi-host control plane
- remote agents
- full Portainer-style management of all Docker objects
- generic root-equivalent host administration through the browser
- deep GitOps reconciliation from remote repositories
- enterprise auth, MFA, and RBAC as a near-term priority
- registry management platform replacement
