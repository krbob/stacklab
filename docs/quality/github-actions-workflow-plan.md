# GitHub Actions Workflow Plan

## Purpose

This document translates the broader CI quality plan into a concrete GitHub Actions layout.

It defines:

- planned workflow files under `.github/workflows/`
- workflow triggers
- job names
- job responsibilities
- which checks should become required
- rollout order

This is now partly historical: several workflows in this document are already implemented, while later release-oriented workflows remain planned.

## Guiding Principles

- keep the first workflows small and stable
- prefer a few high-signal jobs over many noisy jobs
- separate fast PR checks from heavier Docker-backed integration checks
- keep pre-release and deployment-shaped validation out of normal PR flow at first

## Recommended Initial Workflow Set

Recommended future workflow files:

- `.github/workflows/pr-quality.yml`
- `.github/workflows/docker-integration.yml`
- `.github/workflows/advisory-static-analysis.yml`
- `.github/workflows/browser-e2e.yml`
- `.github/workflows/release-build.yml`
- `.github/workflows/tarball-install-smoke.yml`
- `.github/workflows/nightly-release.yml`
- `.github/workflows/stable-release.yml`
- `.github/workflows/hotfix-release.yml`
- `.github/workflows/apt-publish.yml`
- `.github/workflows/apt-repo-smoke.yml`
- `.github/workflows/pre-release-smoke.yml`

Currently implemented:

- `.github/workflows/pr-quality.yml`
- `.github/workflows/docker-integration.yml`
- `.github/workflows/advisory-static-analysis.yml`
- `.github/workflows/browser-e2e.yml`
- `.github/workflows/release-build.yml`
- `.github/workflows/deb-package-smoke.yml`
- `.github/workflows/tarball-install-smoke.yml`
- `.github/workflows/nightly-release.yml`
- `.github/workflows/stable-release.yml`
- `.github/workflows/hotfix-release.yml`
- `.github/workflows/apt-publish.yml`
- `.github/workflows/apt-repo-smoke.yml`

Current `release-build.yml` scope:

- tarball artifacts for `amd64` and `arm64`
- `.deb` artifacts for `amd64` and `arm64`
- manual invocation only

Current release automation direction:

- `deb-package-smoke.yml` validates fresh-install package behavior on Debian
- `tarball-install-smoke.yml` validates tarball install, upgrade, and rollback mechanics in a disposable Debian container
- `nightly-release.yml` is the target prerelease workflow
- `stable-release.yml` is the target monthly stable workflow
- `hotfix-release.yml` is the target manual patch-release workflow
- `apt-publish.yml` is the manual repair and republish path for APT channels
- `apt-repo-smoke.yml` is the manual end-to-end validation path for the published APT repository
- release workflows should also run an automatic post-publish APT smoke step for their own channel

The advisory workflows should run, but they should not become required too early.

Release automation note:

- future scheduled workflows should not run exactly at the top of the hour
- GitHub schedule runs can be delayed or dropped under load
- every scheduled release workflow should also support `workflow_dispatch`

## 1. `pr-quality.yml`

## Purpose

This is the fast baseline workflow for every normal pull request.

It should provide quick feedback and be stable enough to become branch protection.

## Recommended triggers

- `pull_request`
- `push` to `main`

## Recommended jobs

### `frontend-quality`

Runner:

- `ubuntu-latest`

Setup:

- Node `22`
- npm cache enabled

Commands:

```bash
cd frontend
npm ci
npm run typecheck
npm run lint
npm run build
```

Purpose:

- validate frontend correctness
- keep TypeScript strictness enforced
- keep frontend lint green
- ensure production build keeps working

### `backend-test`

Runner:

- `ubuntu-latest`

Setup:

- Go `1.26.2`
- Go module cache enabled

Commands:

```bash
go test ./cmd/... ./internal/...
```

Purpose:

- run backend unit and non-Docker integration tests

### `backend-hygiene`

Runner:

- `ubuntu-latest`

Commands:

```bash
test -z "$(gofmt -l cmd internal)"
go vet ./cmd/... ./internal/...
```

Purpose:

- enforce formatting
- catch basic Go correctness issues not covered by tests

## Required check recommendation

