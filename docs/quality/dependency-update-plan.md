# Dependency Update Plan

## Purpose

This document records the recommended policy for dependency updates in Stacklab, including:

- whether and when to adopt Renovate
- what CI checks must exist before doing so
- which updates should remain manual
- how Docker-backed CI should harden the application against upgrade regressions

This document records the intended steady-state dependency policy for the repository.

## Current Recommendation

Recommendation:

- use Renovate continuously
- keep manual merge as the default
- allow selective automerge only for trusted, low-risk update classes
- use nightly prereleases and monthly stable releases as the soak and publication loop

Reasoning:

- Stacklab is an operational tool, not a static website
- several critical code paths depend on WebSocket behavior, terminal PTY handling, Docker CLI interaction, and Compose behavior
- many regressions from dependency bumps would not be caught by unit tests alone

## Why Renovate Still Makes Sense

Benefits:

- keeps Go and npm dependencies from drifting too far behind
- surfaces security and maintenance updates in a predictable cadence
- makes updates reviewable in small PRs instead of large irregular catch-up batches
- works well with branch protection once CI is trustworthy

Main risk:

- dependency updates can introduce subtle regressions in runtime areas that are hard to validate with static checks alone

Conclusion:

- Renovate is useful for Stacklab
- it should be gated by good CI rather than trusted blindly

## Adoption Timing

Do not widen automerge scope if:

- frontend and backend integration is still visibly unstable
- core PR validation is not green and reliable
- key end-to-end workflows still change shape every few days

Enable low-risk automerge when all of the following are true:

- `main` has a stable baseline of automated checks
- the current application milestone is functionally complete enough that dependency updates are noise, not constant overlap
- Docker-backed integration tests exist for the critical operator workflows

Practical rule:

- first stabilize CI
- then enable Renovate
- only after that allow selective automerge

## Current Renovate Policy

Use a conservative rollout.

Active repository config:

- `.github/renovate.json5`

### Merge policy

Current policy:

- manual merge remains the default
- low-risk classes may automerge once per month after the stable release and a release-age delay
- risky and major updates remain manual

This does not mean deep manual auditing of each diff. It means:

- inspect the scope of the update
- confirm required checks are green
- merge intentionally

### PR volume limits

Recommended limits:

- keep PR volume bounded instead of unlimited

Why:

- avoids dependency-update noise during active product work
- keeps review effort bounded
- keeps CI load manageable without depending on dashboard schedule overrides

### Grouping policy

Recommended first-pass grouping:

- one group for npm devDependencies
- one group for npm runtime dependencies
- one group for non-high-risk Go modules
- one group for GitHub Actions updates
- keep high-risk Go runtime modules separate

Do not start with one PR per package unless update volume is very low.

Recommended repository config:

- `prConcurrentLimit = 6`
- `prHourlyLimit = 4`
- `rebaseWhen = "behind-base-branch"` when branch protection requires PRs to be up to date with `main`
- grouped PRs for:
  - frontend runtime dependencies
  - frontend dev dependencies
  - non-high-risk Go modules
  - GitHub Actions
- keep these Go runtime modules separate and manual:
  - `github.com/gorilla/websocket`
  - `github.com/creack/pty`
  - `modernc.org/sqlite`
- major updates kept separate
- selective automerge only for:
  - frontend `devDependencies` patch/minor/pin/digest updates
  - selected GitHub Actions patch/minor/pin/digest updates:
    - `actions/checkout`
    - `actions/setup-go`
    - `actions/setup-node`
- automerge runs once per month on the `2nd`, after the stable release on the `1st`
- minimum release age before automerge:
  - frontend tooling: `3 days`
  - selected GitHub Actions: `7 days`

Note on scheduling:

- Stacklab initially relied on a weekly Renovate schedule
- in practice, the Dependency Dashboard "create all awaiting schedule PRs" flow did not reliably bypass the schedule in the hosted Renovate environment
- because of that, the preferred starting policy is:
  - no global schedule
  - low concurrency limits
  - grouped PRs
  - manual merge after green CI

This keeps update volume controlled while avoiding confusing "pending but not created" behavior.

## Risk-Based Update Classes

### Lower-risk candidates

Good candidates for selective automerge:

- frontend devDependencies
- linting and formatting tools
- selected GitHub Actions that repeatedly prove safe in CI
- TypeScript toolchain updates that repeatedly prove safe in CI

### Medium-risk candidates

Keep manual:

- frontend runtime dependencies
- React Router
- CodeMirror
- charting and UI libraries
- GitHub Actions that affect release, Pages, packaging, or deployment behavior

### High-risk candidates

Keep manual unless there is very strong confidence:

- `github.com/gorilla/websocket`
- `github.com/creack/pty`
- `modernc.org/sqlite`
- any dependency that directly affects process execution, PTY lifecycle, WebSocket transport, or persistent state
- anything that changes Docker or Compose interaction behavior

## Relationship To Monthly Release Automation

Automatic monthly stable publication is only sensible if dependency policy remains risk-aware.

