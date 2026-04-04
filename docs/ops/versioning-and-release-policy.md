# Versioning and Release Policy

## Purpose

This document defines how Stacklab should version releases and how often releases should happen.

It also defines the recommended balance between:

- manual operator control
- lightweight automation
- dependency maintenance via Renovate

## Current Recommendation

Recommendation:

- keep release publication manual
- make release reminders automatic
- adopt a monthly release window when the project is stable enough

This is the intended long-term model. It does **not** mean we should implement monthly release automation immediately.

## Why Not Fully Automatic Releases

Stacklab is an admin tool that:

- edits Compose definitions
- manages live containers
- opens terminal sessions
- depends on Docker and host behavior

Because of that, fully automated release publication is too aggressive for the current phase.

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

The right model is:

- automated reminder
- human approval
- controlled publication

## Recommended Versioning Scheme

Recommendation:

- use calendar versioning

Preferred format:

- `YYYY.MM.PATCH`

Examples:

- `2026.04.0`
- `2026.04.1`
- `2026.05.0`

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

When Stacklab is production-deployed and CI is stable:

- one planned maintenance release per month

This should be treated as a release window, not as a hard obligation to publish something every month no matter what.

Practical rule:

- if there are worthwhile changes and the release candidate is green, publish
- if the branch is unstable or there is nothing meaningful to ship, skip that month

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

- Renovate opens dependency update PRs
- CI validates them
- humans merge them intentionally
- monthly release picks up already-merged, already-green updates

This keeps dependency maintenance continuous while keeping published releases controlled.

## Semi-Automatic Reminder Model

Recommendation:

- use GitHub Actions on a monthly schedule
- do **not** publish a release automatically

Instead, the scheduled workflow should do one small thing:

- open or refresh a "monthly release reminder" issue

Suggested behavior:

1. runs on the first weekday of each month
2. checks whether a release for the current month already exists
3. if not, opens an issue like:
   - `Release reminder: 2026.04`
4. includes a checklist
5. assigns or mentions the maintainer

This solves the "I will forget" problem without removing human judgment.

## Suggested Monthly Release Checklist

The reminder issue should include:

1. review merged changes since the previous release
2. review Renovate PRs that are already green and safe to merge
3. ensure required CI checks are green on `main`
4. run Linux smoke validation
   `amd64` is primary; use `arm64` smoke too when shipping or validating that architecture
5. build the release artifact
6. publish release notes and tag
7. optionally deploy to the homelab host

## Release Decision Rules

Publish the monthly release only if all of these are true:

- required CI checks are green
- no known critical regression is open
- backend and frontend integration is stable for the intended scope
- Linux smoke validation passes for the intended release architecture, with `amd64` as the primary baseline

Do not publish just because the reminder fired.

## Recommended Automation Boundaries

Automate:

- reminder issue creation
- CI validation
- release artifact build
- draft release creation later, if desired

Do not automate yet:

- final release publication
- host deployment
- rollback decisions

## Future Evolution

The release process can become more automated later if all of these become true:

- CI is strong and trustworthy
- Docker-backed integration tests are stable
- Linux release smoke is routine on the intended architecture set
- production deployments are low-risk

At that point, we may consider:

- automatic draft release creation every month
- optional automerge for low-risk dependencies
- stronger release trains

But even then, final publication should probably remain human-approved for Stacklab.

## Recommendation For The Current Phase

Do now:

- keep release publication manual
- keep versioning policy documented
- keep the release artifact workflow manual-on-demand
- plan for monthly reminder automation later

Do later:

- implement scheduled GitHub Actions reminder
- adopt monthly `CalVer` release train once the app is closer to its first real deployment
