# Scope

## Product Statement

Stacklab is a host-native, web-based control panel for managing Docker Compose stacks on a single Linux `amd64` homelab host.

The system is intentionally `Compose-first`:

- stack definitions live on disk
- operators can still use `docker compose` manually outside Stacklab
- Stacklab does not become the source of truth for stack definitions

## Target Environment

- one managed host
- local network only
- no public internet exposure required
- no remote agent model in v1
- no requirement to manage non-Compose workloads

## Primary User

Single operator or a very small trusted household team managing one homelab machine.

## Core Jobs To Be Done

- inspect all Compose stacks in one place
- understand stack and container runtime state quickly
- edit stack definitions safely
- run operational actions without dropping to a separate terminal for routine tasks
- inspect logs, stats, and container shell sessions when diagnosing problems
- update images or rebuild services with predictable behavior

## Source Of Truth

Primary source of truth:

- `/opt/homelab/stacks/<stack>/compose.yaml`
- `/opt/homelab/stacks/<stack>/.env`
- `/opt/homelab/config/<stack>/`
- `/opt/homelab/data/<stack>/`

Secondary application state:

- local SQLite database for settings, schedules, audit entries, and cached metadata

## Design Constraints

- Docker Compose only
- Linux `amd64` only
- host-native deployment preferred over running Stacklab as a privileged management container
- filesystem remains user-readable and user-editable without Stacklab
- security must assume terminal features are high-risk even on LAN

## Success Criteria

- operator can create, inspect, edit, deploy, and troubleshoot stacks without losing manual CLI compatibility
- stack state in UI matches actual Docker runtime state
- failed operations are explicit and recoverable
- UI developer can build screens against stable backend contracts

