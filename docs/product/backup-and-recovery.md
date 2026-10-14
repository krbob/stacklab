# Native Backup And Recovery

## Status And Purpose

Implemented native module, 2026-10-13. [ADR 0005](../adr/0005-native-backup-orchestration.md)
records the architectural decision; the [architecture](../architecture/backup-and-recovery.md),
[API semantics](../api/backup-and-recovery.md), and
[operator procedure](../ops/backup-and-recovery.md) describe the delivered
contracts and constraints. The [reliability plan](backup-reliability.md) records
the accepted review corrections. This document describes the product contract.
Cloud account authorization, provider-specific live validation and actual
application recovery exercises remain deployment responsibilities.

Stacklab manages backup and recovery for its own configuration and the
Compose workloads it manages. The operator should be able to select data by
stack and service, keep copies in several locations, and recover without
maintaining a separate backup container. This covers application recovery;
whole-host disk imaging and operating-system recovery remain outside scope.

## Execution Model

Stacklab orchestrates native restic processes and use native rclone where
required by the destination. It owns the plans, scheduling, source
preparation, job progress, notifications, and recovery workflow. Restic owns the encrypted backup format, deduplication, repository integrity checks,
retention operations, and data restoration.

Backrest is a reference for useful backup workflows, not a runtime dependency.
Repositories must remain usable with the supported restic tooling without a
running Stacklab installation. Stacklab-specific recovery metadata is
versioned and stored with the backup, not only in the live Stacklab database.

## Destinations

The supported destination types are:

| Destination | Transport | Operator configuration |
| --- | --- | --- |
| Google Drive | restic through rclone | Authorized Drive connection and repository folder |
| Backblaze B2 and other compatible S3 services | restic S3 backend | Endpoint, region where required, bucket, prefix, and credentials |
| Local disk | restic local backend | Repository directory and expected backing filesystem |
| NAS | restic SFTP backend or a host-mounted share | SSH connection or validated mounted directory |

Provider quotas, prices and access policies are deployment choices and must be
checked when configuring a destination; no provider quota is hard-coded.

A destination represents one repository and has an access test, credential
configuration, availability state, and repository-check history. Credentials
and repository passwords belong in private service state, outside workspace
Git, API read responses, logs, and audit payloads. The setup flow must explain
how to keep recovery credentials independently of the machine being backed up.

For a disk or mounted NAS, validate the expected filesystem before writing;
an absent mount must not silently redirect a backup into the host root disk.
Show when a local destination shares a device with its source. Validate free
space for capture and upload retries, and exclude repositories, caches, and
temporary capture data from source selection. NAS protocols and filesystem
metadata support must be tested before being advertised as supported.

## Source Selection And Coverage

The operator can choose the following scopes independently:

| Scope | Selectable content |
| --- | --- |
| Stacklab recovery | Service configuration, application settings, backup plans, required credential state, and a consistent SQLite backup |
| Stack definition | The selected stack directory, including Compose, `.env` and relative data unless explicitly excluded |
| Managed configuration | Selected files and directories under `<STACKLAB_ROOT>/config` |
| Service data | Selected bind mounts and named volumes, with file and directory exclusions |

Use the configured paths from [Filesystem Layout](../architecture/filesystem-layout.md),
including overridden state and database locations. Sensitive configuration is
explicitly identified in the selection and protected by the encrypted
repository. Operators must be able to see what a recovery set includes and
which prerequisites they must supply separately.

Selection is anchored to the stack ID, Compose service name, and logical
mount/volume identity; ephemeral container IDs are not plan identities. Resolve
these references against Compose and Docker at execution time. A missing or
changed source must be reported rather than silently omitted from a successful
backup. Shared volumes are captured once per recovery set, with all consumer
relationships retained and all writers considered during consistency handling.

The stack view shows covered, excluded, and unconfigured persistent data.
Adding a new mount or service must surface a coverage change for review rather
than automatically uploading previously unselected data. Container writable
layers, downloadable image layers, logs, and caches are excluded by default;
Compose image references are recorded as recovery prerequisites; image digests
and downloadable image layers are not currently captured.

## Plans And Multiple Destinations

A plan selects sources and their consistency mode, then assigns one or more
destinations. Each assignment has an enabled state, schedule, and daily,
weekly, and monthly retention policy. Schedules support manual, every N hours,
daily and weekly occurrences with explicit timezone previews. A manual run can target all or
selected destinations. For example, one data selection can be copied daily
to a NAS and B2, and weekly to Drive, with different retention in each place.

For destinations due in the same run, prepare one recovery set using the
selected consistency mode and deliver that set to each destination. Record
capture time separately from upload completion time. Each repository must
contain an independently recoverable copy; restoring from a cloud copy must
not require the local repository to survive.

