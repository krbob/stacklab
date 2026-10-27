# Backup And Recovery API Semantics

## Contract And Execution

[OpenAPI](openapi.yaml) defines request/response shapes. Backup routes use the
normal operator session and same-origin mutation checks. No secret appears in a
read response: destination updates accept a write-only `secrets` object;
omitting it keeps the existing credentials. Destination identity and backend
are immutable after creation. Configuration changes are audited without request
bodies, passwords or credential-bearing subprocess output.

Configuration writes return the saved destination or plan. Run, retry,
repository initialization, access test, integrity check, prune, stale-lock removal and restore return
`202` with a normal job envelope. Follow the existing job detail/event API and
global activity for completion; HTTP acceptance is not success. Cancellation
uses the existing job endpoint. Capture and apply take exclusive lifecycle
coordination; upload and repository work reserve the affected repository resources.
Remote snapshot and catalog reads conflict with mutations of the same repository,
while unrelated repositories and stack operations remain available during upload. Concurrent reads are allowed; destination edits
return conflict until those leases are released.

A run's durable capture and per-destination records are separate from the job
state. A run with one failed destination finishes with a failed job while
preserving successful copies and snapshot IDs. Retry selects failed copies or
failed maintenance. Upload/verification retry requires its still-valid prepared
set. Maintenance-only retry preserves `state=succeeded`, `verification`,
`verified_at` and transfer timestamps, and does not require staging or its TTL.
`retention`, `retention_error`, `retention_started_at` and `retention_completed_at`
describe the maintenance attempt independently. Mixed retries require prepared
data only for selected transfers. No retry silently recaptures or repeats a
successful upload. Once every transfer is verified, staging can be removed even
when maintenance needs attention.

## Source Identity And Filesystems

Discovery returns selectable logical sources. Saving and executing a plan
re-resolve them against Compose and Docker; a changed mount identity requires
review. Source exclusions are relative GNU tar patterns. Physical aliases
share one archive and must agree on exclusions. Coverage compares explicit
selection to discovered persistent mounts; adding a service never implicitly
adds its data to a plan.

Local filesystem detection reports the numeric device identity for an existing
path. The operator must review that the intended disk/share is mounted, then
pin the identity in the destination. Every repository access verifies it again.
An absent mount or symlink/path drift is an error, not a fallback to root storage.

## Verification And Recovery

A copy's `recovery_objects_verified` result means the saved manifest and all
expected recovery objects were checked for presence, type and size. Run-tag
adoption uses the same check; incomplete snapshots cannot satisfy retry. Legacy
`snapshot_metadata_verified` evidence remains unverified until reconciled. Neither
result asserts that every data pack or application record was verified. Explicit repository checks support metadata-only, full data read, or
a 10% data sample. The last successful check records its read policy. An
isolated restore test verifies downloaded content and extracts its source
archives; application startup/semantic checks remain an operator exercise. A
full restore test applies only to that exact snapshot. Sources requiring offline
extraction can be retrieved normally, but cannot pass an automatic restore test.

Destinations expose `metadata_check` and `data_check`, each with `status`, `scope`,
`checked_at` and optional `error`. Status is unknown, verified, sampled or failed;
scope is metadata, subset, all or unknown. `last_check_attempt_policy`,
`last_check_attempt_status`, `last_check_attempt_at` and `last_check_attempt_error`
describe the latest attempt separately. Transport, cancellation and inconclusive
errors preserve earlier dated evidence. Explicit data corruption remains failed
until a successful full read; metadata and sampled success cannot clear it.
`last_check_at` and `last_check_policy` retain the latest successful attempt.
The compatibility aggregates `check_status` and protection `integrity_status`
are conservative, but **verified does not necessarily mean a full data read**.
Inspect the evidence scope rather than interpreting the aggregate as that proof.

