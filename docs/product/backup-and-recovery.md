# Native Backup And Recovery

## Status And Purpose

Accepted product direction, 2026-10-13; not implemented. The
[roadmap](../roadmap.md#near-term-product-goals) gives this work near-term
priority. [ADR 0005](../adr/0005-native-backup-orchestration.md) records the
architectural decision. This document defines the intended product behavior,
not currently available screens, API routes, or database schemas.

Stacklab will manage backup and recovery for its own configuration and the
Compose workloads it manages. The operator should be able to select data by
stack and service, keep copies in several locations, and recover without
maintaining a separate backup container. This covers application recovery;
whole-host disk imaging and operating-system recovery remain outside scope.

## Execution Model

Stacklab will orchestrate native restic processes and use native rclone where
required by the destination. It will own the plans, scheduling, source
preparation, job progress, notifications, and recovery workflow. Restic will
own the encrypted backup format, deduplication, repository integrity checks,
retention operations, and data restoration.

Backrest is a reference for useful backup workflows, not a runtime dependency.
Repositories must remain usable with the supported restic tooling without a
running Stacklab installation. Stacklab-specific recovery metadata will be
versioned and stored with the backup, not only in the live Stacklab database.

## Destinations

The first release will support these destinations:

| Destination | Transport | Operator configuration |
| --- | --- | --- |
| Google Drive | restic through rclone | Authorized Drive connection and repository folder |
| Backblaze B2 and other compatible S3 services | restic S3 backend | Endpoint, region where required, bucket, prefix, and credentials |
| Local disk | restic local backend | Repository directory and expected backing filesystem |
| NAS | restic SFTP backend or a host-mounted share | SSH connection or validated mounted directory |

Backblaze B2 through S3 is the initial cloud destination chosen for homelab
validation. Its first 10 GB of storage are free according to the
[B2 pricing page](https://www.backblaze.com/cloud-storage/pricing), checked on
2026-10-13. This is a deployment choice, not a product-wide default or a
hard-coded quota; the other destination types remain in the first-release
scope, and provider pricing must be checked when configuring a deployment.

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
| Stack definition | Compose, optional `.env`, and selected stack-local supporting files |
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

The stack view will show covered, excluded, and unconfigured persistent data.
Adding a new mount or service must surface a coverage change for review rather
than automatically uploading previously unselected data. Container writable
layers, downloadable image layers, logs, and caches are excluded by default;
image references and available digests are recorded as recovery prerequisites.

## Plans And Multiple Destinations

A plan selects sources and their consistency mode, then assigns one or more
destinations. Each assignment has an enabled state, schedule, and daily,
weekly, and monthly retention policy. Start with explicit daily and weekly
schedules in the displayed host timezone. A manual run can target all or
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

The first release needs two explicit capture modes:

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

The first release must support the complete recovery workflow:

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
saved content, Compose definitions, volume/service relationships, capture
method, and relevant Stacklab and image versions. Fresh-host recovery must
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

The route map, API payloads, persistence model, permission helper contract,
and dependency installation details must be designed with the implementation.
Current UI, API, and installation references continue to describe shipped
behavior until those changes land.

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
