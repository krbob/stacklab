# Domain Documentation

These documents define the concepts and operational rules shared by the API,
backend, and UI.

## Current documents

- [Stack model](stack-model.md) — canonical identifiers, stack/service/container
  entities, discovery, state derivation, and filesystem/runtime drift.
- [Operation model](operation-model.md) — read, streaming, and mutating
  operations; jobs; audit behavior; locking; cancellation; and recovery.

Use the [OpenAPI and WebSocket contracts](../api/README.md) for wire formats,
the [data documentation](../data/README.md) for persistence ownership, and the
[architecture documentation](../architecture/README.md) for system and trust
boundaries.
