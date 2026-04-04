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
- backend test coverage exists but is still relatively low and should be treated as a trend, not a merge gate
- Docker-backed integration tests now cover WebSocket flows and core Compose lifecycle behavior
- a dedicated GitHub Actions workflow can now run that suite, but it should prove stable before becoming a required gate
- a representative OpenAPI-backed contract suite now exists for core success-path endpoints
- advisory GitHub Actions checks now exist for `staticcheck` and `govulncheck`
- an advisory browser E2E workflow now exists for lightweight Playwright smoke

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
- `npm run lint`
- `npm run build`

### Backend

- `go test ./cmd/... ./internal/...`
- `go vet ./cmd/... ./internal/...`
- `gofmt -l cmd internal` with failure on unformatted files

Purpose:

- catch obvious breakage fast
- keep the codebase syntactically healthy
- keep PR feedback loop short

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

Then add:

- Docker-backed integration suite

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

Coverage should be collected and reported.

It should not yet be enforced as a hard global threshold.

Reasoning:

- current backend coverage is still modest
- critical runtime value comes more from the right integration tests than from a bigger percentage
- a hard threshold too early encourages filler tests

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

## Future policy option

Later, when coverage is healthier, adopt one of these:

- non-decreasing global coverage
- non-decreasing coverage for selected critical packages
- comment-only reporting in PRs without hard failure

Recommendation:

- prefer package-focused or trend-focused coverage policy over a single global threshold

## Recommended Command Set

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
go test ./cmd/... ./internal/... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

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

- publish coverage numbers in CI
- do not enforce hard thresholds yet

### Stage 6: Add pre-release Linux `amd64` validation

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

- `release-build-linux-amd64`
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
5. collect coverage as a trend
6. only later consider stricter coverage or heavier release-environment checks
