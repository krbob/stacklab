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

## Visual Layout Follow-Up — 2026-10-21

Browser inspection reproduced touching Protection panels and destination recovery
steps, warnings touching surrounding content, clipped dialog titles with long
names, and shorter native select controls in WebKit. The layout now separates
panels by 16 px, groups copy actions, wraps mobile tabs and uses two summary
columns on phones. Card titles and form-control sizing are consistent.

Chromium and WebKit inspection covers all five backup tabs, snapshot browsing,
destination editing and check/cleanup dialogs at 1440, 768 and 390 px. An additional
Chromium pass uses 320 px and long unbroken plan/destination names. Screenshots and
DOM measurements confirm section separation and no horizontal overflow in page,
panel or dialog containers. These are local frontend fixtures with mocked API
responses, separate from the installed-package evidence below. Local captures
and measurements are in `/tmp/stacklab-ui-review-20261021/`.

All 673 frontend unit tests and 10 maintained Chromium backup scenarios pass,
along with typecheck, lint, production build and the font/CSP build check.

## Round-Six Diagnostics And Unlock Validation — 2026-10-21

The [sixth-round contract](../product/backup-diagnostics-and-unlock.md) was committed
before implementation (`3b10910`). Diagnostic context and unlock safeguards are in
`5472c6c` and `71d5e1a`; current-evidence UI and unlock chronology are in `b9ffe00`
and `e690082`. Maintained integration harness changes are in `d7a3d09`, `9004ef7`
and `e690082`.

| Exercise | Evidence and result |
| --- | --- |
| JSON messages with trailing newlines; unknown tree/blob diagnostics mixed with transport; lock refresh versus missing-pack evidence | Deterministic plain, per-record JSON and multiline JSON regressions pass. Assertions include resulting hold and maintenance eligibility, not just the diagnostic label. |
| SFTP interruption during inventory | Real restic 0.18.0, Debian 13 arm64, owned OpenSSH server. A protocol gate stops OPENDIR(data) before it reaches sftp-server; no timing guess or fabricated diagnostic. Returned `unavailable`, preserved prior full proof and created no hold. Captured stderr is in the committed corpus. |
| SFTP interruption during pack reads; real truncation afterward | Separate 8 MiB transport gate. Both fixtures recover through fresh-lock refusal, simulated old-dead-owner unlock and a full check. Actual truncated encrypted bytes establish integrity failure. Final 0.18 runs: 26.88 s (pack-read gate), 26.33 s (inventory gate). |
| Minimum restic 0.14.0 | Both SFTP gates pass with explicit cancellation after the 60-second outage budget, followed by recovery/truncation checks (131.49 s and 121.07 s). This proves cancellation, not a naturally returned transport error within that budget. Native `rebuild-index`, full check and Stacklab archive recovery also pass (14.60 s). |
| Live root-owned process and same-hostname private PID namespace | Real Linux/Docker fixtures pass. The namespace-local PID is demonstrably live inside and absent outside; its fresh lock is refused. EPERM and live old owners remain protected. Minimum-age boundaries, future timestamps, uncertain probes and changed/reordered lock inventories have deterministic regressions. |
| Actual index repair and recovery | Native `repair index` on 0.18/0.19 and `rebuild-index` on 0.14 rebuild removed indexes using intact packs, followed by full checks and recovered payload assertions. This is distinct from the byte-restoration tests. |
| HTTPS S3 corruption | Owned rclone server and pinned CA; real restic 0.19.1 full check, truncated pack, blocked maintenance and original-byte restoration/full check pass (12.48 s). The separate independently configured S3 endpoint test is explicitly skipped. |
| Full Linux archive/helper suite | Clean `b9ffe00` source, empty working-tree patch, maintained `run-linux.sh`. Five privileged exercises pass: ACL/xattr/hardlink/mode-000 recovery, PostgreSQL exact replacement, scope boundaries and SIGKILL during extraction. Final diagnostic/native tests were rerun from `d7a3d09` after the summary-classification refinement. |
| Backend and repository checks | Full backend package coverage passes (global 71.7%, HTTP API 54.1%); formatting/vet, generated API, docs, release/hygiene and monitoring checks pass. Monitoring reports its 17 optional skips explicitly. Backup/jobs/notifications race tests pass, including the refined classifier (backup 174.955 s). |
| Frontend and browser regressions | All 673 unit tests pass; final undated-unlock refinement additionally passes the focused page suite. Typecheck, lint, production build and bundle-size gate pass. Chromium Playwright backup suite: 10/10, including mobile cleanup request/token checks. |

