# Backup Diagnostics And Unlock Corrections

## Status And Scope

Implemented and validated, 2026-10-21, following the sixth independent review.
The [validation record](../quality/backup-validation.md#round-six-diagnostics-and-unlock-validation--2026-10-21)
identifies native, installed and synthetic evidence and their limits.
This extends [repository safety](backup-repository-safety.md) and
[ADR 0009](../adr/0009-backup-maintenance-safety.md). Existing scoped evidence,
current retention policy and evidence-bound manual prune remain authoritative.

## Implementation Contract

1. Preserve restic diagnostic records and their context, including trailing newlines
   in JSON messages. Distinguish failed inventory/index reads, pack reads and lock
   refresh warnings. Confirmed structural damage wins over incidental transport
   failures. Missing-reference diagnostics depend on usable inventory; unrelated
   lock/download failures do not invalidate that inventory. Operational errors
   explain only their associated diagnostics. An unexplained repository diagnostic
   creates a hold even when another operation reported a transport failure. Generic
   summaries alone do not turn an explained transport failure into corruption/hold.
2. Ordinary unlock requires complete local ownership metadata, an absent PID and a
   lock at least 30 minutes old. Refuse future timestamps, uncertain probes and all
   live owners, including EPERM, regardless of age. Fresh locks may belong to live
   processes in another PID namespace despite matching hostnames. Age is a minimum
   safeguard, not proof of safety. Re-list lock identities immediately before unlock
   and refuse a changed set. External writers must remain stopped throughout; this
   cannot close the final race with independent clients. Use a distinct refusal
   diagnostic and retain ordinary, unprivileged restic execution.
3. Document the shared-host PID namespace requirement and an operator procedure for
   foreign-host or unreadable locks. No force-unlock UI or privileged restic helper.
   Manual repair instructions must be diagnosis-specific and version-specific for
   supported restic releases. Require an independent repository copy, execution as
   the service identity with explicit credentials, and coordinated downtime. Explain
   irreversible snapshot repair, pack repair's index update, and incomplete recovery
   payloads. Do not prescribe blanket ownership changes on shared repositories.
4. Show holds as a requirement for the appropriate check, separately from historical
   proof and restoration availability. Avoid repeated alerts and an incorrect request
   to create another copy. A successful unlock supersedes an older lock failure for
   suggested next actions while preserving that failure in history. Refresh destructive
   review on opening, guard loading/errors/missing review tokens, refresh more often
   while open, and explain when changed evidence resets typed confirmation. Keep
   evidence timestamps in the review token. Check dialogs use current evidence and
   never reduce the configured scope below the required scope. Explain deleted-plan
   maintenance as no longer applicable without rewriting its historical failure.
5. Correct validation wording: native corruption is not native unresolved-diagnostic
   evidence; restoring original bytes is not a restic repair. Expired orphaned run
   history is bounded by the existing 200-record limit, not pinned forever.

## Seventh-Review Extension — 2026-10-27

Approved for implementation. Preserve the contracts above while closing the following
diagnostic, liveness and interaction gaps:

1. Interpret terminal `List(type) failed: cause` as an operation failure and associate
   the repeated cause with that operation, independently of provider wording. Retain
   specific credential/permission failures where known. Failed data/index inventory
   only invalidates the corresponding missing-reference evidence. Separate repair
   advice from object diagnostics. Known corruption always wins; object decode errors,
   unknown errors mixed with transport and filenames containing transport words must
   not silently become availability failures. Cover complete modern repair-advice
   sequences as well as minimum-version plain output.
2. Fold diagnostics as they arrive with bounded memory. Preserve corruption evidence
   anywhere in the stream, including beyond the retained diagnostic-output limit.
   Oversized or unrepresentable diagnostics conservatively prevent a successful check
   or an exclusively operational interpretation. Diagnostic text never reaches public
   logs/API responses. Test fixtures must observe the production runner, including its
   SIGINT/graceful cleanup and bounded SIGKILL fallback.
3. Stop a check after a terminal inventory failure or exhausted backend access, instead
   of retrying every remaining pack for hours. Use a bounded sustained-failure budget
   that successful progress can reset; a healthy large check must not inherit a short
   total-duration limit. Preserve any corruption/unknown object evidence already seen
   when stopping. User cancellation remains distinct from automatic backend failure.
4. Refresh review on opening and after a conflict/manual retry, tracking completion of
   the requested refresh rather than wall-clock comparisons. Background polling must
   preserve input, focus and layout. Changed evidence still invalidates confirmation;
   failed freshness checks block submission. Initial focus must work during loading.
   Explain scope changes and keep backend tool availability consistent across views.
5. Complete operator examples for S3/rclone using private credential files without
   secrets in argv/history, explicit region/CA/cache and the service identity. Prefer
   existing credentials on an intact host. Document lost-host fencing, ordinary unlock
   before force removal, PID namespaces and synchronized clocks. Distinguish a guard
   refusal from an unlock that ran but left locks behind.

Validation includes a real S3 listing fault, partial-download/corruption mixtures,
bounded cancellation and recovery through the production runner, plus browser typing
across delayed polling and evidence changes. Synthetic and native evidence must be
identified separately. Check installed package identity before and after exercises;
preserve the test host's package hold during validation. Record prior CI runs
39569375858 and 39629002747 as successful. Temporary local screenshots are not retained
release artifacts. Commit this extension before implementation, then commit focused
changes, push and verify final-revision CI.

## Verification And Delivery

Commit this contract before implementation. Add regressions for JSON record boundaries,
mixed operational/unexplained diagnostics, lock-refresh isolation, minimum lock age,
PID namespaces and changing lock inventories. Exercise SFTP inventory interruption
at a deterministic listing boundary, independently of the existing pack-read test.
Use minimum restic 0.14 and installed/local versions where available; record limits.

Exercise an actual supported restic index repair, a full check, and Stacklab recovery
on the disposable Debian host. Verify the installed package/API/browser, including
hold and orphaned-plan UI provenance. Run backend, race, frontend, contract and hygiene
checks. Record exact source/package revisions and CI results in the
[validation record](../quality/backup-validation.md). Commit focused changes, push,
and verify required CI jobs on the final revision. Keep private review inputs and
credentials outside commits.

## Retained Limits

No automatic repair, forced unlock, broad frontend rewrite or new global job outcome.
No blanket hold for every transport failure with a damage summary. No assumption that
lock age or a matching hostname proves a remote process is dead. No claim that one
backend or restic version covers all possible diagnostic formats.
