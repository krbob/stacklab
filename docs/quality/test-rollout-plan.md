# Test and CI Rollout Plan

## Purpose

This document turns the broader quality strategy into an execution plan for the next implementation steps.

It answers:

- what to do next
- in what order
- what should be implemented now versus later

## Current State

Today the repository already has:

- backend unit tests
- backend HTTP and WebSocket integration tests without Docker
- opt-in backend Docker-backed integration tests for WebSocket flows and core Compose lifecycle flows
- frontend Vitest tests for selected components and API client behavior
- frontend runtime hook tests for log, stats, job, and terminal streams
- frontend typecheck, lint, and production build

The biggest remaining quality gaps are:

- no browser-level E2E smoke
- no browser-level E2E coverage

## Rollout Principles

### 1. Land stable CI before adding more test categories

We should first enforce the checks that are already green locally.

That gives us immediate regression protection without inventing new failure modes.

### 2. Prefer runtime-relevant tests over ornamental coverage work

For Stacklab, Docker-backed integration and WebSocket coverage matter more than inflating a global percentage.

### 3. Add heavier checks only after lighter ones are stable

Required checks should become strict only after they prove reliable in normal development.

## Execution Plan

## Step 1: Baseline PR CI

Status:

- implemented

Scope:

- GitHub Actions workflow for normal pull requests and pushes to `main`
- frontend tests and build
- backend tests
- backend formatting and `go vet`

Required jobs:

- `frontend-quality`
- `backend-test`
- `backend-hygiene`

Commands:

```bash
cd frontend
npm ci
npm test
npm run typecheck
npm run lint
npm run build

go test ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
test -z "$(gofmt -l cmd internal)"
```

Success criteria:

- all commands are green locally
- workflow is green on GitHub
- no flaky behavior appears in the first few PR runs

## Step 2: Make baseline checks required

Status:

- implemented

Scope:

- branch protection on `main`
- require:
  - `frontend-quality`
  - `backend-test`
  - `backend-hygiene`

Do not add heavier jobs as required yet.

## Step 3: Expand Docker-backed backend integration

Status:

- implemented

Primary target:

- turn the current partial Docker-backed backend tests into broader lifecycle coverage

Add coverage for:

- `up`
- `down`
- `restart`
- `create`
- `delete`
- `save_definition`
- `resolved-config`
- orphaned stack behavior
- runtime discovery after real Compose actions

Recommendation:

- keep these as Go integration tests under a separate tag or profile
- keep them deterministic and fixture-driven

## Step 4: Add `docker-integration.yml`

Status:

- implemented

Scope:

- GitHub Actions Linux `x64` runner
- Docker Engine and Compose available
- run Docker-backed backend integration suite

Initial posture:

- advisory or non-required at first

Promote to required only when:

- it is consistently green
- it does not show flakiness under normal PR traffic

## Step 5: Expand frontend runtime tests

Implement after the CI baseline is in place.

Highest-value frontend targets:

- `AuthProvider`
- `WsProvider`
- `use-log-stream`
- `use-stats-stream`
- `use-job-stream`
- `use-terminal`

Why these first:

- they contain reconnect logic
- they contain session-expiry behavior
- they contain stream deduplication and state-machine behavior

These are more valuable than adding many low-signal visual component tests.

## Step 6: Add API contract validation

Status:

- initial representative success-path suite implemented

Goal:

- validate selected HTTP responses against `docs/api/openapi.yaml`

Start with representative endpoints:

- `POST /api/auth/login`
- `GET /api/session`
- `GET /api/stacks`
- `GET /api/stacks/{stackId}`
- `GET /api/stacks/{stackId}/definition`
- `GET /api/jobs/{jobId}`
- `GET /api/audit`

Recommendation:

- start with a narrow contract suite
- do not try to validate every endpoint in one pass

Current implemented scope:

- login
- session
- meta
- stack list and detail
- definition read
- resolved-config draft preview
- definition save
- job fetch
- per-stack and global audit
- stack create and delete responses

## Step 7: Add static analysis advisory checks

Status:

- implemented

Recommended checks:

- `staticcheck ./...`
- `govulncheck ./...`

Initial posture:

- advisory

Promote only after:

- signal is high
- noise is manageable

## Step 8: Add lightweight browser E2E smoke

Status:

- implemented as advisory browser smoke

Implement later, after API/runtime coverage is healthier.

Keep it intentionally small.

Recommended scenarios:

1. login -> dashboard
2. editor load -> preview -> save -> audit visibility
3. create stack -> delete stack
4. global audit page load and action visibility

This should be smoke, not a giant browser test pyramid.

Current workflow:

- `.github/workflows/browser-e2e.yml`

Current posture:

- advisory, not required yet

## Step 9: Add pre-release environment validation

Keep this outside normal PR validation at first.

Scope:

- packaged Linux `amd64` build
- `systemd` service smoke
- reverse proxy smoke if configured
- upgrade and rollback smoke
- Debian package install and upgrade later

Environment:

- staging Debian `amd64` VM
- staging homelab host

## Recommended Immediate Order

The next concrete implementation order should be:

1. expand API contract validation
2. add advisory `staticcheck` and `govulncheck`
3. add lightweight browser E2E smoke
4. add pre-release environment validation

## What We Are Doing Now

Current action:

- observe browser E2E stability before promoting it higher

The next high-leverage moves are Docker-backed stability and more precise runtime coverage, not a larger browser pyramid.