The final installed package is `2026.10.0~review6fix2` (runtime
`2026.10.0-review6fix2`), built from `e690082a1456`. SHA256:

- Debian package: `7a7a617e55b212274c8b8715ed8ee71fb906e57cf0df1876f67119bc358faeaa`.
- Installed backend: `6e5bb5872c4f58d5f468dbef66b029fd80af3da245df560f1c59b4ce68d89509`.
- Final native Linux test binary: `0c655cbe4ca4b99a3fc12313aafe4b10a7c531a7fe2c503918ca7350bd5e49fb`.

The maintained upgrade harness passes all five checks, including legacy/current
sudoers, protected service configuration recovery and preservation of plans,
destinations and operator policy. The original pre-exercise configuration has nine
plans and eight destinations; temporary owned fixtures account for additional
entries during intermediate upgrades.

The installed API exercise on the final package passes **12 assertions across 14
jobs**. Two check jobs intentionally fail on native corruption. It exercises real
capture, unlock, full/selected recovery, reviewed ACL replacement, changing prune
acknowledgement, and offline native index repair as the installed `stacklab` user.
Before that repair it stops the service and copies the whole owned repository;
after repair it verifies newly generated index ownership, a native full check, an
API full check and recovered content/ACLs/xattrs. Its final cleanup confirms the
original nine plans/eight destinations and operator policy. An initial exercise's
cleanup met a legitimate inventory-read lease (409); the maintained harness now
waits boundedly for that read before retrying rejected fixture deletion. The final
exercise, including cleanup, passes without bypassing repository reservations.

The installed Chromium exercise passes **22 assertions with 17 screenshots**, using
real API responses and packaged assets. It covers desktop and 390 px layouts, hold
guidance, resolving check scope, exact-name prune acknowledgement, unlock review,
recovery provenance and historical deleted-plan maintenance without an impossible
retry. Desktop review and history screenshots were visually inspected. A temporary
hold was explicitly injected into persisted state for an **owned, natively captured
and repaired fixture repository**; its earlier metadata/full proof was retained.
This proves installed hold presentation and continued real snapshot retrieval,
not that native restic produced that unknown diagnostic. The deleted-plan history
was the existing real `r6-del` run. The owned hold/configuration was subsequently
removed, browser sessions revoked and private authentication exports deleted.

Four browser cases are explicitly skipped: unavailable helper, system-caused pause,
the stored-data-failure default on the selected held destination, and switching
between two destinations when the harness is pinned to one owned repository.
The failed-data default and review invalidation remain covered by unit and
Playwright regressions. Final required CI is checked on the exact pushed revision;
the delivery message links that run rather than treating earlier green revisions
as proof of the final branch head.

Lock-age integration tests explicitly simulate 31 minutes passing **only after**
proving refusal at the real clock. They do not claim thirty minutes of wall-clock
waiting or prove every external owner is stopped. The production guard has no test
bypass. Preflight still cannot close the final race with independent clients.
Native transport and corruption evidence does not prove every unknown restic message
or backend. Historical unrecorded diagnostics are not reconstructed by this upgrade;
run the resolving full check when current evidence requires it.

## Round-Five Repository Safety Validation — 2026-10-15

