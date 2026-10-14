# ADR 0008: Backup Integrity Evidence

## Status

Accepted, 2026-10-14. Supplements [ADR 0007](0007-backup-completion-and-recovery-scope.md).
Implementation follows the [evidence and recovery plan](../product/backup-evidence-and-recovery.md).

## Context

A metadata-only check can succeed after a full data check detects corruption.
Representing both through one mutable status erases known data damage. A verified
copy can also be followed by failed retention; combining their outcomes makes a
usable recovery point appear to be a failed upload and encourages duplicate work.

## Decision

Persist metadata integrity, data integrity and the latest check attempt separately.
Record scope and time with the evidence. A successful metadata check updates only
metadata evidence. A successful sampled read is useful evidence about that sample,
but cannot clear a known data failure. A successful full-data check can clear it.
Generic repository unavailability and cancellation update the attempt without
claiming newly proven corruption. Migrate historical ambiguous failures as unknown
scope failures that require a full successful check to clear.

Keep the aggregate integrity field for API compatibility, derived conservatively
from both evidence dimensions. Availability and individual operation outcomes
remain independent. Refuse automatic destructive retention on known integrity
failure without preventing a new capture/copy from completing.

Persist bounded snapshot classifications keyed by full snapshot ID and classifier
version, with fair continuation across scans. Distinguish proven incomplete,
invalid, unsupported and legacy snapshots from temporarily unavailable evidence.
Never infer completeness from timestamp ordering or a successful metadata check.

A copy that uploaded and passed completeness verification remains successful when
subsequent retention fails. Record the maintenance error separately, keep the
recovery point visible and let retry repeat maintenance without duplicate upload.

## Consequences

State changes are additive and conservative for existing installations. The UI can
explain usable copies, repository integrity and maintenance outcomes independently.
Cached negative classification reduces repeated remote reads; versioning allows a
future classifier to reconsider unsupported formats. Cache bounds require a durable
continuation cursor so large inventories do not starve older snapshots.