Isolated restore requires a new directory outside live workspace and repository
paths. Restic include patterns select saved archives/paths, not arbitrary files
inside a tar archive. New clients select `source_ids` or regular `files`
(`source_id`, relative `path`); these modes cannot be combined with legacy
`includes`. The complete manifest accompanies partial selection. Recovered source archives
with supported metadata are also extracted into `files/<sourceID>` beneath the isolated target for
inspection and individual file recovery. The archives remain available for
reviewed apply. Inspection files belong to the service account; original numeric
ownership remains in the archives and is restored by the privileged apply helper.
`test=true` requires the complete recovery set. Successful isolated directories
are registered; only these directories can supply manifests to the reviewed
online apply workflow.

Selected-file requests consult available authenticated indexes before accepting
a job. Hardlinks, symlinks and directories are not regular-file selections:
choose the regular target or recover the complete source. Unindexed legacy
sources still require validation after their archive has been retrieved.

`GET /backups/restores` lists registered results by stable ID, including the
originating job, creation time, selection and availability. The UI follows job
completion into that result. Apply requests may use `restore_id` instead of
`restored_directory`; both paths enforce registration, destination/snapshot
identity and complete-source eligibility. A partial-file result is inspection
only. Removing a directory makes its registration unavailable; it does not
create permission to read another host path.

Each result retains `recovery_status` and optional `classified_at` from the snapshot
classification available at registration. This provenance survives inventory
eviction and removal of the remote snapshot. Legacy/unclassified results are
`unknown`. Review repeats the provenance and validates each selected archive,
including its recorded size. An incomplete overall set can contain an intact
selected source; its label alone does not prohibit recovery. Exact replacement
requires a recorded archive size and matching local archive; unknown-size legacy
sources remain available for inspected merge or offline recovery.

Snapshot `/files` and `/compare` routes accept source ID, directory/search and
offset/limit (default 100, maximum 500 entries). Listings show immediate directory
children, while a search scans descendants. Comparison takes `base_snapshot_id`,
covers the full source or selected subtree, and separates content and metadata
changes, including ACL/xattr digests. Source indexes are bounded to 100,000
entries and 12 MiB. An oversized index preserves capture and reports
`index_available=false` with `index_unavailable_reason`; no partial index is shown
as complete. Old snapshots can build a bounded local index after isolated restore;
that fallback also reports limitations instead of promising unlimited browsing. Download via
`/restores/{restoreId}/download?source_id=…&path=…` returns an authenticated
`application/octet-stream` attachment with `private, no-store` and `nosniff`.
Only selected regular members are downloadable. Remote downloads always go
through a tracked isolated-restore job first.

The current archive representation transfers the whole containing source even
for one file. Catalog `transfer_bytes` reports that source archive size, not the
selected file size. Source/file selection uses authenticated restic object
metadata to reserve space for the selected archives, indexes and manifest.
Restore also reserves extraction space and fails on
insufficient capacity; `test=true` still requires a complete recovery set.

Online apply has a separate preview/commit boundary:

1. Restore a snapshot into an isolated directory and inspect its manifest.
2. Select source IDs and explicitly map each to an extraction directory or an
   existing supported local Docker volume. File sources keep their basename.
3. Request preview. It reports mappings, affected writers, overwrite scope and
   warnings and returns a ten-minute, single-use review token.
4. Commit the same request with that token. Changed archives, manifest, mapping,
   writer state or resume choice invalidate the review. The job reacquires the
   global resource and revalidates the target before writing.

Mappings default to `mode=replace` for complete directories; explicit `merge`
retains unrelated files. Single-file sources only replace their selected basename.
The helper prepares all archives before downtime and repeats safety checks before
writing. Replace preserves mountpoints and retains rollback data while extraction
is in progress. It is not an atomic multi-source transaction. Failed/interrupted
writes leave services stopped and expose a recovery journal, even if an immediate
rollback succeeded. The default leaves services stopped; requested resume also
requires validated Compose definitions.

Stacklab's private SQLite/state scope cannot be applied while the server is
running. Use the [offline recovery procedure](../ops/backup-and-recovery.md).
Fresh-host repository browsing and isolated recovery require only independently
supplied access credentials; the original Stacklab database is not required.

## Retention And Limits