These job names should eventually become required:

- `frontend-quality`
- `backend-test`
- `backend-hygiene`

## Notes

- this workflow should stay fast
- avoid adding Docker, release packaging, or staging deployment concerns here

## 2. `docker-integration.yml`

## Purpose

This is the most important medium-speed validation workflow for Stacklab.

It should exercise the real Docker and WebSocket seams that dependency updates and refactors are most likely to break.

## Recommended triggers

Initial:

- `pull_request`
- `workflow_dispatch`

Optional later:

- `push` to `main`

## Recommended runner

- GitHub-hosted `ubuntu-latest`

This is preferred over a VM for normal PR validation.

## Recommended job

### `docker-integration`

Setup:

- Node `22`
- Go `1.26.2`
- Docker and Compose available on runner

Recommended preparation:

- build frontend assets
- build backend binary or run backend directly
- create isolated fixture directories under a temporary root
- seed fixture Compose stacks for:
  - image-only service
  - build-based service
  - invalid compose definition
  - stopped stack
  - stack with healthcheck

Recommended validation scope:

- login/session flow
- stack discovery
- stack detail fetch
- definition read
- draft resolved preview
- definition save
- create stack
- delete stack
- `up`, `down`, `restart`
- WebSocket jobs stream
- WebSocket logs stream
- WebSocket stats stream
- WebSocket terminal open, attach, input, resize, close

Recommended command shape:

- backend tests already using integration tags where useful
- plus a small smoke harness script for full-stack fixture validation if needed later

## Required check recommendation

This should become required only after it is stable and low-flake:

- `docker-integration`

Until then, it can exist as advisory or be run only on `main` and `workflow_dispatch`.

## Notes

- this is the workflow that most directly hardens Stacklab against Renovate regressions
- for Stacklab, this job is more important than a high coverage percentage

## 2a. `advisory-static-analysis.yml`

## Purpose

This workflow provides extra backend code-quality and vulnerability signal without blocking merges.

## Recommended triggers

- `pull_request`
- `push` to `main`
- `workflow_dispatch`

## Recommended jobs

### `backend-staticcheck`

Commands:

```bash
go run honnef.co/go/tools/cmd/staticcheck@latest ./cmd/... ./internal/...
```

### `backend-vulncheck`

Commands:

```bash
go run golang.org/x/vuln/cmd/govulncheck@latest ./cmd/... ./internal/...
```

## Required check recommendation

Keep both advisory at first:

- `backend-staticcheck`
- `backend-vulncheck`

Promote only after they prove stable and useful over several normal PR cycles.

## 2b. `browser-e2e.yml`

## Purpose

This workflow provides lightweight browser-level smoke against the real backend harness and built frontend.

It should stay intentionally small and focus on critical end-user paths rather than deep runtime protocol coverage.

## Recommended triggers

- `pull_request`
- `push` to `main`
- `workflow_dispatch`

## Recommended jobs

### `browser-e2e`

Setup:

- Node `24`
- Go from `go.mod`
- Playwright Chromium installed on the runner

Recommended validation scope:

- login redirect and successful login
- dashboard visibility and stack navigation
- editor load, preview, and save flow
- create and delete stack flow
- global audit page visibility

## Required check recommendation

Keep this advisory at first:

- `browser-e2e`

Promote only after:

- repeated green runs
- no significant flakiness
- selectors and fixture isolation prove stable over normal PR traffic

## Notes

- this workflow should complement, not replace, backend Docker-backed integration tests
- on failure it should upload Playwright screenshots or traces and backend harness logs

## 3. `release-build.yml`

## Purpose

This workflow builds the distributable Linux release artifacts described in the release plan.

It is not a normal PR gate at the beginning.

## Current posture

- implemented
- manual `workflow_dispatch`
- uploads workflow artifacts only
- does not publish GitHub Releases yet

## Recommended triggers

Initial:

- `workflow_dispatch`

Later:

- `push` tags like `v*`

## Recommended jobs

### `release-build`

Runner:

- `ubuntu-latest`

Setup:

- Node `22`
- Go `1.26.2`

Commands:

