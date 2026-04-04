# Runtime Integration Plan

This document turns ADR 0003 into an implementation plan.

## Goal

Keep Compose lifecycle operations on `docker compose` CLI while migrating runtime-heavy behavior toward Docker Engine API based adapters.

## Why This Split

### Compose lifecycle is a good CLI fit

Operations such as:

- `up`
- `down`
- `stop`
- `restart`
- `pull`
- `build`
- `recreate`
- `config`

map directly to the user's own mental model and host CLI usage.

Keeping them on `docker compose`:

- preserves CLI compatibility
- avoids reimplementing Compose semantics
- reduces the chance of behavioral drift versus manual operations

### Runtime streaming is not a good repeated-CLI fit

Operations such as:

- stats
- logs
- exec / terminal
- container list and inspect refresh loops

pay a higher cost when implemented via repeated shelling to CLI commands:

- more process spawning
- more parsing of command output
- less structured error handling
- weaker fit for long-lived streams and reconnect semantics

## Target Backend Shape

The backend should evolve toward explicit providers:

- `ComposeLifecycleProvider`
- `RuntimeProvider`
- `HostObservabilityProvider`
- `GitWorkspaceProvider`

The important split here is:

- `ComposeLifecycleProvider` owns Compose lifecycle and validation
- `RuntimeProvider` owns container runtime data and streams

## Recommended Stages

## Stage 1: Formalize Provider Boundaries

Goal:

- make the current architecture intentionally hybrid, even before all implementations change

Work:

- introduce provider-oriented interfaces
- keep the current implementation under those interfaces
- avoid further spreading direct CLI calls through handlers

Outcome:

- future migrations become local refactors, not handler rewrites

## Stage 2: Migrate Stats First

Goal:

- remove the highest-frequency runtime polling overhead first

Why first:

- current `docker stats --no-stream` loop is the clearest runtime inefficiency
- stats are already isolated in the WebSocket stream model

Work:

- implement stats retrieval via Docker Engine API
- keep response shape unchanged
- preserve polling cadence initially if needed, even after implementation changes

Success condition:

- no API contract change
- lower runtime overhead than repeated CLI spawns

## Stage 3: Migrate Logs

Goal:

- replace CLI log fetching with runtime-native log streaming

Work:

- use Docker Engine API log streams
- preserve existing multiplexed WS contract
- keep current service filtering semantics

Success condition:

- no visible frontend contract change
- cleaner and more reliable append/follow behavior

## Stage 4: Migrate Terminal / Exec

Goal:

- move container exec and terminal transport away from host-shell wrapping of `docker exec`

Work:

- implement exec/session attachment on top of Docker Engine API
- preserve session semantics:
  - open
  - attach
  - close
  - reconnect behavior

Success condition:

- existing UI terminal behavior remains intact
- backend session management becomes simpler and less shell-dependent

## Stage 5: Migrate Runtime Inspect / Listing

Goal:

- consolidate runtime state gathering under the runtime adapter

Work:

- move container list and inspect operations toward API-backed runtime reads
- keep filesystem-first stack discovery unchanged
- keep runtime state mapping semantics unchanged

Success condition:

- stack discovery still starts from filesystem
- runtime augmentation no longer depends on scattered CLI parsing

## Explicit Non-Goals Of This Migration

- replacing `docker compose` lifecycle with a full Compose SDK rewrite
- changing Stacklab into a Docker-first product
- changing the WebSocket contract shape for frontend consumers
- introducing multi-host support as part of runtime refactor

## Testing Expectations

Each migration stage should preserve:

- existing REST contracts
- existing WebSocket contracts
- Docker-backed integration tests
- browser E2E flows where relevant

Additional tests should be added per stage for the new provider implementation.

## Recommended Immediate Next Step

Do not start the runtime migration immediately.

Priority should remain on the product roadmap:

1. host observability
2. config workspace
3. local Git workspace visibility

After those milestones, Stage 1 and Stage 2 of this plan become the most sensible runtime refactor work.