The [fifth-round contract](../product/backup-repository-safety.md) and
[ADR 0009](../adr/0009-backup-maintenance-safety.md) were committed before code
(`c78e70d`). Backend safety and regressions are in `c433beb`; UI and generated
contract changes are in `21b8e12`. The clean package and Linux fixture source is
`cae4cdbe66d639c66ff51f07953aaab3f04cd606`, including native S3 and installed API
regressions. The Linux runner recorded an empty working-tree patch and archived
its source, diagnostic corpus, binaries and hashes.

| Exercise | Evidence type | Result |
| --- | --- | --- |
| Full backend tests and package coverage gates | macOS, Go 1.27.1 | Passed; global reported coverage 71.7%, HTTP API 54.1% |
| Backup, jobs and notifications race tests | Native restic 0.19.1 plus deterministic coordination | Passed; backup package 166.372 s |
| Formatting/vet, generated API, docs, repository hygiene and monitoring tests | Canonical repository checks | Passed; monitoring explicitly skips its optional privileged/container cases |
| Durable hold, legacy migration, scope-sensitive clearing, mixed diagnostics and late cancellation | Synthetic Go regressions; native stderr corpus proves corruption classification, not an unresolved hold | Passed; historical proof retained independently of current usability |
| Current retention after changed/deleted/disabled plan or assignment, keep-all and healthy subsets | Go service regressions | Passed; admission and execution both revalidate policy |
| Three real snapshots from different UTC days, daily 1 → 30 → 1 | Native restic | Passed; expansion preserves all three, later reduction actually forgets two; no duplicate capture/upload |
| Old maintenance retry after a newer upload failure, including restart/pruned history | Go regressions | Passed; maintenance cannot replace copy chronology |
| Changed failure time/scope/hold between prune admission and execution | Service and HTTP tests | Passed; missing/stale review returns dedicated 409 |
| Linux archive/backup suite and privileged helper tests | Maintained `run-linux.sh`, Debian 13 arm64 | Passed |
| ACL/xattr/hardlink/mode-000 replacement, PostgreSQL exact replacement, scope refusal and SIGKILL during extraction | Maintained privileged Linux fixture | All five assertions passed |
| Live root-owned lock versus non-root Stacklab; same-UID live/dead lock | Native restic and service jobs | Passed; EPERM blocks unlock and the dead owner's lock is removable |
| SFTP interruption → stale exclusive lock → guarded unlock → full check → truncated encrypted pack → restored bytes/full check | Owned sshd, TCP fault gate, 64 MiB data, restic 0.18.0 | Passed, 39.31 s; operational failure and actual corruption have different effects |
| HTTPS S3 full check → truncated pack → integrity failure → restored bytes/full check | Owned rclone 1.75.2 server and pinned fixture CA | Passed on macOS/restic 0.19.1 and Debian/restic 0.18.0; final Linux run 10.78 s |
| UI current evidence, token invalidation, stale-review 409, unlock refresh and maintenance eligibility | Vitest plus Chromium Playwright | New regressions passed; Playwright 10/10 passed, including mobile submitted request payload |
| Supported minimum restic 0.14.0: missing index, truncated local/S3 pack, live/dead same/foreign UID lock, actual current-policy retention | Isolated official Linux binary on PATH | Passed; installed restic 0.18.0 unchanged |
| Supported minimum restic 0.14.0: interrupted SFTP with explicit cancellation, stale-lock recovery, actual pack truncation and restoration of original encrypted bytes | Same native SFTP fixture with version-specific interruption handling | Passed, 134.46 s; cancellation preserves proof without creating a hold, corruption records failure |
| Installed `.deb` upgrade from review5; legacy/current sudoers readiness and protected configuration recovery | Maintained upgrade harness | All five checks passed; nine plans/eight destinations and operator policy preserved |
| Actual capture, unlock, full/test/selected recovery, reviewed ACL apply and native corruption with stale/current prune review | Maintained installed API harness | Ten assertions across 12 jobs passed; two check jobs intentionally failed on damaged data |
| Installed desktop/mobile inspection including real failed integrity, exact-name cleanup acknowledgement, refresh/selection and restore provenance | Maintained browser harness, no API mocks | 21 assertions passed, 16 screenshots; session revoked and local credential file removed |

