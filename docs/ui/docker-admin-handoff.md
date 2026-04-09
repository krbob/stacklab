# Docker Admin Handoff

This handoff covers the UI work for Docker Admin:

- read-only Docker service status
- read-only Docker Engine metadata
- read-only `daemon.json` visibility
- managed settings validation preview for selected Docker daemon keys

Backend contract draft:

- `docs/api/docker-admin.md`

## Confirmed Information Architecture

Recommended route:

- `/docker`

Recommended nav placement:

- `Stacks`
- `Host`
- `Config`
- `Maintenance`
- `Docker`
- `Audit`
- `Settings`

Rationale:

- `/host` stays focused on host and Stacklab observability
- `/maintenance` stays focused on workflows and cleanup
- Docker daemon configuration is its own operator surface and will later grow a write/apply flow

## Screen Shape

Recommended sections:

1. Docker service
   - unit status
   - enabled/disabled
   - started time
2. Engine
   - Engine version
   - API version
   - Compose version
   - root dir
   - storage/logging/cgroup driver
3. `daemon.json`
   - file path
   - exists / valid JSON
   - owner/group/mode
   - summary of important configured keys
   - read-only raw JSON viewer below the summary
4. managed settings
   - opinionated form for selected keys only:
     - DNS
     - registry mirrors
     - insecure registries
     - live restore
   - preview / validate action before any future apply

## Recommended Layout

Desktop:

- top row of compact cards for service and engine
- second section for `daemon.json` summary
- raw JSON viewer below the summary

Tablet:

- stacked cards
- raw JSON viewer full width below

## Required UI States

- loading overview
- degraded service status when `systemd` is unavailable
- degraded engine status when Docker is unreachable
- missing `daemon.json`
- unreadable `daemon.json`
- invalid JSON in `daemon.json`
- managed settings preview response
- write-capability-disabled state

## Important Product Notes

- do not show a raw editable `daemon.json` textarea
- the writable slice is a managed settings form, not arbitrary JSON editing
- `write_capability.supported = false` means preview is available but actual privileged apply is not yet wired
- if `permissions.writable = false`, that remains informational; actual apply will later go through a separate privileged path

## Future-Compatible Hooks

The v1 UI should leave room for later:

- diff before apply
- restart Docker
- rollback if restart fails

That means:

- keep the `daemon.json` area conceptually separate from host metrics
- do not bury it inside `/host`
- avoid a UI shape that assumes the file is only informational forever

## Managed Settings Guidance

The first writable UX should be built around selected keys only:

- `dns`
- `registry_mirrors`
- `insecure_registries`
- `live_restore`

Recommended flow:

1. operator edits the managed settings form
2. UI calls `POST /api/docker/admin/daemon-config/validate`
3. UI shows:
   - changed keys
   - restart warning
   - merged preview content
4. future apply button is shown but disabled while `write_capability.supported = false`

Do not build:

- free-form JSON editor as the primary write surface
- hidden apply controls that pretend the current release can restart Docker already

## Future Apply Flow Notes

Backend now exposes a future-compatible apply endpoint:

- `POST /api/docker/admin/daemon-config/apply`

It returns a global job and uses the same long-running job model as maintenance.

That means the eventual apply UX can reuse:

- global activity
- step cards
- job detail links

Current backend behavior:

- if the privileged helper is not configured, apply returns `501 not_implemented`
- preview validation is the only supported write-adjacent action by default
