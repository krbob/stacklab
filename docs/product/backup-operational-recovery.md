# Backup Operational Recovery

## Status And Scope

Implemented and validated, 2026-10-14, following the fourth independent review.
The [validation record](../quality/backup-validation.md#round-four-operational-recovery-validation--2026-10-14)
separates deterministic, native, installed API and browser evidence and their limits.
This contract extends [evidence and recovery](backup-evidence-and-recovery.md)
and [ADR 0008](../adr/0008-backup-integrity-evidence.md). The priority is recovery
from ordinary remote-storage interruptions while preserving accurate historical
evidence and deliberate destructive maintenance.

## Implementation Contract

1. Classify explicit data corruption independently from failed transport. Generic
   restic summaries alone do not establish corruption. Explicit integrity failures
   retain precedence when diagnostic output also contains transport errors.
   Inconclusive checks update the latest attempt without manufacturing or clearing
   integrity evidence. Migrate recognized historical non-integrity error codes as
   attempts, including states already converted to failed/unknown; preserve truly
   ambiguous historical failures conservatively.
2. Provide an explicit repository job to remove stale locks using ordinary
   `restic unlock`, never `--remove-all`. Reserve the repository through the existing
   job resource boundary, record the outcome and explain that a live owner's lock
   may remain. Do not automatically unlock or mark integrity verified afterward.
3. Refuse scheduled prune on known integrity failure. Manual prune requires a
   specific acknowledgement of that failure in the API as well as the UI. Explain
   the risk before submission. A check dialog opened after known data failure
   defaults to a full read, explains the transfer cost and preserves the operator's
   ability to run a lighter diagnostic without clearing the previous failure.
4. Retry failed retention independently from upload and verification. Preserve the
   successful copy and its proof, update only maintenance outcome/error and emit
   useful progress. Maintenance-only retry does not depend on staging or its TTL;
   failed transfers still require their original prepared set. Release staging when
   no remaining transfer needs it. Keep repository admission and policy safeguards.
5. Expose the first outstanding deadline independently from evidence usability.
   Protection must show overdue alongside missing, unavailable or unverified data;
   use the same schedule semantics as notification and heartbeat evaluation.
6. Make polling silent background work, preserve explicit retry feedback, coalesce
   focus refreshes and avoid listing the same snapshot inventory repeatedly for one
   completed change. Keep timely updates for an individual destination when another
   destination remains busy. Explain blocker codes and display each error once.
7. Persist snapshot provenance with a restored result and show it again during
   apply review. Validate selected sources for replacement; do not reject an intact
   source solely because another source or an index is missing from the snapshot.
   Keep isolated retrieval available. Explain hardlink selection limits and reject
   unsupported selected entries before scheduling the restore job when verifiable.
8. Align the narrative API, OpenAPI and generated client. Document repair and stale
   lock recovery, actual restic-version assumptions, schedule activation and the
   global classification-cache bound. Keep the browser smoke harness in the repo,
   with configurable URL/session and no embedded credentials. Separate native,
   unit, installed API and browser evidence in the validation record.

## Verification And Delivery

Commit this contract before implementation. Deliver focused commits with tests.
Exercise genuine corruption, transport interruption and mixed diagnostics; both
legacy migration forms; stale versus active locks; prune acknowledgement; failed
maintenance retry with unavailable transport and expired/removed staging; overdue
with unusable evidence; restore provenance and source validation; UI background
refresh and inventory invalidation. Preserve the 100-file selection boundary test
with a realistic local timeout rather than increasing the global test timeout.

Use the disposable Debian host for native SFTP interruption, unlock and return to
service plus installed-package/API/browser smoke. Record exact revisions and any
unexecuted cases. Run backend, frontend, generated-contract and repository checks,
push the resulting commits and verify CI on the exact pushed revision. Keep private
credentials and recovery archives out of commits and public test artifacts.

## Retained Decisions

- Repository kind and address remain immutable; changing them requires a new
  destination. Physical replacement at the same address is a separate concern.
- Only a full successful data check clears known data failure. Sampled success
  does not establish that previously damaged data has been read.
- No automatic unlock, generic repair pipeline or deletion of partial/unknown
  snapshots. Repository repair remains a documented, diagnosis-specific operation.
- A malformed restic inventory is not evidence that one snapshot is invalid;
  destructive retention remains blocked when its input cannot be interpreted.
- Keep existing helper privilege boundaries, long verification budgets and
  whole-plan repository admission. Do not add a check event store, redesign the
  classification cache or rewrite the UI architecture in this correction round.
