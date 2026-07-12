# Stacklab Documentation

This directory contains the current product, engineering, and operations
documentation for Stacklab. Start with the audience map below; use the
narrowest current contract when documents overlap.

## Current sources of truth

1. Executable code and tests describe implemented behavior.
2. [OpenAPI](api/openapi.yaml) is the canonical REST contract, and the
   [WebSocket protocol](api/websocket-protocol.md) is the canonical streaming
   contract.
3. Database migrations are authoritative for the SQLite schema; the
   [data index](data/README.md) identifies their locations and companion
   references.
4. Current product, domain, architecture, UI, operations, and quality
   references explain intent, constraints, and supported workflows.

Treat a disagreement between implementation and a published contract as drift
to fix in the same change. Do not resolve it by copying another route list,
schema, or state model into a new document.

## For operators

- [Install from APT](ops/install-from-apt.md) — primary Debian and Ubuntu path.
- [First run](ops/first-run.md) — access, password bootstrap, readiness, and
  initial verification.
- [Install from a tarball](ops/install-from-tarball.md) — supported secondary
  path for other Linux distributions.
- [Systemd and reverse proxying](ops/systemd.md) — service and HTTPS deployment.
- [Upgrade validation](ops/upgrade-validation-checklist.md) — release and
  rollback checks.

The complete operator map is in [Operations](ops/README.md).

## For contributors

- [Contributing](../CONTRIBUTING.md) and
  [local development](ops/local-dev.md) — setup and change workflow.
- [Developer checks](quality/developer-checks.md) — canonical local quality
  baseline.
- [System architecture](architecture/README.md) — components, security, and
  filesystem boundaries.
- [API contracts](api/README.md) — REST, WebSocket, and generated frontend
  types.
- [Data model](data/README.md) — schema and retention ownership.
- [Domain model](domain/README.md) — stack identity, state, jobs, and locking.

## For product and UI work

- [Product documentation](product/README.md) — scope, non-goals, and the
  implemented baseline.
- [Product roadmap](roadmap.md) — current direction and prioritized follow-up.
- [Information architecture](ui/information-architecture.md) — navigation,
  routes, and responsive policy.
- [Screen states](ui/states-and-empty-cases.md) and
  [dynamic accessibility](ui/accessibility-dynamic-status.md) — current UI
  behavior contracts.

## Reference sections

- [Architectural decisions](adr/README.md)
- [Architecture](architecture/README.md)
- [API](api/README.md)
- [Data](data/README.md)
- [Domain](domain/README.md)
- [Operations](ops/README.md)
- [Product](product/README.md)
- [Quality](quality/README.md)
- [UI](ui/README.md)

## Historical material

Dated reviews, execution reports, design handoffs, proposals, and completed
plans are historical evidence, not current contracts. Their own status notes
must identify that role. When historical material conflicts with the sources
above, follow the current source of truth. Retired documents remain available
through Git history rather than being kept in the primary navigation.
