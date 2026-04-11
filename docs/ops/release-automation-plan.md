# Release Automation Plan

## Purpose

This document defines the target release automation model for Stacklab, including:

- nightly builds
- monthly stable releases
- manual hotfix releases
- Git tags and GitHub Releases
- APT publication for `nightly` and `stable`

This document now describes the intended operating model for release automation.

## Desired Release Model

Stacklab should eventually support three release tracks:

- `nightly`
- `stable`
- `hotfix`

### Nightly

Nightly is an automated prerelease build for the default branch.

Rules:

- runs on a nightly GitHub Actions schedule
- builds only if `main` has changed since the previous nightly
- runs only if required release validation is green
- publishes:
  - GitHub prerelease
  - APT `nightly` package

Nightly is for:

- early operator testing
- confirming packaging and upgrade automation
- catching issues before the next monthly stable

### Stable

Stable is the monthly release.

Rules:

- version format: `YYYY.MM.0`
- example: `2026.05.0`
- intended publish date: the `1st` day of the month
- published to:
  - GitHub Release
  - APT `stable`

Stable should represent the already-green state of `main`, not a monthly merge event.

### Hotfix

Hotfix is a manual release outside the normal monthly window.

Rules:

- version format: `YYYY.MM.X`
- examples:
  - `2026.05.1`
  - `2026.05.2`
- triggered manually
- published to:
  - GitHub Release
  - APT `stable`

Hotfix exists for:

- production regressions
- urgent compatibility fixes
- packaging or deployment issues discovered after a stable release

## Versioning and Tagging

Stacklab should continue using calendar versioning.

Stable and hotfix tags:

- `2026.05.0`
- `2026.05.1`

Nightly tags should be unique and disposable:

- `nightly-20260404-abcdef1`

Nightly package versions should sort below the future stable release for the same month.

Recommended Debian version shape:

- `2026.05.0~nightly20260404+gabcdef1`

Why:

- `~nightly` sorts before `2026.05.0`
- upgrading from nightly to stable is natural for APT
- every nightly remains uniquely identifiable

## Can the Monthly Stable Release Be Automatic?

Yes, but only under explicit conditions.

An automatic stable release on the `1st` day of the month is reasonable **if**:

- high-risk updates are still merged manually
- only low-risk dependency classes are allowed to automerge
- `main` is kept continuously releasable
- required checks are all green at release time
- there are meaningful changes since the previous stable release

This means the monthly stable should publish the current state of `main`.

It should **not** depend on a batch of PRs all being merged at release time.

### Why this is safer than a monthly merge wave

Monthly merge waves are brittle:

- multiple green PRs can still conflict semantically when merged together
- release day becomes both integration day and publication day
- failures are harder to isolate

The safer model is:

- merge safe updates continuously
- manually merge risky updates when ready
- keep `main` green
- let the monthly release workflow publish what is already stable

## Recommended Monthly Release Train

The preferred operating model is a short integration window followed by a longer soak period.

Recommended cadence:

- on the `1st`: publish the monthly stable from the already-green state of `main`
- early in the month after the stable release: allow selective automerge for low-risk Renovate PRs
- rest of the month: run nightly prereleases from `main`
- during the month: manually merge risky updates only when they are intentionally reviewed

This gives Stacklab three distinct phases:

- dependency integration
- nightly soak and homelab validation
- stable publication

It is intentionally **not**:

- merge everything on release day
- immediately release whatever was just merged

### Low-risk merge window

The intended long-term model is:

- keep low-risk automerge narrowly scoped
- prefer a short merge window early in the month
- keep medium-risk and high-risk PRs manual

This should cover only updates that repeatedly prove safe in CI and in nightly testing.

### Nightly soak period

Nightly prereleases should be the operator feedback loop between dependency integration and stable publication.

Purpose:

- test the package and upgrade path on a real homelab host
- catch regressions that pass CI but behave badly on Debian or Docker
- validate that the coming monthly stable is not just "green in Actions" but also healthy in real use

### Automatic stable on the 1st

Publishing the stable release on the `1st` is reasonable only if all of the following are true:

