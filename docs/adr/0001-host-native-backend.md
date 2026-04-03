# ADR 0001: Host-Native Backend Deployment

## Status

Accepted

## Context

Stacklab needs deep access to:

- `/var/run/docker.sock`
- stack files under `/opt/homelab`
- PTY-backed shell sessions
- local process execution for `docker compose`
- local Git state

One possible deployment model is to run Stacklab itself as a privileged Docker container. Another is to run the backend as a native host service.

## Decision

The Stacklab backend will run as a native host service managed by `systemd`.

The frontend will be served as static assets by the backend or an optional local reverse proxy.

## Rationale

- a privileged management container would still require near-host-level access
- host shell and PTY handling are simpler and less fragile on the host
- local filesystem paths and build contexts remain straightforward
- local Git integration remains direct
- deployment as a single backend binary keeps operational overhead low

## Consequences

### Positive

- simpler process execution model
- clearer access to Docker and stack directories
- fewer bind-mount and namespace edge cases
- easier debugging during early development

### Negative

- less portable as a turnkey self-hosted container app
- stronger need for careful service hardening
- installation story must document `systemd` clearly

## Follow-Up

- define systemd unit and service account model
- define runtime directories and permissions
- harden terminal and WebSocket paths

