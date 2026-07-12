# Versioning and Release Policy

## Purpose

This document defines Stacklab version numbers, the active release train, its
automated quality boundary, dependency-update timing, and retention policy.

The policy describes the workflows that are operational today. Workflow files
remain the executable source of truth when implementation details change.

Status reviewed: 2026-07-12.

## Versioning

Stable releases use calendar versions:

```text
YYYY.MM.PATCH
```

- `YYYY.MM.0` is the regular release for a month;
- `PATCH > 0` is an additional stable hotfix in the same month;
- tags and GitHub release names use the same version.

Examples:

- `2026.07.0` — July stable release;
- `2026.07.1` — July hotfix.

Nightly Debian packages are prereleases of the next monthly stable and use a
Debian-sortable version such as:

```text
2026.08.0~nightly20260712+r123.gabcdef1
```

The workflow run number prevents two builds on the same date from comparing as
equal. A nightly GitHub prerelease uses a separate immutable tag of the form
`nightly-YYYYMMDD-<short-sha>`.

## Active Release Train

### Nightly

`.github/workflows/nightly-release.yml` runs every day at 03:17 in the
`Europe/Warsaw` timezone and can also be dispatched manually.

For a changed `main` revision it:

1. runs the shared release quality gate;
2. builds `amd64` and `arm64` tarballs and Debian packages;
3. smoke-tests the `amd64` package and tarball install paths;
4. creates a GitHub prerelease;
5. publishes the signed APT `nightly` channel;
6. smoke-installs the exact version from the published repository;
7. removes old nightly prereleases beyond the retention limit.

If the newest nightly already points to the current commit, publication is
skipped. Nightly is the soak channel for changes intended for the next stable.

### Monthly stable

`.github/workflows/stable-release.yml` runs automatically on the first day of
each month and supports manual dispatch as an operational fallback.

The default version is `YYYY.MM.0`. Publication is skipped when that tag already
exists or when there are no commits after the previous stable. Otherwise the
workflow applies the same quality, dual-architecture build, install-smoke, APT
publication, and post-publication APT checks as the release train requires.

The schedule publishes the already-green state of `main`; it does not merge a
dependency batch or bypass normal review on release day. A known critical
regression must be fixed or the scheduled run must be cancelled before it
reaches publication.

### Hotfix

`.github/workflows/hotfix-release.yml` is manual-only and requires an explicit,
unused version such as `2026.07.1`. It runs the shared quality gate and package
smokes, creates a normal GitHub release, publishes the packages into APT
`stable`, and verifies the published version.

Security fixes, production regressions, and urgent platform compatibility fixes
are valid hotfix reasons. The decision to cut or roll back a hotfix remains a
human responsibility.

## Automated Quality Boundary

All nightly, stable, and hotfix workflows call
`.github/workflows/release-quality-gate.yml` for the exact source revision.
Publication cannot proceed unless all of these reusable workflows succeed:

- baseline PR quality: frontend generation/drift, tests, typecheck and build;
  backend tests and coverage gates; Go and repository hygiene;
- Docker-backed integration smoke;
- browser E2E smoke;
- Debian/systemd package smoke.

Release workflows then build both supported architectures and independently
smoke-test the produced `amd64` Debian package and tarball before publication.
The exact package version is installed again from APT after publication. Manual
real-host validation complements these gates for risky changes; it is not a
hidden approval step in the scheduled workflows. See
[release-plan.md](release-plan.md).

## APT Channels And Retention

Both channels are signed and published from the `gh-pages` repository state:

- `stable` receives monthly releases and hotfixes and retains the newest six
  package versions;
- `nightly` receives changed daily builds and retains the newest seven package
  versions;
- GitHub retains the newest 14 Stacklab nightly prereleases;
- stable GitHub releases are not removed by the nightly cleanup job.

The manual `.github/workflows/apt-publish.yml` workflow can republish an existing
GitHub release to either channel and optionally override package retention. It
is an operational recovery tool, not a replacement for the normal release
train.

## Renovate Window

Renovate may open and rebase dependency PRs continuously. Eligible green PRs
can automerge only on days 2 and 3 of the month in `Europe/Warsaw`, immediately
after the regular stable publication and before the rest-of-month nightly soak.

The following updates are never included in that automatic window:

- major-version updates;
- GitHub Actions updates;
- the high-risk runtime modules `modernc.org/sqlite`,
  `github.com/gorilla/websocket`, and `github.com/creack/pty`.

Those PRs require deliberate review and merge. Failed or missing required checks
also prevent automerge. The window controls merge timing; it does not request
reviewers or subscribe maintainers to PR notifications.

## Operating Rules

- Keep `main` releasable; the monthly workflow has no separate staging branch.
- Do not weaken the shared quality gate to make a scheduled release pass.
- Validate risky host, migration, helper, Docker daemon, self-update, and
  architecture-specific changes before they reach the monthly boundary.
- Use nightly packages for soak and upgrade checks; do not treat a prerelease as
  the stable operator channel.
- Use a hotfix for an urgent correction instead of retagging or replacing an
  existing release.
- Deployment to an operator's host and rollback decisions remain manual.

## Published Baseline

The automated monthly stable line has been active since `2026.04.0`, with
scheduled nightly prereleases in between. The GitHub Releases page is the
canonical current version list. This history demonstrates the cadence; it is
not a promise to publish an unchanged or failing revision merely because the
calendar fires.
