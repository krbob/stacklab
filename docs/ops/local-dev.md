# Local Development

## Purpose

This document defines the current local development workflow for running the
Stacklab backend and frontend together from source.

## Source Layout

Source layout:

- Go backend at repository root
- React frontend in `frontend/`
- documentation in `docs/`

Packaged application files are installed under `/usr/lib/stacklab`; the manual
tarball profile uses `/opt/stacklab/app`. Development state stays under the
repository-local paths described below and must not reuse either production
layout.

## Tooling Requirements

Required local tools:

- Go `1.26.5` from `go.mod`
- Node.js `24.18.0` from `.nvmrc`
- the npm version bundled with that Node.js release
- GNU Make
- ShellCheck `0.11.0` for repository hygiene
- Docker Engine
- Compose v2 available as either `docker compose` or standalone `docker-compose`

## Host Assumptions

Local development assumes:

- Linux or macOS workstation
- Docker available locally
- Compose v2 command available locally
- ability to create temporary test stacks

## Backend Development

Backend entrypoint:

```text
cmd/stacklab
```

Run command:

```bash
go run ./cmd/stacklab
```

Recommended local environment:

```bash
export STACKLAB_ROOT="$PWD/.local/stacklab"
export STACKLAB_DATA_DIR="$PWD/.local/var/lib/stacklab"
export STACKLAB_HTTP_ADDR="127.0.0.1:8080"
export STACKLAB_LOG_LEVEL="debug"
export STACKLAB_BOOTSTRAP_PASSWORD="stacklab-dev"
```

Notes:

- `STACKLAB_BOOTSTRAP_PASSWORD` is used only to initialize the first password hash when the auth store is empty
- once the password row exists, changing the bootstrap variable does not overwrite the stored password

## Frontend Development

Frontend path:

```text
frontend/
```

Run commands:

```bash
cd frontend
npm ci
npm run dev
```

The frontend dev server runs:

- Vite on `127.0.0.1:5173`

The checked-in Vite configuration proxies:

- frontend dev server proxies `/api` and `/api/ws` to the backend

## Local Data Directories

Recommended local dev directories:

```text
.local/
  stacklab/
    stacks/
    config/
    data/
  var/
    lib/
      stacklab/
```

Rules:

- do not point local dev at production `/opt/stacklab`
- keep test stacks isolated under `.local/`

## Local Test Workflow

Before handing off a change, run the complete source-tree baseline from the
repository root:

```bash
make check
```

This verifies the active toolchain, backend tests and hygiene, frontend tests,
typecheck and build, then delegates repository hygiene to the existing QA-03
script. Go commands are explicitly limited to `cmd` and `internal`, so they do
not traverse `frontend/node_modules`. See
[`../quality/developer-checks.md`](../quality/developer-checks.md) for focused
targets and the canonical version declarations.

Recommended daily workflow:

1. create or update a local test stack under `.local/stacklab/stacks`
2. run backend with local environment variables
3. run frontend dev server
4. exercise stack list, editor, logs, stats, and terminal against the local Docker daemon

Notes for local host observability:

- `/host` is fully representative only on Linux hosts that expose the same primitives as production
- on macOS, Stacklab service logs are expected to be unavailable because there is no `journald` service unit to read
- some host metrics shown on `/host` may be partial or look unusual on macOS because Docker runs through Docker Desktop / virtualization rather than as a native Linux host daemon
- use Linux staging hosts to validate the final `/host` experience

## API Workflow

Recommended contract-first loop:

1. update `docs/api/openapi.yaml` when REST changes
2. update `docs/api/websocket-protocol.md` when streaming changes
3. only then change backend and frontend code

## Seed Test Cases

Useful local test stacks:

- one simple `image`-only stack
- one `build`-based stack
- one stack with a healthcheck
- one stopped stack
- one invalid compose definition used only for editor validation testing

Retention/UI seed helper:

```bash
go run ./scripts/dev/seed-retention-fixtures.go --db /var/lib/stacklab/stacklab.db --run-prune
```

What it seeds:

- one recent audit/job pair with retained detailed output
- one older audit/job pair where the job summary remains but `job_events` are purged by retention
- one very old audit/job pair that should disappear entirely after retention cleanup

Useful when validating:

- audit history rows remain visible within retention
- job detail drawer shows `Detailed output for this job is no longer retained.`
- operational retention cleanup removes stale sessions and stale records without breaking recent audit links

## Logging

During local development:

- backend logs go to terminal stdout/stderr
- frontend logs go to browser console and Vite output

The tracked OpenAPI client is generated with `npm run generate:api`; the
canonical drift check and focused quality commands are documented in
[`../quality/developer-checks.md`](../quality/developer-checks.md).
