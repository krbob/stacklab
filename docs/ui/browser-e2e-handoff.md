# Browser E2E Handoff

This document defines the current backend harness for browser-level E2E smoke tests.

It is intended for Playwright-style tests owned by the UI developer.

## Scope

The current browser E2E layer should stay intentionally small.

Recommended first scenarios:

1. login -> dashboard -> open seeded stack
2. editor preview -> save -> audit visibility
3. create stack -> verify appears in UI -> delete definition -> verify redirect
4. global audit page loads and paginates

Do **not** try to cover all runtime behavior here.

The following are already covered better elsewhere:

- WebSocket protocol behavior
- jobs replay semantics
- terminal PTY lifecycle
- Docker-backed lifecycle behavior

Those already have backend integration tests and frontend hook tests.

## Backend Command

Use the provided harness:

```bash
scripts/e2e/run-backend.sh
```

Default behavior:

- clones the static fixture root into a fresh temp working directory
- creates a fresh SQLite store under that temp directory
- serves the built frontend from `frontend/dist`
- starts Stacklab on `127.0.0.1:18081`
- bootstraps the login password as `stacklab-e2e`

## Required prerequisites

Before starting the backend:

```bash
cd frontend
npm ci
npm run build
cd ..
```

Then start the backend:

```bash
scripts/e2e/run-backend.sh
```

Default login credentials:

- password: `stacklab-e2e`

## Relevant environment variables

The harness sets defaults, but these can be overridden:

| Variable | Default |
| --- | --- |
| `STACKLAB_HTTP_ADDR` | `127.0.0.1:18081` |
| `STACKLAB_BOOTSTRAP_PASSWORD` | `stacklab-e2e` |
| `STACKLAB_FRONTEND_DIST` | `<repo>/frontend/dist` |
| `STACKLAB_E2E_WORKDIR` | temporary directory under `/tmp` |
| `STACKLAB_ROOT` | `<workdir>/root` |
| `STACKLAB_DATA_DIR` | `<workdir>/var/lib/stacklab` |
| `STACKLAB_DATABASE_PATH` | `<workdir>/var/lib/stacklab/stacklab.db` |

## Fixture root

Static fixture source:

```text
test/fixtures/e2e/root/
```

Seeded content:

```text
test/fixtures/e2e/root/
  stacks/
    demo/
      compose.yaml
      .env
  config/
    demo/
  data/
    demo/
```

The seeded `demo` stack is intentionally simple:

- one service: `app`
- image reference interpolated from `.env`
- good enough for:
  - dashboard visibility
  - stack detail
  - editor load
  - resolved preview
  - save flow

Important:

- tests must treat the static fixture root as read-only
- mutations happen against the temp copied root created by `scripts/e2e/run-backend.sh`

## CI shape

The browser E2E workflow is not implemented yet, but the intended CI sequence is:

```bash
cd frontend
npm ci
npm run build
cd ..

scripts/e2e/run-backend.sh > /tmp/stacklab-e2e.log 2>&1 &
STACKLAB_PID=$!

until curl -fsS http://127.0.0.1:18081/api/health >/dev/null; do
  sleep 1
done

cd frontend
npx playwright test

kill "${STACKLAB_PID}"
```

On failure, CI should upload:

- Playwright trace/screenshots
- `/tmp/stacklab-e2e.log`

## Practical guidance for UI developer

Recommended selectors:

- prefer `data-testid` for:
  - login form submit
  - dashboard stack cards
  - editor save / save-and-deploy buttons
  - create stack form submit
  - delete dialog confirm
  - audit rows / load-more button

Avoid overfitting tests to:

- exact Tailwind classes
- decorative text
- animated loading placeholders

## Runtime assumptions

Browser E2E should currently assume:

- backend is real
- frontend is real production build
- filesystem is real but isolated in temp
- SQLite is real
- Docker may or may not be present

Because of that:

- the first browser E2E suite should focus on non-Docker-critical flows
- Docker-dependent runtime behavior should remain primarily validated by backend integration tests
