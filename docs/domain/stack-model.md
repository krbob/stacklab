# Stack Model

## Purpose

This document defines the canonical domain model for stacks, services, containers, and stack state presentation in Stacklab v1.

## Core Principle

Stack identity comes from the filesystem, not from Docker runtime objects.

A Stacklab stack exists when a directory under the managed stacks root contains a canonical `compose.yaml` file.

## Canonical Identifiers

### Stack ID

The stack ID is the stack directory name.

Validation rules:

- lowercase ASCII only
- digits allowed
- words separated by single dashes
- must be filesystem-safe and URL-safe

Canonical regex:

```text
^[a-z0-9]+(?:-[a-z0-9]+)*$
```

UI should validate this pattern before submission. Backend must validate it again and reject invalid input.

### Service ID

The service ID is the Compose service name inside `compose.yaml`.

### Container ID

The container ID is the Docker container ID reported by the runtime.

## Entities

### Stack

Represents one filesystem-defined Compose stack.

Core fields:

- `id`
- `name`
- `root_path`
- `compose_file_path`
- `env_file_path`
- `config_path`
- `data_path`
- `created_at`
- `updated_at`

Derived fields:

- `services[]`
- `containers[]`
- `runtime_state`
- `config_state`
- `activity_state`
- `display_state`
- `health_summary`
- `last_deployed_at`
- `last_action`

### Service

Represents one Compose service definition.

Core fields:

- `name`
- `mode`
- `image_ref`
- `build_context`
- `dockerfile_path`
- `ports[]`
- `volumes[]`
- `depends_on[]`
- `healthcheck_present`

`mode` values:

- `image`
- `build`
- `hybrid`

Rules:

- `image` means an image reference is the main deployment source
- `build` means the service is built from source without relying on a remote image reference as the primary source
- `hybrid` means both `build` and `image` are present and Compose policy determines actual behavior

### Container

Represents one runtime container belonging to a stack service.

Core fields:

- `id`
- `name`
- `service_name`
- `status`
- `health_status`
- `started_at`
- `image_id`
- `image_ref`
- `ports[]`
- `networks[]`

`status` values mirror normalized Docker runtime status, for example:

- `created`
- `running`
- `restarting`
- `paused`
- `exited`
- `dead`

## Discovery Rules

A stack is discovered when:

- a directory exists under the managed stacks root
- that directory contains `compose.yaml`

v1 rules:

- `compose.yaml` is canonical
- `compose.yml` and `docker-compose.yml` are not first-class supported inputs in v1
- the backend may later add non-canonical discovery warnings, but UI should not depend on them in MVP

## State Model

Stack state is intentionally split into three orthogonal dimensions and one UI-facing derived dimension.

### Runtime State

Describes what Docker is currently doing for the stack.

Allowed values:

- `defined`
- `running`
- `partial`
- `stopped`
- `error`
- `orphaned`

Definitions:

- `defined`: stack exists on disk but has no current runtime containers and no known successful deployment yet
- `running`: all expected runtime containers are running and none are in error-like status
- `partial`: some expected containers are running, but not all
- `stopped`: stack has known runtime containers or deployment history, but all current containers are stopped
- `error`: at least one expected container is in `restarting`, `dead`, or another error-like runtime state, or deployment reconciliation failed
- `orphaned`: runtime containers exist that claim the stack identity, but the canonical stack definition is missing on disk

### Config State

Describes the relationship between filesystem configuration and the last known deployed revision.

Allowed values:

- `unknown`
- `in_sync`
- `drifted`
- `invalid`

Definitions:

- `unknown`: no deploy baseline exists yet
- `in_sync`: current stack definition matches the last deployed revision known to Stacklab
- `drifted`: stack definition changed after the last known deploy
- `invalid`: current compose definition fails validation

Rules:

- `drifted` is not a runtime failure
- `invalid` blocks mutating deployment actions except safe validation-oriented actions

### Activity State

Describes whether Stacklab is actively operating on the stack.

Allowed values:

- `idle`
- `locked`

Definitions:

- `idle`: no mutating job currently owns the stack lock
- `locked`: a mutating job is currently in progress for this stack

Rules:

- `locked` is not a primary stack health state
- UI should present `locked` as an overlay, spinner, or action-disabled state rather than replacing the main runtime badge

### Display State

This is the UI-facing primary badge state derived from `runtime_state`.

Allowed values:

- `defined`
- `running`
- `partial`
- `stopped`
- `error`
- `orphaned`

Derivation rule:

- `display_state = runtime_state`

Additional UI decorations:

- `config_state = drifted` should add a warning-style secondary indicator
- `config_state = invalid` should add an error-style secondary indicator
- `activity_state = locked` should add a progress indicator and disable conflicting actions

## Recommended UI Mapping

### Primary Badge

- `running`: success
- `partial`: warning
- `stopped`: neutral
- `error`: danger
- `defined`: muted
- `orphaned`: danger or warning-danger

### Secondary Indicators

- `drifted`: warning with config change icon
- `invalid`: danger with validation icon
- `locked`: info/progress spinner

## Health Summary

The stack should also expose a summarized health rollup independent from primary state.

Suggested fields:

- `healthy_container_count`
- `unhealthy_container_count`
- `unknown_health_container_count`

Health summary does not replace stack state. It supplements it.

## Drift Detection Baseline

For v1, drift is measured against the last successful mutating deployment action tracked by Stacklab.

Inputs to the drift baseline may include:

- normalized `compose.yaml` content hash
- `.env` content hash

Changes under stack-scoped paths in the managed config workspace are not part of v1 drift detection unless explicitly wired into future deployment hashing.

## Deletion Semantics

Stack deletion concerns multiple independent objects:

- runtime resources
- stack definition directory
- config directory
- data directory

These are never assumed to be the same action.

Default safe remove behavior in v1:

- remove runtime resources only
- keep stack definition unless the user explicitly requests deletion
- keep config
- keep data
