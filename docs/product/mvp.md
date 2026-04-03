# MVP

## MVP Goal

Deliver a stable first version that covers the full daily loop of managing Compose stacks on one host without trying to replace all Docker tooling.

## Included In MVP

### Stack Discovery And Overview

- scan stacks from `/opt/homelab/stacks`
- detect runtime state from Docker and Compose labels
- show stack list with state, health summary, service count, and last action

### Stack Details

- show services, containers, ports, image or build mode, mounts, and health state
- show resolved runtime state even when filesystem and Docker drift temporarily

### Compose Editing

- edit `compose.yaml`
- edit stack `.env`
- validate through `docker compose config`
- show resolved config output before deploy

### Operational Actions

- `up`
- `down`
- `stop`
- `restart`
- `pull`
- `build`
- `recreate` through compose lifecycle commands

### Diagnostics

- live logs
- container shell
- container CPU, memory, and network stats
- aggregated stack stats

### Stack Creation And Removal

- create a new stack with standard directory layout
- remove runtime safely
- remove stack definition conservatively
- never delete stack data by default

### Security And Control

- single-user authentication
- session management
- audit log for mutating actions
- per-stack operation locking

## Deferred Beyond MVP

- multi-host support
- remote agents
- host shell enabled by default
- long-term metrics storage
- Docker object management outside Compose
- advanced RBAC
- public internet deployment model

## MVP Exit Conditions

- read-only views are accurate
- mutating actions are serialized and auditable
- compose validation blocks invalid deploys
- UI contracts are stable enough for parallel frontend work

