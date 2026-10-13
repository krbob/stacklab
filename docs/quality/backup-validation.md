# Backup Validation

Backup validation combines deterministic coordination tests with native Linux
archive and restic exercises. The production target is Debian with GNU tar;
macOS runs portable unit tests and skips GNU-specific metadata checks.

Run the focused suite with:

```sh
go test ./internal/backup ./internal/store ./cmd/stacklab-backup-helper
```

When restic is installed, `TestNativeRepositoryRoundTrip` exercises an encrypted
repository, integrity checking, retention and recovery with a new Stacklab
database. `TestNativeCaptureRestorePreservesFileMetadata` runs on Linux and
checks contents, modes, modification time, hard-link identity, symbolic links,
supported user xattrs, and rejection when the staging byte budget is exceeded.

Coordination tests cover shared sources, unmanaged writers, changed Compose
mounts, compensation after capture failure, durable interruption journals,
previously stopped services, descendant process cancellation, and keeping
partially restored services stopped. Archive validation rejects path traversal,
escaping links, writes through links, special files and existing target
ancestor symlinks. A safe final symlink can be restored repeatedly. Privileged
file modes and Linux file capabilities are rejected; shared set-group-ID
directories remain supported. Restore preview rejects paths outside managed or explicitly approved
roots before stopping any service.

## Protected Configuration Exercise

On an isolated Linux test host, install the freshly built backup helper and its
matching sudoers policy. Keep `/etc/stacklab/stacklab.env` owned by root with mode
`0600`. Run this test as the non-root Stacklab service identity:

```sh
STACKLAB_TEST_BACKUP_HELPER=/usr/lib/stacklab/bin/stacklab-backup-helper \
  go test ./internal/backup -run TestNativeProtectedServiceConfigurationRecovery -v
```

The test exercises the exact-file `service-config` helper action, private
staging, a SQLite online snapshot containing committed state, and isolated
recovery. It checks that recovered configuration remains mode `0600` and never
prints configuration contents or credentials. Test-owned directories and
repositories are removed automatically by Go's test cleanup.

For cross-architecture testing, `go test -c` can produce Linux test binaries;
copy the binaries to the isolated host and run their `-test.run` filters as the
service account. Remove those copied binaries afterward. Manually created
Compose fixtures, repositories, service overrides and temporary sudoers files
require explicit cleanup after the operator has finished inspecting results.

## Acceptance Scenarios

Use the [operator recovery procedure](../ops/backup-and-recovery.md) for the
complete application workflow. Exercise local repositories, an absent expected
NAS mount, a shared Docker volume with running and stopped consumers, a failed
additional destination, destination retry, and an actual backend interruption
during capture. Confirm service compensation before scheduling resumes.

For reviewed recovery, verify numeric ownership and filesystem metadata using
the helper, confirm affected writers match the preview, validate the restored
Compose definition, and start services only when selected. Repeat repository
discovery and restore with a fresh Stacklab database and independently retained
credentials. Provider OAuth consent and real Google Drive/B2 connectivity need
provider credentials; local transport fixtures do not establish those results.

## Implementation Validation Record

The initial implementation was validated on 2026-10-13 with Go 1.27.1 and
Node 24.21.0. Portable checks used macOS; host integration used Debian 13 arm64
with restic 0.18.0. Package installation and upgrade used Debian 12 with a real
systemd process and restic 0.14.0, the supported minimum.

| Exercise | Result |
| --- | --- |
| Local encrypted repository, retention, integrity check and recovery with fresh SQLite state | Passed |
| SFTP with a pinned host key and an isolated SSH identity | Passed |
| S3 over HTTPS using a Linux rclone S3 fixture | Passed |
| rclone repository bridge using a local backend | Passed |
| Shared volume with two running writers and one stopped consumer | Passed; one archive, correct service compensation |
| One successful destination and one failed destination, followed by retry | Passed; retained capture and successful snapshot reused |
| Actual process kill during stopped capture and subsequent startup | Passed; journal recovered the original running services |
| Reviewed in-place volume recovery and one-use review token | Passed; content, ownership and permissions recovered |
| Protected root-owned service configuration and SQLite online snapshot | Passed as the non-root service account |
| Browser workflows at desktop and mobile widths | Passed, including actual reviewed recovery |

The native SFTP and S3 tests are opt-in. Set `STACKLAB_TEST_SFTP_REPOSITORY`,
`STACKLAB_TEST_SFTP_KEY_FILE` and `STACKLAB_TEST_SFTP_HOSTS_FILE` for an isolated
SFTP repository. Set `STACKLAB_TEST_S3_REPOSITORY`, `STACKLAB_TEST_S3_ACCESS_KEY`
and `STACKLAB_TEST_S3_SECRET_KEY` for an isolated HTTPS S3 repository; the region
can be supplied with `STACKLAB_TEST_S3_REGION`. These tests write remote data;
use dedicated disposable repositories and clean them up after validation.
