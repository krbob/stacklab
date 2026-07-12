# Continuous Integration

The workflow files under `.github/workflows/` are the source of truth for CI.
This document explains the current gates and their local equivalents; it is not
a rollout plan.

## Pull Requests And Main

The following workflows run for every pull request and every push to `main`:

| Workflow | Job or scope | Local equivalent |
| --- | --- | --- |
| `pr-quality.yml` | `frontend-quality` | `make check-frontend` |
| `pr-quality.yml` | `backend-test` with package coverage floors | `make check-backend-coverage` |
| `pr-quality.yml` | `backend-hygiene` | `make check-backend-hygiene` |
| `pr-quality.yml` | `repository-hygiene` | `make check-hygiene` |
| `docker-integration.yml` | Docker/Compose-backed HTTP and WebSocket integration tests | `go test -tags=integration ./internal/httpapi -count=1` |
| `browser-e2e.yml` | production frontend and backend exercised through Playwright | See [Browser End-to-End Tests](browser-e2e.md) |

`make check` is the canonical source-tree gate before a push. It verifies the
pinned toolchains, backend tests and hygiene, the generated OpenAPI client,
frontend tests/typecheck/build, documentation integrity, and repository
hygiene. Docker integration and browser E2E remain separate because they
require a running Docker daemon and additional runtime setup.

The backend coverage job reports global statement coverage as a trend and
enforces package-specific floors defined in
`scripts/quality/check-go-coverage.sh`. Its HTML, function, package, and
Markdown reports are retained as a workflow artifact for 14 days.

## Repository Hygiene

The `repository-hygiene` job enforces:

- valid GitHub Actions syntax with actionlint;
- ShellCheck for tracked scripts and Debian maintainer scripts;
- Gitleaks against complete Git history and normalized current lockfiles;
- valid local Markdown links, anchors, document structure, and section indexes;
- regression coverage for release-note generation and immutable compare ranges;
- generated third-party notice consistency;
- `npm audit --omit=dev`;
- zero-warning ESLint.

The documentation check operates on tracked Markdown and never requests
external URLs. Its focused local equivalent is `make check-docs`.

GitHub Actions use full commit SHAs. Tool versions and the ShellCheck archive
checksum are pinned. See [Repository Hygiene Checks](repository-hygiene.md) for
the exact update procedure and secret-scan exception policy.

## Advisory Analysis

`advisory-static-analysis.yml` runs `staticcheck` and `govulncheck` for pull
requests, pushes to `main`, and manual dispatches. These jobs provide additional
signal but are intentionally separate from the reproducible baseline. Changing
whether they block merges is a branch-protection decision in GitHub, not a
property of the workflow file.

## Packaging And Release Gates

Packaging smoke workflows run on relevant path changes and can also be started
manually:

- `deb-package-smoke.yml` builds a release artifact and Debian package, then
  tests installation and upgrade under systemd in a disposable environment;
- `tarball-install-smoke.yml` builds and smoke-tests the manual tarball install
  path on Debian.

`release-quality-gate.yml` is called by release workflows with an exact source
SHA. It requires all of these reusable checks to succeed:

1. PR baseline quality;
2. Docker integration;
3. browser E2E;
4. Debian/systemd package smoke.

Nightly, stable, and hotfix workflows run that gate before building or
publishing artifacts. Publication-specific validation and rollback procedures
are documented in the operations section.

## Failure Artifacts

- backend coverage is uploaded even when the coverage job fails;
- Playwright screenshots, traces, HTML report, raw results, and backend log are
  uploaded when browser E2E fails;
- failed workflow logs remain the first source for command output and runner
  context.

Do not fix a gate by weakening its threshold, adding a broad secret exception,
or skipping a failing test without documenting and reviewing the policy change.

## Changing CI

When changing workflows or quality scripts:

1. update the workflow and its local command together where applicable;
2. run the narrow local check, then `make check`;
3. run Docker integration or browser E2E when the changed seam requires it;
4. update this document only when the gate structure or ownership changes;
5. keep job names stable when branch protection depends on them.

Toolchain versions and local setup are documented in
[Reproducible Developer Checks](developer-checks.md).
