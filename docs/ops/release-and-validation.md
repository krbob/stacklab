# Release and Validation

## Purpose

This document records the supported Stacklab release artifacts, install modes,
upgrade boundaries, automated release gates, and the manual validation expected
for changes whose risk cannot be reproduced completely in GitHub-hosted runners.

## Supported Install Modes

### Debian-family package-managed installs

APT or a local `.deb` is the primary operator path on Debian-family,
`systemd`-based hosts.

Layout:

- `/usr/lib/stacklab` — immutable application payload;
- `/etc/stacklab/stacklab.env` — operator environment configuration;
- `/srv/stacklab` — managed stacks and configuration;
- `/var/lib/stacklab` — application state.

Fresh installs, package-managed upgrades, package downgrades, and APT-backed
Stacklab self-update are supported within this install mode.

### Manual tarball installs

Tarballs are the secondary path for other Linux distributions or operators who
explicitly choose release directories and the host-side upgrade script.

Layout:

- `/opt/stacklab/app`;
- `/opt/stacklab/stacks`;
- `/opt/stacklab/config`;
- `/opt/stacklab/data`;
- `/var/lib/stacklab`.

Fresh installs, verified upgrades through `host-tools/upgrade.sh`, and rollback
to a previous release directory are supported. Upgrades must not modify managed
workspace content under `/opt/stacklab/{stacks,config,data}`.

### Unsupported transitions

Migration between tarball and package-managed layouts is not automated:

- tarball to `.deb` or APT;
- `.deb` or APT to tarball;
- in-place conversion between `/opt/stacklab` and `/srv/stacklab`.

Changing install mode is a deliberate manual migration with an independent
backup and rollback plan.

## Release Artifacts

Every stable, nightly, and hotfix build produces:

- `stacklab-<version>-linux-amd64.tar.gz`;
- `stacklab-<version>-linux-arm64.tar.gz`;
- `stacklab_<version>_amd64.deb`;
- `stacklab_<version>_arm64.deb`;
- a SHA-256 checksum beside each artifact.

Tarballs include `LICENSE`, `NOTICE`, and generated
`THIRD_PARTY_NOTICES.md`. Debian packages expose project and distributed
third-party license text through `/usr/share/doc/stacklab/copyright`, with
`NOTICE` alongside it. Packaging smoke verifies representative required files
and attributions.

## Automated Release Gates

The nightly, stable, and hotfix workflows use the same exact-revision gate from
`.github/workflows/release-quality-gate.yml`.

### Before artifacts can be published

Automation requires:

1. frontend API generation/drift check, unit tests, typecheck, and production
   build;
2. backend tests, coverage thresholds, formatting, vet, and repository hygiene;
3. Docker-backed integration smoke;
4. browser E2E smoke;
5. Debian package installation under the systemd smoke harness;
6. successful tarball and `.deb` builds for `amd64` and `arm64`;
7. install/upgrade smoke of the produced `amd64` `.deb` and install/upgrade,
   checksum-rejection, and rollback smoke of the produced `amd64` tarball.

A failed required job blocks the publish job. The workflows do not accept a
manual approval as a substitute for a failed check.

### After publication

Automation waits for the target APT channel to expose the exact package version
and installs it from the public repository on `amd64`. This detects signing,
metadata, Pages propagation, repository layout, and package-download failures
that pre-publication artifact smoke cannot cover.

This is a post-publication verification, not a pre-publication gate: the GitHub
release and APT commit already exist when it runs. A failure makes the release
workflow fail and must be treated as a release incident. Diagnose the channel,
repair or republish it when safe, and use a new hotfix version for an application
defect; never silently replace an existing release tag.

## Recommended Manual Validation

Manual validation is risk-based and complements automation. It is not an
undocumented gate that every scheduled stable run waits for.

Perform it before merging the risky change, or against a nightly while there is
still soak time before the next stable. Use a disposable Linux host or VM that
matches the affected production profile.

### Changes that require a real-host pass

- package maintainer scripts, ownership, permissions, systemd units, or install
  layout changes;
- SQLite migrations, retention changes, or recovery behavior involving existing
  operator data;
- privileged helper protocols, workspace permission repair, or Docker daemon
  apply/restart behavior;
- APT self-update, repository signing/publication, downgrade, or rollback;
- tarball upgrade scripts, symlink switching, checksum handling, or rollback;
- Docker Engine or Compose compatibility changes that depend on the target host;
- shutdown, cancellation, interruption, disk-full, or Docker-restart recovery;
- `arm64`-specific packaging/runtime changes, because release install smoke is
  currently executed on `amd64`.

### Minimum evidence

Record:

- source revision and tested artifact/package version;
- OS, architecture, Docker Engine, Compose, and systemd versions;
- fresh install or upgrade origin and resulting layout/ownership;
- the risky operation exercised and its observable result;
- service logs plus rollback or recovery outcome when relevant.

Use the maintained procedures rather than duplicating commands here:

- [install-from-apt.md](install-from-apt.md);
- [install-from-tarball.md](install-from-tarball.md);
- [upgrade-validation-checklist.md](upgrade-validation-checklist.md).

## Validation Ownership By Release Type

### Nightly

Nightly runs daily for a changed `main` revision. Use it for unattended
regression detection, package installation checks, and soak. A failed nightly
does not automatically alter the stable channel, but it must be understood
before the next monthly release.

### Monthly stable

Stable runs automatically on the first day of the month when `main` changed
since the previous stable. Its automated gate is complete and self-contained.
Any manual validation required by risky changes should already be attached to
those changes or completed against a nightly; release day is not the first time
to exercise them.

### Hotfix

Hotfix publication is a manual decision, but its technical checks are not
optional. The same automated quality gate and install-mode smokes run before
publication. Scope manual validation to the regression and affected install
mode while confirming that the correction does not break upgrade or rollback.

## Local Development Boundary

Day-to-day work may happen on macOS or another non-Linux system. That is valid
for most frontend, API, and domain work. It does not substitute for Linux
validation of systemd, helper-backed privileged flows, package behavior,
tarball switching, real Docker integration, or filesystem semantics.
