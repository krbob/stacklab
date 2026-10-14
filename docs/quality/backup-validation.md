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

## Round-Two Repair Validation — 2026-10-14

The round-two repair has a [maintained Linux integration runner](../../scripts/dev/backup-e2e/README.md)
and a separate installed-package upgrade exercise. The isolated runner creates
its own helper configuration, spool, data and PostgreSQL fixtures. Reproduce it
with the pinned toolchain:

```sh
GOTOOLCHAIN=go1.27.1 scripts/dev/backup-e2e/run-linux.sh \
  test-user@test-vm unique-run-id /tmp/backup-evidence-unique-run-id arm64
```

Run `r2-20261014c` built clean production revision
`552abacc3c8c4ec7dab886151ccada53963be379` with Go 1.27.1 and ran on Debian 13 arm64,
GNU tar 1.35, restic 0.18.0, Docker 26.1.5 and PostgreSQL 17 Alpine.
`revision.txt`, an empty production working-tree patch, `source.tar.gz`, toolchain
and host versions, binary hashes and raw logs are retained in the local artifact
directory `/tmp/stacklab-backup-e2e-r2-20261014c`.

The first PostgreSQL fixture attempt exposed a harness readiness race: the image's
temporary initialization server answered `pg_isready` just before shutting down.
The maintained harness now waits for the final PostgreSQL daemon as PID 1. Only
the integration fixture was repeated with the same clean production binaries;
all assertions passed. The initial failure log and corrected harness are retained
alongside `integration-rerun.log` and `integration.json`. The earlier working-tree
run `r2-20261014b` remains historical evidence, superseded by this clean build.

| Exercise | Result |
| --- | --- |
| Pinned full portable Go suite (`./cmd/... ./internal/...`) | Passed |
| Pinned race suites: backup, jobs, helper, notifications | Passed |
| Go vet, repository hygiene, docs, generated API checks | Passed |
| Coverage gates | Passed; backup 75.9%, HTTP API 53.6%; global 70.8% reported, without a global floor |
| Frontend on pinned Node 24.21.0 | 620 tests passed |
| Maintained Playwright desktop/mobile scenarios | 5 passed |
| Live installed-package browser smoke | Desktop and mobile passed against the upgraded VM with real authenticated API responses |
| Full backup test binary as the non-root service identity | 112 top-level tests passed; 3 explicit opt-in skips |
| Root helper test binary | 15 top-level tests passed; no skips |
| Real restic partial/incomplete snapshot retry and retention | Passed, including the non-root exit-code-3 permission fixture |
| Controlled delayed storage with real restic | Reading stalled at 44,910,454 / 268,435,456 bytes and was cancelled after 5m1.869s; an unchanged finalization phase survived 5m20s and completed after storage release at 5m22.192s |
| Managed restore cleanup with inaccessible (`000`) directory | Passed on Linux |
| Every required helper action's protocol probe | Passed |
| PostgreSQL stopped capture and populated exact replacement | Original 1,000 rows and checksum recovered; 500 later rows, later table and marker removed; target inode preserved |
| Filtered or unknown-scope exact replacement | Refused; excluded live file preserved |
| Explicit filtered merge | Passed; excluded live file remained identical |
| Managed root exact replacement | Refused before mutation |
| Actual SIGKILL during helper extraction | Killed the process group only after phase `writing` and a newly extracted nonempty file existed; rollback and partial target remained, release refused the interrupted record, fixture service stayed stopped |
| Actual installed package upgrade | `2026.10.0~review2` → `2026.10.0~review2fix`, built from `552abac` |
| Legacy and current sudoers | Legacy permissions produced `helper_ready=false`; installing the current validated narrow example restored readiness through the periodic probe, without another application restart |
| Existing state through upgrade | Configuration bytes, six plan IDs, six destination IDs, operator pause and host-review state preserved |
| Installed protected service configuration recovery | Passed as `stacklab`; configuration remained unreadable directly and recovered privately with a consistent SQLite snapshot |
| Existing VM application and containers | Running after validation; isolated test containers removed |

The three initial native-suite skips were SFTP, S3 and protected configuration.
The protected-configuration test subsequently passed against the newly installed
package; SFTP and S3 were not repeated in this repair batch. The kill exercise
validates privileged extraction interruption, while maintained Go regressions
cover application stop journals, daemon-side fencing, missing writer inspection,
explicit acknowledgement and durable audit before journal removal.

The installed upgrade used `scripts/dev/backup-e2e/upgrade-installed.py` with the
candidate `.deb`, clean backup test binary and a fresh root-only backup directory.
The complete previous configuration, application state, helper spool, sudoers
and installed application were archived before mutation at
`/root/stacklab-backup-upgrade-r2-20261014/before.tar`. Upgrade results and the
protected-configuration log are copied into the local artifact directory.
Authentication was preserved; private browser session material is excluded from
public evidence and repository files.

| Artifact | SHA-256 |
| --- | --- |
| Clean `backup.test` | `a58b91a7d6b1aced24e16dd6f5b7794c6acb413e7b7c092a378ae9fe3b9e7dce` |
| Clean `helper.test` | `f7af023f7ee4e59942e2c9271a2fcf5dc6fe4e5422d26c8f7881867b23e167ec` |
| Isolated fixture helper | `d4f043b1f006c786f50546b403d103f17f3ab7bd3cc9281933abbeaf6ab1b47c` |
| Installed `.deb` | `19bd361326f29484b5b4ccff45bd4869e7abdc1e8d397be472a76e5b6d33c1c3` |

The controlled-storage watchdog experiment used clean production revision
`552abac`, Go **1.27.2** on macOS arm64, restic 0.19.1 and rclone 1.75.2; it is
separate from the pinned Go 1.27.1 gates above. Both the cancelled reading case
and completed finalization case passed encrypted repository checks. The completed
8 MiB backup additionally passed `restore --verify` and a SHA-256 comparison.
The original experiment log is `/tmp/stacklab-watchdog-validation/result.log`;
the fixture is retained as opt-in
`TestNativeTransferWatchdogControlledTransport`, documented with its invocation
in the integration runner README. This is evidence for deliberately delayed
storage, not general WAN throughput or a live exercise of the 30-minute maximum
finalization deadline.

Policy race regressions cover bounded notification batches, fair retries across
failed channels, malformed policy preservation, valid grace fallback,
pause/overdue/resolved delivery across restart and missing-writer acknowledgement
without starting replacements. Notification delivery is at least once: a crash
or storage failure after delivery can cause a repeat. Each observation attempts
at most 16 transitions within a 20-second delivery budget, with a persisted
cursor and priority for pause transitions; a healthy external heartbeat is
attempted before channel delivery.

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
