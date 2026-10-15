# ADR 0008: Backup Integrity Evidence

## Status

Accepted, 2026-10-14. Amended by [ADR 0009](0009-backup-maintenance-safety.md)
on 2026-10-15 for unresolved checks, maintenance policy and unlock guarantees.
Supplements [ADR 0007](0007-backup-completion-and-recovery-scope.md).
Implementation follows the [evidence and recovery plan](../product/backup-evidence-and-recovery.md).
The [operational recovery contract](../product/backup-operational-recovery.md)
clarifies transport classification, migration and deliberate maintenance.

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
Recognized historical transport, cancellation, permission and lock failures remain
failed attempts; this also corrects an already migrated unknown-scope failure with
an unambiguous non-integrity code. Generic command failure is not sufficient to
infer new corruption. The restic diagnostic classifier must preserve explicit
corruption when transport failures occur in the same check.

Keep the aggregate integrity field for API compatibility, derived conservatively
from both evidence dimensions. Availability and individual operation outcomes
remain independent. Refuse automatic destructive retention on known integrity
failure without preventing a new capture/copy from completing.
Manual prune on known failure requires specific operator acknowledgement enforced
by the API. Stale-lock removal is an explicit reserved job using ordinary unlock;
it neither forces removal of active locks nor clears integrity evidence.

Persist bounded snapshot classifications keyed by full snapshot ID and classifier
version, with fair continuation across scans. Distinguish proven incomplete,
invalid, unsupported and legacy snapshots from temporarily unavailable evidence.
Never infer completeness from timestamp ordering or a successful metadata check.

A copy that uploaded and passed completeness verification remains successful when
subsequent retention fails. Record the maintenance error separately, keep the
recovery point visible and let retry repeat maintenance without duplicate upload.
Maintenance-only retry does not require the prepared upload data or its lifetime.

## Alternatives

Treating every failed check as corruption misrepresents temporary network failures.
Treating every diagnostic containing a network error as harmless can hide genuine
corruption reported by the same check. Explicit data failures therefore take
precedence; otherwise an inconclusive attempt leaves prior dated evidence intact.
The command diagnostic vocabulary is version-dependent and requires native tests
for the supported restic range.

A sampled check after repair cannot prove that all previously damaged data was
read. Retain the full-check requirement despite its cost. Automatic unlock or a
generic sequence of repair commands would exceed the evidence available to the
application; keep explicit unlock and diagnosis-specific external repair instead.

## Consequences

State changes are additive and conservative for existing installations. The UI can
explain usable copies, repository integrity and maintenance outcomes independently.
Cached negative classification reduces repeated remote reads; versioning allows a
future classifier to reconsider unsupported formats. Cache bounds require a durable
continuation cursor so large inventories do not starve older snapshots.
The 4096-entry classification bound is global. Stopped retention allows repository
growth while new copies continue, so unresolved integrity failure needs operator
attention. A full data check can download the entire repository and incur provider
charges. These costs are preferable to silently clearing evidence or deleting data
during unresolved damage; the operations guide must explain the recovery path.
