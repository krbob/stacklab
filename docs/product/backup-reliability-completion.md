# Backup Reliability Completion

## Status And Scope

Implemented and exercised on 2026-10-14, following the second independent review.
This plan supplements [the original reliability contract](backup-reliability.md)
and [ADR 0007](../adr/0007-backup-completion-and-recovery-scope.md). Existing
snapshots, archive representation and additive `backup.v1` state remain supported.
The [validation record](../quality/backup-validation.md) records commands, source
revision, environment, executed scenarios and remaining measurement limits. The
acceptance scenarios below remain regression requirements for subsequent releases.
Third-round findings add the [evidence and recovery correction gate](backup-evidence-and-recovery.md);
this historical completion record does not close those newly identified cases.

## Delivery Order And Acceptance

### 1. Destructive Recovery Scope

- A filtered source is not a complete image of a populated target. Refuse exact
  replacement for sources with excludes rather than silently switching to merge
  or deleting excluded data. Explicit merge and operator-controlled offline
  recovery remain available; a future exclusion-preserving exact replacement
  requires identical capture/restore pattern semantics and its own tests.
- Reject replacement of managed stacks/config/data roots and configured path
  boundaries in both preview and the privileged helper. Inspected local Docker
  volumes remain independently authorized targets. Review must identify the
  exact target and explain all-content removal, not merely removal of newer files.
- Bind source completeness and target boundaries to immutable prepared inputs.
  Revalidate at mutation time. Retain rollback until extraction and root metadata
  restoration both succeed; preserve stopped services and recovery records after
  an interrupted write. Do not claim a transaction across sources.
- Delete service-owned isolated recovery trees even when archived directory
  modes are read-only, without following symlinks or changing operator-owned
  targets. Avoid holding the service state mutex throughout a large deletion.
- Diagnose helper/sudoers compatibility before a recovery emergency. Preserve
  narrow privileged commands and provide actionable upgrade instructions.

### 2. Verified Retention And Protection Evidence

- Retention must never prefer an incomplete candidate over a verified copy,
  including equal-time snapshots after exit 3 and retry. Qualify snapshots by
  durable completeness evidence; do not use timestamp offsets as the guarantee.
  Preserve unclassified legacy snapshots conservatively. Candidate cleanup must
  be limited by instance, plan and run identity and must not erase the only copy.
- Distinguish complete, proven incomplete and temporarily unverifiable snapshots.
  A timeout or unavailable repository must not trigger duplicate upload merely
  because verification could not finish. Separate interactive metadata budgets
  from verification budgets; classify cancellation and timeout explicitly.
- Derive the last operation error from the latest applicable attempt, not map
  iteration order. Preserve protection severity while a replacement is running;
  expose activity independently. Store bounded recovery-test evidence per exact
  snapshot and scope, and distinguish check attempt from successful check time.

### 3. Actionable Recovery And Continuous Observation

- Publish effective scheduling pause and reasons independently of the user's
  pause preference. System pauses send deduplicated notifications and do not
  disable overdue observation; report recovery of protection as a transition.
- Keep first-host review for legacy state without a previously recorded identity.
  A current machine ID alone cannot prove an in-place upgrade. Do not bypass the
  safeguard by assuming that every existing database belongs to the current host.
- Reconcile journal intent with observed Docker state. Provide an explicit,
  audited resolution for confirmed missing/recreated original containers without
  starting replacements. Corrupt or ambiguous journals remain fail-closed.
  Retain global capture exclusion when shared-source provenance is insufficient
  to narrow it safely; make this limitation and resolution visible. Return an
  actionable conflict instead of a generic internal server error.

### 4. Independent Work And Storage Lifecycle

- Admit scheduled work through a bounded, supervised executor. A slow upload or
  repository check must not stop unrelated schedules, inventory or cleanup.
  Preserve capture serialization, repository identity reservations, shutdown
  recovery and self-update drain. Release each copy's repository reservation and
  job resource after its upload/verification/retention finishes.
