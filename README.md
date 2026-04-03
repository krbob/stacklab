# Stacklab

Stacklab is a host-native, Compose-first control panel for Docker Compose stacks on a single Linux host.

It is built for a homelab-style environment:

- one managed host
- Linux `amd64` as the target platform
- LAN-only usage
- Docker Compose as the source of truth
- filesystem-first management instead of a database-owned stack model

## Status

Stacklab is an active prototype.

Implemented today:

- authentication and session handling
- stack discovery from the filesystem and Docker runtime
- stack list and stack detail views
- Compose definition editor with resolved-config preview
- stack lifecycle actions and job progress
- live logs, stats, and container terminal
- stack create/delete flows
- audit history
- backend and frontend automated tests

Not done yet:

- production release automation
- scheduled dependency maintenance workflows
- first real Linux `amd64` deployment trial
- final hardening for long-term production use

## Architecture

Recommended production shape:

- backend: Go
- frontend: React + Vite + TypeScript
- runtime: host-native service, not a Docker management container
- state: filesystem + SQLite operational metadata

Canonical host layout:

- `/opt/stacklab/app`
- `/opt/stacklab/stacks`
- `/opt/stacklab/config`
- `/opt/stacklab/data`

## Quick Start

### Prerequisites

- Go
- Node.js + npm
- Docker Engine + Docker Compose

### Local development

Backend:

```bash
STACKLAB_BOOTSTRAP_PASSWORD=change-me go run ./cmd/stacklab
```

Frontend dev server:

```bash
cd frontend
npm ci
npm run dev
```

Default local paths are under `.local/stacklab` and `.local/var/lib/stacklab`.

### Built frontend mode

If you want the Go backend to serve the production frontend bundle:

```bash
cd frontend
npm ci
npm run build

cd ..
STACKLAB_BOOTSTRAP_PASSWORD=change-me go run ./cmd/stacklab
```

Then open:

- `http://127.0.0.1:8080`

## Tests

Backend:

```bash
go test ./...
```

Frontend:

```bash
cd frontend
npm test
npm run typecheck
npm run lint
npm run build
```

## Documentation

Project documentation lives in [`docs/`](docs/README.md).

Good entry points:

- [`docs/product/scope.md`](docs/product/scope.md)
- [`docs/product/mvp.md`](docs/product/mvp.md)
- [`docs/architecture/system-overview.md`](docs/architecture/system-overview.md)
- [`docs/ops/systemd.md`](docs/ops/systemd.md)
- [`docs/ops/release-plan.md`](docs/ops/release-plan.md)

## Current Constraints

- target platform is Linux `amd64`, even though development and much of integration testing can happen on macOS
- Stacklab currently assumes a single local operator model
- host shell is intentionally deferred beyond the current MVP
- `source=last_valid` for resolved config is not implemented yet
