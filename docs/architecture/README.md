# Architecture Docs

This section contains current system-level architecture, deployment boundaries,
runtime flows, and security decisions.

Start with:

- [System Overview](system-overview.md) — components, ownership, lifecycle, and
  primary data flows.
- [Filesystem Layout](filesystem-layout.md) — portable `<STACKLAB_ROOT>` paths
  and the Debian/APT and tarball deployment profiles.
- [Security Model](security-model.md) — the single local operator, sessions,
  trust boundaries, terminal access, and privileged host actions.
- [Filesystem Metadata and Atomic Writes](filesystem-metadata-policy.md) —
  preservation and durability rules for managed file replacement.

[Runtime Integration Plan](runtime-integration-plan.md) records the staged
Docker integration direction. Architectural decisions and supersession notes
live in the [ADR index](../adr/README.md).
