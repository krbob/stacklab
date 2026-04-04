# Local Development

## Purpose

This document defines the local development workflow for Stacklab source code.

It is intentionally minimal and focused on getting backend and frontend running together during early development.

## Source Layout

Planned source layout:

- Go backend at repository root
- React frontend in `frontend/`
- documentation in `docs/`

This repository is the source tree that later gets deployed under `/opt/stacklab/app`.

## Tooling Requirements

Recommended local tools:

- Go `1.25+`
- Node.js `22 LTS+`
- npm `10+`
- Docker Engine
- Compose v2 available as either `docker compose` or standalone `docker-compose`

Helpful but optional:

- `make`
- `just`
- `watchexec`

## Host Assumptions

Local development assumes:

- Linux or macOS workstation
- Docker available locally
- Compose v2 command available locally
- ability to create temporary test stacks

## Backend Development

Planned backend entrypoint:

```text
cmd/stacklab
```

Expected run command:

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

Planned frontend path:

```text
frontend/
```

Expected run commands:

```bash
cd frontend
npm install
npm run dev
```

Recommended frontend dev server:

- Vite on `127.0.0.1:5173`

Recommended proxy behavior:

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

Recommended daily workflow:

1. create or update a local test stack under `.local/stacklab/stacks`
2. run backend with local environment variables
3. run frontend dev server
4. exercise stack list, editor, logs, stats, and terminal against the local Docker daemon

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

## Logging

During local development:

- backend logs go to terminal stdout/stderr
- frontend logs go to browser console and Vite output

## Future Improvements

Likely follow-up additions later:

- `Makefile` or `justfile`
- seed scripts for local test stacks
- dev TLS or reverse-proxy profile
- automated OpenAPI type generation for frontend
