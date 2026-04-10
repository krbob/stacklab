# Release and Validation Plan

## Purpose

This document records the release, upgrade, rollback, and local validation model for Stacklab.

The initial release-artifact build is now implemented, and the first host-side tarball upgrade path is now defined.

## Current Decision

Recommendation:

- build release artifacts in CI now
- use a small host-side upgrade script for tarball installs and upgrades
- keep publication and deployment manual for now

Reasoning:

- the application is already stable enough to justify repeatable artifacts
- the remaining operational risk is in publication, upgrade, and rollback automation
- a build artifact is useful now without committing us to auto-deploy

Current validation status:

- host-native staging deployments have been exercised successfully on Ubuntu `arm64`, Ubuntu `amd64`, and Debian `amd64`
- a repeatable tarball install/upgrade path now exists
- the tarball upgrade flow has been exercised on Debian `amd64` across multiple release directories
- the remaining gap is `.deb` packaging, release publication automation, and later APT publication, not basic host-native viability

## What We Should Do Now

At the current stage:

- keep the release artifact build in CI
- use the host-side tarball install/upgrade path
- keep deployment assumptions documented
- avoid spending time yet on full release publication or APT automation before `.deb` exists

This is the right time to standardize the artifact and upgrade shape, but not yet the right time to automate host rollout.

## Trigger To Start Implementing Release Automation

Start the next phase of release work when at least one of these is true:

- the first deployment to a real Linux host is planned
- the MVP user flows are stable enough that weekly upgrade churn is no longer extreme
- UI and backend integration for editor, jobs, logs, stats, terminal, create/delete, and audit is functionally complete

Practical recommendation:

- keep the current artifact workflow simple
- implement publication and upgrade automation shortly before the first recurring homelab deployment

## Target Production Model

Stacklab should be released as a host-native Linux application, with `amd64` as the primary release architecture and `arm64` also supported.

Target release contents:

- backend binary `stacklab`
- frontend static bundle
- version metadata
- example `systemd` unit and environment file template
- host-side `upgrade.sh`

Packaging note:

- the current baseline release shape is still a host-native artifact plus `systemd`
- a future Debian-first `.deb` package may become the preferred installation method on Debian hosts
- if that happens, tarball artifacts should remain available at least as a fallback and for non-Debian validation

Target runtime layout:

```text
/opt/stacklab/
  app/
    releases/
      <version-or-timestamp>/
        bin/stacklab
        frontend/dist/
        metadata/
          version.txt
          commit.txt
    current -> releases/<version-or-timestamp>
  stacks/
  config/
  data/
/var/lib/stacklab/
```

Why this layout:

- application releases are isolated from user-managed stacks
- upgrades can be implemented as atomic symlink switches
- rollback becomes straightforward

## Target Artifact Format

Preferred artifact set:

- one `tar.gz` per supported architecture per release

Suggested contents:

```text
stacklab-<version>-linux-<arch>/
  bin/stacklab
  frontend/dist/...
  metadata/version.txt
  metadata/commit.txt
  systemd/stacklab.service.example
  host-tools/upgrade.sh
```

Why this format:

- easy to publish via GitHub Releases
- easy to unpack on a simple Linux host
- does not depend on Docker on the deployment side

## Implemented Tarball Upgrade Flow

The current supported upgrade path is:

1. obtain the release tarball
2. extract it on the host
3. run `host-tools/upgrade.sh`
4. let the script install the release, switch `current`, restart the service, and verify health

See:

- [install-from-tarball.md](install-from-tarball.md)
- [upgrade-validation-checklist.md](upgrade-validation-checklist.md)

## Planned Upgrade Flow

Recommended future host-side process:

1. download a specific release artifact
2. unpack into a new directory under `/opt/stacklab/app/releases/`
3. validate expected files are present
4. switch `/opt/stacklab/app/current` to the new release
5. restart `stacklab.service`
6. verify `/api/health`
7. if verification fails, switch the symlink back and restart again

