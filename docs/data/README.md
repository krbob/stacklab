# Data documentation

Stacklab keeps stack definitions on the filesystem and stores operational
metadata in SQLite. Start with the schema reference, then use the retention
document for data lifecycle and audit policy.

## Index

- [SQLite schema](./sqlite-schema.md) — current tables, columns, indexes,
  migrations, and transactional boundaries.
- [Retention and audit](./retention-and-audit.md) — retention windows, pruning
  behavior, audit events, and privacy constraints.

## Sources of truth

- [`internal/store/migrations.go`](../../internal/store/migrations.go) is the
  authoritative SQLite schema and migration registry.
- [`internal/store/store.go`](../../internal/store/store.go) contains the
  persistence models, queries, and transaction boundaries.
- [`internal/retention/service.go`](../../internal/retention/service.go)
  defines the default retention windows.

Documentation must be updated in the same change as a migration or persistence
contract change. If prose differs from executable migrations, the migrations
take precedence.
