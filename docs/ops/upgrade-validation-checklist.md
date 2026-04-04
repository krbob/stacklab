# Upgrade Validation Checklist

## Purpose

This checklist defines the expected validation pass for a real Stacklab upgrade on a Linux host.

It is intentionally short and operational.

## Preconditions

- existing Stacklab deployment is healthy
- current release path is known
- a new release tarball for the correct architecture is available
- the host has Docker and Compose available

## Upgrade Execution

1. Record the current symlink target:

   ```bash
   readlink -f /opt/stacklab/app/current
   ```

2. Run the upgrade:

   ```bash
   sudo ./host-tools/upgrade.sh
   ```

3. Confirm the new symlink target:

   ```bash
   readlink -f /opt/stacklab/app/current
   ```

## Required Checks

### Service health

- `systemctl status stacklab` is `active (running)`
- `curl -fsS http://127.0.0.1:8080/api/health` succeeds

### Basic product checks

- login works
- dashboard loads
- stack list is present
- stack detail loads
- logs stream works
- stats stream works
- terminal opens

### Mutating checks

- `resolved-config` preview works
- `save_definition` works
- `restart` works
- create stack works
- delete stack works

### Persistence checks

- existing `/opt/stacklab/stacks` content remains intact
- existing `/opt/stacklab/config` content remains intact
- existing `/opt/stacklab/data` content remains intact
- audit and SQLite state survive restart

## Rollback Drill

If the upgrade fails:

1. repoint `current` to the previous release
2. restart `stacklab.service`
3. verify `/api/health`

Command shape:

```bash
sudo ln -sfn /opt/stacklab/app/releases/<previous> /opt/stacklab/app/current
sudo systemctl restart stacklab
curl -fsS http://127.0.0.1:8080/api/health
```

## Exit Criteria

The upgrade is accepted when:

- service health is green
- all required checks pass
- no managed stack/config/data paths were mutated unexpectedly
- rollback procedure is clear and executable
