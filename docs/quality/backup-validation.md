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
partially restored services stopped. Privileged extraction validation rejects path traversal,
escaping links, writes through links, special files and existing target
ancestor symlinks. A safe final symlink can be restored repeatedly. Privileged
file modes and Linux file capabilities are rejected; shared set-group-ID
directories remain supported. Restore preview rejects paths outside managed or explicitly approved
roots before stopping any service.

## Reliability Repair Validation — 2026-10-14

The independent review identified failures beyond the earlier fixtures below.
The [reliability plan](../product/backup-reliability.md) and
[ADR 0006](../adr/0006-backup-reliability-boundaries.md) define the repaired
contracts. Prior green checks are historical evidence, not evidence that those
review findings were absent.

Maintained regressions now cover real restic partial snapshots (including exit
code 3), required-object verification before retry adoption, repository leases
after capture, independent reads/lifecycle work during blocked transfer, graceful
cancellation, snapshot deletion and stale inventory, failed integrity checks,
clock rollback, and restore-test evidence scoped to a snapshot. Policy tests
cover recovered/legacy host review, overdue grace and deduplication, first-future
schedules, write-only heartbeat settings, redirect refusal, and suppression of
false heartbeats even while a replacement copy is running.

Capture tests cover durable stop phases, missing identities, diagnostic startup,
large archives beyond index limits, special metadata capture, bounded catalog
fallback and corrupt archive rejection. Helper regressions exercise sealed
root-owned disk inputs, grouped filesystem capacity, replace rollback, retained
interrupted rollback, target descriptor pinning, and single-file neighbor
preservation. Library tests distinguish deleting owned results from forgetting
custom registrations, reject symlink substitution, and preserve original archive
downloads for sources that cannot be extracted automatically.

| Exercise | Result |
| --- | --- |
| Canonical `make check`: Go tests/vet, API generation, frontend, build, docs and hygiene | Passed; 74 frontend files, 605 tests |
| Full backup/jobs race suite and affected helper/application race tests | Passed |
| Full backup test binary on Debian 13 arm64, restic 0.18.0 | Passed; actual missing-object and partial-read retry cases included |
| Root helper tests on Debian | 11 passed, including filesystem capacity and descriptor safety |
| Root-owned service configuration and SQLite snapshot through the installed helper | Passed as the non-root service account |
| Debian Bookworm arm64 with restic 0.14.0, non-root process | Roundtrip, incomplete snapshot and real exit-3 retry regressions passed; no skips |
| Two repository copies of PostgreSQL 17 and shared bind data | Passed; one captured set, complete-object verification, successful staging removed |
| Full restic data check and isolated recovery test | Passed |
| Reviewed replacement into populated live PostgreSQL and bind targets | Passed; database returned to the captured row set, newer files removed, services resumed |
| Ownership, ACL and user xattr after reviewed apply | Passed |
| Native FIFO and absolute-symlink capture, retrieval and archive download | Passed; automatic extraction test refused while original types and link targets remained recoverable |
| Managed deletion and custom-result forgetting through HTTP API | Passed; only owned data was removed, custom files remained |
| Replayed restore approval | Rejected; repository availability and verified integrity remained intact |
| Actual SIGKILL during Docker stop, with durable intent, received TERM and still-running container | Passed; startup fenced the pending stop and the original writer stayed running beyond its previous stop grace |
| Missing original writer after interruption | Passed; panel and diagnostic APIs returned HTTP 200 while retaining the unresolved journal and actionable error |
| Real Chromium typing/debounce, tab preservation and different schedule/browser timezones | 2 maintained browser regressions passed |
| Built frontend on the VM, Chromium desktop 1440×1000 and mobile 390×844 | Pause/resume, recovery diagnostics, focus/tab state, archive download and apply review passed; no page errors or horizontal overflow |
| Backend coverage gates | Passed; HTTP API 53.2%, global reported coverage 70.0% |
| Linux arm64 release artifact and generated dependency notices | Passed; stale dependency notices regenerated from the existing lockfiles |
| Debian 12 arm64 real-systemd package install, A-to-B upgrade and repeated configuration | Passed; helper ownership/mode, sudoers and service write paths verified, spool permissions repaired without deleting recovery evidence |
| Ubuntu 24.04 and Debian 13, root and non-root helper/native extraction | Passed after reproducing and fixing GNU tar's empty-selection behavior; directory-only extraction preserves the target inode, metadata and unrelated files |