Retention filters snapshots by Stacklab installation ownership and plan ID.
It does not forget foreign snapshots when attaching an existing repository;
repository-wide prune only reclaims data no longer referenced by snapshots.
Repository locking serializes uploads, forget/prune, checks and restore.
Snapshot lists include `recovery_status` and `classified_at`. Classifications are
complete, incomplete, legacy, unsupported, invalid or unknown. Unknown means no
current classification; it is neither corruption nor proof of completeness. The
versioned cache has a global 4096-entry bound and a per-scan classification budget.

A destination can expose `check_hold` (`scope`, `checked_at`, `error`) independently
of historical metadata/data proof. A hold makes aggregate `check_status=unverified`
and current evidence unusable unless known corruption already makes it failed.
A full successful check clears any hold; metadata/sample success clears only a
metadata hold. Transport errors neither create nor clear holds.

`POST /backups/destinations/{id}/prune` accepts `acknowledge_integrity_failure` and
`acknowledgement_review_token`. Known failure or a check hold requires explicit
`true` and the current destination's `maintenance_review_token`; omission or a
stale token returns 409 `maintenance_acknowledgement_required`. The token identifies
unresolved evidence, is not a credential, and is validated at admission and execution.
Healthy repositories still accept an empty request. Scheduled prune remains blocked
by failure/hold. Acknowledgement does not clear evidence or repair data.

`POST /backups/destinations/{id}/unlock` queues guarded ordinary `restic unlock`,
never force-unlock. Active Stacklab repository jobs conflict with admission. Execution
inspects owners and refuses live local PIDs (including EPERM), malformed/uncertain
metadata, future timestamps, locks younger than 30 minutes and remote-host locks.
A changed lock inventory before ordinary unlock is also refused. Guard refusals use
`unlock_refused` in the job diagnostic; unreadable or vanished lock metadata also refuses the guard. Review is bounded to 128 locks. `unlock_incomplete` means ordinary unlock ran but locks remain or the postflight inventory was unavailable. Age alone cannot prove safety across PID
namespaces; the service must share the host PID namespace. Jobs report remaining locks; nonzero/unreadable
remaining inventory fails the operation. External clients must remain stopped:
preflight and unlock are not atomic across clients. Integrity evidence is unchanged.

Retention retry uses the current enabled plan/assignment and policy, revalidated
under reservation; captured run configuration still determines capture/transfer
identity. Keep-all performs no listing/deletion and can resolve maintenance failure
without clearing an integrity hold. Maintenance chronology cannot supersede a newer
copy failure or its run identity. Explicit healthy subsets remain retryable.

Staging has a per-plan byte budget and a one-to-168-hour lifetime. The service
keeps active runs, runs within their staging retry window and the newest 200 expired
terminal summaries. Deleted-plan retention failures remain historical; the old
retention policy no longer authorizes a maintenance retry.
Older repository snapshots remain browsable. The recovery library retains up to 200 registrations and rejects new restores
until space is made by explicit deletion; it never silently evicts ownership
metadata. `DELETE /backups/restores/{restoreId}` removes the registration and, for
Stacklab-owned results, the directory. Custom target files remain untouched. Failed internal restore targets
are cleaned automatically. Neither
metadata retention nor deleting a destination destroys a restic repository.

Schedules use explicit timezone-aware calendar occurrences. On each scheduler tick, at most
one missed occurrence per destination is caught up; destinations due together
share capture. A resource collision stays due for retry on the next scheduler
tick. Failed scheduled execution is a normal failed job and notification,
without an unbounded automatic retry loop.

`interval` schedules require `interval_hours` from 1 through 168 and use UTC
epoch-aligned occurrences. Calendar schedules retain IANA timezone handling;
nonexistent DST times are skipped. `POST /backups/schedule-preview` returns the
next 1–10 occurrences (default five) and effective timezone using those rules.
Destinations add `check_schedule`, `check_read_data` and `prune_schedule`;
omitted settings retain the manual legacy defaults. Checks and prune share
normal jobs, admission and native repository locks. Scheduled prune defers pack
cleanup after per-plan forget; legacy configurations still prune after forget.
New or changed backup and maintenance schedules start at the next future
occurrence; unchanged schedules retain their marker and catch up missed work
after restart. Legacy schedules lacking a marker remain eligible for catch-up.

