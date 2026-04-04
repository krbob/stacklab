# Maintenance Inventory Contract Draft

This document defines the first slice of Milestone 7:

- image inventory
- manual prune preview
- manual prune execution

It intentionally comes before broader networks/volumes inventory.

## Product Intent

This milestone exists to extend Stacklab from:

- stack lifecycle operations
- bulk update workflows

into:

- safe maintenance visibility
- explicit cleanup workflows

without turning Stacklab into a generic Docker control plane.

The first version should help answer:

- which images are present on the host
- which stacks currently use them
- which images look unused
- what a prune would remove
- how to run cleanup deliberately and auditably

## Goals

- show a host-level image inventory that stays relevant to Compose operators
- connect images back to Stacklab-managed stacks when possible
- let the operator preview cleanup before executing it
- keep destructive actions explicit, bounded, and auditable

## Non-Goals

- registry browser
- vulnerability scanner
- generic CRUD for all Docker images
- broad Docker object management unrelated to Compose operations
- automatic prune policies in this milestone

## Scope Split

Milestone 7 should be implemented in this order:

1. image inventory
2. prune preview
3. prune execution
4. later:
   - read-only networks inventory
   - read-only volumes inventory
   - selective actions only where directly useful

## Image Inventory

## `GET /api/maintenance/images`

Purpose:

- list locally available images in a maintenance-oriented shape

Query parameters:

- `q` optional text filter
- `usage` optional:
  - `all`
  - `used`
  - `unused`
- `origin` optional:
  - `all`
  - `stack_managed`
  - `external`

Response:

```json
{
  "items": [
    {
      "id": "sha256:abc123...",
      "repository": "ghcr.io/example/app",
      "tag": "latest",
      "reference": "ghcr.io/example/app:latest",
      "size_bytes": 483183820,
      "created_at": "2026-04-04T12:11:00Z",
      "containers_using": 2,
      "stacks_using": [
        {
          "stack_id": "demo",
          "service_names": ["app"]
        }
      ],
      "is_dangling": false,
      "is_unused": false,
      "source": "stack_managed"
    }
  ]
}
```

Notes:

- `source = stack_managed` means Stacklab can associate the image with at least one managed stack
- `source = external` means the image exists on the host but Stacklab cannot map it back to a managed stack
- `is_unused = true` means no running or stopped container currently uses the image
- image inventory should remain read-only in the first implementation

## Prune Preview

## `GET /api/maintenance/prune-preview`

Purpose:

- estimate what a cleanup action would remove before the operator confirms it

Query parameters:

- `images` optional boolean, default `true`
- `build_cache` optional boolean, default `true`
- `stopped_containers` optional boolean, default `true`
- `volumes` optional boolean, default `false`

Response:

```json
{
  "preview": {
    "images": {
      "count": 3,
      "reclaimable_bytes": 913928192,
      "items": [
        {
          "reference": "ghcr.io/example/old-web:1.0.0",
          "size_bytes": 483183820,
          "reason": "unused_image"
        }
      ]
    },
    "build_cache": {
      "count": 8,
      "reclaimable_bytes": 312475648
    },
    "stopped_containers": {
      "count": 2,
      "reclaimable_bytes": 0
    },
    "volumes": {
      "count": 0,
      "reclaimable_bytes": 0
    },
    "total_reclaimable_bytes": 1226403840
  }
}
```

Notes:

- preview may be approximate when Docker only exposes coarse reclaimable data
- `volumes = true` should remain opt-in and visually high-risk
- the first version can omit per-item details for some categories if Docker APIs are too coarse

## Prune Execution

## `POST /api/maintenance/prune`

Purpose:

- run a host-level cleanup job with explicit scope

Request body:

```json
{
  "scope": {
    "images": true,
    "build_cache": true,
    "stopped_containers": true,
    "volumes": false
  }
}
```

Response:

```json
{
  "job": {
    "id": "job_01hr...",
    "stack_id": null,
    "action": "prune",
    "state": "running",
    "workflow": {
      "steps": [
        {
          "action": "prune_images",
          "state": "running"
        },
        {
          "action": "prune_build_cache",
          "state": "queued"
        }
      ]
    }
  }
}
```

Notes:

- this should be a global workspace-scoped job
- the UI should treat it similarly to bulk maintenance:
  - one top-level job
  - step list
  - raw output/result details

## Error Model

Suggested error codes:

- `validation_failed`
- `docker_unavailable`
- `conflict`
- `internal_error`

Examples:

- `volumes = true` with unsupported preview/execution mode -> `400 validation_failed`
- prune requested while another global maintenance job is running -> `409 conflict`

## Job/Event Model

The prune workflow can reuse the existing job stream.

Recommended events:

- `job_step_started`
- `job_log`
- `job_step_finished`

Recommended step actions:

- `prune_images`
- `prune_build_cache`
- `prune_stopped_containers`
- `prune_volumes`

## Recommended First UI Shape

Recommended first maintenance IA:

- keep `/maintenance` as the global route
- keep existing update workflow as one section or tab
- add a second section/tab for:
  - `Images`
  - `Cleanup`

Recommended order:

1. `Update`
2. `Images`
3. `Cleanup`

This keeps all maintenance flows in one discoverable place without turning the sidebar into a Docker object navigator.

## Tests

Recommended initial backend tests:

- image inventory with:
  - used image
  - unused image
  - image mapped to a managed stack
  - image not mapped to any managed stack
- prune preview validation and shape
- prune execution job creation and step sequencing
- Docker-backed integration tests for preview/execution semantics
