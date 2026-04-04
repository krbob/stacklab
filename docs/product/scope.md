# Scope

## Product Statement

Stacklab is a host-native, web-based control panel for managing Docker Compose stacks on a single Linux host, with `amd64` as the primary architecture and `arm64` also supported.

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
- edit supporting configuration under `/opt/stacklab/config` without leaving the product
- run operational actions without dropping to a separate terminal for routine tasks
- inspect logs, stats, and container shell sessions when diagnosing problems
- update images or rebuild services with predictable behavior
- understand whether an issue is caused by the host, Docker, or the stack itself
- perform selected maintenance workflows without turning Stacklab into a generic Docker control plane
- keep local workspace changes reviewable and easy to commit back to Git

## Product Principles

- Compose-first, not generic-Docker-first
- filesystem-first, not database-owned stacks
- single-host-first, not fleet-first
- explicit operator control, not hidden reconciliation
- conservative destructive actions with clear scope
- Git-aware, but not an always-on GitOps controller
- local-workspace-first Git integration over remote-reconciliation-first Git integration
- host-native features should be used where they genuinely improve operations

## Source Of Truth

Primary source of truth:

- `/opt/stacklab/stacks/<stack>/compose.yaml`
- `/opt/stacklab/stacks/<stack>/.env`
- `/opt/stacklab/config/<stack>/`
- `/opt/stacklab/data/<stack>/`

Application home:

- `/opt/stacklab/app/`

Secondary application state:

- local SQLite database for settings, schedules, audit entries, and cached metadata

## Design Constraints

- Docker Compose only
- Linux only
- `amd64` is the primary supported architecture
- `arm64` is also supported
- host-native deployment preferred over running Stacklab as a privileged management container
- filesystem remains user-readable and user-editable without Stacklab
- security must assume terminal features are high-risk even on LAN
- Docker objects outside Compose may be exposed selectively only when they directly support Compose operations or safe host maintenance

## Success Criteria

- operator can create, inspect, edit, deploy, and troubleshoot stacks without losing manual CLI compatibility
- operator can inspect and edit relevant config files under `/opt/stacklab/config` without needing a separate host editor for common workflows
- operator can understand what changed locally before committing or pushing Git changes
- stack state in UI matches actual Docker runtime state
- failed operations are explicit and recoverable
- UI developer can build screens against stable backend contracts