The first CI run exposed Ubuntu GNU tar treating an empty `--files-from` as the
whole archive. Extraction now skips empty passes, preventing `--unlink-first`
from touching directory-only metadata archives. CI also exposed a focus assertion
that ran before React's focus effect; it now waits for the observable focus state.

The SFTP/S3 native tests remain opt-in and were not rerun in this repair batch.
This does not establish real-provider throughput, multi-terabyte capture time,
all network filesystem semantics, or recovery from physical disk failure. Tar
capture and validation still perform multiple full reads; large-source downtime
and free-space needs must be measured for the intended workload. Replace is
recoverable per source, not an atomic transaction across all mapped sources.

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

## Protection And Recovery Experience Checks

The experience extension has deterministic tests for durable copy evidence after
history cleanup, old capture retried later, retention failure, changed selections,
disabled/manual assignments, DST/interval scheduling, future-first configuration,
missed-occurrence coalescing, maintenance conflicts and repository read leases.
Concurrent progress/overview tests run under the race detector. Restore review
tests cover both stable IDs and legacy directories, including partial-file
rejection and destination/snapshot mismatch.

`TestNativeEncryptedCatalogComparePartialRestoreDownload` uses real encrypted
snapshots to verify indexes, content/metadata comparison, selected-file recovery,
library identity and exact downloaded bytes. Additional tests cover legacy
catalogs, recursive comparisons, pagination, bounded indexes, staging accounting,
cancelled reads, symlink/traversal rejection and unavailable registrations.
HTTP tests enforce authentication, same-origin mutation checks, pagination
bounds and safe attachment headers. Route and response tests use OpenAPI.

Frontend tests cover protection/coverage errors, setup and immutable editing,
history filters/paging, real writer preflight, interval preview and maintenance,
file comparison, recovery completion, downloads and reviewed apply. Browser
acceptance must exercise both desktop and narrow layouts on the built frontend;
unit tests alone do not establish a usable continuous recovery flow.

The extension was exercised on 2026-10-13/14 with Go 1.27.1, Node 24.21.0,
Debian 13 arm64/restic 0.18.0, and Chromium at 1440×1000 and 390×844.

| Exercise | Result |
| --- | --- |
| Canonical `make check`: Go tests/vet, generated API, frontend, build, docs and hygiene | Passed; 73 frontend files, 577 tests |
| Backend package coverage gates | Passed; HTTP API 53.4%, global reported coverage 70.9% |
| Backup/helper race tests, including simultaneous progress and overview reads | Passed |
| Linux archive metadata, nested-directory merge, hardlink boundary and interruption-safe indexing | Passed |
| Two native destination copies with shared writers, indexed snapshots and file comparison | Passed |
| Partial restore, exact authenticated download, partial apply rejected through both ID and directory APIs | Passed |
| ID-based reviewed volume apply, preserved ownership/mode and prior running states | Passed |
| Replayed review rejected while repository availability remains accurate | Passed |
| Repository check and prune jobs; deterministic independent scheduling tests | Passed |
| Desktop browser: history filters, comparison, isolated restore, automatic result selection, download and reviewed apply | Passed |
| Mobile browser: no horizontal overflow; guided destination creation, generated password, initialization, plan selection and first backup | Passed |
| Immutable destination editing and partial-source apply scope in UI | Passed |
| Linux arm64 release tarball build | Passed |

The VM exercise found and fixed GNU tar's inability to merge existing nonempty
directories with `--unlink-first`. The regression also verifies unrelated nested
files remain and replacing an existing hardlink does not modify its external
inode. Failed extraction correctly kept affected services stopped before the
fix was applied and retested.

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
