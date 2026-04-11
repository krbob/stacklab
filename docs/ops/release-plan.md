# Release and Validation Plan

## Purpose

This document records the supported Stacklab release artifacts, install modes,
upgrade boundaries, and validation policy.

## Current Decision

Stacklab now ships in two supported host-native forms:

- Primary: Debian-family hosts via `.deb` packages and the published APT repository
- Secondary: generic Linux hosts via manual release tarballs

Important boundaries:

- `.deb` and APT are the primary operator path and the main stable release gate
- tarballs remain supported for other Linux distributions and explicit manual installs
- migration between tarball and package-managed installs is not supported
- Stacklab self-update is supported only for APT-managed installs

## Supported Install Modes

### Debian-family package-managed installs

Use this mode when the host is Debian-family and `systemd`-based.

Layout:

- `/usr/lib/stacklab`
- `/etc/stacklab/stacklab.env`
- `/srv/stacklab`
- `/var/lib/stacklab`

This is the preferred production shape for Debian and Ubuntu hosts.

### Manual tarball installs

Use this mode on other Linux distributions or when the operator explicitly wants
release directories and a host-side upgrade script.

Layout:

- `/opt/stacklab/app`
- `/opt/stacklab/stacks`
- `/opt/stacklab/config`
- `/opt/stacklab/data`
- `/var/lib/stacklab`

This remains supported, but it is not the primary release path on Debian-family
hosts.

## Unsupported Transitions

These transitions are intentionally out of scope:

- tarball to `.deb`
- `.deb` to tarball
- in-place migration between `/opt/stacklab` and `/srv/stacklab`

If an operator wants to change install mode, treat it as a manual migration.

## Release Artifacts

Each release should publish:

- `stacklab-<version>-linux-amd64.tar.gz`
- `stacklab-<version>-linux-arm64.tar.gz`
- `stacklab_<version>_amd64.deb`
- `stacklab_<version>_arm64.deb`
- checksums

Rationale:

- `.deb` serves the primary Debian-family path
- tarball keeps Stacklab usable on non-Debian Linux hosts

## Upgrade and Rollback Policy

### `.deb` and APT

Supported:

- fresh install from the published APT repository
- `.deb` install from a local artifact
- package-managed upgrade to a newer Stacklab version

Rollback:

- via normal package downgrade mechanics
- never by switching to the tarball layout

### Tarball

Supported:

- fresh install from a release tarball
- upgrade via the packaged `host-tools/upgrade.sh`
- rollback by switching the current release symlink back to a previous release

Tarball upgrades must not modify operator-managed workspace content under:

- `/opt/stacklab/stacks`
- `/opt/stacklab/config`
- `/opt/stacklab/data`

## Validation Policy

Stable releases should validate both install modes, but not with equal weight.

### Primary stable release gate

These checks are expected to stay green before publishing stable:

- normal CI on `main`
- Docker-backed integration checks
- browser E2E
- `.deb` package smoke on Debian
- published APT smoke for the release channel
- manual Debian-family install or upgrade validation on a real Linux host

### Secondary release validation

Tarball remains supported, so it should also be exercised for releases:

- release workflows must keep building tarball artifacts for `amd64` and `arm64`
- at least one manual tarball install or upgrade smoke should run before stable release sign-off

Current gap:

- automated tarball install smoke does not yet match the current `.deb` smoke path

That gap should be closed, but it does not change the primary release-gate role
of `.deb` and APT.

## Current Automated Coverage

Implemented today:

- `release-build.yml` builds tarball and `.deb` artifacts for `amd64` and `arm64`
- `deb-package-smoke.yml` validates fresh package install behavior on Debian
- release workflows run package smoke before publishing
- release workflows run post-publish APT smoke for their channel

Still worth adding:

- automated tarball install or upgrade smoke on Linux

## Manual Stable Sign-Off

Before cutting stable, run at least:

1. one Debian-family `.deb` or APT install or upgrade validation
2. one tarball install or upgrade validation

Use:

- [install-from-apt.md](install-from-apt.md)
- [install-from-tarball.md](install-from-tarball.md)
- [upgrade-validation-checklist.md](upgrade-validation-checklist.md)

## Local Development and Pre-Release Reality

Day-to-day development can still happen on macOS or another non-Linux machine.
That remains valid for most product work.

Before stable publication, Linux validation still matters for:

- `systemd`
- helper-backed privileged flows
- package install and upgrade behavior
- tarball install and rollback behavior
- real Docker runtime integration on the target host