Recommended model:

- low-risk dependency classes may automerge once per month after the stable release
- high-risk and major updates remain manual
- nightly prereleases soak the resulting `main` during the rest of the month
- monthly stable release publishes the already-green state of `main` on the `1st` of the next month

Operationally, this should mean:

- stable `YYYY.MM.0` publishes on the `1st`
- low-risk automerge runs on the `2nd`, after that stable release
- nightly then soaks the resulting `main` until the next stable

Do **not** design the release process around merging all Renovate PRs on release day.

That would make the `1st` of the month both:

- dependency integration day
- release publication day

This is much riskier than:

- merging safe updates continuously
- merging risky updates intentionally
- using nightly prereleases as a soak period
- publishing the stable state already present on `main`

## Required CI Before Renovate

Renovate should not be enabled before these checks exist and are reliable.

### Baseline required checks

Minimum PR validation:

- backend unit and integration suite: `go test ./...`
- frontend install: `npm ci`
- frontend typecheck
- frontend lint
- frontend production build
- Linux `amd64` backend build

These checks are necessary but not sufficient.

### Docker-backed integration checks

Because Stacklab is a Docker/Compose control plane, PR validation should also include Linux runner integration tests that exercise the real runtime seams.

Recommended CI environment:

- GitHub Actions Linux `x64` runner
- Docker Engine available on the runner
- Compose v2 available on the runner through either `docker compose` or standalone `docker-compose`

Recommended integration coverage:

- authentication bootstrap and login
- stack discovery from fixture directories
- `GET /api/stacks`
- `GET /api/stacks/{stackId}`
- `GET /api/stacks/{stackId}/definition`
- `POST /api/stacks/{stackId}/resolved-config`
- `PUT /api/stacks/{stackId}/definition`
- `POST /api/stacks`
- `DELETE /api/stacks/{stackId}`
- lifecycle actions such as `up`, `down`, and `restart`
- WebSocket `jobs.subscribe`
- WebSocket `logs.subscribe`
- WebSocket `stats.subscribe`
- WebSocket `terminal.open`, `terminal.attach`, and `terminal.close`

Why this matters:

- these are the paths most likely to regress under dependency changes
- they validate real behavior that unit tests alone cannot prove

## Linux Runner vs VM

### GitHub-hosted Linux runner

This should be the default CI target for Renovate PR validation.

Advantages:

- close enough to production for most application behavior
- supports Docker and Compose-based validation
- much cheaper and simpler than maintaining a separate VM layer for every PR

This is the right place to harden the app against most dependency regressions.

### VM or staging Linux host

Still useful, but for a different purpose.

Recommended use:

- pre-release validation
- systemd service checks
- host permission model checks
- upgrade and rollback checks

Not recommended as the default gate for every Renovate PR:

- too heavy
- slower feedback
- more operational cost

## Required Branch Protection

Before enabling Renovate, branch protection should require all core checks to pass before merge.

Recommended required checks:

- backend tests
- frontend typecheck
- frontend lint
- frontend build
- Docker-backed integration suite

Optional later additions:

- release artifact build
- smoke validation of packaged Linux `amd64` artifact

## Recommended Rollout Stages

### Stage 1: Preparation

- finish current integration milestone
- make the main CI pipeline stable
- reduce flaky tests to near zero

### Stage 2: Enable Renovate conservatively

- weekly schedule
- grouped PRs
- no automerge
- low PR concurrency

### Stage 3: Observe

Watch for:

- flaky checks
- repeated false-positive update PRs
- dependency classes that routinely cause regressions
- dependency classes that prove consistently safe

### Stage 4: Selective automerge

Only after a stable observation period.

Candidates:

- low-risk devDependency groups

Still manual:

- runtime dependencies
- high-risk Go runtime modules in separate PRs
- WebSocket, PTY, SQLite, Docker-adjacent libraries

## Manual Merge Policy

At the beginning, yes: Renovate PRs should be merged manually.

That manual review should stay lightweight:

- verify the scope of the update
- confirm required checks are green
- merge

It should not require full manual exploratory testing for every patch-level bump unless:

- the changed package is high-risk
- CI failed or recently became flaky
- the PR touches a dependency class known to affect runtime behavior

## What We Should Not Do Initially

- do not enable broad automerge on day one
- do not rely only on unit tests
- do not use a VM for every dependency update PR
- do not let Renovate open an unbounded number of PRs
- do not tie dependency updates directly to automatic deployment

## Recommended Near-Term Sequence

1. finish the current UI/backend integration milestone
2. stabilize frontend lint and related CI checks
3. add a proper PR validation workflow
4. add Docker-backed integration validation on GitHub Actions Linux `x64`
5. only then enable Renovate
6. keep merge manual at first
7. revisit selective automerge later

## Summary

The recommended model for Stacklab is:

- Renovate enabled, but cautiously
- strong CI before adoption
- Linux Docker-backed integration tests as the main hardening layer
- VM or staging host checks as pre-release validation, not per-update validation
- manual merges first, selective automerge later
