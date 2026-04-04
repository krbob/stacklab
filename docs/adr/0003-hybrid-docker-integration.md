# ADR 0003: Hybrid Docker Integration

## Status

Accepted

## Context

Stacklab is intentionally:

- Compose-first
- filesystem-first
- host-native

That creates two different categories of Docker interaction:

1. lifecycle operations on Compose stacks
2. runtime inspection and streaming operations on containers

Today the product already shells out to `docker compose` and `docker` CLI commands for most of this work. That has been sufficient to validate the MVP, but it also exposes a technical boundary:

- lifecycle operations map naturally to `docker compose`
- runtime polling and streaming through repeated CLI process execution is less efficient and less structured

Comparative analysis against Dockge, Dockhand, and Arcane reinforces this split:

- Compose lifecycle is still commonly driven through CLI semantics
- runtime-heavy features benefit from Docker Engine API or SDK access

## Decision

Stacklab will adopt a hybrid Docker integration model:

- `docker compose` CLI remains the primary lifecycle interface
- Docker Engine API becomes the preferred interface for runtime-heavy features

Specifically:

- keep Compose CLI for `up`, `down`, `stop`, `restart`, `pull`, `build`, `recreate`, and `config`
- migrate `stats`, `logs`, `exec`, and eventually runtime inspect/listing toward Docker Engine API based implementations

## Rationale

- preserves compatibility with manual operator workflows
- keeps Compose semantics aligned with what users run on the host
- avoids reimplementing Compose behavior prematurely
- reduces process-spawn overhead for runtime streaming
- improves structured error handling for runtime-oriented features
- supports more efficient long-lived streams for stats, logs, and exec

## Consequences

### Positive

- lifecycle actions remain predictable and operator-familiar
- runtime features gain a cleaner long-term implementation path
- architecture can evolve incrementally instead of through a large rewrite
- future maintenance features can choose the appropriate adapter intentionally

### Negative

- the backend now has two Docker integration styles instead of one
- adapter boundaries must be defined clearly to avoid mixing concerns
- short-term codebase complexity increases while migration is in progress

## Follow-Up

- define adapter boundaries explicitly
- migrate runtime features in stages
- keep integration tests covering both CLI lifecycle and runtime streams