- Measure overdue from the scheduled protection deadline, including time queued.
  Show queued/running context rather than moving the deadline to job start.
- Test the upload watchdog on slow transfers before changing its definition of
  progress; repeated status messages alone are not proof of useful progress.

### 5. UI, API And Operator Documentation

- Invalidate Recovery snapshots/results after relevant operations, manual refresh
  and navigation while preserving filters and valid selections. Never silently
  substitute another snapshot when the selected one disappears.
- Track recovery and destination-wizard jobs beyond dialog/tab changes. Invalidate
  apply review after edits; preserve error/focus stability during polling; isolate
  expensive coverage discovery from progress polling; tolerate unknown timezones.
- Expose inventory freshness, actual pause reasons, helper readiness, recovery
  resolution and distinct protection/activity. Align OpenAPI bounds and nullable
  responses with real API behavior and regenerate frontend types.
- Explain during setup that each capture makes a full unencrypted local staging
  copy. A Stacklab-state source contains every destination's credentials and
  service configuration and sends them to every destination selected for that
  plan. Recommend a separate state-recovery plan using consciously trusted stores.
- Document helper upgrade, exact restore restrictions, snapshot-scoped evidence,
  scheduler behavior and reproducible validation. Keep optional feature growth
  (partial exercises, exported recovery kits, bandwidth controls, configurable
  staging, direct-restic/hybrid capture and provider-specific options) separate.

## Validation And Release Gate

Each implementation commit includes meaningful regression tests. Required
sequences, rather than only individual successful API calls:

1. Excluded live data survives a refused replacement; explicit merge preserves
   neighbors; normal PostgreSQL replacement removes newer database content.
2. API and helper reject managed-root replacement. Prepared input tampering and
   target changes fail before writes. Interrupted mutation retains recoverable
   data and does not automatically start services.
3. Real restic exit 3, retry, retention, inventory and restore preserve the complete
   point. Transient verification failure creates no duplicate snapshot.
4. Missing/recreated writer, journal resolution, automatic pause alert and resolved
   notification; host review remains required for unrecognized legacy state.
5. Slow/unavailable repository alongside another scheduled plan, finished-copy
   browsing, maintenance, cancellation, shutdown and self-update coordination.
6. Failed run followed by success; missing/integrity evidence during active work;
   recovery exercises for two snapshots sharing a repository; read-only tree deletion.
7. Backup completion appears in Recovery without losing search/selection; wizard
   job completion, changed apply inputs, policy banners and timezone fallback.
8. Debian package upgrade with old and current sudoers, helper readiness and live
   Docker/PostgreSQL recovery; built browser desktop/mobile exercises.

CI must install real restic for native regressions, run relevant race tests and
exercise privileged helper paths on Linux. Synchronize cancellation tests on
process readiness. Keep reproducible scenario scripts and logs for destructive
VM tests; distinguish executed results from inspection and untested conditions.
Commit coherent steps, push the final tested revision and check its CI result.

## Retained Limits

- Filtered sources support explicit merge, not exact replacement. Interrupted
  replacement preserves rollback data and stopped writers; restoration of that
  rollback remains an operator procedure, not an automatic multi-source transaction.
- Prepared archives still receive repeated validation during apply. Large-dataset
  downtime optimization, parallel service stops, and moving startup reconciliation
  behind the HTTP listener remain separate work; recovery fencing can exceed the
  plan's soft disruption budget and long stop grace can require a manual retry.
- Protection notifications use the existing **Job failed** channel selection.
  Delivery is bounded and retried with fair rotation, but a dedicated event
  preference, configurable heartbeat interval and delivery backoff are deferred.
- Expired staging is cleaned by the background admission loop; a manual request
  does not synchronously run that cleanup. Metadata verification and time budgets
  do not establish performance on a very large repository with a cold cache.
- Native transport and recovery evidence is recorded per test environment.
  Successful local fixtures do not certify every NAS or cloud provider, and
  archive staging remains a full unencrypted local copy.