Important rule:

- upgrades must not touch `/opt/stacklab/stacks`, `/opt/stacklab/config`, or `/opt/stacklab/data`

## Planned Rollback Flow

Rollback should be intentionally simple:

1. identify previous release directory
2. repoint `/opt/stacklab/app/current`
3. restart `stacklab.service`
4. verify health

This is one of the main reasons to prefer release directories plus a stable symlink.

## Planned GitHub Actions Scope

The first implemented release workflow should stay small.

Current responsibilities:

- build Linux `amd64` and `arm64` release artifacts on demand
- build matching `.deb` artifacts on demand
- upload tarballs, `.deb` packages, and checksums to the workflow run

Planned later responsibilities:

- build `.deb` packages
- publish artifacts to GitHub Releases
- later publish `nightly` and `stable` APT metadata

Recommended non-goals for the first iteration:

- do not SSH into the homelab host from GitHub Actions
- do not auto-deploy from CI
- do not create GitHub Releases automatically
- do not auto-deploy Stacklab from CI directly onto the homelab host

Reasoning:

- manual host-triggered or operator-triggered upgrades are safer for an admin tool
- rollback remains easier to reason about
- the release pipeline stays transparent

## Host Upgrade Script

The current host-side script is:

- `scripts/release/upgrade.sh` in the repository
- packaged as `host-tools/upgrade.sh` in release artifacts

Responsibilities:

- accept a tarball, URL, or extracted artifact directory
- install the release into `releases/`
- switch the symlink
- restart the service
- run a health check
- rollback automatically if health check fails

## Release Automation Direction

The target release automation model is:

- nightly prerelease builds from `main`
- monthly stable releases on the `1st`
- manual hotfix releases

See:

- [release-automation-plan.md](release-automation-plan.md)

Important principle:

- scheduled stable publication is viable only if `main` is continuously releasable
- release day should publish the current green state, not trigger a large integration event

## Local Validation Strategy

### Day-to-day development on macOS

Most of Stacklab can be tested on a MacBook, including Apple Silicon, as a development environment.

Recommended local setup:

- Go and Node installed locally
- Docker Desktop or Colima providing Docker Engine and Compose
- local test root under `.local/stacklab`

What is realistic to test locally on macOS:

- backend HTTP API
- frontend application
- auth and session handling
- SQLite state
- stack discovery
- editor and resolved preview
- jobs and progress
- logs, stats, and terminal
- create/delete flows
- most Compose-driven behavior

What is **not** fully validated on macOS:

- `systemd`
- Linux service account and permission model
- exact Linux runtime behavior on the target architecture
- final host deployment procedure

Conclusion:

- macOS is good for daily development and most integration testing
- it is not the final production-equivalent verification environment

### Pre-release validation on Linux

Before the first real deployment, we should verify on a Linux environment matching the intended host architecture.

Preferred options:

- the real homelab host
- a small staging VM or VPS running Linux `amd64` or `arm64`

Acceptable fallback:

- local Linux VM matching the target architecture

This validation should cover:

- service start under `systemd`
- reverse-proxy reachability
- Docker socket access
- real Compose lifecycle actions
- terminal PTY behavior
- upgrade and rollback flow

## Recommendation For The Current Phase

Do this now:

- keep this plan in the repo
- keep using the manual `workflow_dispatch` release build
- validate real upgrades with the tarball flow

Do not do this yet:

- production upgrade script
- automatic deployment to the homelab server
- Debian APT repository publication

## Recommended Future Implementation Order

When release work starts, implement in this order:

1. finalize release directory layout
2. add version metadata to the backend build
3. define the release artifact format
4. add GitHub Actions build-and-release workflow
5. add host-side `upgrade.sh`
6. test upgrade and rollback on Linux `amd64`
7. only then use it on the real homelab host

Related future work:

- see [`debian-package-plan.md`](debian-package-plan.md) for the Debian package and APT repository option
