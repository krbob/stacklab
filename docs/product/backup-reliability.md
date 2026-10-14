# Backup Reliability And Recovery Hardening

## Status And Scope

The first implementation was exercised on 2026-10-14. Independent second-round
review reopened release blockers in replacement scope, retention and operational
recovery. Their accepted implementation and validation contract is the
[reliability completion plan](backup-reliability-completion.md). The original
plan was committed before implementation. Measured results and remaining validation limits are recorded in
[backup validation](../quality/backup-validation.md). The requirements below
remain the acceptance contract for subsequent changes.

The review of the native backup baseline and protection/recovery experience
identified failures in capture, interruption recovery, repository isolation,
and protection evidence. Green unit tests and the earlier successful recovery
fixtures did not establish correct behavior under these failures. This work
repairs those contracts before further backup feature expansion.

Keep the Compose-aware source model, one captured set for multiple destinations,
native restic encryption, write-only credentials, reviewed apply, and recovery
without Stacklab. Preserve existing snapshots and additive `backup.v1` state.
The representation decision and alternatives are recorded in
[ADR 0006](../adr/0006-backup-reliability-boundaries.md).

## Delivery Order

### 1. Complete Copies And Honest Protection

- A snapshot found by a run tag is a candidate, not proof of a complete backup.
  Restic may commit a partial snapshot and exit with code 3. Verify the expected
  manifest and source/index objects before adopting a candidate or recording a
  successful copy. Retrying an incomplete candidate must upload the prepared set
  again. Metadata verification is still distinct from a full data check.
- Keep freshness, repository availability, integrity, snapshot presence, last
  operation, and recovery exercise evidence separate. Aggregate Protection must
  surface failed checks, missing copies, stale evidence, and clock anomalies.
  Network failure is not proof that a snapshot was deleted or data is corrupt.
- Reconcile snapshot presence on successful repository inspection and maintenance;
  do not run a remote command on every dashboard poll. Record the time and scope
  of evidence. Recovery tests apply to the tested snapshot and plan, not every
  plan using the destination.
- Apply one bounded clock-skew policy to backup/maintenance occurrence markers,
  retained checkpoints and health. Future timestamps must not silently suppress
  scheduling or prevent a later valid backup from replacing bad evidence.

### 2. Capture And Interrupted Service Recovery

- Separate faithful capture validation from privileged extraction policy.
  Absolute symlinks, special entries and privileged metadata may be stored
  without following links or granting those privileges during recovery.
  Unsafe extraction remains rejected explicitly; never silently omit data.
- Index limits must degrade inspection, not discard an otherwise valid backup.
  Preserve bounded memory, time and storage; publish why an index is unavailable.
  A truncated index must never be presented as complete. Legacy local indexing
  must obey the same honest behavior. Corrupt archives and cancellation remain
  failures rather than optional-index warnings.
- Persist stop/resume phases and reconcile Docker state after interruption,
  including an outstanding daemon-side stop, a never-started stop and a replaced
  or missing container. Do not blindly wait for every container to exit, delete
  unresolved journals, or recreate services from stale identities.
- Start the diagnostic panel when backup recovery needs intervention. Preserve
  an actionable degraded state and block unsafe work until reconciliation; a
  bad backup state must not put the entire process into a restart loop.
- Respect graceful stop settings. Distinguish container stop time from the
  overall downtime budget. Estimate staging needs without requiring the entire
  configured maximum to be free; enforce the actual byte limit and free-space
  reserve throughout capture.

### 3. Coordination, Diagnostics And Storage Lifecycle

- Hold disruptive lifecycle coordination during capture/apply, then release it
  before uploading immutable staging. Repository reads/mutations use scoped
  reservations keyed by repository identity, with credentials and staging pinned.
  Preserve drain/self-update behavior and shared-writer protection.
- An unavailable destination must not block unrelated stack operations or
  repository browsing. Connection/no-progress deadlines must be bounded without
  imposing a short total deadline on a healthy large upload. Parallel transfer
  is optional; correctness and isolation come first.
- Report actionable, secret-safe failure categories for unavailable repositories,
  wrong credentials, locks, incomplete backups and verification failures. Respect
  the supported restic version range. Cancel gracefully before bounded forced
  termination. Never automatically force-unlock a repository.
- Remove successful staging when retries no longer need it; clean expired data
  before new captures. Keep retry lifetime distinct from upload execution time.
- Add explicit removal of registered isolated recovery results and clean failed
  internal restore targets. Never recursively remove arbitrary operator-chosen
  directories during metadata eviction. Expose storage ownership and limitations.

