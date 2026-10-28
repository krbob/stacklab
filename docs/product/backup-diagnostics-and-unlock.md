# Backup Diagnostics And Unlock Corrections

## Status And Scope

Implemented and validated through the eighth-review corrective scope, 2026-10-28.
The [validation record](../quality/backup-validation.md#round-eight-operation-lifecycle-and-compatibility--2026-10-28)
identifies native, installed and synthetic evidence and their limits.
This extends [repository safety](backup-repository-safety.md) and
[ADR 0009](../adr/0009-backup-maintenance-safety.md). Existing scoped evidence,
current retention policy and evidence-bound manual prune remain authoritative.

## Implementation Contract

1. Preserve restic diagnostic records and their context, including trailing newlines
   in JSON messages. Distinguish failed inventory/index reads, pack reads and lock
   refresh warnings. Confirmed structural damage wins over incidental transport
   failures. Missing-reference diagnostics depend on usable inventory; unrelated
   lock/download failures do not invalidate that inventory. An operational record
   explains itself and generic summaries; it never explains an independent object
   diagnostic. An unexplained repository diagnostic creates a hold even when
   another operation reported a transport failure. Generic
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

Implemented and validated. The contracts above remain in force with the following
diagnostic, liveness and interaction corrections:

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
   The ten-minute budget is checked on continuing failure reports, with a new window
   after six minutes without failures for that operation: restic 0.14–0.15 has no successful-retry record, so
   silence cannot safely trigger cancellation. Completely silent hangs remain bounded
   by backend request timeouts and the existing overall job limit.
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

## Eighth-Review Extension — 2026-10-28

Keep the supported minimum at restic 0.14. Treat diagnostics as an operation
lifecycle, rather than a collection of independent error phrases:

1. Share parsing of backend retry, recovery and terminal records between evidence
   classification and the failure watchdog. Track bounded, operation-specific
   pending causes. Restic 0.14–0.15 has no retry-success record; 0.16 adds that
   record; 0.17 adds terminal backend failure records. A repeated standalone cause
   can establish legacy inventory failure. Missing references after an unresolved
   inventory retry cannot prove corruption; absent a terminal cause they remain
   inconclusive. Successful retries restore inventory confidence. Unrelated pack
   reads never invalidate data listing. Associate terminal Load/Stat failures and
   legacy `pack … failed to download` with their causes, preserving specific access
   failures and independent unknown object diagnostics.
2. Retain explicit corruption even inside retry records or after cancellation.
   Once cancellation is observed, subsequent shutdown diagnostics cannot introduce
   unknown-object holds or missing-reference evidence. Preserve evidence collected
   before cancellation and distinguish manual cancellation from an automatic stop.
   Ignore only explicitly recognized cache housekeeping warnings, never all unknown
   errors from a process that exited successfully.
3. Expire inactive retry entries per operation, including on versions without
   success records. Use a six-minute quiet window per key, longer than modern
   restic's maximum normal backoff, before starting a new ten-minute failure
   budget. Evaluate only on failure reports; do not put a deadline on silent healthy
   reads. Keep bounded state and conservatively handle overflow without allowing
   obsolete entries to create a deadline for unrelated recovered operations.
4. Keep the prune dialog mounted and focus inside it when acknowledgement becomes
   required or ceases to be required. Explain those transitions separately from a
   changed evidence token, clear reset guidance when typing resumes, and expose
   initial refresh/read-only state accessibly without shifting the dialog.
5. Preserve cancellation and access diagnoses during unlock preflight; distinguish
   completed removal from remaining locks in events and next-step guidance. Clarify
   ordinary CLI unlock's PID namespace risks and incomplete-operation outcomes.
   Correct the installed harness's HTTP error reporting and verify package identity
   both before and after exercises, including package holds after installation.

Commit this extension before implementation. Verify legacy and modern diagnostic
formats, cancellation races, mixed real damage/transport failures, recovered retries
and focus transitions. Keep fast watchdog tests, but separately exercise actual
restic retry exhaustion with the production budget. Retain sanitized native
transcripts with version and scenario provenance. Run the installed Debian fixture,
backend/race/frontend/browser/contract gates, then commit, push and verify CI.

This extension does not change global configuration locking, introduce automatic
repair, relax unknown-diagnostic holds, or raise the supported tool minimum.

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
