# Backup Repository Safety

## Status And Scope

Implemented and validated, 2026-10-15, following the fifth independent review.
The [validation record](../quality/backup-validation.md#round-five-repository-safety-validation--2026-10-15)
identifies tested revisions, installed recovery exercises and retained limits.
This contract extends
[operational recovery](backup-operational-recovery.md) and is governed by
[ADR 0009](../adr/0009-backup-maintenance-safety.md). Preserve historical evidence while making
current destructive-maintenance decisions explicit and conservative.

## Implementation Contract

1. Separate confirmed corruption, confirmed operational failure and unresolved
   repository diagnostics. Explicit structural or cryptographic damage takes
   precedence over incidental transport warnings. A bare EOF, retry warning or
   generic damaged-repository summary is not sufficient evidence on its own.
   Preserve confirmed corruption even if a later timeout interrupts the command.
2. Persist unresolved check diagnostics as `check_hold`, including scope, attempt
   time and error. Keep prior successful metadata/data evidence. A hold makes
   current aggregate integrity unverified and blocks automatic destructive work;
   known corruption remains failed. A successful full check clears any hold;
   metadata or sampled success can clear only a metadata hold. Unknown or sampled
   inconclusive scope requires a full check. Transport, cancellation, permission
   and lock errors do not create a new hold or clear an existing one.
   Migrate only the recognized historical inconclusive-check message when no
   relevant newer successful check supersedes it. Backfill a missing legacy failed
   attempt without inventing a timestamp. Do not guess from arbitrary command errors.
3. Retry retention against the current enabled plan and destination assignment,
   both at admission and immediately before execution under repository reservation.
   Keep immutable capture/transfer selection in the original run snapshot. Reject
   deleted/disabled plans or assignments. Current keep-all retention completes
   without listing or deleting snapshots, even if integrity requires attention;
   it does not clear that integrity state. Destructive retention requires usable
   current evidence. An explicit healthy destination subset remains retryable.
   Maintenance-only retry remains independent of staging and its lifetime.
4. Keep copy and maintenance chronology separate, including persisted observations.
   Finishing retention for an older run must not hide a newer upload failure or
   change the latest copy's run identity. Reconstruct legacy observations from
   retained runs where possible; preserve unresolved orphaned errors conservatively.
5. Guard explicit unlock before invoking ordinary restic unlock. Inspect lock
   metadata and conservatively refuse live local owners, including other UIDs,
   using a non-signalling process probe. Treat permission-denied as an existing
   process; refuse malformed or uncertain ownership. Protect live owners even if
   the lock timestamp is old. Do not add privileged restic execution, forced unlock
   or automatic repair. Report remaining locks after the attempt. This inspection
   is not atomic with external clients: require quiescent external writers and
   document remote-host/PID-namespace limits instead of promising a distributed lock.
6. Bind manual prune acknowledgement to the current unresolved evidence. Expose a
   derived `maintenance_review_token`; require both acknowledgement and the matching
   `acknowledgement_review_token` at admission and execution. Missing/stale review
   returns HTTP 409 `maintenance_acknowledgement_required`. Recompute from unresolved
   metadata/data failures and hold, not unrelated successful operations. Scheduled
   prune remains blocked. Refresh an open UI review from current repository state
   and reset typed confirmation when that evidence changes.
7. Explain historical proof versus current hold in the UI. Offer contextual unlock
   for lock failures and refresh recovery inventory afterward. Disable destructive
   retention retry when blocked, with the appropriate metadata/full-check action;
   preserve healthy siblings and keep-all cleanup. Label an earlier maintenance
   failure as previous while retry is pending/running. Keep the existing component
   architecture except focused extraction needed for review correctness.
8. Align OpenAPI, generated client, API narrative and operations guidance. Manual
   repair requires stopping Stacklab and coordinating other repository clients;
   pausing schedules alone is insufficient. Describe version-specific repair
   commands without raising the minimum restic version unnecessarily. Make browser
   harness conditional cases explicitly passed or skipped.

## Verification And Delivery

Commit this contract before implementation. Use focused implementation commits and
meaningful regressions. Exercise native missing-index and truncated-pack diagnostics,
real SFTP transport failure versus truncation, and a native S3 corruption case when
available. Preserve provenance for actual diagnostic fixtures and distinguish them
from synthetic cases. Exercise supported restic versions where available.

Cover hold persistence/migration and scope-sensitive clearing; changing retention
policy, deleted/disabled plan/assignment and keep-all; old retry after newer upload
failure; foreign-UID and aged active locks; missing/stale prune review at both
boundaries; UI confirmation invalidation and recovery refresh. Keep original proof
and copy outcomes intact throughout these cases.

Run backend, frontend, generated-contract and repository checks. Validate a committed
package on the disposable Debian host, including native, installed API and maintained
browser evidence. Record exact source/package revisions and unexecuted cases in the
[validation record](../quality/backup-validation.md). Push and verify all required CI
jobs on the exact final revision. Keep review input, credentials and private recovery
archives outside commits.

## Retained Limits

No automatic repair, force-unlock, broad root restic helper, event-store redesign or
large UI rewrite. Do not add a new job outcome solely for retention failure. Keep
copy completeness separate from current repository fitness. No blanket restriction
to retrying only the newest run. Native evidence for one backend/version does not
prove every storage provider or diagnostic variant.
