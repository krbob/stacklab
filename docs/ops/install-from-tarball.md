# Install From Tarball

## Purpose

This document defines the first supported host-native install and upgrade flow for Stacklab using release tarballs.

It remains a supported host-native install and upgrade path alongside the `.deb`
and APT workflows.

## Supported Platforms

Current release artifacts are produced for:

- Linux `amd64` as the primary release architecture
- Linux `arm64` as a supported secondary release architecture

Recommended production baseline:

- Debian `amd64`

## What You Need On The Host

Minimum prerequisites:

- Linux with `systemd`
- Docker Engine
- Compose support through either:
  - `docker compose`
  - standalone `docker-compose`
- `tar`
- `curl` or `wget`

Recommended packages on Debian-like hosts:

```bash
sudo apt-get update
sudo apt-get install -y docker.io docker-compose-v2 curl tar
```

Fallback if `docker-compose-v2` is unavailable:

```bash
sudo apt-get install -y docker.io docker-compose curl tar
```

## Artifact Contents

A release tarball contains:

```text
stacklab-<version>-linux-<arch>/
  bin/stacklab
  frontend/dist/
  metadata/
    version.txt
    commit.txt
    build_time.txt
    platform.txt
  systemd/
    stacklab.service.example
    stacklab.env.example
    stacklab-docker-admin.sudoers.example
  host-tools/
    upgrade.sh
```

`host-tools/upgrade.sh` is the supported installation and upgrade entrypoint.

The tarball also includes:

- `bin/stacklab-docker-admin-helper`
- `systemd/stacklab-docker-admin.sudoers.example`

These are only needed if you later opt into Docker daemon apply workflows.

## First Install

1. Download the artifact and checksum for the correct architecture.
2. Verify the checksum.
3. Extract the tarball.
4. Run the packaged upgrade script with `--install-unit`.

Example:

```bash
tar -xzf stacklab-2026.04.0-linux-amd64.tar.gz
cd stacklab-2026.04.0-linux-amd64
sudo STACKLAB_BOOTSTRAP_PASSWORD='change-me' ./host-tools/upgrade.sh --install-unit
```

This does the following:

- installs the release under `/opt/stacklab/app/releases/`
- creates or updates `/opt/stacklab/app/current`
- creates the `stacklab` service account if missing
- installs `/etc/systemd/system/stacklab.service` if missing
- installs `/etc/stacklab/stacklab.env` if missing
- restarts `stacklab.service`
- waits for `/api/health`

If you already run Stacklab under a different service account, provide it explicitly:

```bash
sudo ./host-tools/upgrade.sh --install-unit --service-user bob --service-group bob
```

## Upgrade

For upgrades, use the same flow against the new tarball.

Example:

```bash
tar -xzf stacklab-2026.04.1-linux-amd64.tar.gz
cd stacklab-2026.04.1-linux-amd64
sudo ./host-tools/upgrade.sh
```

Or, without extracting manually:

```bash
sudo ./scripts/release/upgrade.sh ./dist/release/stacklab-2026.04.1-linux-amd64.tar.gz
```

## Installed Paths

The tarball-based flow assumes:

- application releases: `/opt/stacklab/app/releases`
- current release symlink: `/opt/stacklab/app/current`
- managed stacks: `/opt/stacklab/stacks`
- managed config: `/opt/stacklab/config`
- managed data: `/opt/stacklab/data`
- runtime state: `/var/lib/stacklab`

The upgrade script does **not** modify:

- `/opt/stacklab/stacks`
- `/opt/stacklab/config`
- `/opt/stacklab/data`

Those remain operator-managed.

## Health Check And Rollback

Default health URL:

- `http://127.0.0.1:8080/api/health`

Default behavior:

- switch `current`
- restart `stacklab.service`
- wait for health
- if health fails and a previous release exists, roll back to the previous symlink target and restart again

Useful override:

```bash
sudo ./host-tools/upgrade.sh --health-url http://127.0.0.1:18080/api/health
```

## Service File Notes

The example unit assumes:

- service name: `stacklab`
- service user/group: `stacklab`
- unit path: `/etc/systemd/system/stacklab.service`
- env file path: `/etc/stacklab/stacklab.env`

If you already have a working unit, the upgrade script leaves it in place and only updates the app release under `/opt/stacklab/app`.

## Verification Commands

After install or upgrade:

```bash
systemctl status stacklab
journalctl -u stacklab -n 200
curl -fsS http://127.0.0.1:8080/api/health
```

## Known Limits

- the tarball flow is host-native, not containerized
- it does not publish GitHub Releases automatically
- it does not yet install a `.deb`
- it assumes the host already has Docker and Compose available
