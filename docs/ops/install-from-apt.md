# Install From APT

This document describes the supported Debian-family install path using the
published Stacklab APT repository.

Current scope:

- Debian-family hosts running `systemd`
- fresh package-managed installs
- `amd64` primary, `arm64` also supported

Not in scope yet:

- automatic migration from an existing tarball-based `/opt/stacklab` install

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
echo 'deb [arch=amd64 signed-by=/usr/share/keyrings/stacklab-archive-keyring.gpg] https://krbob.github.io/stacklab/apt stable main' \
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
echo 'deb [arch=amd64 signed-by=/usr/share/keyrings/stacklab-archive-keyring.gpg] https://krbob.github.io/stacklab/apt nightly main' \
  | sudo tee /etc/apt/sources.list.d/stacklab.list
```

Then install:

```bash
sudo apt-get update
sudo apt-get install stacklab
```

## Notes

- The package layout is Debian-native:
  - `/usr/lib/stacklab`
  - `/etc/stacklab/stacklab.env`
  - `/srv/stacklab`
  - `/var/lib/stacklab`
- The package also installs the Docker admin helper binary:
  - `/usr/lib/stacklab/bin/stacklab-docker-admin-helper`
- A sample `sudoers` allowlist is installed at:
  - `/usr/share/doc/stacklab/examples/stacklab-docker-admin.sudoers.example`
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
  - install a narrow `sudoers` rule for the helper
