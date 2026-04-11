# systemd Deployment

## Purpose

This document defines the recommended host-native deployment model for Stacklab v1 using `systemd`.

It is the operational consequence of:

- ADR 0001: host-native backend deployment
- Compose-first filesystem model
- single-host Linux `amd64` target

The runtime model in this document has been validated in staging on:

- Ubuntu `arm64`
- Ubuntu `amd64`
- Debian `amd64`

## Deployment Model

Stacklab runs as:

- one backend service managed by `systemd`
- one static frontend bundle served by the backend or colocated assets
- one SQLite database under `/var/lib/stacklab`

Stacklab itself is **not** deployed as a Docker management container.

## Canonical Paths

Recommended host layout:

- application home: `/opt/stacklab/app`
- managed stacks: `/opt/stacklab/stacks`
- managed config: `/opt/stacklab/config`
- managed data: `/opt/stacklab/data`
- runtime state: `/var/lib/stacklab`
- logs: systemd journal by default

## Service Account

Recommended dedicated service account:

- user: `stacklab`
- group: `stacklab`

Responsibilities:

- run the backend process
- read and write required paths under `/opt/stacklab`
- access Docker via `/var/run/docker.sock`
- read Stacklab service logs from `journald` when `/host` log viewing is enabled

## Permissions Model

Minimum practical permissions for the service account:

- read/write on `/opt/stacklab/stacks`
- read/write on `/opt/stacklab/config`
- read/write on `/opt/stacklab/data` only when destructive actions explicitly require it
- read/write on `/var/lib/stacklab`
- read/write access to `/var/run/docker.sock` via group membership or equivalent host configuration
- read access to `journald`, typically via `systemd-journal` group membership when that group exists

Recommended operational posture:

- avoid running the service as `root`
- prefer membership in the Docker socket-owning group if acceptable on the host
- prefer membership in `systemd-journal` when service log viewing in `/host` should work on the host
- keep permissions explicit rather than broad `0777` directory access
- treat container-created ownership drift as an operator problem to surface and repair explicitly, not as a reason to run the whole web app as `root`
- prefer aligning container `uid:gid` or `PUID/PGID` with the managed workspace where practical

## Runtime Configuration

Recommended environment variables:

| Variable | Example | Purpose |
|---|---|---|
| `STACKLAB_ROOT` | `/opt/stacklab` | Canonical managed root |
| `STACKLAB_DATA_DIR` | `/var/lib/stacklab` | SQLite and runtime files |
| `STACKLAB_HTTP_ADDR` | `127.0.0.1:8080` | Bind address |
| `STACKLAB_LOG_LEVEL` | `info` | Log level |
| `HOME` | `/var/lib/stacklab/home` | Stable writable home for Compose and service runtime |
| `DOCKER_CONFIG` | `/var/lib/stacklab/docker` | Writable Docker config path when `ProtectHome=true` |

Recommended default binding:

- `127.0.0.1:8080` when fronted by a local reverse proxy

Alternative:

- host private interface address when Traefik or another reverse proxy in Docker must reach the service from bridge networking

## Example systemd Unit

Example file:

```ini
[Unit]
Description=Stacklab
After=network-online.target docker.service
Wants=network-online.target docker.service

[Service]
Type=simple
User=stacklab
Group=stacklab
SupplementaryGroups=docker systemd-journal
WorkingDirectory=/opt/stacklab/app/current
Environment=STACKLAB_ROOT=/opt/stacklab
Environment=STACKLAB_DATA_DIR=/var/lib/stacklab
Environment=STACKLAB_HTTP_ADDR=127.0.0.1:8080
Environment=STACKLAB_LOG_LEVEL=info
Environment=HOME=/var/lib/stacklab/home
Environment=DOCKER_CONFIG=/var/lib/stacklab/docker
ExecStart=/opt/stacklab/app/current/bin/stacklab
Restart=on-failure
RestartSec=5
TimeoutStartSec=30
TimeoutStopSec=30
# Must remain false if any opt-in sudo helper is enabled.
NoNewPrivileges=false
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=/opt/stacklab /var/lib/stacklab /etc/docker

[Install]
WantedBy=multi-user.target
```

## Hardening Notes

The example unit is intentionally conservative and may need adjustment depending on:

- how Docker socket permissions are granted
- whether PTY operations require additional allowances on the host
- how the backend serves frontend assets

Recommended hardening goals:

- `NoNewPrivileges=false` when any opt-in privileged helper is enabled through `sudo`
- `PrivateTmp=true`
- read-only system paths where feasible
- explicit `ReadWritePaths`

