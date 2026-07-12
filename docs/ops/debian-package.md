# Debian Package and APT Model

## Status And Support Boundary

The `.deb` and signed APT repository are implemented and are the primary
production install path for Debian-family hosts running `systemd`. Packages are
built for `amd64` and `arm64`.

The package supports one host-native Stacklab service with Docker Engine,
Compose v2, and Git available on the host. Debian systems without `systemd` are
outside this package profile. Manual tarballs remain the secondary install
mode; switching between `/srv/stacklab` and `/opt/stacklab` layouts is a manual
migration, not a package upgrade.

See [Release and Validation](release-and-validation.md) for release gates and
install-mode transitions.

## Build Contract

`scripts/release/build-deb.sh` consumes the staged release directory produced
by `scripts/release/build-artifact.sh`. It accepts only `amd64` or `arm64`,
requires the binary/frontend artifact plus `LICENSE`, `NOTICE`, and generated
third-party notices, and produces:

```text
dist/release/stacklab_<version>_<arch>.deb
dist/release/stacklab_<version>_<arch>.deb.sha256
```

The generated control metadata declares:

- `adduser` and `systemd`;
- Docker Engine from `docker.io`, Docker CE, or Moby packages;
- a compatible Docker CLI package;
- Compose through `docker-compose` or `docker-compose-plugin`;
- `git`;
- `ca-certificates` as a recommendation.

The package is built with `dpkg-deb --root-owner-group`. Its architecture and
Debian version come from the release build inputs rather than being patched
after the package is assembled.

## Installed Files

| Path | Ownership and purpose |
| --- | --- |
| `/usr/lib/stacklab/bin/stacklab` | Package-owned backend binary |
| `/usr/lib/stacklab/bin/stacklab-*-helper` | Package-owned Docker admin, workspace repair, and self-update helpers |
| `/usr/lib/stacklab/frontend/dist` | Package-owned frontend served by the backend |
| `/usr/lib/stacklab/metadata` | Package-owned release metadata |
| `/lib/systemd/system/stacklab.service` | Package-owned service unit |
| `/etc/stacklab/stacklab.env` | Root-owned mode `0600` conffile preserved across upgrades |
| `/usr/share/doc/stacklab` | Project notice, generated third-party licenses, and opt-in sudoers examples |
| `/srv/stacklab` | Operator-owned managed workspace, never application payload |
| `/var/lib/stacklab` | Private runtime state, service home, Docker config, and SQLite |

The package does not own stack definitions, config payloads, or data under
`/srv/stacklab`. Application upgrades replace `/usr/lib/stacklab` while leaving
the filesystem-first workspace and `/var/lib/stacklab` in place. The complete
boundary is documented in
[Filesystem Layout](../architecture/filesystem-layout.md).

## Service Unit And Environment

The maintained unit is `packaging/debian/stacklab.service`. It runs as the
dedicated `stacklab:stacklab` identity with:

- working directory and managed root `/srv/stacklab`;
- executable `/usr/lib/stacklab/bin/stacklab`;
- optional environment file `/etc/stacklab/stacklab.env`;
- private state created through `StateDirectory=stacklab`;
- Docker socket access through the `docker` supplementary group;
- `Restart=on-failure`, a 30-second stop timeout, `PrivateTmp=true`,
  `ProtectSystem=full`, and `ProtectHome=true`;
- explicit write access to `/srv/stacklab`, `/var/lib/stacklab`, and
  `/etc/docker` for the opt-in Docker admin helper.

The packaged environment fixes the production paths, serves the frontend from
`/usr/lib/stacklab/frontend/dist`, uses writable `HOME` and `DOCKER_CONFIG`
directories below `/var/lib/stacklab`, binds to loopback by default, and enables
secure cookies for the expected HTTPS reverse-proxy deployment.

Privileged helpers are installed but disabled by default. Enabling one requires
an explicit environment setting and a narrow sudoers rule based on the example
under `/usr/share/doc/stacklab/examples`; the package never installs a broad
privilege grant automatically.

See [systemd Deployment](systemd.md) for the runtime and reverse-proxy model.

## Maintainer Script Lifecycle

The implemented `postinst` is idempotent. On configuration it:

1. creates the system group and non-login service account when absent;
2. creates `/srv/stacklab/{stacks,config,data}` only when missing;
3. creates private runtime, home, and Docker config directories;
4. enforces mode `0600` on the environment file, SQLite/WAL/SHM files, and
   stack-root `.env` files it encounters;
5. adds the service account to existing `docker` and `systemd-journal` groups;
6. reloads and enables the service, starts it on first install, or performs a
   best-effort restart after upgrade;
7. prints a bootstrap-password hint when authentication is not yet configured.

The implemented `prerm` stops `stacklab.service` for package removal or
deconfiguration. There is no destructive `postrm`: remove/purge does not delete
the service account, `/srv/stacklab`, or `/var/lib/stacklab`. Standard `dpkg`
conffile rules govern `/etc/stacklab/stacklab.env`.

Package scripts do not configure a reverse proxy, reset authentication, or
rewrite operator Compose definitions.

## Automated Validation

`.github/workflows/deb-package-smoke.yml` runs for relevant package/release
changes, on `main`, by manual dispatch, and as a reusable release gate. It:

1. verifies the exact source revision;
2. builds the production release artifact and `amd64` package;
3. installs it in a disposable Debian environment with real `systemd`;
4. performs an A-to-B package upgrade;
5. verifies service identity, health, frontend serving, legal files,
   environment preservation, SQLite preservation and modes, workspace/runtime
   fixtures, session continuity, and service restart.

Nightly, stable, and hotfix workflows additionally build tarballs and packages
for both architectures, smoke the produced `amd64` package and tarball, and
block publication when the shared quality gate or install-mode smoke fails.
After publication they wait for the target APT channel and install the exact
published `amd64` package from that repository.

## APT Repository

`scripts/release/publish-apt-repo.sh` maintains a static repository under the
GitHub Pages publication branch. It:

- accepts only `stable` or `nightly`;
- publishes `amd64` and `arm64` package indexes under `main`;
- generates `Packages`, compressed indexes, and `Release` metadata with Debian
  tooling;
- signs `Release` as both `InRelease` and `Release.gpg` with the configured GPG
  key;
- publishes binary and armored archive keyrings;
- retains the newest 6 stable versions and 7 nightly versions by default using
  Debian version ordering.

Stable, nightly, and hotfix release workflows publish their channel directly.
`.github/workflows/apt-publish.yml` is the manual repair/republish path for
packages already attached to a GitHub release. Publication jobs are serialized
through the shared `apt-pages-publication` concurrency group.

Operator installation and keyring commands live in
[Install from APT](install-from-apt.md), not in this implementation reference.

## Known Validation Boundaries

- Automated systemd install/upgrade smoke currently runs on `amd64`; `arm64`
  artifacts are built, but architecture-specific runtime changes still require
  a real-host pass.
- Automated package smoke covers fresh install and A-to-B upgrade. It does not
  yet exercise remove and purge end to end, so changes to maintainer-script or
  retention behavior require the manual
  [Upgrade Validation Checklist](upgrade-validation-checklist.md).
- Downgrading across a forward-only SQLite migration requires the matching
  verified database backup; installing an older `.deb` alone is insufficient.
