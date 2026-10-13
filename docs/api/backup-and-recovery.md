# Backup And Recovery API Semantics

## Contract And Execution

[OpenAPI](openapi.yaml) defines request/response shapes. Backup routes use the
normal operator session and same-origin mutation checks. No secret appears in a
read response: destination updates accept a write-only `secrets` object;
omitting it keeps the existing credentials. Destination identity and backend
are immutable after creation. Configuration changes are audited without request
bodies, passwords or credential-bearing subprocess output.

Configuration writes return the saved destination or plan. Run, retry,
repository initialization, access test, integrity check and restore return
`202` with a normal job envelope. Follow the existing job detail/event API and
global activity for completion; HTTP acceptance is not success. Cancellation
uses the existing job endpoint. Backup operations take the global mutation lock
and may return conflict while another operation owns resources.

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
inside a tar archive. The complete manifest accompanies partial selection. Recovered source archives
are also extracted into `files/<sourceID>` beneath the isolated target for
inspection and individual file recovery. The archives remain available for
reviewed apply. Inspection files belong to the service account; original numeric
ownership remains in the archives and is restored by the privileged apply helper.
`test=true` requires the complete recovery set. Successful isolated directories
are registered; only these directories can supply manifests to the reviewed
online apply workflow.

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
It does not prune foreign snapshots when attaching an existing repository.
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