Track capture, upload, verification, and retention results separately per
destination. A successful NAS copy must remain visible when a cloud upload
fails, while the overall run must report that configured copies are missing.
Retry a failed destination without repeating successful uploads. Preserve
prepared data within an explicit space and lifetime budget; if it has expired,
report that a retry requires a new capture. Retention and repository maintenance
must be scoped and serialized so plans sharing a repository cannot remove
each other's retained recovery sets or race an upload or restore.

Scheduling, missed runs, cancellation, interruption, and restart recovery must
have explicit outcomes. Reuse and extend the existing job lifecycle and audit
model; this plan does not add new values to the published job-state contract.

## Data Consistency And Service Coordination

Two explicit capture modes are available:

- File capture for immutable data, prepared application dumps, or data whose
  live-copy behavior the operator has reviewed. Label live file copies with
  their consistency limits; copying an active database directory is not an
  application-consistent database backup.
- Stop selected writers, capture their data into private local staging, and
  restore the prior service state before remote upload. Bound the disruption
  window and recover service state on errors, cancellation, and process restart.
  Services already stopped must stay stopped.

Source preparation must coordinate with every service writing a selected
shared volume. Stacklab's own SQLite state requires a SQLite-aware online
backup while Stacklab is running. Database-specific dump/restore profiles are
a follow-up; the first release can include existing prepared dumps or use the
stop-and-capture mode.

Extend job resource coordination to cover affected stacks, shared data,
repositories, and prepared recovery sets. Prevent conflicts with stack
updates, restores, volume removal, Docker cleanup, and daemon changes. Native
execution must respect the non-root service and filesystem permission model;
unsupported volume drivers or inaccessible paths must be reported explicitly.

## Verification And Restore

The recovery workflow includes:

1. Browse recovery sets by stack, service, capture time, and destination, with
   selected sources, exclusions, consistency mode, and verification results.
2. Restore a file or selection into a separate directory for inspection.
3. Preview stack recovery: destination paths and volumes, overwritten content,
   affected services, permission requirements, and missing dependencies.
4. Restore selected configuration and data with supported ownership and file
   metadata preserved, then validate Compose and start services only as
   explicitly requested. In-place replacement requires a separate reviewed
   action; it must not be the default file-restore behavior.
5. Recover onto a fresh host using repository access and independently stored
   recovery credentials, without needing the previous Stacklab database.

Each recovery set includes a versioned manifest mapping logical sources to
saved content, volume/service relationships, capture
method, and relevant Stacklab and image references. Compose files are included
when their stack-definition source is selected. Fresh-host recovery must
allow reviewed path and volume remapping. Missing external networks, image
registries, build inputs, secrets, or other dependencies must be visible; the
product must not imply that a saved image digest makes an image available.

Repository verification and an actual restore test are distinct results. Ship
repository integrity checks with an explicit data-read policy and a manual
isolated restore-test workflow. Show when each last succeeded; an uploaded
snapshot alone does not prove that the application can recover. Document a
restic-based recovery procedure alongside the Stacklab workflow, and keep the
minimum access and decryption material outside the repository it unlocks.

## Product Integration

Provide a global backup view for destinations, plans, recovery sets, and
failures, plus a stack-level view for service data selection and coverage.
Use existing global activity and job detail for execution, and existing
notification channels for failures, missing scheduled copies, and recovery
results. Show last successful capture per destination, pending or failed
copies, repository size, and verification/restore-test age.

The global `/backups` screen and each stack's Backups tab expose configuration,
coverage and recovery. The implementation uses existing jobs and notifications,
versioned SQLite application settings, private credential/staging files, and a
narrow native archive helper. Reviewed complete-directory restore defaults to
replacement; merge remains explicit and single-file recovery preserves neighbors;
private Stacklab state recovery is deliberately offline. See the canonical
implementation documents linked above for limitations and installation details.

## Delivery Boundaries

The first release includes native execution; all destination types above;
per-stack and per-service selection; multiple destinations; scheduling and
retention; file and stop-and-capture modes; consistent Stacklab state capture;
repository checks; manual restore testing; and file, stack, and fresh-host
recovery. Exercise partial cloud failure, absent NAS mounts, shared volumes,
interrupted capture, permission preservation, and recovery without the original
host before calling that release complete.

Follow-up work includes database-specific profiles, optional pre-update
backups, scheduled isolated restore tests, and migration of existing Backrest
plans. Existing restic repositories may be evaluated for adoption, but their
snapshots must not be pruned under new policies without an explicit ownership
and retention review. Do not require migration to a proprietary backup format.

Whole-host imaging, arbitrary host administration, multi-host agents, and
building a new storage or encryption engine remain outside this module.
