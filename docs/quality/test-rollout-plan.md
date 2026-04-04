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
- opt-in backend Docker-backed integration tests for WebSocket flows
- frontend Vitest tests for selected components and API client behavior
- frontend typecheck, lint, and production build

The biggest remaining quality gaps are:

- no GitHub Actions workflows yet
- no required PR quality gates
- incomplete Docker-backed coverage for mutating Compose lifecycle flows
- no OpenAPI contract validation layer
- no browser-level E2E smoke

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

Implement now.

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

Do shortly after Step 1 proves stable.

Scope:

- branch protection on `main`
- require:
  - `frontend-quality`
  - `backend-test`
  - `backend-hygiene`

Do not add heavier jobs as required yet.

## Step 3: Expand Docker-backed backend integration

Implement after baseline CI is stable.

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

Implement after Step 3 has enough stable tests to justify a dedicated workflow.

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

Implement after baseline CI and broader runtime tests.

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

## Step 7: Add static analysis advisory checks

Implement after Step 1 or in parallel if low-friction.

Recommended checks:

- `staticcheck ./...`
- `govulncheck ./...`

Initial posture:

- advisory

Promote only after:

- signal is high
- noise is manageable

## Step 8: Add lightweight browser E2E smoke

Implement later, after API/runtime coverage is healthier.

Keep it intentionally small.

Recommended scenarios:

1. login -> dashboard
2. create stack -> deploy -> logs
3. editor save -> progress -> audit
4. terminal open -> close

This should be smoke, not a giant browser test pyramid.

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

1. land `pr-quality.yml`
2. make sure it is green locally and on GitHub
3. enable branch protection for baseline checks
4. expand backend Docker-backed lifecycle tests
5. add `docker-integration.yml`
6. expand frontend hook/provider tests
7. add API contract validation
8. add advisory `staticcheck` and `govulncheck`

## What We Are Doing Now

Current action:

- implement Step 1 immediately

This is the highest-leverage move because it protects the repository today without waiting for the rest of the rollout.
