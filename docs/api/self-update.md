# Stacklab Self-Update Contract Draft

This document defines the first Stacklab self-update slice.

Scope:

- APT-managed Stacklab installs only
- read-only version and update availability overview
- helper-backed self-update workflow that upgrades the `stacklab` package
- runtime status for the most recent self-update attempt

Non-goals in this slice:

- tarball self-update
- host-wide package management
- full `apt upgrade`
- reboot management
- rollback to a previous package version

## Product Intent

This milestone exists to move Stacklab closer to:

- "the last routine SSH login"

The first concrete goal is:

- operators can see whether a newer Stacklab package is available
- operators can trigger the upgrade from the UI
- Stacklab restarts itself safely and reports the result back after recovery

## Install Mode Rules

Self-update is intentionally narrow:

- supported when Stacklab is installed from the Stacklab APT package
- unsupported for tarball installs under `/opt/stacklab`

Reason:

- only the APT-managed install path has a safe, well-defined package upgrade workflow

## Helper Model

The main app still does not run as `root`.

Self-update requires an opt-in privileged helper:

- `stacklab-self-update-helper`

Recommended env:

- `STACKLAB_SELF_UPDATE_HELPER_PATH=/usr/lib/stacklab/bin/stacklab-self-update-helper`
- `STACKLAB_SELF_UPDATE_USE_SUDO=true`
- `STACKLAB_SELF_UPDATE_PACKAGE_NAME=stacklab`
- `STACKLAB_SELF_UPDATE_HEALTH_URL=http://127.0.0.1:8080/api/health`

Operational notes:

- if `sudo` is used, `stacklab.service` must run with `NoNewPrivileges=false`
- the helper should be allowlisted narrowly through `sudoers`
- the helper upgrades only the configured Stacklab package, not arbitrary packages

## REST Endpoints

## `GET /api/stacklab/update/overview`

Purpose:

- show current Stacklab version, install mode, APT package state, and the current self-update runtime status

Response:

```json
{
  "current_version": "2026.04.0",
  "install_mode": "apt",
  "package": {
    "supported": true,
    "message": "",
    "name": "stacklab",
    "installed_version": "2026.04.0",
    "candidate_version": "2026.04.1",
    "configured_channel": "stable",
    "update_available": true
  },
  "write_capability": {
    "supported": true
  },
  "runtime": {
    "job_id": "job_123",
    "pending_finalize": false,
    "requested_version": "2026.04.1",
    "installed_version": "2026.04.0",
    "result": "",
    "message": "",
    "started_at": "2026-04-10T11:50:00Z",
    "finished_at": null
  }
}
```

Tarball or unsupported package state example:

```json
{
  "current_version": "2026.04.0",
  "install_mode": "tarball",
  "package": {
    "supported": false,
    "message": "Stacklab self-update is only available for APT installs.",
    "name": "stacklab",
    "update_available": false
  },
  "write_capability": {
    "supported": false,
    "reason": "Stacklab self-update is only available for APT installs."
  }
}
```

Notes:

- returns `200` even when self-update is unsupported
- unsupported state is represented in the payload, not as a generic hard failure
- `runtime` is present when a self-update is currently running or when the latest result is still retained

## `POST /api/stacklab/update/apply`

Purpose:

- start the global Stacklab self-update workflow

Request:

```json
{
  "expected_candidate_version": "2026.04.1",
  "refresh_package_index": true
}
```

Response:

```json
{
  "started": true,
  "job": {
    "id": "job_123",
    "stack_id": null,
    "action": "self_update_stacklab",
    "state": "running",
    "requested_at": "2026-04-10T11:50:00Z",
    "started_at": "2026-04-10T11:50:00Z",
    "finished_at": null,
    "workflow": {
      "steps": [
        { "action": "apt_update", "state": "running" },
        { "action": "upgrade_package", "state": "queued" },
        { "action": "verify_restart", "state": "queued" }
      ]
    }
  },
  "package": {
    "supported": true,
    "name": "stacklab",
    "installed_version": "2026.04.0",
    "candidate_version": "2026.04.1",
    "configured_channel": "stable",
    "update_available": true
  },
  "runtime": {
    "job_id": "job_123",
    "pending_finalize": false,
    "requested_version": "2026.04.1",
    "started_at": "2026-04-10T11:50:00Z"
  }
}
```

Notes:

- this is always a global job because Stacklab upgrades and restarts itself
- the update may restart the HTTP process during the workflow
- the helper writes workflow state back to SQLite and the recovered app finalizes audit/notifications on startup
- if another self-update is already running, this returns `409 invalid_state`
- if the helper path or `sudoers` are not configured, this returns `503 self_update_unavailable`

## Workflow Model

The first workflow uses these steps:

- `apt_update`
- `upgrade_package`
- `verify_restart`

`apt_update` is omitted when `refresh_package_index = false`.

## Notifications

This slice reuses the existing job notification model:

- failed self-updates can emit `job_failed`
- Stacklab does not introduce a dedicated self-update success notification in v1

## Future-Compatible Hooks

The contract should leave room for later:

- explicit channel selection in the UI
- "check again" / package index refresh affordance
- self-update release notes preview
- dedicated self-update success notification
- rollback to a previous package version
