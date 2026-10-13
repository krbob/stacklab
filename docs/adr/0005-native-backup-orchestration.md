# ADR 0005: Native Backup Orchestration

## Status

Accepted direction, 2026-10-13. Implementation pending; this ADR does not
describe an already available backup module.

## Context

Stacklab knows Compose definitions, service lifecycle, mounts, jobs, and the
managed workspace. Backups configured in a separate application require the
operator to recreate those relationships and coordinate consistency and
recovery separately. The product needs selectable application data and
multiple destinations, including Google Drive, S3-compatible storage, local
disks, and NAS systems.

The existing [host-native deployment](0001-host-native-backend.md) and
[filesystem layout](0004-deployment-aware-filesystem-layout.md) provide the
execution and ownership boundaries for this feature. The detailed scope lives
in the [backup and recovery product plan](../product/backup-and-recovery.md).

## Decision

Implement backup orchestration within Stacklab using native restic processes
and native rclone processes for destinations that need them. Do not depend on
a Backrest server or a continuously running backup container. Stacklab will
coordinate source selection, consistency, scheduling, jobs, and recovery;
restic will provide the encrypted repository format and backup primitives.

Use restic's S3 backend for Backblaze B2 and other compatible stores, its local
and SFTP backends for disk and NAS targets, and its rclone integration for
Google Drive. Keep repositories independently recoverable through restic.
Store versioned Stacklab recovery manifests with the protected data, and keep
the access and decryption material needed to open them independently available.

Bind selection to stable Compose and volume identities, and prepare one
recovery set with an explicit consistency mode for destinations due in a run.
Keep destination results and retention independent. Reuse and extend
Stacklab's existing lifecycle, job resources, scheduler, audit, and
notifications rather than introducing an unrelated automation system.

Preserve the non-root service model. Access to protected data must use narrowly
scoped, validated mechanisms; do not run the entire backend as root or expose
a generic privileged command runner. Repository credentials stay in private
service state. Installation and upgrades must account for restic/rclone
versions and systemd filesystem restrictions on both supported architectures.

## Consequences

- Backup and recovery can coordinate with Compose updates, shared-volume
  writers, and stack state using one operator interface.
- Restic keeps backup data portable and recoverable if Stacklab is unavailable.
- Stacklab owns the correctness of capture, interruption recovery, restore
  mapping, credential handling, and partial failures across destinations.
- Native tool dependencies, staging space, filesystem permissions, NAS mounts,
  and supported volume drivers require explicit validation and diagnostics.
- Backup and restore ship together; successful upload alone is not the
  acceptance criterion for the feature.

## Follow-Up

Define API, persistence, UI, dependency packaging, and narrowly scoped data
access contracts during implementation. Validate the complete recovery path
on both supported deployment profiles. Database-specific profiles, automatic
pre-update backups, and scheduled restore exercises follow the first release.
