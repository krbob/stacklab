# Versioning and Release Policy

## Purpose

This document defines how Stacklab should version releases and how often releases should happen.

It also defines the recommended balance between:

- manual operator control
- lightweight automation
- dependency maintenance via Renovate

## Current Recommendation

Recommendation:

- keep automated nightly publication
- keep automated monthly stable publication on the `1st`
- keep hotfix publication manual
- keep risky dependency merges manual
- allow selective low-risk automerge only in a short window after the monthly stable

## Why Not Fully Automatic Releases Immediately

Stacklab is an admin tool that:

- edits Compose definitions
- manages live containers
- opens terminal sessions
- depends on Docker and host behavior

Because of that, fully automated release publication is still too aggressive for the current phase.

Automatic release publication would increase the chance of:

- shipping a dependency update that passed basic CI but behaves badly on a real host
- publishing unstable application changes just because the calendar says so
- treating release cadence as more important than deployment confidence

## Why Purely Manual Releases Are Also Weak

Purely manual release habits tend to decay.

Real risk:

- monthly maintenance gets skipped
- dependency updates accumulate
- several small safe updates turn into one larger risky batch

So the right model is not "everything manual" and not "everything automatic".

The right operating model is:

- automated nightly publication
- controlled monthly stable publication
- human-controlled hotfixes

## Recommended Versioning Scheme

Recommendation:

- use calendar versioning

Preferred format:

- `YYYY.MM.PATCH`

Examples:

- `2026.04.0`
- `2026.04.1`
- `2026.05.0`
- `2026.05.0~nightly20260404+gabcdef1` for Debian nightly package versions

Meaning:

- `YYYY` = year
- `MM` = release month
- `PATCH` = extra release in the same month

Why this is a good fit:

- release age is obvious at a glance
- monthly maintenance releases become easy to reason about
- emergency fixes still fit naturally without inventing a second scheme

## Recommended Release Cadence

### Target steady-state cadence

When Stacklab packaging, CI, and host validation are stable:

- one planned maintenance release per month
- automated nightly prerelease builds for the default branch

This should be treated as a release window, not as a hard obligation to publish something every month no matter what.

Practical rule:

- if `main` is green and changed meaningfully, publish the monthly stable
- if there is no meaningful change since the previous stable, skip that month cleanly

### Releases outside the monthly window

These should still be allowed:

- security hotfix release
- bugfix release for a production regression
- urgent compatibility fix

Example:

- planned monthly release: `2026.04.0`
- urgent hotfix later that month: `2026.04.1`

## Relationship To Renovate

Renovate should help prepare releases, not force them.

Recommended model:

- Renovate opens dependency update PRs continuously
- CI validates them
- low-risk classes may automerge only in a short early-month window
- risky or major updates are still merged intentionally
- monthly stable release picks up already-merged, already-green, already-soaked updates

This keeps dependency maintenance continuous while keeping published releases controlled.

## Release Automation Model

Current automation model:

- nightly prerelease workflow on a schedule
- monthly stable workflow on the `1st`
- manual `workflow_dispatch` for hotfix releases

Recommended release train:

- automatic stable publication on the `1st` from the already-green state of `main`
- early-month selective low-risk automerge for trusted Renovate classes after the stable release
- nightly prereleases from `main` as the soak channel during the rest of the month

Important constraint:

- monthly stable should publish the already-stable state of `main`
- it should not depend on a batch merge event on release day

Operational fallback:

- keep `workflow_dispatch` for nightly and stable workflows
- if a scheduled run is delayed or dropped, a human can rerun it manually

## Suggested Monthly Stable Checklist

The stable workflow or its manual fallback should effectively verify:

1. review merged changes since the previous release
2. review Renovate PRs that are already green and safe to merge
3. ensure required CI checks are green on `main`
4. run Linux smoke validation
   `amd64` is primary; use `arm64` smoke too when shipping or validating that architecture
5. build release artifacts and `.deb`
6. create tag and release notes
7. publish to APT `stable`
8. run post-publish APT install smoke
9. optionally deploy to the homelab host

## Release Decision Rules

Publish the monthly release only if all of these are true:

- required CI checks are green
- no known critical regression is open
- backend and frontend integration is stable for the intended scope
- Linux smoke validation passes for the intended release architecture, with `amd64` as the primary baseline

Do not publish just because the calendar fired.

## Recommended Automation Boundaries

Automate later:

- nightly publication
- release artifact build
- GitHub release creation
- APT publication

Keep manual longer:

- hotfix decision making
- deployment to the homelab host
- rollback decisions
- risky dependency merges

## Future Evolution

The release process can become more automated later if all of these become true:

- CI is strong and trustworthy
- Docker-backed integration tests are stable
- Linux release smoke is routine on the intended architecture set
- production deployments are low-risk

At that point, we may consider:

- automatic nightly prereleases
- automatic monthly stable publication
- selective automerge for low-risk dependencies
- stronger release trains

## Recommendation For The Current Phase

Do now:

- keep versioning policy documented
- keep nightly, stable, and hotfix workflows operational
- keep selective automerge narrowly scoped
- keep nightly soak as the validation loop for the next stable

Do later:

- add stronger host-native post-publish smoke
- add nightly retention and cleanup
- widen low-risk automerge only if soak keeps proving safe
