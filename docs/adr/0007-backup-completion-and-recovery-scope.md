# ADR 0007: Backup Completion And Recovery Scope

## Status

Accepted, 2026-10-14. Supplements [ADR 0006](0006-backup-reliability-boundaries.md).
Implementation follows the [completion plan](../product/backup-reliability-completion.md).

## Context

Second-round review demonstrated that successful individual operations do not
establish end-to-end safety: replacement deleted excluded live files, retention
could discard a verified retry in favor of a partial snapshot, and an unresolved
writer journal stopped both future captures and overdue notifications.

## Decision

Exact replacement requires a complete captured scope. Reject filtered-source
replacement and managed-root targets at both application and helper boundaries.
Do not silently change database recovery to merge. Keep rollback data until all
replacement steps, including root metadata, succeed.

Treat verified completeness as a prerequisite for destructive retention, separate
from snapshot time and run tags. Transient inability to verify is not evidence of
incompleteness. Legacy snapshots without sufficient evidence remain conservative
retention exclusions until their status can be established.

Separate execution permission, observation of protection and operator notification.
System-imposed pauses remain observable. A reviewed resolution may acknowledge
an original container that Docker confirms no longer exists; it must not start a
replacement container or claim that a damaged restore has been repaired.

Schedule independent work through bounded supervised execution. Maintain resource
identity and shutdown guarantees, release a finished copy independently, and
retain scheduled deadlines even when a job spends time waiting.

## Alternatives And Consequences

- Preserving arbitrary GNU tar excludes during exact replacement needs identical
  pattern semantics, including nested directories and links. Rejecting the unsafe
  operation is preferable to an approximate matcher. Explicit merge remains useful
  for suitable file data and does not promise exact database recovery.
- Timestamp offsets do not establish completeness and can distort freshness.
- Automatically trusting legacy host identity could enable capture and retention
  from an unintended restored clone. A one-time visible review is retained.
- Unbounded goroutines or shortening healthy transfer timeouts do not provide
  isolation with predictable shutdown. Bounded supervision is required.
- The archive representation remains unchanged during these fixes. A later
  comparison should benchmark unchanged/changed 50 GiB sources, downtime, bytes
  read/written and staging peak against direct restic to one repository followed
  by copy, and a files/stop hybrid. Revisit before claiming support for workloads
  whose capture time or staging exceeds configured operating budgets.
- Full-state secret escrow remains intentional. Operators can use a separate
  state-only plan for trusted destinations; stripping selected credentials needs
  a separate recovery contract. Setup must explain the current scope explicitly.
