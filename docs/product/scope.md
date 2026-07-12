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

One logical local operator managing one homelab machine. A person or a very
small trusted household may share the installation, but v1 still exposes one
password-protected application identity, `local`, displayed as
`Local Operator`. Concurrent sessions do not become separate users, and there
is no per-person attribution, account management, or role-based authorization.
The `stacklab` Unix service account is only the host process identity.

## Core Jobs To Be Done

- inspect all Compose stacks in one place
- understand stack and container runtime state quickly
- edit stack definitions safely
- edit supporting configuration under `<STACKLAB_ROOT>/config` without leaving the product
- run operational actions without dropping to a separate terminal for routine tasks
- run safe bulk update workflows that replace ad-hoc host scripts for stack maintenance
- inspect logs, stats, and container shell sessions when diagnosing problems
- update images or rebuild services with predictable behavior
- understand whether an issue is caused by the host, Docker, or the stack itself
- perform selected maintenance workflows without turning Stacklab into a generic Docker control plane
- manage a narrow set of Docker daemon settings and service operations through explicit, audited workflows
- keep local workspace changes reviewable and easy to commit back to Git
- selectively commit only the files relevant to one stack when needed, without forcing unrelated changes into the same commit

## Product Principles

- Compose-first, not generic-Docker-first
- filesystem-first, not database-owned stacks
- single-host-first, not fleet-first
- explicit operator control, not hidden reconciliation
- conservative destructive actions with clear scope
- Git-aware, but not an always-on GitOps controller
- local-workspace-first Git integration over remote-reconciliation-first Git integration
- host-native features should be used where they genuinely improve operations
- dedicated non-root service account by default, with explicit privileged repair flows only where truly needed
- privileged host actions must stay narrowly allowlisted and auditable rather than expanding into a general host shell

## Source Of Truth

Primary source of truth:

- `<STACKLAB_ROOT>/stacks/<stack>/compose.yaml`
- `<STACKLAB_ROOT>/stacks/<stack>/.env`
- `<STACKLAB_ROOT>/config/<stack>/`
- `<STACKLAB_ROOT>/data/<stack>/`

Supported production roots:

- Debian/APT package: `<STACKLAB_ROOT>` is `/srv/stacklab`
- manual tarball: `<STACKLAB_ROOT>` is `/opt/stacklab`

Application files are deployment artifacts, not source-of-truth workspace
content. The Debian package installs them under `/usr/lib/stacklab`; the
tarball profile uses `/opt/stacklab/app`. Both profiles keep private runtime
state under `/var/lib/stacklab` by default. See
[Filesystem Layout](../architecture/filesystem-layout.md).

Secondary application state:

- local SQLite database under `STACKLAB_DATA_DIR` for settings, schedules,
  audit entries, and cached metadata

## Design Constraints

- Docker Compose only
- Linux only
- one application-level operator identity; multi-user accounts and RBAC are out
  of scope
- `amd64` is the primary supported architecture
- `arm64` is also supported
- host-native deployment preferred over running Stacklab as a privileged management container
- filesystem remains user-readable and user-editable without Stacklab
- security must assume terminal features are high-risk even on LAN
- Docker objects outside Compose may be exposed selectively only when they directly support Compose operations or safe host maintenance
- Stacklab should not run as `root` by default merely to work around container-created file ownership problems
- Docker daemon configuration may be exposed selectively where it removes routine SSH needs for homelab operations, but only through explicit and constrained workflows

## Success Criteria

- operator can create, inspect, edit, deploy, and troubleshoot stacks without losing manual CLI compatibility
- operator can inspect and edit relevant config files under
  `<STACKLAB_ROOT>/config` without needing a separate host editor for common
  workflows
- operator can understand what changed locally before committing or pushing Git changes
- operator can stage a commit from selected files only, including a one-stack workflow when desired
- stack state in UI matches actual Docker runtime state
- failed operations are explicit and recoverable
- permission problems caused by container-created files are visible and diagnosable without requiring Stacklab itself to run as `root`
- UI developer can build screens against stable backend contracts