## Protection, Coverage And Execution

`GET /backups` includes server-derived per-plan/per-destination `protection`.
The captured timestamp, selected-source fingerprint and last verified snapshot
are durable evidence independent of retained run summaries. A retry does not
make captured data newer. Current, overdue, outdated selection, manual,
disabled and never-captured copies remain distinguishable. `activity` separately
reports queued/running work without replacing the protection state. A useful
snapshot and a failed retention step can coexist. Availability, check outcome
and last operation outcome are separate destination fields.
`first_outstanding_due` identifies the oldest unsatisfied occurrence, and
`overdue_since` is that deadline plus grace once elapsed. Both are independent
of evidence usability, so unavailable or unverified copies can remain visibly
overdue. The older `due_at` field retains the latest occurrence for compatibility.
Changing an assignment's schedule establishes a new activation boundary and grace;
unchanged schedules retain their overdue history across restart.

`GET /backups/coverage` aggregates discovered stack/global sources and configured
plans, with per-stack discovery errors. Coverage is configuration, not proof of
a successful copy. `GET /backups/plans/{planId}/preflight` resolves current
sources, shared writers and consistency warnings; execution revalidates them.
Run/copy records add phases, timestamps, duration and available byte/file
counters. Progress is bounded structured data, never raw credential-bearing
command output.


## Reliability And Operational Controls

- `GET/PUT /backups/policy` exposes scheduling pause, recovered-host review,
  overdue grace and heartbeat status. `heartbeat_url` is write-only; blank/omitted
  preserves it and `clear_heartbeat` removes it. Resuming after a host change
  requires `acknowledge_host=true`. Pause also suppresses automatic retention.
  Existing plans without saved host identity require the same one-time review.
  `effective_paused` and `pause_reasons` include automatic safety blocks, independent
  of the user's `paused` preference. Monitoring and transition notifications continue.
- `GET /backups/recovery` lists durable service-recovery journals. Malformed
  journals remain visible for manual inspection. `POST /backups/recovery/{runId}`
  returns a job: `retry` retries the recorded service identities; explicit
  `acknowledge_hold` removes a reviewed restore hold without starting services or
  deleting helper rollback data. `acknowledge_missing` requires `inspected=true`
  and an operator reason (8–1000 Unicode code points after trimming); every unfinished original container
  must be proven absent. All use exclusive lifecycle coordination and durable
  resolution audit. Observations distinguish current Docker state/replacements
  from historical phases; prepared helper tokens are never returned.
- Overview `helper_configured`, `helper_ready` and `helper_error` distinguish
  unconfigured integration from current privileged recovery protocol
  readiness. Capture may remain usable when helper recovery permissions need repair.
- Overview `recovery_error` explains degraded backup operation while the panel
  remains available. Unsafe capture/apply is blocked until recovery is resolved.
- Copy protection includes `snapshot_status`, `snapshot_checked_at`,
  `integrity_status`, `availability_status` and snapshot-scoped
  `last_restore_test_at`; snapshot lists expose `restore_tested_at` and destinations
  distinguish `last_check_attempt_at` from the last successful check. Inventory failure or expired evidence is not silently
  treated as a healthy current copy. Clock anomalies require attention.
  `evidence_usable` separates verification evidence from a currently running
  transfer. `schedule_activated_at` excludes occurrences before a new schedule
  was configured from overdue alerts.
- `DELETE /backups/restores/{restoreId}` removes a library registration. For
  `managed=true`, it also deletes the isolated directory. For custom targets it
  forgets the result and preserves all files; the operator owns cleanup.
- `GET /backups/restores/{restoreId}/archive?source_id=…` downloads original tar
  bytes from a complete-source registered result. Partial-file results cannot
  expose unselected archive content. Authentication and private attachment headers
  match individual-file downloads. `recovery_restrictions` describes metadata
  requiring operator-controlled offline extraction.

Exact replace rejects filtered sources and managed/configured root boundaries.
The source scope comes from the registered manifest and is sealed by the helper;
clients cannot override it in mappings. Explicit merge retains unarchived content.
