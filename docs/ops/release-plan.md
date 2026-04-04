# Release and Validation Plan

## Purpose

This document records the release, upgrade, rollback, and local validation model for Stacklab.

The initial release-artifact build is now implemented, but publication and deployment automation remain intentionally limited.

## Current Decision

Recommendation:

- build release artifacts in CI now
- keep publication and deployment manual for now

Reasoning:

- the application is already stable enough to justify repeatable artifacts
- the remaining operational risk is in publication, upgrade, and rollback automation
- a build artifact is useful now without committing us to auto-deploy

Current validation status:

- host-native staging deployments have been exercised successfully on Ubuntu `arm64`, Ubuntu `amd64`, and Debian `amd64`
- the remaining gap is packaging and repeatable upgrade automation, not basic host-native viability

## What We Should Do Now

At the current stage:

- keep the release artifact build in CI
- keep deployment assumptions documented
- avoid spending time yet on GitHub Release publication or upgrade scripts

This is the right time to standardize the artifact shape, but not yet the right time to automate host rollout.

## Trigger To Start Implementing Release Automation

Start the next phase of release work when at least one of these is true:

- the first deployment to a real Linux `amd64` host is planned
- the MVP user flows are stable enough that weekly upgrade churn is no longer extreme
- UI and backend integration for editor, jobs, logs, stats, terminal, create/delete, and audit is functionally complete

Practical recommendation:

- keep the current artifact workflow simple
- implement publication and upgrade automation shortly before the first recurring homelab deployment

## Target Production Model

Stacklab should be released as a host-native Linux `amd64` application, not as a Docker management container.

Target release contents:

- backend binary `stacklab`
- frontend static bundle
- version metadata
- optional example `systemd` unit and environment file template

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

Preferred artifact:

- one `tar.gz` per release for `linux-amd64`

Suggested contents:

```text
stacklab-<version>-linux-amd64/
  bin/stacklab
  frontend/dist/...
  metadata/version.txt
  metadata/commit.txt
  systemd/stacklab.service.example
```

Why this format:

- easy to publish via GitHub Releases
- easy to unpack on a simple Linux host
- does not depend on Docker on the deployment side

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

- build a Linux `amd64` release artifact on demand
- upload the artifact and checksum to the workflow run

Planned later responsibilities:

- publish that artifact to GitHub Releases

Recommended non-goals for the first iteration:

- do not SSH into the homelab host from GitHub Actions
- do not auto-deploy from CI
- do not create GitHub Releases automatically
- do not build a self-updater into Stacklab itself

Reasoning:

- manual host-triggered upgrades are safer for an admin tool
- rollback remains easier to reason about
- the release pipeline stays transparent

## Planned Host Upgrade Script

When we implement deployment tooling, prefer one small host-side script such as `upgrade.sh`.

Responsibilities:

- accept a version or release URL
- fetch the artifact
- unpack it into `releases/`
- switch the symlink
- restart the service
- run a health check
- rollback automatically if health check fails

This script should live outside the app runtime itself or at least be runnable independently of the service.

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
- exact Linux `amd64` runtime behavior
- final host deployment procedure

Conclusion:

- macOS is good for daily development and most integration testing
- it is not the final production-equivalent verification environment

### Pre-release validation on Linux `amd64`

Before the first real deployment, we should verify on a Linux `amd64` environment.

Preferred options:

- the real homelab host
- a small staging VM or VPS running Linux `amd64`

Acceptable fallback:

- local Linux `amd64` VM on the MacBook

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
- continue feature integration

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
