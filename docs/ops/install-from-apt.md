# Install From APT

This document describes the supported Debian-family install path using the
published Stacklab APT repository.

This is the primary recommended production path for Debian-family hosts.

Supported scope:

- Debian-family hosts running `systemd`
- fresh package-managed installs
- package-managed upgrades
- `amd64` primary, `arm64` also supported

Unsupported transitions:

- migration from an existing tarball-based `/opt/stacklab` install
- switching an existing package-managed install to the tarball layout

## Repository Key

Install the published Stacklab APT signing key:

```bash
sudo mkdir -p /usr/share/keyrings
curl -fsSL https://krbob.github.io/stacklab/apt/stacklab-archive-keyring.gpg \
  | sudo tee /usr/share/keyrings/stacklab-archive-keyring.gpg >/dev/null
```

## Stable Channel

Add the stable channel:

```bash
arch="$(dpkg --print-architecture)"
echo "deb [arch=${arch} signed-by=/usr/share/keyrings/stacklab-archive-keyring.gpg] https://krbob.github.io/stacklab/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/stacklab.list
```

Then install:

```bash
sudo apt-get update
sudo apt-get install stacklab
```

## Nightly Channel

If you want the nightly prerelease channel instead:

```bash
arch="$(dpkg --print-architecture)"
echo "deb [arch=${arch} signed-by=/usr/share/keyrings/stacklab-archive-keyring.gpg] https://krbob.github.io/stacklab/apt nightly main" \
  | sudo tee /etc/apt/sources.list.d/stacklab.list
```

Then install:

```bash
sudo apt-get update
sudo apt-get install stacklab
```

## Upgrades

For normal repository-backed upgrades:

```bash
sudo apt-get update
sudo apt-get install stacklab
```

For a local package artifact:

```bash
sudo apt-get install ./stacklab_<version>_<arch>.deb
```

For other Linux distributions or manual host-native installs, use the tarball
flow instead:

- [install-from-tarball.md](install-from-tarball.md)

## Notes

- The package layout is Debian-native:
  - `/usr/lib/stacklab`
  - `/etc/stacklab/stacklab.env`
  - `/srv/stacklab`
  - `/var/lib/stacklab`
- The package also installs the Docker admin helper binary:
  - `/usr/lib/stacklab/bin/stacklab-docker-admin-helper`
- The package also installs the workspace repair helper binary:
  - `/usr/lib/stacklab/bin/stacklab-workspace-admin-helper`
- The package also installs the Stacklab self-update helper binary:
  - `/usr/lib/stacklab/bin/stacklab-self-update-helper`
- A sample `sudoers` allowlist is installed at:
  - `/usr/share/doc/stacklab/examples/stacklab-docker-admin.sudoers.example`
  - `/usr/share/doc/stacklab/examples/stacklab-workspace-admin.sudoers.example`
  - `/usr/share/doc/stacklab/examples/stacklab-self-update.sudoers.example`
- The package depends on:
  - `systemd`
  - Docker Engine
  - Compose
  - `git`
- After install, adjust `/etc/stacklab/stacklab.env` as needed and start or
  restart the service with `systemctl`.
- Docker daemon apply remains opt-in:
  - set `STACKLAB_DOCKER_ADMIN_HELPER_PATH`
  - set `STACKLAB_DOCKER_ADMIN_USE_SUDO=true`
  - set `STACKLAB_DOCKER_ADMIN_BACKUP_DIR`
  - install a narrow `sudoers` rule for the helper
  - keep `NoNewPrivileges=false` in `stacklab.service`
  - include `/etc/docker` in the unit `ReadWritePaths`
- Workspace permission repair also remains opt-in:
  - set `STACKLAB_WORKSPACE_ADMIN_HELPER_PATH`
  - set `STACKLAB_WORKSPACE_ADMIN_USE_SUDO=true`
  - install a narrow `sudoers` rule for the helper
  - keep `NoNewPrivileges=false` in `stacklab.service`
- Stacklab self-update also remains opt-in:
  - set `STACKLAB_SELF_UPDATE_HELPER_PATH`
  - set `STACKLAB_SELF_UPDATE_USE_SUDO=true`
  - optionally override `STACKLAB_SELF_UPDATE_PACKAGE_NAME`
  - optionally override `STACKLAB_SELF_UPDATE_HEALTH_URL`
  - install a narrow `sudoers` rule for the helper
  - keep `NoNewPrivileges=false` in `stacklab.service`
  - keep `ProtectSystem=full`; the self-update helper is launched through a transient `systemd-run` unit so `dpkg` can update `/etc` and `/usr` without relaxing the main Stacklab service sandbox

## Repository Retention

The APT repository is an install and update channel, not the long-term release
archive.

Current publication policy:

- `stable` keeps the newest 6 package versions in the APT pool
- `nightly` keeps the newest 7 package versions in the APT pool
- older GitHub Releases remain the release archive for manual rollback and
  investigation
- nightly GitHub prereleases are pruned separately by the nightly workflow,
  keeping the newest 14 nightly prereleases

The retention count can be overridden when republishing a channel with
`scripts/release/publish-apt-repo.sh --keep-versions N`; use `0` to keep all
package versions in that channel.
