# Browser E2E Handoff

> **Status: active reference.** This document describes the current Playwright
> harness, fixtures, CI gate, and test ownership.

## Scope

Browser E2E is an integration smoke layer over the real production frontend,
HTTP API, WebSocket connection, filesystem, SQLite store, Git workspace, and
selected Docker runtime flows.

The current suite covers:

- authentication, dashboard navigation, stack creation, editor save, and audit;
- Config Files, Stack Files, Git diff/commit/push, and persistence after reload;
- Settings navigation and maintenance schedule persistence;
- Maintenance inventory/cleanup and Docker Admin status;
- live stack logs, stats, and an interactive terminal session;
- desktop Chromium plus a focused mobile Chromium navigation smoke.

Protocol edge cases, replay semantics, reconnect behavior, terminal limits, and
large-stream bounds remain owned by backend integration tests and frontend hook
tests. Browser E2E proves that representative production UI paths are wired to
those lower-level implementations.

## Prerequisites

Use the repository-pinned Node version and a running Docker daemon:

```bash
docker info
docker compose version

cd frontend
npm ci
npm run build
cd ..
```

The Playwright Chromium binary must also be installed:

```bash
cd frontend
npx playwright install chromium
cd ..
```

## Backend harness

Start the isolated backend with:

```bash
scripts/e2e/run-backend.sh
```

The harness:

- copies the versioned fixture root into a fresh temporary working directory;
- creates a fresh SQLite store outside the copied workspace;
- initializes the copied workspace as a Git repository on `main`;
- creates a local bare `origin`, pushes the baseline, and configures
  `origin/main` as upstream without network or credentials;
- excludes runtime `data/` and the permission-blocked fixture from Git status;
- creates the permission-blocked config fixture after the Git baseline;
- serves the production frontend from `frontend/dist`;
- starts Stacklab on `127.0.0.1:18081` by default;
- bootstraps the login password as `stacklab-e2e`.

Default login credential:

- password: `stacklab-e2e`

## Running Playwright

With the backend running in another terminal:

```bash
cd frontend
STACKLAB_E2E_URL=http://127.0.0.1:18081 \
STACKLAB_E2E_PASSWORD=stacklab-e2e \
npm run test:e2e
```

The default configuration uses one worker so filesystem, Git, Docker, and
settings mutations remain deterministic. The desktop project runs the full
suite. The mobile project runs the responsive navigation smoke on a Pixel 7
viewport.

Permission repair is opt-in locally:

```bash
STACKLAB_E2E_ENABLE_WORKSPACE_REPAIR=1 scripts/e2e/run-backend.sh
```

It requires the generated workspace admin helper to be executable through
`sudo`. CI enables this mode; a normal local run skips only that scenario.

## Relevant environment variables

| Variable | Default |
| --- | --- |
| `STACKLAB_HTTP_ADDR` | `127.0.0.1:18081` |
| `STACKLAB_BOOTSTRAP_PASSWORD` | `stacklab-e2e` |
| `STACKLAB_FRONTEND_DIST` | `<repo>/frontend/dist` |
| `STACKLAB_E2E_WORKDIR` | fresh temporary directory |
| `STACKLAB_ROOT` | `<workdir>/root` |
| `STACKLAB_DATA_DIR` | `<workdir>/var/lib/stacklab` |
| `STACKLAB_DATABASE_PATH` | `<workdir>/var/lib/stacklab/stacklab.db` |
| `STACKLAB_E2E_ENABLE_WORKSPACE_REPAIR` | `0` |
| `STACKLAB_WORKSPACE_ADMIN_HELPER_PATH` | `<workdir>/bin/stacklab-workspace-admin-helper` |

## Fixture root

Versioned fixture source:

```text
test/fixtures/e2e/root/
  stacks/
    demo/
      compose.yaml
      .env
      config/
        app.yaml
  config/
    demo/
      .gitkeep
  data/
    demo/
      .gitkeep
```

The seeded `demo` stack supports dashboard, editor, Config Files, Stack Files,
and Git tests. Tests must treat the versioned source as read-only; all mutations
happen in the copied temporary root.

Docker runtime tests create uniquely named disposable stacks through the REST
API and remove runtime, definition, config, and data in `afterEach`. Their Alpine
probe traps termination and uses a short stop grace period so cleanup stays
bounded.

## Failure artifacts

Playwright is configured with:

- the list and HTML reporters;
- screenshots only on failure;
- trace retention on failure;
- a single worker and no retries, so a failure identifies the original attempt.

The HTML report is written to `frontend/playwright-report`; raw results,
screenshots, and traces are written to `frontend/test-results`.

## CI gate

The authoritative workflow is `.github/workflows/browser-e2e.yml`. It:

1. checks out and verifies the exact source SHA;
2. installs the pinned Node and Go toolchains;
3. builds the production frontend and installs Chromium;
4. verifies `docker info` and `docker compose version`;
5. starts the isolated backend with permission repair enabled;
6. fails fast if the backend exits and waits for `/api/ready`;
7. runs the full Playwright suite;
8. prints the backend log on failure and always stops the backend;
9. uploads Playwright artifacts and the backend log on failure.

## Test design guidance

- Prefer accessible roles, labels, and stable `data-testid` hooks.
- Use REST helpers for deterministic setup and cleanup; test the browser path
  that is actually under review.
- Assert response status and durable state for mutations, not only a toast.
- Use unique stack IDs for Docker tests and always clean them in `afterEach`.
- Avoid exact timestamps, container IDs, CPU values, Tailwind classes, and
  animation timing.
- A new Docker-dependent test must pass both in isolation and in the full
  single-worker suite.