- the release candidate is simply the current `main`
- required checks are green
- nightly and package smoke validation are green
- there are meaningful changes since the previous stable
- no known blocking regression remains open

If those conditions are not met, the stable workflow should skip or be rerun manually after the issue is fixed.

## Renovate Policy for Release Automation

Release automation depends on a stricter dependency policy than the current "all manual" baseline.

Recommended long-term split:

### Low-risk classes

Current candidates for selective automerge:

- frontend devDependencies
- linting and formatting tools
- selected GitHub Actions updates:
  - `actions/checkout`
  - `actions/setup-go`
  - `actions/setup-node`
- other tooling dependencies that repeatedly prove safe in CI

### Medium-risk classes

Keep manual:

- frontend runtime dependencies
- UI/editor libraries
- browser automation dependencies
- GitHub Actions that affect release, Pages, packaging, or deployment behavior

### High-risk classes

Keep manual:

- Go dependencies that affect:
  - WebSocket transport
  - PTY/session handling
  - SQLite/persistence
  - Docker/Compose execution
- Docker-facing runtime dependencies
- major version bumps unless explicitly reviewed

Practical consequence:

- automatic monthly stable is viable only if automerge is constrained to low-risk updates
- majors and risky runtime changes must still be merged intentionally before release day
- the stable release should happen before the next early-month automerge window opens

## GitHub Releases and Release Notes

Every stable and hotfix release should have:

- a Git tag
- a GitHub Release page
- release notes

Recommended model:

- use GitHub-generated release notes as the baseline
- optionally refine categories later with `.github/release.yml`
- keep nightly as prereleases
- keep stable and hotfix as normal releases

Current implementation direction:

- Stacklab now includes a commit-message-based release notes generator:
  - `scripts/release/generate-release-notes.sh`
- the same generator is intended to be used by:
  - automatic nightly releases
  - automatic monthly stable releases later
  - manual hotfix releases

This keeps release notes consistent across both automated and manual publication flows.

## Post-Publish Verification

Release automation should not stop at "tag created" or "APT metadata pushed".

Required post-publish verification:

- wait until GitHub Pages serves:
  - the public APT key
  - the channel `InRelease`
- run an automated Debian install smoke against the published repository

Purpose:

- verify that the published repo is reachable, not just committed to `gh-pages`
- catch Pages propagation or metadata issues after publication
- prove that `apt update && apt install stacklab` works from the published channel

## APT Channel Model

The future APT repository should expose at least two channels:

- `stable`
- `nightly`

Meaning:

- `stable` tracks the latest monthly or hotfix release
- `nightly` tracks the most recent prerelease build from `main`

Recommended package name:

- `stacklab`

Recommended architecture support:

- `amd64`
- `arm64`

Recommended publication behavior:

- stable workflow publishes to GitHub Release and APT `stable`
- nightly workflow publishes to GitHub prerelease and APT `nightly`
- hotfix workflow publishes to GitHub Release and APT `stable`

Retention policy:

- APT `stable` keeps the newest 6 package versions in the channel pool
- APT `nightly` keeps the newest 7 package versions in the channel pool
- GitHub nightly prereleases keep the newest 14 prereleases and delete older
  nightly tags after published APT smoke succeeds
- stable and hotfix GitHub Releases are the long-term archive for manual
  rollback and release investigation

## Required Secrets And Repository Setup

To make the automated release path work, the repository will need:

- branch protection that keeps `main` releasable
- GitHub Actions enabled for scheduled workflows
- a `gh-pages` branch or permission to create it
- GitHub Pages configured to serve the publication branch

APT publication requires these repository secrets:

- `APT_GPG_PRIVATE_KEY_BASE64`
  - base64-encoded ASCII-armored private key used to sign repository metadata
- `APT_GPG_PASSPHRASE`
  - optional passphrase for that key
- `APT_GPG_KEY_ID`
  - optional explicit key id or fingerprint used for signing

Nightly and stable workflows use the built-in `GITHUB_TOKEN` for:

- creating tags
- creating GitHub Releases
- pushing APT repository updates to `gh-pages`

The published APT repository should expose the public signing key at:

- `https://krbob.github.io/stacklab/apt/stacklab-archive-keyring.gpg`
- `https://krbob.github.io/stacklab/apt/stacklab-archive-keyring.asc`

## Workflow Set

Target workflow set:

- `release-build.yml`
- `nightly-release.yml`
- `stable-release.yml`
- `hotfix-release.yml`
- `apt-publish.yml`
- `apt-repo-smoke.yml`

### `release-build.yml`

Already exists.

Purpose:

- build tarball artifacts for supported architectures

### `nightly-release.yml`

Purpose:

- nightly prerelease for the default branch

Recommended behavior:

1. run on schedule and `workflow_dispatch`
2. verify required checks or rerun the release build path
3. detect whether `main` changed since the last nightly tag
4. if unchanged, exit cleanly without publishing
5. build release artifacts and `.deb`
6. create nightly tag
7. create GitHub prerelease
8. publish to APT `nightly`
9. generate release notes from commit messages since the previous nightly

### `stable-release.yml`

Purpose:

- monthly stable release

Recommended behavior:

1. run on schedule and `workflow_dispatch`
2. determine current stable version, e.g. `2026.05.0`
3. exit cleanly if that version already exists
4. verify required release gates are green
5. verify there are changes since the previous stable release
6. build release artifacts and `.deb`
7. create stable tag
8. create GitHub Release with notes
9. publish to APT `stable`

Release notes source:

- commit messages since the previous stable or hotfix tag

### `hotfix-release.yml`

Purpose:

- manual stable patch release

Recommended behavior:

1. run only through `workflow_dispatch`
2. require explicit version input, e.g. `2026.05.1`
3. build release artifacts and `.deb`
4. create tag
5. create GitHub Release
6. publish to APT `stable`

Release notes source:

- commit messages since the previous stable or hotfix tag

### `apt-publish.yml`

Purpose:

- build or update signed APT repository metadata from already-built `.deb` artifacts

Recommended behavior:

- takes packages from the release workflow
- updates the selected channel
- signs repository metadata
- publishes static files

Practical role:

- manual repair or re-publication path if APT channel state needs to be rebuilt from an existing release tag

### `apt-repo-smoke.yml`

Purpose:

- manually validate that the published GitHub Pages APT repository can install `stacklab` through normal `apt`

Practical role:

- end-to-end verification of the public key, repository metadata, and package installation path after publication

## Scheduling Notes

GitHub scheduled workflows:

- run only from the default branch
- can be delayed during periods of high Actions load
- can even be dropped during high load, especially near the start of the hour

Practical recommendation:

- do not schedule at `0 * * * *`
- choose an off-hour minute like `17` or `43`
- always keep `workflow_dispatch` as a manual fallback

## Rollout Sequence

Implement this in phases.

### Phase 1

- build `.deb` artifacts
- keep stable releases manual
- keep nightly disabled
- keep tarball and `.deb` as separate install modes; do not plan cross-mode migration

### Phase 2

- publish stable releases manually through a dedicated workflow
- publish to GitHub Release
- publish to APT `stable`

### Phase 3

- add nightly prereleases and APT `nightly`
- skip unchanged nightlies automatically

### Phase 4

- allow selective Renovate automerge for low-risk classes
- automate monthly stable release on the `1st`

## Minimum Preconditions Before Full Automation

Do not automate the monthly stable release until all of these are true:

- required CI is stable over normal feature and Renovate PRs
- package install and upgrade are validated on Debian `amd64`
- package install and upgrade are validated on Debian `arm64`
- release workflow failures are easy to understand and recover from
- APT publication is signed and reproducible
- nightly has already run successfully for some time

## Recommendation for the Current Phase

Do now:

- document the target model
- implement `.deb` build first
- keep stable publication manual
- keep hotfix publication manual
- prepare nightly as a later step

Do later:

- enable nightly prereleases
- enable APT publication
- enable selective automerge for low-risk updates
- finally enable automatic monthly stable publication
