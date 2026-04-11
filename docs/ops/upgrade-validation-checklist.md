# Upgrade Validation Checklist

## Purpose

This checklist defines the expected real-host validation pass for Stacklab
upgrades.

Run it for both supported install modes before a stable release:

- package-managed `.deb` or APT
- manual tarball

## Common Preconditions

- the existing Stacklab deployment is healthy
- the host has Docker and Compose available
- the host runs `systemd`
- the target artifact matches the host architecture
- the operator knows which install mode is under test
- the operator knows whether helper-backed features are intentionally configured on the host

## APT or `.deb` Upgrade Execution

Record the current installed version:

```bash
dpkg-query -W -f='${Version}\n' stacklab
```

Upgrade using either the published repository or a local package file:

```bash
sudo apt-get update
sudo apt-get install stacklab
```

or:

```bash
sudo apt-get install ./stacklab_<version>_<arch>.deb
```

Confirm the new installed version:

```bash
dpkg-query -W -f='${Version}\n' stacklab
```

## Tarball Upgrade Execution

Record the current release path:

```bash
readlink -f /opt/stacklab/app/current
```

Run the packaged upgrade flow:

```bash
sudo ./host-tools/upgrade.sh
```

Confirm the new release path:

```bash
readlink -f /opt/stacklab/app/current
```

## Required Checks After Either Upgrade

### Service health

- `systemctl status stacklab` is `active (running)`
- `curl -fsS http://127.0.0.1:8080/api/health` succeeds

### Core product checks

- login works
- dashboard loads
- stack list is present when the workspace is populated
- stack detail loads
- logs stream works
- stats stream works
- terminal opens
- `/host` loads, Stacklab logs render, and host metrics refresh in place
- `/config` loads, file browse or edit works, and Git `Changes` mode behaves correctly
- `/maintenance` loads and update, images, networks, volumes, and cleanup tabs render
- `/docker` overview loads
- `/settings` loads

### Mutating checks

- resolved-config preview works
- `save_definition` works
- `restart` works
- create stack works
- delete stack works
- config workspace save works
- maintenance update and cleanup jobs render progress and finish cleanly

### Persistence checks

- operator-managed workspace content remains intact
- audit and SQLite state survive restart

For package-managed installs, verify:

- `/srv/stacklab/stacks`
- `/srv/stacklab/config`
- `/srv/stacklab/data`

For tarball installs, verify:

- `/opt/stacklab/stacks`
- `/opt/stacklab/config`
- `/opt/stacklab/data`

## Install-Mode-Specific Checks

### APT or `.deb`

- `/api/stacklab/update/overview` reports `install_mode = apt`
- self-update card is available when the helper is configured
- package metadata shows the expected installed version
- helper-backed flows work when configured:
  - config workspace repair
  - stack workspace repair
  - Docker daemon validate and apply
  - self-update apply

### Tarball

- manual release directories remain intact under `/opt/stacklab/app/releases`
- `/api/stacklab/update/overview` reports the tarball unsupported state for self-update
- rollback via the previous release symlink remains clear and executable
- helper-backed workspace repair and Docker admin still behave correctly if the tarball host opted into them

## Rollback Drill

### APT or `.deb`

If package rollback is needed, use the previous package version:

```bash
sudo apt-get install ./stacklab_<previous_version>_<arch>.deb
```

or the equivalent repository-backed downgrade flow.

### Tarball

If the tarball upgrade fails:

```bash
sudo ln -sfn /opt/stacklab/app/releases/<previous> /opt/stacklab/app/current
sudo systemctl restart stacklab
curl -fsS http://127.0.0.1:8080/api/health
```

## Exit Criteria

The upgrade is accepted when:

- service health is green
- required checks pass for the install mode under test
- operator-managed stack, config, and data paths were not mutated unexpectedly
- rollback remains clear for that install mode