### 4. Reviewed And Exact Recovery

- Prepare and validate all archives and target mappings with the helper before
  stopping services. Use bounded, private disk storage for immutable helper
  inputs rather than an unbounded copy into the host's default temporary area.
  Recheck target safety immediately before mutation.
- Preserve the durable hold-stopped rule once writes might have begun. A failed
  helper may already have modified data; success of the whole helper invocation
  is not a valid marker for the first write.
- Distinguish exact replacement of complete source data from merge. The default
  complete-source workflow must not leave newer database files behind. Explain
  removal scope in the review and provide a recoverable failure procedure.
  Do not claim atomic replacement across Docker mountpoints or multiple sources.
- Keep partial-file inspection separate from full-source apply. Unsupported
  metadata has an explicit recovery limitation and remains available in the
  original archive for an operator-controlled offline procedure.

### 5. Operational Signals And Recovery Preparation

- Add overdue-copy alerts through existing notification delivery, with plan and
  destination context, a grace period and transition-based deduplication.
- Provide an optional external success heartbeat for scheduled protection.
  Only an external monitor can report that Stacklab itself has stopped running;
  its expected interval must include the upload budget and grace period.
- Define a global scheduling pause and a recovery review after restoring state
  on a different host. Pause retention as well as captures; host identity alone
  must not silently transfer ownership of old snapshots.
- Document the secret escrow boundary: a state backup includes credentials for
  other destinations. Keep that intentional DR capability explicit and provide
  an independently stored recovery-kit checklist. Never remove credentials from
  existing state backups while still promising complete state recovery.

### 6. UI And Documentation Corrections

- Preserve file-search focus and navigation controls while data reloads. Keep
  visited Recovery/History state across tab changes; refresh without layout
  jumps or repeated screen-reader announcements.
- Format schedule previews in the effective schedule timezone. The API must
  provide a usable timezone/offset contract for host-local schedules.
- Generate correct absolute SFTP URIs for new destinations. Never rewrite an
  existing repository location automatically: repositories created by the old
  wizard may genuinely live under the SSH user's home directory.
- Scope errors to their operation/dialog, invalidate coverage with overview,
  show manifest loading/retry, and protect unsaved editor changes.
- Generate passwords only when creating a destination. Replacing a saved
  credential is explicit and does not imply repository key rotation.
- Render manual freshness, unknown size and keep-all retention honestly. Expose
  degraded recovery, missing-index and evidence limitations in the normal flows.
- Reconcile product, architecture, API, operations, installation and UI docs.
  Include staging cost/defaults, safe fresh-host recovery, credentials, helper
  requirements, scheduler pause, and cleanup. Remove private deployment links.

## Validation And Release Gate

Each repaired failure needs a regression testing the externally meaningful
invariant, not only the chosen implementation. Convert the independent review
proofs into maintained tests. In particular:

1. Actual restic partial snapshot, failed first run, retry, complete source
   verification and subsequent successful recovery.
2. Large source beyond the index bounds, FIFO/absolute symlink capture, explicit
   extraction restrictions, and bounded catalog fallback.
3. Process interruption during Docker stop, missing/recreated writer and invalid
   journal: panel remains available, journal stays visible, services are not
   silently left down or resumed on partially restored data.
4. Failed/unavailable repository concurrent with unrelated lifecycle work and
   browsing; cancellation, credential changes, shared sources and self-update.
5. Corrupt/missing snapshot, failed check, restored clock, scoped restore-test
   evidence, overdue notification and external heartbeat behavior.
6. Helper preflight errors before downtime; complete-data replacement into a
   populated target, metadata preservation and failure recovery on Debian.
7. Real typing across debounce, tab switches during restore, timezone mismatch,
   SFTP absolute path, dirty forms, refresh and operation-specific errors.
8. Canonical Go/frontend/API/docs/hygiene checks, affected race tests, Linux
   helper/native repository tests and built desktop/mobile browser exercises.

Record performed tests separately from code inspection and untested provider or
filesystem combinations. Commit coherent implementation steps, push the tested
result, and inspect CI for the pushed revision.

## Deferred Product Expansion

Database dump profiles, direct-restic or filesystem-snapshot capture, append-only
storage, rest-server, Backrest import, arbitrary hooks/cron and broader retention
remain separate roadmap work. Do not weaken the one-set/multiple-copy guarantee
or the helper boundary to deliver them as incidental fixes.
