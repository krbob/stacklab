# Docker Registry Auth Contract Draft

This document defines the proposed contract for Docker registry authentication inside Stacklab.

Scope:

- authenticate Docker pulls and builds against private registries
- make the currently configured registry auth visible without exposing secrets
- keep the source of truth in Docker client config, not in SQLite

This is intentionally narrower than a full registry browser or registry administration surface.

## Goals

- let operators use private images without dropping to SSH for `docker login`
- keep Docker auth aligned with the same runtime environment Stacklab already uses for `docker compose`
- preserve the current single-host, Compose-first model

## Non-Goals

- browsing repositories, tags, or manifests
- registry CRUD or registry user administration
- managing Docker credential helpers in the first slice
- storing raw registry passwords in Stacklab's database

## Route Shape

Recommended route:

- keep this inside `/docker`

Rationale:

- this is Docker client/runtime configuration, not stack-specific app config
- operators will look for private-registry setup next to Docker daemon status, not under `/settings`

## Runtime Model

Stacklab already runs with:

- `HOME=/var/lib/stacklab/home`
- `DOCKER_CONFIG=/var/lib/stacklab/docker`

That means:

- `docker login` should write credentials into the same `DOCKER_CONFIG` path the Stacklab service already uses
- subsequent stack actions such as `pull`, `build`, `up`, and maintenance updates should naturally reuse the authenticated Docker client config

Source of truth:

- Docker `config.json` under the effective `DOCKER_CONFIG`

Do not duplicate registry secrets into SQLite.

## Security Model

- the backend must never echo stored secrets back to the browser
- login requests may carry a password or token, but responses return only metadata
- the registry auth surface should be same-origin protected and audited like other mutating operator actions
- credential material is persisted only through Docker's own `config.json` handling in v1

## First Slice

The first slice should support:

- list configured registry auth entries
- login to a registry with username + password/token
- logout from a registry
- optional connection test as part of login

The first slice should not try to support:

- Docker credential stores
- per-registry custom CA management
- tag listing
- repository browsing

## REST Endpoints

## `GET /api/docker/registries`

Purpose:

- return the operator-facing view of configured Docker registry auth entries

Response:

```json
{
  "docker_config_path": "/var/lib/stacklab/docker/config.json",
  "write_capability": {
    "supported": true,
    "reason": ""
  },
  "items": [
    {
      "registry": "ghcr.io",
      "configured": true,
      "username": "bob",
      "source": "docker_config",
      "last_error": "",
      "last_verified_at": "2026-04-14T08:30:00Z"
    },
    {
      "registry": "registry.example.local:5000",
      "configured": true,
      "username": "",
      "source": "docker_config",
      "last_error": "",
      "last_verified_at": null
    }
  ]
}
```

Notes:

- `username` may be empty when Docker config does not expose it cleanly
- `configured=true` only means credentials are present in Docker config, not that the registry is reachable right now

## `POST /api/docker/registries/login`

Purpose:

- authenticate Docker against a registry and persist credentials into the effective `DOCKER_CONFIG`

Request:

```json
{
  "registry": "ghcr.io",
  "username": "bob",
  "password": "ghp_xxx"
}
```

Response:

```json
{
  "job": {
    "id": "job_abc123",
    "action": "docker_registry_login",
    "state": "running",
    "requested_at": "2026-04-14T08:32:00Z",
    "started_at": "2026-04-14T08:32:00Z",
    "workflow": {
      "steps": [
        {
          "action": "docker_login",
          "state": "running",
          "target_stack_id": ""
        }
      ]
    }
  }
}
```

Execution model:

- run `docker login <registry> --username <username> --password-stdin`
- set `DOCKER_CONFIG` explicitly in the child process environment
- publish job progress and terminal success/failure to the existing job stream

Validation:

- `registry` required
- `username` required
- `password` required

Error mapping:

- `400 validation_failed` for malformed input
- `409 invalid_state` if another registry login/logout job is already running and the implementation chooses to serialize them
- `500 internal_error` for Docker client execution failures not attributable to the registry response itself

Behavior on auth failure:

- return `200` with a failed terminal job, consistent with other job-based workflows
- surface stderr in retained job events

## `POST /api/docker/registries/logout`

Purpose:

- remove Docker client credentials for a registry from the effective `DOCKER_CONFIG`

Request:

```json
{
  "registry": "ghcr.io"
}
```

Response:

```json
{
  "job": {
    "id": "job_def456",
    "action": "docker_registry_logout",
    "state": "running",
    "requested_at": "2026-04-14T08:35:00Z",
    "started_at": "2026-04-14T08:35:00Z",
    "workflow": {
      "steps": [
        {
          "action": "docker_logout",
          "state": "running",
          "target_stack_id": ""
        }
      ]
    }
  }
}
```

Execution model:

- run `docker logout <registry>`
- use the same explicit `DOCKER_CONFIG` environment as Stacklab itself

## Audit

Record:

- `docker_registry_login`
- `docker_registry_logout`

Audit detail should include:

- `registry`
- `username` for login

Do not include:

- password
- token
- raw Docker auth blobs

## UI Shape

Recommended first UI slice inside `/docker`:

- new section `Registry auth`
- list of configured registries
- inline metadata:
  - registry
  - configured / not configured
  - username if known
  - last verified time if available
- actions:
  - `Login`
  - `Logout`

Recommended interaction model:

- `Login` opens a focused modal or card form
- `Logout` is a destructive but lightweight confirmation
- progress and errors reuse the existing job drawer / progress model

## Acceptance Criteria

- operator can authenticate to at least one private registry without SSH
- a subsequent `pull` for a private image succeeds using the same Stacklab runtime environment
- registry credentials never appear in API read responses, audit payloads, or UI logs
- missing or unreadable Docker `config.json` degrades cleanly instead of hard-failing the whole `/docker` page
- the feature works on package-managed installs with the existing `DOCKER_CONFIG=/var/lib/stacklab/docker` model

## Suggested Implementation Sequence

### Phase 1: Backend contract and execution path

- add a small `dockerregistryauth` service
- read configured registry entries from the effective Docker `config.json`
- implement `login` and `logout` through the existing Docker CLI execution model
- run both operations as tracked jobs with retained logs and audit entries

### Phase 2: UI inside `/docker`

- add a `Registry auth` section below daemon configuration
- show configured registries and degraded states
- add `Login` and `Logout` flows
- reuse job progress and job drawer instead of inventing a second execution model

### Phase 3: Validation on Linux

- verify package-managed installs use the expected `DOCKER_CONFIG`
- verify a private-image `pull` works after login
- verify `logout` actually removes usable auth for subsequent pulls
- verify secrets never leak into API responses, audit details, or retained job events