```bash
cd frontend && npm ci && npm run build
GOOS=linux GOARCH=amd64 go build -o dist/bin/stacklab ./cmd/stacklab
GOOS=linux GOARCH=arm64 go build -o dist/bin/stacklab ./cmd/stacklab
```

Then package:

- frontend `dist/`
- backend binary
- version metadata
- optional example `systemd` unit

Output:

- upload one artifact per supported architecture
- later create GitHub Release asset

## Advisory check recommendation

At first:

- advisory only

Later:

- useful on tags and release branches
- not necessary as a required PR gate for every change

## 4. `pre-release-smoke.yml`

## Purpose

This workflow is for production-shaped verification.

It should validate concerns that GitHub-hosted Docker runners do not model well:

- `systemd`
- service user permissions
- host-native runtime
- reverse proxy integration
- upgrade and rollback flow

## Recommended triggers

- `workflow_dispatch`

Potential later trigger:

- before publishing a release candidate

## Recommended runner

One of:

- trusted self-hosted Linux `amd64` runner
- staging VM with required tooling

## Recommended jobs

### `pre-release-smoke`

Validation scope:

- install or unpack release artifact
- start service under `systemd`
- verify `/api/health`
- verify login and stack discovery
- verify Docker socket access
- verify reverse proxy path if configured
- verify one upgrade and one rollback

## Required check recommendation

Do not make this a required normal PR check.

Use it for:

- release preparation
- manual release approval
- staging validation

## Workflow Dependencies

Recommended logical order:

1. `pr-quality.yml`
2. `docker-integration.yml`
3. `release-build.yml`
4. `pre-release-smoke.yml`

This order matters operationally too:

- first make fast checks reliable
- then add runtime validation
- then add packaging
- then add production-shaped smoke

## Suggested Job Names For Branch Protection

Use stable job names so branch protection does not have to be renamed repeatedly.

Recommended names:

- `frontend-quality`
- `backend-test`
- `backend-hygiene`
- `docker-integration`

Advisory or later-stage names:

- `backend-static-analysis`
- `backend-vulncheck`
- `coverage-report`
- `release-build`
- `pre-release-smoke`

## Suggested Static Analysis Expansion

After the first workflows are stable, add to `pr-quality.yml` or a separate workflow:

### `backend-static-analysis`

Commands:

```bash
staticcheck ./...
```

### `backend-vulncheck`

Commands:

```bash
govulncheck ./...
```

Recommended status at first:

- advisory

Possible later promotion:

- `backend-static-analysis` can become required if it proves stable
- `backend-vulncheck` may remain advisory if it produces noisy transient results

## Suggested Coverage Reporting

Coverage should be a reporting concern first, not a hard gate.

Recommended job:

### `coverage-report`

Commands:

```bash
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

Optional later:

- upload artifact
- PR comment summary
- package-level trend reporting

Recommended status:

- advisory

## Caching Recommendations

### Node

- cache npm based on `frontend/package-lock.json`

### Go

- cache module downloads
- cache build artifacts conservatively

### Docker

For now:

- keep Docker caching minimal

Reason:

- correctness and stability matter more than squeezing a few extra seconds from early CI

## Failure Philosophy

If a check becomes required, it should be:

- consistently green when the code is healthy
- deterministic
- understandable when it fails

Do not make a flaky or poorly explained job required just because it sounds useful.

## Recommended Rollout Sequence

### Stage 1

Implement:

- `pr-quality.yml`

Make required:

- `frontend-quality`
- `backend-test`
- `backend-hygiene`

### Stage 2

Implement:

- `docker-integration.yml`

Start as:

- advisory or manual

Then promote to required after stability is proven.

### Stage 3

Implement:

- `backend-static-analysis`
- `backend-vulncheck`
- `coverage-report`

Keep:

- advisory at first

### Stage 4

Implement:

- `release-build.yml`

Use for:

- tag builds
- manual release verification

### Stage 5

Implement:

- `pre-release-smoke.yml`

Use for:

- release candidate validation
- deployment rehearsal

## Near-Term Recommendation

The next concrete CI step for Stacklab should be:

1. get frontend lint consistently green
2. implement `pr-quality.yml`
3. implement `docker-integration.yml`
4. only after that revisit Renovate enablement