Potential adjustments:

- relax `ProtectSystem` only for a proven runtime need; Stacklab self-update must not require this because it runs the updater through a transient `systemd-run` unit
- keep `NoNewPrivileges=false` if the Docker admin helper, workspace repair helper, or self-update helper is enabled through `sudo`
- include `/etc/docker` in `ReadWritePaths` if the Docker admin helper is enabled
- prefer `Wants=docker.service` over `Requires=docker.service` so Stacklab survives a Docker daemon restart
- do not enable sandboxing blindly before terminal and Docker access are verified end-to-end

## Service Dependencies

Stacklab depends on:

- Docker Engine
- Compose v2 availability in the host environment, through either:
  - `docker compose`
  - standalone `docker-compose`

Practical packaging note:

- stock Debian may provide Compose through the standalone `docker-compose` package instead of the `docker compose` plugin path
- Stacklab supports both command shapes

Startup behavior recommendation:

- start after Docker when available
- tolerate Docker being temporarily unavailable or restarted
- expose degraded Docker state clearly in logs and UI instead of binding Stacklab lifecycle to `docker.service`
- allow restart policy to recover automatically when Docker becomes available

## Frontend Asset Serving

Two acceptable v1 models:

### Model A: backend serves static assets

Advantages:

- simpler deployment
- one process to manage

### Model B: reverse proxy serves static assets and proxies API

Advantages:

- more flexible for future separation

Recommendation for v1:

- prefer Model A unless deployment needs clearly justify separation

## Reverse Proxy Integration

Typical deployment:

- Stacklab binds to `127.0.0.1:8080`
- reverse proxy terminates TLS
- reverse proxy forwards to Stacklab

Special case:

- if Traefik runs in Docker bridge mode and must reach Stacklab, bind Stacklab to a host address reachable from the container network

Operational caution:

- keep an emergency direct path to Stacklab during setup and upgrades
- do not depend exclusively on a proxy stack that is itself managed by Stacklab without a fallback path

## First-Time Setup Checklist

1. create directories under `/opt/stacklab`
2. create `/var/lib/stacklab`
3. create `stacklab` service account
4. ensure Docker and Compose are installed on the host
5. grant the service account required access to Docker socket
6. install binary and frontend assets under `/opt/stacklab/app`
7. install and enable the systemd unit
8. initialize password on first launch

Practical shortcut:

- for tarball-based installs and upgrades, prefer the packaged `host-tools/upgrade.sh`
- see [install-from-tarball.md](install-from-tarball.md)

## Upgrade Strategy

Recommended upgrade flow:

1. stop the service
2. unpack the new build into a versioned directory under `/opt/stacklab/app/releases/`
3. repoint `/opt/stacklab/app/current`
4. run any schema migrations
5. start the service
6. verify `GET /api/health`

Observed staging note:

- if `ProtectHome=true` is enabled, set explicit writable `HOME` and `DOCKER_CONFIG` paths for the service
- ensure the service account is in the Docker socket-owning group, typically `docker`
- ensure the service account can read `journald`, typically via `systemd-journal`, if `/host` should display Stacklab service logs

Rules:

- stack definitions under `/opt/stacklab/stacks` are not part of the application artifact
- upgrades must not mutate managed stack definitions unless triggered through explicit user actions

## Logging And Diagnostics

Default logging target:

- `journald`

Recommended commands:

```bash
systemctl status stacklab
journalctl -u stacklab -n 200
journalctl -u stacklab -f
```

Logs should include:

- startup configuration summary without secrets
- Docker/Compose availability failures
- job lifecycle milestones
- authentication failures without sensitive details

## Backup Considerations

For application-level recovery, back up:

- `/var/lib/stacklab`
- `/opt/stacklab/app` if local deployment assets are not otherwise reproducible

For operator data recovery, back up separately:

- `/opt/stacklab/stacks`
- `/opt/stacklab/config`
- `/opt/stacklab/data`

## Failure Modes

### Docker unavailable

Expected behavior:

- service may start and report degraded state, or fail fast based on implementation choice
- UI should clearly indicate Docker unavailability

### SQLite unavailable

Expected behavior:

- service should fail to start or enter degraded mode with explicit logs

### Reverse proxy unavailable

Expected behavior:

- Stacklab should still be reachable through its direct bind address if that path is intentionally preserved

## Open Questions For Implementation

Implementation should later decide:

- exact binary path under `/opt/stacklab/app`
- whether frontend assets are embedded or separate
- whether the service account uses Docker group membership or another access pattern
- whether systemd socket activation provides any value for this service
