# CI Quality Plan

## Purpose

This document defines the recommended CI quality gates for Stacklab.

It covers:

- static analysis
- linting
- test execution
- coverage policy
- required vs advisory checks
- staged rollout order

This is a planning document for the current phase. It does not mean every check below should be implemented immediately.

## Current State

Today, the repository already has parts of the quality stack:

- frontend Vitest test suite
- frontend hook tests for WebSocket-driven runtime behavior
- frontend TypeScript typecheck
- frontend ESLint
- frontend production build
- backend unit and integration tests via Go test commands scoped to `cmd/` and `internal/`

Current observations:

- frontend lint is now green and ready for CI enforcement
- backend does not yet have a proper static analysis layer beyond compiling and tests
- global backend coverage remains a trend, while stable package-focused floors
  protect the critical and newly covered packages
- Docker-backed integration tests now cover WebSocket flows and core Compose lifecycle behavior
- a dedicated GitHub Actions workflow can now run that suite, but it should prove stable before becoming a required gate
- a representative OpenAPI-backed contract suite now exists for core success-path endpoints
- advisory GitHub Actions checks now exist for `staticcheck` and `govulncheck`
- an advisory browser E2E workflow now exists for lightweight Playwright smoke
- repository hygiene is enforced with actionlint, ShellCheck, full-history secret scanning, production dependency audit, and zero-warning ESLint

## Quality Principles

### 1. High signal over checkbox quantity

We should prefer a smaller set of checks that catch real regressions over a large set of noisy checks that people learn to ignore.

### 2. Runtime realism matters

Stacklab is a control plane for Docker Compose, PTY sessions, WebSocket streams, and SQLite-backed metadata.

Because of that:

- unit tests are necessary
- static analysis is useful
- but realistic Docker-backed integration tests are more important than abstract coverage percentages

### 3. Coverage is directional, not ceremonial

Coverage should help us see whether testing depth is improving.

It should not become a fake success metric that encourages low-value tests.

## Recommended Check Layers

## Layer A: Fast baseline checks

These should run on every PR and complete quickly.

### Frontend

- `npm ci`
- `npm test`
- `npm run typecheck`
- `npm audit --omit=dev`
- `npm run lint` (`eslint --max-warnings=0`)
- `npm run build`

### Backend

- `go test ./cmd/... ./internal/...`
- `go vet ./cmd/... ./internal/...`
- `gofmt -l cmd internal` with failure on unformatted files

Purpose:

- catch obvious breakage fast
- keep the codebase syntactically healthy
- keep PR feedback loop short

### Scheduler determinism

Scheduler tests must inject both the clock and the host-local location. They must
drive polling through a manual ticker and wait for scheduler workers, including
runtime finalization, instead of mutating process-wide `time.Local`, sleeping, or
polling against wall-clock deadlines.

Use this repeated race run when changing scheduler timing or shutdown behavior:

```bash
go test -race ./internal/scheduler -count=20
```

## Layer B: Static analysis

These checks should be added after the fast baseline is stable.

### Backend static analysis

Recommended:

- `staticcheck ./...`
- `govulncheck ./...`

Why:

- `go vet` catches a narrow class of issues
- `staticcheck` is the right next step for real Go code quality
- `govulncheck` adds security signal for dependencies and call paths

### Frontend static analysis

The primary tools are already:

- TypeScript strict mode
- ESLint

Recommendation:

- keep frontend quality centered on those two tools
- avoid adding extra frontend analyzers unless they catch concrete classes of bugs we are actually seeing

## Layer C: Docker-backed integration checks

These are the most important medium-speed checks for Stacklab.

Recommended environment:

- GitHub Actions Linux `x64` runner
- Docker Engine available
- Compose v2 available through either `docker compose` or standalone `docker-compose`

Recommended coverage:

- authentication login/session flow
- stack discovery from fixture directories
- stack detail reads
- editor preview and save paths
- create and delete flows
- stack lifecycle actions such as `up`, `down`, and `restart`
- WebSocket job stream
- WebSocket logs stream
- WebSocket stats stream
- WebSocket terminal open, attach, input, resize, and close

Purpose:

- validate the operator-critical runtime seams
- catch regressions from dependency updates
- catch regressions that unit tests and linters cannot see

## Layer D: Pre-release environment checks

These should not be required for every PR at first.

Recommended scope:

- packaged Linux `amd64` build smoke
- host-native runtime smoke
- reverse proxy smoke
- upgrade and rollback smoke
- `systemd` service validation

Recommended environment:

- staging Linux `amd64` VM
- staging homelab host
- or later a trusted self-hosted runner

Purpose:

- validate production-shaped concerns
- avoid putting heavy environment-specific checks on every normal PR

## Required vs Advisory Checks

## Initial required checks

These should become branch-protection requirements first:

- frontend typecheck
- frontend lint
- frontend build
- backend tests
- backend `go vet`
- formatting check for Go
- repository hygiene (`repository-hygiene`)

Then add:

- Docker-backed integration suite

The repository hygiene job and its local equivalent are documented in
[`repository-hygiene.md`](repository-hygiene.md).

Why not require everything immediately:

- CI should become stable before it becomes strict
- flaky required checks cause more harm than missing checks

## Initial advisory checks

These can start as non-blocking or informational:

- backend coverage report
- `staticcheck`
- `govulncheck`
- Linux `amd64` release build

Current status:

- `staticcheck` and `govulncheck` are now implemented as advisory GitHub Actions jobs
- they should remain non-required until they prove stable and useful over normal PR traffic

Once they prove stable and high-signal, some can be promoted to required.

## Coverage Policy

## Current stance

Coverage is collected on every backend CI run and uploaded as a profile, HTML
report, function report, package table, and Markdown summary. There is no hard
global threshold: the global value remains a trend signal.

CI enforces explicit floors only for packages with stable, behavior-oriented
tests. The initial floors deliberately leave room for platform-specific and
concurrent paths while preventing silent loss of the new coverage:

| Package | Minimum statement coverage |
| --- | ---: |
| `internal/audit` | 90% |
| `internal/retention` | 95% |
| `internal/fsmeta` | 95% |
| `internal/httpapi` | 50% |
| `internal/selfupdate` | 70% |
| `internal/jobs` | 85% |
| `internal/store` | 75% |

Raise a floor only after the corresponding tests have been stable on Linux CI;
do not lower one merely to merge an uncovered regression.

## Recommended use of coverage

Use coverage to:

- track trend over time
- highlight under-tested critical packages
- guide future test investment

Do not use coverage to:

- block all PRs on an arbitrary percentage
- optimize for line-count instead of behavior

## Preferred coverage strategy

Focus first on critical backend packages:

- `internal/httpapi`
- `internal/stacks`
- `internal/auth`
- `internal/jobs`
- `internal/store`
- `internal/terminal`

Then improve coverage by adding tests for:

- error paths
- locking behavior
- audit and job interactions
- runtime stream edge cases
- integration flows around Docker and WebSockets

## Active threshold policy

The selected-package floors above are merge gates. Global coverage remains
non-gating, so new low-level packages do not create pressure for ornamental
tests. Future changes may add another package floor or a non-decreasing trend
check, but should continue to prefer package-focused policy over one global
percentage.

## Recommended Command Set

The canonical local baseline is:

```bash
make check
```

It composes the backend, frontend, and repository hygiene commands below with
the exact Go and Node versions documented in
[`developer-checks.md`](developer-checks.md).

## Frontend

```bash
cd frontend
npm ci
npm test
npm run typecheck
npm run lint
npm run build
```

## Backend

```bash
go test ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
gofmt -l cmd internal
staticcheck ./...
govulncheck ./...
```

## Coverage

```bash
make check-backend-coverage
```

The command writes the same `coverage/` artifact locally that CI uploads.

## Docker-backed integration

Exact commands may evolve, but the target is:

- Linux runner
- fixture stacks
- end-to-end API and WebSocket validation

## Recommended Rollout Order

### Stage 1: Make existing checks green

- keep frontend tests, typecheck, lint, and build green
- keep `go test ./cmd/... ./internal/...` green

### Stage 2: Add backend hygiene checks

- add `go vet`
- add Go formatting check

### Stage 3: Add backend static analysis

- add `staticcheck`
- add `govulncheck` as advisory first

### Stage 4: Add Docker-backed integration suite

- start with the highest-value runtime flows
- stabilize them before making them required

### Stage 5: Add coverage reporting

- implemented: publish backend coverage artifacts in CI
- implemented: enforce stable package-focused floors without a global gate

### Stage 6: Add pre-release Linux validation

- packaged build smoke
- host-native runtime smoke
- reverse proxy and upgrade/rollback smoke later

## Recommended CI Job Shape

Suggested first CI job split:

- `frontend-quality`
- `backend-test`
- `backend-static-analysis`
- `docker-integration`
- `coverage-report`

Later additions:

- `release-build`
- `pre-release-smoke`

## What Not To Do

- do not start with a huge lint configuration for Go
- do not gate merges on a global coverage target too early
- do not put `systemd` or VM-heavy checks on every normal PR
- do not add noisy analyzers that produce work without catching real defects

## Recommendation Summary

The practical order for Stacklab should be:

1. make current frontend and backend checks consistently green
2. add `go vet` and formatting checks
3. add `staticcheck`
4. add Docker-backed integration checks on GitHub Actions Linux `x64`
5. collect global coverage as a trend and gate stable critical packages
6. only later consider a global trend gate or heavier release-environment checks
