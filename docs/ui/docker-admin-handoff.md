# Docker Admin Handoff

This handoff covers the UI work for Docker Admin v1:

- read-only Docker service status
- read-only Docker Engine metadata
- read-only `daemon.json` visibility

Backend contract draft:

- `docs/api/docker-admin.md`

## Confirmed Information Architecture

Recommended route:

- `/docker`

Recommended nav placement:

- `Stacks`
- `Host`
- `Docker`
- `Config`
- `Maintenance`
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

## Important Product Notes

- this milestone is read-only
- there is no write/apply button yet
- do not imply that changes can be saved in this version
- if `permissions.writable = false`, that is information for the operator, not an invitation to show a disabled editor

## Future-Compatible Hooks

The v1 UI should leave room for later:

- edit selected keys
- diff before apply
- restart Docker
- rollback if restart fails

That means:

- keep the `daemon.json` area conceptually separate from host metrics
- do not bury it inside `/host`
- avoid a UI shape that assumes the file is only informational forever
