# Backup And Recovery API Semantics

## Contract And Execution

[OpenAPI](openapi.yaml) defines request/response shapes. Backup routes use the
normal operator session and same-origin mutation checks. No secret appears in a
read response: destination updates accept a write-only `secrets` object;
omitting it keeps the existing credentials. Destination identity and backend
are immutable after creation. Configuration changes are audited without request
bodies, passwords or credential-bearing subprocess output.

Configuration writes return the saved destination or plan. Run, retry,
repository initialization, access test, integrity check, prune and restore return
`202` with a normal job envelope. Follow the existing job detail/event API and
global activity for completion; HTTP acceptance is not success. Cancellation
uses the existing job endpoint. Backup operations take the global mutation lock
and may return conflict while another operation owns resources. Remote snapshot
and catalog reads reserve a repository read lease and can return conflict while
a backup mutation is active. Concurrent reads are allowed; destination edits
return conflict until those leases are released.

A run's durable capture and per-destination records are separate from the job
state. A run with one failed destination finishes with a failed job while
preserving successful copies and snapshot IDs. Retry selects only unsuccessful
copies from a still-valid prepared set. It does not recapture or repeat a
successful upload. If upload succeeded but verification/retention failed, the
snapshot ID is retained and that later stage can be retried.

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

A copy's `snapshot_metadata_verified` result means restic can read the snapshot
metadata. It does not assert that every data pack or application record was
verified. Explicit repository checks support metadata-only, full data read, or
a 10% data sample. The last successful check records its read policy. An
isolated restore test verifies downloaded content and extracts its source
archives; application startup/semantic checks remain an operator exercise.

Isolated restore requires a new directory outside live workspace and repository
paths. Restic include patterns select saved archives/paths, not arbitrary files
inside a tar archive. New clients select `source_ids` or regular `files`
(`source_id`, relative `path`); these modes cannot be combined with legacy
`includes`. The complete manifest accompanies partial selection. Recovered source archives
are also extracted into `files/<sourceID>` beneath the isolated target for
inspection and individual file recovery. The archives remain available for
reviewed apply. Inspection files belong to the service account; original numeric
ownership remains in the archives and is restored by the privileged apply helper.
`test=true` requires the complete recovery set. Successful isolated directories
are registered; only these directories can supply manifests to the reviewed
online apply workflow.

`GET /backups/restores` lists registered results by stable ID, including the
originating job, creation time, selection and availability. The UI follows job
completion into that result. Apply requests may use `restore_id` instead of
`restored_directory`; both paths enforce registration, destination/snapshot
identity and complete-source eligibility. A partial-file result is inspection
only. Removing a directory makes its registration unavailable; it does not
create permission to read another host path.

Snapshot `/files` and `/compare` routes accept source ID, directory/search and
offset/limit (default 100, maximum 500 entries). Listings show immediate directory
children, while a search scans descendants. Comparison takes `base_snapshot_id`,
covers the full source or selected subtree, and separates content and metadata
changes, including ACL/xattr digests. Source indexes are bounded to 100,000
entries and 12 MiB; oversized indexes fail explicitly. Old snapshots return `index_available=false`;
restore them first, then use `/restores/{restoreId}/files`. Download via
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

Apply merges selected archive content; unrelated target files remain. It stops
current writers and preserves ownership through the privileged helper. It
validates restored Compose definitions before an explicitly requested service
resume. The default leaves services stopped. Failure before data modification
can compensate stopped writers. Once writes begin, interruption or failure
leaves them stopped for operator inspection, including after Stacklab restart.
There is no automatic rollback of a partially overwritten application dataset.

Stacklab's private SQLite/state scope cannot be applied while the server is
running. Use the [offline recovery procedure](../ops/backup-and-recovery.md).
Fresh-host repository browsing and isolated recovery require only independently
supplied access credentials; the original Stacklab database is not required.

## Retention And Limits

Retention filters snapshots by Stacklab installation ownership and plan ID.
It does not forget foreign snapshots when attaching an existing repository;
repository-wide prune only reclaims data no longer referenced by snapshots.
Repository locking serializes uploads, forget/prune, checks and restore.

Staging has a per-plan byte budget and a one-to-168-hour lifetime. The service
keeps live/retryable run summaries and the newest 200 expired terminal summaries.
Older repository snapshots remain browsable. The newest 200 successful isolated
restore directories remain registered for apply; older directories stay on disk
under operator control. Restore again to register a fresh recovery set. Neither
metadata retention nor deleting a destination destroys a restic repository.

Schedules use explicit timezone-aware calendar occurrences. At startup, at most
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
disabled, running and never-captured copies remain distinguishable. A useful
snapshot and a failed retention step can coexist. Availability, check outcome
and last operation outcome are separate destination fields.

`GET /backups/coverage` aggregates discovered stack/global sources and configured
plans, with per-stack discovery errors. Coverage is configuration, not proof of
a successful copy. `GET /backups/plans/{planId}/preflight` resolves current
sources, shared writers and consistency warnings; execution revalidates them.
Run/copy records add phases, timestamps, duration and available byte/file
counters. Progress is bounded structured data, never raw credential-bearing
command output.