Required CI subsequently passed all four jobs on [`3c466e6`](https://github.com/krbob/stacklab/actions/runs/38704460659), including all 665 frontend tests. The next audit-only revision
[`399d6ee`](https://github.com/krbob/stacklab/actions/runs/38707086800) also passed all four jobs.

The local full frontend run passed 664/665 tests: only the existing 101-interaction
selection-boundary test exceeded its 15-second limit on the heavily loaded host.
The same test passed alone without changes to its assertions or timeout. Typecheck,
lint, production build, bundle gate and generated-client consistency passed. This
local limitation is separate from the required clean CI run on the final pushed
revision; it is not reported as an all-green local suite.

The package is `2026.10.0~review5fix` (runtime `2026.10.0-review5fix`). SHA256:

- Debian package: `17b5b8668e867107a4c8c62918eab4982fee22c9a706163e46b1a21aa76b8fad`.
- Linux backup test binary: `3983b64fcf7834967799bd19af320669a52d64f486d2f0abe0731e2e7d687ed1`.

The installed backend SHA256 is
`bcbd2a0dbb8f7c2e87f89cc88d9069edb26a37afbc972babe1d876230ae24fd6`.
The API fixture removed only its own plans, destination, restores and files, and
confirmed existing configuration/pause unchanged. The browser explicitly skipped
three absent states: unavailable helper, system-caused pause and a check hold.
Hold behavior remains covered by UI/service regressions; its absence is not a
passed installed-browser assertion. Desktop/mobile screenshots were inspected.

The final SFTP fixture refinement (`044d686`) is test-only. Restic 0.14 can keep retrying
interrupted pack reads beyond the fixture's 60-second outage budget; only that
version is explicitly cancelled at the deadline. The fixture waits for command
exit before restarting SSH, then verifies guarded unlock, a successful full check,
real pack corruption and recovery. This proves cancellation and recovery on 0.14,
not a naturally returned transport error within 60 seconds. Production deadlines
were not changed. Its binary SHA256 is
`04880f5fc972570d4e65a32495a98c1024a685eb9dde22525dabb481734c3b9c`, built from
`cae4cdbe66d639c66ff51f07953aaab3f04cd606` plus the recorded SFTP-test patch.
The same binary passed again on installed restic 0.18.0 in 39.34 s, including
the natural `unavailable` outcome without the legacy cancellation branch.

Local evidence lives under `/tmp/stacklab-r5-linux`, `/tmp/stacklab-r5-release`,
`/tmp/stacklab-r5-coverage`, `/tmp/stacklab-r5-browser`,
`/tmp/stacklab-r5-restic014-sftp`, `/tmp/stacklab-r5-restic018-sftp`,
`/tmp/stacklab-r5-api-result.json` and
`/tmp/stacklab-r5-*.log`. Frontend agent commands
were captured in the development session; their stdout was not separately saved.
The Playwright last-run artifact is `/tmp/stacklab-r5-playwright/.last-run.json`.
Private installed upgrade/API archives remain on the disposable VM under their
run-specific `/root/stacklab-backup-*` directories. Credentials/session files are
not committed or included in shareable evidence.

### Retained Interpretation Limits

S3 fixtures verify actual HTTPS read/check diagnostics using a local S3 server;
they do not establish every cloud provider's behavior or repeat the full upload
pipeline. The newer rclone is an isolated fixture binary on PATH, not an installed
runtime upgrade. Native checks cover the tested restic versions; future diagnostic
wording remains guarded by unresolved-check holds when recognized as inconclusive.

Unlock preflight is not atomic with external clients. Unknown/foreign-host owners
and live local PIDs are conservatively refused; PID reuse or different namespaces
with identical hostnames require operator coordination. No force unlock, privileged
restic invocation or automatic repair was added. Historical proof remains dated;
manual acknowledged prune never clears failed integrity or a hold.

## Round-Four Operational Recovery Validation — 2026-10-14

The [fourth-round contract](../product/backup-operational-recovery.md) was committed
before implementation (`680d437`). Backend changes are in `93f5d44`, `5e598c4` and
`f6f9be7`; the UI is in `3094cf7`, with final explanatory copy and maintained
installed fixtures in `86ad7dc`. The final production revision exercised on the
VM is `86ad7dca88efb99141fa1dec0a87311f2ca5ca9d`.
The round-four delivery ended at `78f8764acf1c5f04b42441a7999d9504fc12de6b`;
all four jobs passed in [CI run 38587596008](https://github.com/krbob/stacklab/actions/runs/38587596008).
The final commits changed validation/browser tooling and documentation, not the
production revision exercised above.

| Exercise | Evidence type | Result |
| --- | --- | --- |
| Explicit corruption versus interrupted/mixed transport diagnostics; both historical migration forms | Deterministic Go regression | Passed; failed attempts preserve dated evidence |
| Maintenance retry after staging expiry/removal and unavailable transport, pause/restart/cancellation, progress events | Deterministic Go regression | Passed; verified copy and transfer timestamps remain intact |
| First outstanding deadline with missing/unverified/unavailable evidence | Deterministic Go regression | Passed; overdue remains independent of evidence usability |
| Frozen restore provenance, selected-source validation, bounded pre-admission index reads and hardlink rejection | Go regression plus native encrypted catalog test | Passed; ordinary short-ID restores retain the full immutable ID |
| Unlock admission/authentication, prune acknowledgement and OpenAPI route coverage | HTTP/contract regression | Passed |
| Full backend suite, formatting and vet | Portable Go suite/vet | Passed with Go 1.27.1; backup package 98.031 s |
| Frontend unit suite, generated contract, typecheck, build and asset checks | Frontend unit/contract/build | 652/652 passed with Node 24.21.0; final explanatory copy also passed 77 focused tests |
| Repository hygiene, documentation links, release scripts, lint and production dependency audit | Static/build checks | Passed |
| Desktop/mobile regression fixtures | Mocked browser regression | Nine Chromium scenarios passed |
| Full backup suite and privileged helper suite | Native Linux plus deterministic tests | 147 backup and 16 helper top-level tests passed; five explicit opt-in skips |
| ACL/xattr/hardlink replacement, PostgreSQL exact replacement, filtered/managed-root refusal and actual extraction interruption | Native Linux | Five maintained integration checks passed |
| Interrupted SFTP full check, stale lock after server return, Stacklab unlock and successful full check | Native restic plus service jobs | Passed in 23.06 s; prior integrity evidence survives interruption and unlock |
| Encrypted pack corruption, fresh-service recovery and active/stale locks with minimum restic 0.14.0 | Native Linux | Three tests passed in 30.711 s |

The Linux runner used the clean final production revision, Debian 13 arm64,
restic 0.18.0, GNU tar 1.35 and Docker 26.1.5. Its source archive, empty working-tree
patch, hashes and raw logs are at `/tmp/stacklab-backup-e2e-r4-20261014a`.
The PostgreSQL fixture restored 1000 captured rows, removed 500 later rows and
the later table, and preserved the target directory inode. The SIGKILL exercise
proves helper rollback preservation and release refusal after actual writes;
application journal coordination remains a separate deterministic test boundary.

The opt-in SFTP fixture uses a dedicated SSH container and a byte gate: it stops
the server after at least 8 MiB of the second full read of a 64 MiB random capture.
The final clean-revision evidence is `/tmp/stacklab-r4-sftp-recovery-c`; test binary
SHA256 is `cca4d4fe939069705cd3e7ce6148dabd73601a3e8ddfd15660c85b5ce9097ae3`.
The first attempt failed during SSH provisioning because the VM could not reach
Debian mirrors. A maintained, preloaded SSH image resolved provisioning. An
intermediate successful run recorded a working-tree patch; the final run was
repeated from the committed revision with an empty patch. This fixture exercises
native restic and service jobs, not HTTP admission or browser actions.

Minimum-version evidence is at `/tmp/stacklab-r4-minimum-restic`. The official
restic 0.14.0 arm64 archive was checked against its published SHA256SUMS, then used
through an isolated PATH; the installed restic 0.18.0 remained unchanged. The test
binary came from `93f5d44d95de71bfa12a4c3e03a7c9fdb5d63eb1` with SHA256
`90615c319f3afbb78bbb3a72f2f060afe79da650df728f7c712d0af5bfebc769`.
It covers the classifier, corruption persistence and lock behavior introduced in
that commit, rather than claiming every later UI/admission change was tested on
the minimum version. Portable native tests also ran with restic 0.19.1.

Final full checks exposed a missing unlock entry in the HTTP test's route inventory
and two existing asynchronous frontend test races (clipboard confirmation and
terminal reconnect). The inventory was corrected and the assertions now await
rendered state. The corrected full checks passed; no global timeout or assertion
was weakened. The backup selection-boundary test retains its own realistic timeout.

The installed upgrade from `2026.10.0~review4` to `2026.10.0~review4fix` passed all
five checks using the maintained harness from the clean production revision:
legacy-sudoers warning and detection, corrected readiness without another restart,
preserved configuration/eight plans/eight destinations/operator policy, and protected
configuration recovery under the installed service identity. Package SHA256 is
`5ff37933abf75ae1661b508c4865ecdafc1556292469f265b527d7a530122ade`.
Private evidence and the full previous-state archive remain at
`/root/stacklab-backup-upgrade-r4-20261014a` on the disposable VM. The corrected
sudoers and upgraded package remain installed; existing automatic-backup pause is
preserved.

The installed API exercise then passed seven jobs on the same revision:
initialization, capture, ordinary unlock, isolated full restore, full restore test,
selected-file retrieval and reviewed replacement. The ordinary restore requested
an eight-character snapshot prefix; its registered result retained the full 64-character
ID, `recovery_status=complete` and `classified_at`, with identical provenance in
the apply preview. Unlock left both integrity-evidence objects unchanged.
Full restore/test/apply preserved read-only modes, service-owned numeric IDs,
access/default ACLs, xattrs and hardlink identity; selected-file retrieval retained
its intended private 0600 mode. Replacement removed the unarchived fixture file.
All fixture resources were removed, existing configuration and policy remained
unchanged, and test-session logout succeeded.

The committed API harness SHA256 is
`91e48c63e8f2f45055e39417dda47654ce60dd909b2713dd8f8a819c63cd022b`.
Installed application SHA256 is
`355c77b259370583e68a32b6e3b1c1b4e15cffd06de9431dfb8e16a62bb38c2d`.
Private logs, job events and the executed script remain at
`/root/stacklab-backup-api-r4-20261014a`; the local copy and cleanup summary are
at `/tmp/stacklab-r4-live-api`. This run used the committed harness, not a later
edited equivalent.

The first installed browser run passed thirteen assertions, including full-check
default/cost guidance, explicit unlock review and exact-name prune acknowledgement,
then stopped on an ambiguous harness selector matching both visible and retained
hidden restore controls. The browser session was revoked even on that failure.
Commit `7ae77c7` changes the selector to the visible accessible combobox; the
application did not change. The original evidence remains at
`/tmp/stacklab-r4-live-ui`.

The corrected committed browser harness then passed all eighteen assertions with
no skipped checks on the installed package, at 1440×1000 and 390×844. It used a
healthy local repository for snapshot selection and a separate known data-failure
fixture for check/prune decisions. The recovery library and apply dialog displayed
the retained legacy result's `unknown` provenance; the installed API exercise above
separately establishes newly persisted `complete` provenance. All sixteen screenshots
and `results.json` are at `/tmp/stacklab-r4-live-ui-final`. There were no page/asset
errors or horizontal overflow. The harness blocks browser mutations, so these are
dialog/read-flow assertions; actual unlock and restore/apply ran in the API fixture.
Both browser sessions were revoked and their local/remote authentication files
removed. Final harness SHA256 is
`41cc84137eecef3424c5c59c167f9a6cfa5e6ff4535866a64e6019369c644f33`.

The Linux suite explicitly skipped protected installed configuration, externally
configured SFTP/S3 endpoints, the opt-in SFTP interruption and the slow watchdog.
The interruption ran separately above; installed configuration is exercised by
the package harness. S3/provider endpoints and the long watchdog were not rerun in
this round. These local evidence directories are not public CI artifacts. Keep
credentials, authentication state and recovery archives private; review and redact
logs before sharing. CI coverage/race artifacts remain tied to their exact source
revision and expire after 14 days.

## Round-Three Repair Validation — 2026-10-14

The [third-round contract](../product/backup-evidence-and-recovery.md) adds scoped
integrity evidence, first-outstanding protection deadlines, durable snapshot
classification, ACL recovery and UI refresh regressions. Backend implementation
is in `78893f1`; final frontend implementation is in `7f6945b`.

The maintained Linux runner executed clean revision
`b361b4d1ac293481a0e973ca4798371e41e18291` with an empty working-tree patch, Go 1.27.1,
Debian 13 arm64, GNU tar 1.35, restic 0.18.0 and Docker 26.1.5. The subsequent
`7f6945b` change affects frontend polling error presentation only. Evidence is
retained locally at `/tmp/stacklab-backup-e2e-r3-20261014a`; this directory is not a
published CI artifact. Source archive, binary hashes, versions and raw logs are
included. The fixture creates and removes its own configuration, spool and data.

| Sequence | Evidence type | Result |
| --- | --- | --- |
| Non-root capture/restore of inherited POSIX ACLs, file 0444, directory 0555, xattrs and links | Native Linux | Passed |
| Root replacement with ACLs, xattrs, hardlinks and a mode-000 directory | Native Linux | Passed |
| Actual encrypted restic data-pack corruption → failed full check → successful metadata check → repaired bytes and successful subset → full check | Native Linux | Failure stays visible until the full successful check |
| Overdue observation through multiple occurrences and restart, with notifications enabled/disabled | Deterministic Go regression | Passed; no renewed grace or false heartbeat |
| Negative classification cache, restart, transient error, 64-candidate budget and inventory larger than 4096 | Deterministic Go regression | Passed; bounded state and forward progress |
| Successful copy with failed retention, maintenance-only retry, retry while paused | Deterministic Go regression | Passed; verified copy remains usable and skipped work cannot erase failure |
| Full native backup suite and root helper suite | Linux suite: native + deterministic | Passed |
| PostgreSQL exact replacement | Native Linux | 1000 captured rows restored; 500 later rows and the later table removed; target inode retained |
| Filtered replacement and managed-root refusal | Native Linux | Passed; explicit merge preserves excluded live data |
| Helper SIGKILL during actual extraction | Native Linux | Rollback and partial target retained; release refused; fixture container stays stopped |
| Portable backend, formatting and vet | Portable Go suite/vet | Passed with Go 1.27.1 |
| Frontend tests, generated API contract, typecheck, build, asset checks and lint | Frontend unit/contract/build checks | Passed; final suite 637 tests with Node 24.21.0 |
| Browser regression fixtures | Mocked browser regression | Eight Chromium desktop/mobile scenarios passed |

The SIGKILL fixture exercises the helper's write boundary; it does not simulate
the application's service journal. Application journal admission and recovery
remain covered by deterministic backend tests. SFTP/S3 endpoint exercises,
minimum-version restic integration and the slow-transfer watchdog were not rerun
for this round; the earlier watchdog evidence and its limits remain below.

The first installed upgrade exercise retained a 5.88 GB private state archive and
passed detection of legacy sudoers and the package upgrade warning. Its old
100-second readiness wait expired before the next eligible one-minute observer
tick: all six helper probes succeeded at 120 seconds. The harness now permits
180 seconds, accounting for the one-minute failed-result cache, observer cadence
and probe execution. This was a test deadline correction, not evidence of a
successful initial upgrade acceptance run. Initial results remain preserved at
`/root/stacklab-backup-upgrade-r3-20261014a`.

The corrected installed upgrade exercise then passed all checks for
`2026.10.0~review3fix2`, built from production revision `7f6945b`:

- the package warned about legacy sudoers, and runtime readiness detected them;
- installing the current packaged example restored readiness without another restart;
- existing configuration bytes, all seven plans/destinations and pause/host-review
  policy were preserved;
- protected service-configuration recovery passed under the installed service identity.

Package SHA256 is
`82a6ed5ef270e8dc634728df3f1582c9703cdaeec019c1d5f7b709b61b5efb7f`.
The final harness revision is `a3862cf`; private upgrade evidence remains at
`/root/stacklab-backup-upgrade-r3-20261014b`. The archive includes credentials and
must not be published. The test deliberately leaves the upgraded package and
reviewed current sudoers installed.

A read-only browser inspection of the installed final package passed twelve
assertions on desktop and a 390 px mobile viewport: scoped integrity evidence,
helper readiness, manual capture during operator pause, immutable repository
fields, real Local2 snapshot classification and preserved selection, with no
JavaScript/asset failures or horizontal overflow. Eleven reviewed screenshots and
`results.json` remain locally at `/tmp/stacklab-r3-live-ui`. The test session was
revoked and authentication files were removed. Concurrent API fixtures meant the
browser did not assert fixed plan/destination counts. Initial smoke attempts were
corrected for two fixture assumptions: `/api/meta` exposes application version,
not the package commit field, and destination selection must use identity rather
than a presumed English display name. The successful run followed those corrections.

The installed API ACL exercise also passed on `7f6945b084bc`, using a host process
and fresh dedicated sources/repository. All six jobs succeeded: initialization,
capture, isolated full restore, full restore-test, selected-file restore and
reviewed replacement. Full restore/test/apply preserved content, modes, access/default ACLs, user xattrs
and hardlink identity. Numeric ownership matched for the service-owned fixture;
isolated extraction intentionally uses the service identity rather than restoring
arbitrary archived owners. Privileged apply restores archived ownership. Replacement removed
an unarchived target file. Selected regular-file retrieval preserved content and
used the intended private inspection mode 0600; it does not reproduce archived
ownership/ACLs. The original metadata remains in the source archive.

The [maintained API fixture](../../scripts/dev/backup-e2e/README.md#installed-application-acl-recovery)
records package/binary identity, jobs and assertions. Successful evidence is at
`/root/stacklab-backup-api-r3-20261014-final4`. It removed its own source/target,
repository, plan and restored results, preserved the original seven plans and
seven destinations plus operational policy, and logged out. Earlier fixture
attempts correctly exposed two test assumptions: tar hardlink members are not
regular-file selections, and a long-lived administrative container with a writable
`/:/host` mount is itself a writer to the recovery target. The final fixture selects
an independent regular member and runs on the host; it retains the no-writers guard.

The executed ACL script hash was
`a60ae3746b0ead184335a201d5ec9b5c9d318e61edd6445c76981b8c2705d4a8`;
the version committed in `52d66d5` has SHA256
`8d8a1e0da71d9c2b42d19ecc53aa09b9aa1797b22f89be70be917e3318c93747`.
The sole difference makes logout failure fail the overall result and exit status;
capture/restore/apply assertions are identical. They are not identical artifacts.

The PR Quality workflow installs restic, rclone, ACL and xattr tools, runs backup,
job and notification race tests, and runs the helper race binary as root with
isolated test paths. Its exact-revision coverage artifact contains logs and
reports; backup coverage is reported rather than a dedicated percentage gate.
Local package/state archives, credentials and browser sessions are never CI
artifacts. The successful round-three run is
[38568192686](https://github.com/krbob/stacklab/actions/runs/38568192686),
source `52d66d54731a92c256079cc2a7f04ebe3f212914`. CI artifacts expire after
14 days; release evidence should be retained separately after redaction.

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
