# Product Roadmap

This roadmap tracks product direction after the production and release baseline
became operational. Stacklab remains a single-host, Linux, Compose-first tool:
stack definitions stay operator-owned on disk, high-impact actions stay explicit
and auditable, and new features must not turn the product into a fleet control
plane or a generic root-equivalent host console. The roadmap is intentionally
limited to work that is not already implemented.

Status reviewed: 2026-07-12.

## Current Baseline

Implemented and exercised on Linux staging hosts:

- single-user authentication, session revocation, and bounded retention;
- filesystem and Docker runtime stack discovery, detail views, lifecycle jobs,
  audit history, live logs, stats, and container terminals;
- Compose and `.env` editing, resolved-config preview, auxiliary file editing,
  stack create/delete, and local Git status, diff, commit, and push;
- managed workspace permission diagnostics and helper-backed repair;
- stack metadata from the Compose `x-stacklab` extension, including validated
  icon identifiers and external links exposed on stack cards;
- safe bulk stack updates, image updates, cleanup, and scheduled maintenance;
- operation-review summaries for destructive or high-impact actions, showing
  scope, target, effect, and recovery guidance before confirmation;
- WebSocket-pushed global activity with REST snapshot/reconnect fallback and
  retained job detail;
- a System Health center exposing backend, Docker, and realtime transport state,
  last-success context, retry actions, and diagnostic links;
- host overview, Stacklab service logs, selected Docker daemon settings, and
  private registry login/logout;
- webhook and Telegram notifications for jobs, maintenance, runtime health, and
  Stacklab self-health events;
- a built-in, server-rendered Compose template catalog;
- generated frontend types from `docs/api/openapi.yaml` plus CI drift checks
  against the committed contract;
- ordered graceful shutdown that stops new work, closes realtime connections,
  waits for background workers, and closes storage last;
- browser E2E, Docker-backed integration tests, package/systemd smoke, and Linux
  validation on `amd64` and `arm64` where applicable;
- Debian packages and tarballs, signed APT `stable` and `nightly` channels,
  APT-backed self-update, and bounded release/package retention;
- a manual GitHub workflow that rebuilds the README screenshot gallery from the
  Docker-backed E2E harness and opens a reviewable docs PR;
- bounded SQLite retention for sessions, audit, job summaries, and detailed job
  events, with frontend-only stats history explicitly kept out of the backend.

The automated monthly stable line has been active since `2026.04.0`; the
GitHub Releases page is the canonical current version list. Nightly prereleases
of changed `main` revisions provide the soak channel between stable releases.

## Near-Term Product Goals

### 1. Template Library And Starter Catalog

- collect operator feedback on the built-in starter catalog;
- improve the existing templates without hiding their generated Compose;
- keep variables small, explicit, and server-rendered;
- avoid a remote catalog until the local workflow has stable usage data.

### 2. Operator Polish

- make progress, completion, partial-failure, and recovery language consistent
  across all remaining long-running workflows;
- refine small-screen and keyboard operation for dense maintenance and Docker
  administration views;
- add targeted diagnostics where an operator can act on the result rather than
  adding more passive dashboard metrics.

### 3. Recovery And Reliability

- add stack definition backup/export for operator recovery and migration;
- expand Compose and Docker diagnostics for restart policies, healthchecks,
  startup ordering, resource limits, log growth, and daemon configuration;
- add notifications for the existing digest-based image update checks without
  immediately changing a running stack;
- smoke-test terminal and job WebSocket streams under the production CSP in
  Safari and explicitly extend `connect-src` only if the test proves necessary.

## Mid-Term Product Goals

- add light internationalization groundwork, then selected translations after
  UI copy stabilizes;
- refine runtime health alerts with per-stack exclusions, configurable
  thresholds/cooldowns, and direct remediation links;
- add `ntfy` or Gotify as the next self-hosted notification channel; add email
  only when operator demand justifies its delivery and configuration cost;
- add optional scheduled-update config snapshot commits only when a job starts
  from a clean workspace, changes controlled files, and succeeds;
- add backup orchestration for managed config, data directories, and named
  volumes, including retention and restore metadata, without becoming a generic
  whole-host backup product;
- add optional Docker credential-helper integration after portability and
  package-upgrade behavior are validated;
- evaluate additional narrowly managed Docker settings, especially log
  retention and stable `host-gateway` IPs, while keeping high-risk settings
  advisory-only;
- consider repository bootstrap only where it complements the existing local
  Git workspace rather than introducing remote reconciliation.

## Later / Conditional

- vulnerability scanning as an optional image-maintenance module;
- read-only Docker object inventory where it materially improves diagnosis;
- a CLI or API-key automation surface with a deliberately smaller permission
  boundary than the browser session;
- limited file/template tooling that directly supports Compose-first operation;
- evaluate GoReleaser, nfpm, or tag-triggered build decomposition only if it
  reduces the current release surface without weakening APT validation.

## Explicitly De-Prioritized

- multi-host control plane;
- remote agents;
- full Portainer-style management of every Docker object;
- generic root-equivalent host administration through the browser;
- deep GitOps reconciliation from remote repositories;
- enterprise authentication, MFA, and RBAC as a near-term priority;
- replacing a registry management platform.
