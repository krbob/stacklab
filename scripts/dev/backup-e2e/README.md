# Backup integration on an isolated Linux host

`run-linux.sh` builds the current working tree for Linux, copies Go test binaries
and a dedicated helper to a disposable Docker host, and records source revision,
working-tree patch, toolchain, host versions, binary hashes and raw test logs.
The host needs GNU tar, restic, POSIX ACL/xattr tools (`acl`, `attr`), Python 3, Docker, `debian:trixie-slim` and
`postgres:17-alpine`, plus an installed Stacklab package for version reporting.
It does not need Go. Run the native backup suite as a
non-root account so the real restic exit-code-3 permission fixture remains valid.
The account must be allowed to administer Docker: the runner explicitly uses
Docker plus `chroot /host` for privileged tests.

```sh
scripts/dev/backup-e2e/run-linux.sh \
  test-user@test-vm unique-run-id /tmp/backup-evidence-unique-run-id arm64
```

Use an SSH agent/key. Password authentication is available through the
`SSHPASS` environment variable and locally installed `sshpass`; do not put
credentials in scripts or evidence files. A new host key is accepted, while a
changed existing host key fails.

The fourth argument accepts `arm64` or `amd64`. Choose a fresh run ID and artifact
directory for each execution; existing paths are refused. The privileged fixture
accepts only `/var/lib/stacklab-backup-e2e-<id>` as its workspace and refuses an
existing directory. It compiles helper configuration/spool paths into a separate
fixture binary and never changes the installed Stacklab helper, sudoers,
configuration, state, repositories or containers. The PostgreSQL container has a
unique name, a fixture ownership label and no published network ports.

The suite checks:

- native index repair (`rebuild-index` on restic 0.14–0.15, `repair index` on newer
  versions), followed by full verification and actual Stacklab archive recovery;

- the full native backup test binary, including real encrypted repositories and
  non-root incomplete/partial snapshot retry and retention;
- the helper test binary as root, including descriptor, capacity and rollback
  regressions;
- each privileged recovery action's read-only protocol probe;
- read-only files with inherited POSIX ACLs under the non-root service identity,
  plus privileged replacement preserving ACLs, xattrs, hardlinks and a mode-000 directory;
- stopped PostgreSQL capture followed by a populated exact replacement: restored
  row checksum/count, removal of later rows and a table, and unchanged target
  directory inode;
- refusal of filtered/unknown-scope replacement and managed-root replacement,
  with an excluded live file preserved through explicit merge;
- an actual `SIGKILL` of the helper process group after phase `writing` **and** a
  newly extracted nonempty target file exist, followed by assertions that
  rollback and partial target data remain, release refuses the interrupted
  record, and the fixture service remains stopped.

The final scenario covers privileged extraction interruption. It does not
simulate the application's service journal; journal admission, fencing, missing
writer inspection and explicit acknowledgement have maintained Go regressions.
The script stops only its own PostgreSQL container and always removes that
container. On failure it preserves its workspace and prints the path for
inspection. After diagnosis, remove only that exact workspace and its matching
`/tmp/stacklab-backup-e2e-<id>` directory. A successful run removes both after
copying evidence. No generic wildcard cleanup is used.

Review `backup-tests.log` for skips: SFTP/S3 require independent opt-in endpoints,
and protected installed service configuration requires a separate helper-enabled
exercise. A successful run does not claim those scenarios ran. `integration.json`
lists individual successful assertions; its `succeeded` field must be true.

Package upgrades are a separate integration concern. Build the current release
artifact and Debian package with `scripts/release/build-artifact.sh` and
`scripts/release/build-deb.sh`, then use the maintained
`scripts/release/run-systemd-deb-smoke.sh` in a disposable systemd host/container.
Do not treat these fixture helper tests as evidence that an installed sudoers
policy was upgraded. Preserve the old installed helper/configuration/state before
an additional in-place VM upgrade exercise, and record package versions and
whether the old sudoers policy was detected by helper readiness.

The opt-in `TestNativeSFTPInterruptedInventoryRecovery` uses an SFTP OPENDIR gate
before `sftp-server` lists repository data, then stops its owned sshd container.
It complements `TestNativeSFTPInterruptedCheckRecovery`'s pack-read byte gate. Both
use `STACKLAB_TEST_SFTP_RECOVERY=1` and optionally `STACKLAB_TEST_SFTP_IMAGE` with a
prebuilt SSH image. Both observe the production native runner, including SIGINT and its bounded escalation. The fixtures inject a two-second backend failure budget instead of the production ten-minute budget, and assert availability failure with prior proof retained. They accept successful graceful lock cleanup; when a stale lock remains they assert the specific minimum-age refusal, age only the fixture clock, unlock and reset the clock before rechecking.

`STACKLAB_TEST_FOREIGN_UID_UNLOCK=1` enables real root-owner and private-PID-namespace
lock tests on disposable Linux/Docker. The namespace fixture requires a privileged
container to select a high namespace-local PID. It keeps the host hostname, proves
the PID is alive inside the namespace and absent outside, and asserts fresh-lock
refusal. Dead-owner recovery simulates 31 minutes passing in the Stacklab test clock;
restic still reads the original native lock. No production age bypass is added.

Run the installed API harness directly as a host process (root SSH, a VM guest
agent or a detached transient systemd service), not inside an administration
container that bind-mounts the host root. Writer discovery correctly treats that
live container as a potential writer and refuses reviewed replacement. Exit any
such administration containers before the apply phase.

The installed API harness requires automatic backups to be paused. Besides ACL
recovery and byte-restoration corruption checks, it stops Stacklab, copies its own
repository independently, removes that repository's index, and runs a real index
repair and full check as the installed service user. It restarts Stacklab and checks
API-driven full verification and restored ACL/xattr/content again. Existing plans,
credentials and repositories are preserved. Its private evidence directory retains
the independent fixture repository copy; remove it deliberately after inspection.

## Installed package upgrade

On a disposable VM subscribed to the Stacklab APT repository, prevent unattended
upgrades from replacing the candidate during validation. Record
`dpkg-query -W stacklab` before and after the exercises. Set `sudo apt-mark hold stacklab`
after **each** manual `dpkg -i` installation, which can clear the previous hold,
and verify it with `apt-mark showhold`. The API harness also compares installed
commit, package version and binary hash at the beginning and end.

`upgrade-installed.py` is a separate, intentionally invasive exercise for a
disposable VM with an existing Stacklab package. It stops and upgrades the
application and briefly replaces its backup sudoers policy with a narrow legacy
fixture. It does not reset authentication or change existing source data. Before
mutation it archives `/etc/stacklab`, `/var/lib/stacklab`, the complete helper
spool, backup sudoers and `/usr/lib/stacklab` into a new root-only backup directory.
Allow enough disk space for that uncompressed copy.
This installed-package harness requires the default `/var/lib/stacklab` data
directory and rejects a custom data directory before authenticating or changing
the installed service. The isolated harness supports its own dedicated paths.

Run as root, supplying an absolute `.deb` path and the clean revision's Linux
backup test binary:

```sh
python3 scripts/dev/backup-e2e/upgrade-installed.py \
  --deb /tmp/stacklab_candidate_arm64.deb \
  --backup-dir /root/stacklab-backup-upgrade-unique-run-id \
  --backup-test /tmp/backup.test \
  --password-file /root/private-test-password
```

Supply the current login password through a root-only file or `--password-stdin`.
If neither is specified, an existing `STACKLAB_BOOTSTRAP_PASSWORD` is a fallback;
it must still authenticate. The script uses the credential only in memory and
refuses to reset the installed password. Sessions are revoked at completion by
default. If invoking
through a Docker/chroot administration shell, add `--network host` so the local
API URL reaches the VM rather than the container's loopback interface. The API
URL can be changed using `--base-url`.

The exercise verifies that legacy permissions produce `helper_ready=false`,
installs and validates the current packaged narrow sudoers example, and waits
for the periodic probe to report ready without another application restart. The
harness allows up to three minutes: the one-minute failed-readiness cache and minute
observer ticks can produce nearly two minutes of delay, plus the probe's
15-second budget. Healthy readiness is cached for 15 minutes.
It also checks preserved configuration bytes, plan/destination IDs, pause and host-review
state, then runs the protected-configuration integration test as `stacklab`.
The result and package hash are recorded in `result.json`; package output and
test output remain in the root-only backup directory. Only `--export-browser-session --browser-host test-vm` creates `browser-auth.json`
and retains that session for an optional live browser check. Log out and remove
the file after the check; it is private session material: never commit it or
include it in public evidence bundles. The upgraded package and corrected
sudoers remain installed. The previous state archive is retained for recovery. This script does not roll
back a package on failure: stop Stacklab, inspect `result.json` and `dpkg.log`, then
use the recorded source version to obtain the original `.deb`. Reinstall that
version before restoring matching configuration/state/helper-spool from `before.tar`
with ownership, ACLs and xattrs intact. Keep the current failed state separately
for diagnosis, and do not start either version until its state and policy match.
The archive also contains old application binaries for emergency recovery, but
restoring them alone does not reconcile dpkg's installed-version database.

Evidence directories from a local invocation are **not published CI artifacts**.
Share only reviewed result summaries, versions/hashes and redacted test logs.
Never upload `before.tar`, overview/policy exports, cookies or credentials. The
PR Quality workflow separately publishes its native/race/helper logs and coverage
under the exact checked source SHA; backup coverage is reported, not a dedicated
package percentage gate.

## Installed application ACL recovery

Run without unrelated repository jobs: configuration changes share the existing
global configuration lease. An HTTP 409 means admission was refused and is reported
with its actual method and server diagnosis. This fixture does not relax that lease.

`api-acl-installed.py` exercises the running application's authenticated API after
an installed-package upgrade. Run it as a host process on a disposable VM, with
GNU tar, restic and `acl`/`attr` installed and the packaged helper ready. It does
not upgrade the package or change the existing operator pause. The API must use
a localhost HTTP(S) origin; the default is `http://127.0.0.1:8080`.

Supply the expected candidate commit explicitly, along with fresh dedicated
workspace and evidence directories matching the run ID:

```sh
python3 scripts/dev/backup-e2e/api-acl-installed.py \
  --run-id unique-run-id \
  --expected-commit 0123456789ab \
  --work-dir /var/lib/stacklab-backup-api-unique-run-id \
  --evidence-dir /root/stacklab-backup-api-unique-run-id \
  --password-file /root/private-test-password
```

Replace the example commit with the installed candidate's Git revision. Both
expected and installed commit metadata must be valid hexadecimal IDs before
comparison. Authentication accepts a private password file or `--password-stdin`,
with the existing bootstrap credential as fallback. Passwords and session cookies
stay out of the evidence; the test session is logged out on completion.

The fixture creates its own managed configuration source and target, local
repository, manual plan and recovery results. It verifies seven successful jobs:
repository initialization, capture, explicit stale-lock removal, isolated full
restore, full restore test, selected-file recovery and reviewed replacement.
Unlock must leave integrity evidence unchanged. The ordinary full restore uses
an eight-character snapshot prefix; its result must retain the resolved full ID,
`recovery_status=complete` and `classified_at`. The apply review must preserve the
same provenance. Full recovery and reviewed apply
must preserve read-only file modes, numeric ownership, inherited access/default
ACLs, user xattrs and hardlink identity. Selected-file inspection deliberately
produces private `0600` copies: this assertion covers content and privacy, while
the untouched source archive retains original metadata. Replacement must remove
the fixture's extra unarchived file. The final cleanup uses the API to delete only
its own results, plan and destination, including results containing read-only
directories, then removes its own source/target/repository paths. Existing plan
and destination IDs and operator policy must remain unchanged.

These fixture files use the `stacklab` service UID/GID, so ownership equality in
isolated restore does not establish preservation of arbitrary foreign owners.
Unprivileged isolated extraction uses the service identity; the untouched archive
retains original ownership for privileged reviewed recovery. A failed test-session
logout marks the report unsuccessful and exits with an error, even when recovery
assertions passed.

Close recovery views during the exercise so browser inventory reads do not
compete with configuration cleanup. If administrative access is through Docker,
use a short launcher to start the Python command as a host `systemd-run` unit,
then let the launcher exit before the recovery preview. Do not run the complete
exercise inside a container with a writable host-root bind mount: Stacklab
correctly detects that container as a writer of the target. The harness refuses
apply whenever its target has container writers. Avoid other such administrative
containers during this brief apply window; read-only `systemctl` polling over SSH
does not create a writer.

`result.json`, per-job event logs and a copy of the invoked script remain in the
root-only evidence directory. The result records the installed package version,
source commit and application binary SHA256. On failure, fixtures remain for
inspection; use the recorded exact IDs and paths for cleanup rather than generic
wildcards. Audit/job history remains in Stacklab after successful cleanup. Review
the result before sharing it; credentials, sessions and existing application
state are never exported by this harness.

## Real transfer watchdog

The opt-in Go fixture runs real restic against temporary encrypted repositories
served by rclone through a localhost proxy. Install `restic` and `rclone` on a
Linux or macOS host, then run:

```sh
STACKLAB_TEST_SLOW_BACKUP=1 go test ./internal/backup \
  -run '^TestNativeTransferWatchdogControlledTransport$' \
  -parallel=2 -count=1 -v -timeout=9m
```

The two cases run in parallel and normally finish in about five and a half
minutes. One stalls pack uploads and requires the production watchdog to cancel
after five minutes without measured progress. The other holds final index
completion while byte and file counters remain complete and unchanged for five
minutes and twenty seconds. It then releases the request and requires successful
completion, `check --read-data`, `restore --verify` and an identical payload
SHA256. The canceled-upload repository must also pass `check --read-data`.

The fixture uses approximately 264 MiB of random source data plus temporary
repository/restored data, starts only its own rclone processes, listens on
loopback and Unix sockets, and removes its temporary directories and processes.
Each case has an eight-minute context deadline. It does not use Docker or change
installed Stacklab configuration. Restic must support `--stuck-request-timeout`;
the fixture sets this to ten minutes so restic's own backend deadline does not
mask Stacklab's watchdog. The initial successful run used restic 0.19.1 and
rclone 1.75.2 on macOS arm64.

This demonstrates bounded cancellation during a real stalled upload and survival
of delayed finalization beyond the old five-minute deadline. It does not measure
a general WAN transfer rate or wait for the thirty-minute finalization deadline;
deterministic watchdog tests cover that upper bound.

## Interrupted SFTP check and stale-lock recovery

The opt-in `TestNativeSFTPInterruptedCheckRecovery` owns a temporary Debian sshd
container, SSH keys, encrypted repository and TCP fault proxy. Run as a non-root
Linux account with Docker administration, `ssh-keygen`, restic and the
`debian:trixie-slim` image. It installs OpenSSH only inside its dedicated container.
Use a committed source revision, cross-build the backup test binary if necessary,
and record its SHA256 and the installed restic version:

```sh
STACKLAB_TEST_SFTP_RECOVERY=1 ./backup.test \
  -test.run '^TestNativeSFTPInterruptedCheckRecovery$' -test.v -test.timeout=10m
```

If the test host cannot reach Debian package mirrors, build the maintained
`scripts/dev/backup-e2e/Dockerfile.sftp` on a connected Docker host, transfer the
image with `docker save`/`docker load`, and set
`STACKLAB_TEST_SFTP_IMAGE=stacklab-sftp-fixture:r4` alongside the opt-in flag.
For example, build it with `docker build -f scripts/dev/backup-e2e/Dockerfile.sftp
-t stacklab-sftp-fixture:r4 .`. Match the test host's architecture. The override
must provide Debian's OpenSSH server and the standard account-management tools;
it changes fixture provisioning, not the interruption or recovery assertions.

A healthy full check establishes evidence over 64 MiB of random data. A byte gate
interrupts the next full read after at least 8 MiB, then the fixture restarts its
SSH server, verifies the remaining stale lock, invokes Stacklab's ordinary unlock
job and requires a successful full check. The previous evidence must survive the
transport failure and unlock. It then truncates an actual encrypted pack on the
SFTP backend, requires an integrity failure, restores the exact bytes and requires
a successful full check. This exercises native repository service jobs, not
HTTP or browser admission. The fixture removes its own container and temporary
files; it never touches installed Stacklab state or existing Docker workloads.
The normal `TestNativeUnlockPreservesActiveLockAndRemovesKilledOwnerLock` fixture
separately verifies that a live owner's lock is refused.

`STACKLAB_TEST_FOREIGN_UID_UNLOCK=1 ./backup.test -test.run
'^TestNativeUnlockRefusesForeignUIDOwner$' -test.v` adds a Linux/Docker fixture with
root restic in host PID/UTS namespaces and Stacklab as the unprivileged test user.
It confirms EPERM preserves the live lock and removal works after that owner exits.
It owns only its temporary repository and container. Old-lock timestamps, malformed
owners, PID overflow, remote hosts and uncertain probes have deterministic tests.

The installed API ACL harness additionally truncates one of its own encrypted
packs, observes failed integrity through HTTP, rejects missing/stale prune review,
restores the exact bytes, accepts current acknowledgement, and confirms only a full
successful check clears the saved failure. These mutations never touch an existing
operator repository.

## Isolated S3 corruption check

`STACKLAB_TEST_NATIVE_S3_CORRUPTION=1 ./backup.test -test.run
'^TestNativeS3TruncatedPackIsIntegrityFailure$' -test.v -test.timeout=5m` owns an
HTTPS S3 server, pinned temporary CA, bucket and repository. It requires a fixture
rclone with `serve s3` and `--no-cleanup` (tested with 1.75.2); this is not a new
Stacklab runtime minimum. An isolated binary on the fixture process's PATH can be
used when the installed rclone is older.

The fixture prepares an encrypted snapshot locally, serves it through S3 and
exercises healthy full check, physical pack truncation, failed full check and
restored bytes followed by successful full check. Server restart after each
mutation clears its caches. This proves S3 read/check classification, not the
complete S3 upload pipeline or behavior of every cloud provider.

## Installed browser smoke

`browser-installed.cjs` runs against a real installed application with no API
response mocks. Supply a private Playwright storage-state file exported by the
upgrade harness, an explicit origin/version and a fresh output directory:

```sh
node scripts/dev/backup-e2e/browser-installed.cjs \
  --base-url http://test-vm:8080 \
  --auth-file /secure/browser-auth.json \
  --output-dir /tmp/backup-browser-unique-run-id \
  --expected-version 2026.10.0-candidate
```

Use the repository's pinned Node version and installed frontend dependencies.
An optional `--destination-id` selects an existing local repository with a readable
snapshot; otherwise the harness discovers one. It checks actual desktop/mobile
rendering, immutable repository fields, scoped evidence and preserved recovery
selection. `--integrity-destination-id` requires a particular existing repository
with known data corruption for the full-check default and prune acknowledgement
checks. Without it, the harness discovers such a fixture when available and
records absent optional failure-state fixtures as skipped checks.
No backup configuration, source data or repositories are changed. Browser-originated
mutations are blocked; the harness's only intended mutation is revoking its own
session. It verifies revocation using the original session, removes the credential
file and treats logout failure as a failed run. Screenshots, result JSON, harness
hash and source commit are private local evidence and require review before sharing.

## Native retry exhaustion and diagnostic lifecycle

Fast SFTP/S3 fixtures still inject a two-second failure budget to verify watchdog
cleanup. They do not establish how restic's own retries end. Run the separate
exhaustion mode on both restic 0.14 and the installed stable version:

```bash
STACKLAB_TEST_NATIVE_S3_CORRUPTION=1 \
STACKLAB_TEST_NATIVE_CHECK_RETRY_EXHAUSTION=1 \
go test -count=1 -timeout=20m -v ./internal/backup \
  -run '^TestNativeS3TruncatedPackIsIntegrityFailure$'
```

This uses the production ten-minute Stacklab budget. Modern restic is explicitly
run with `RESTIC_FEATURES=backend-error-redesign=false` to reach its terminal
failure within ten retries; 0.14 already uses that retry policy. The fixture checks
for terminal backend/object diagnostics, not just `unavailable`. It covers S3 500
on listing and pack GET, partial responses, corruption mixed with transport, and
healthy checks after access recovery. Rclone 1.75.2 is a fixture dependency;
production still supports 1.60. Set `STACKLAB_TEST_DIAGNOSTIC_DIR` to an existing
private directory to retain the three owned S3 fault transcripts. Review and
sanitize native output before adding it to the corpus.

The same exhaustion switch is available with `STACKLAB_TEST_SFTP_RECOVERY=1` for
Linux SFTP fixtures. Observe the test timeout separately from Stacklab's budget.
Synthetic production-runner tests exercise cancellation residue deterministically;
a successful native run alone does not rule out those races.

If a browser cannot route directly to the VM, create an SSH tunnel using an account
with SSH access (`ssh -N -L 18080:127.0.0.1:8080 operator@test-vm`) and give the browser
harness `--base-url http://127.0.0.1:18080`. Direct VM URLs remain valid where routable.

## Installed service stop during SFTP checks

`service-stop-installed.py` runs as root on the exclusive disposable VM, with
backup schedules paused and a prebuilt `Dockerfile.sftp` image. It seeds its own
64 MiB encrypted repository and uses the installed API for full checks. A temporary
systemd PATH drop-in selects a wrapper that executes the real restic binary with
its native download limit, keeping a check active while storage and SSH stay
available. It verifies the wrapper is accessible to the service account and inspects
the real check child's `/proc` arguments, repository and service cgroup; an
unreadable wrapper can silently fall back to the system restic on PATH and
invalidate the interruption test.

```sh
python3 service-stop-installed.py --run-id unique-stop-id \
  --package /tmp/stacklab-candidate.deb --expected-commit EXACT_INSTALLED_COMMIT \
  --image stacklab-sftp-fixture:r4 --legacy-restic /usr/local/bin/restic-0.14 --early-lock
```

For each selected native version, it installs the candidate package and tests two
restarts while a lock exists and the check job is running. Supply `--initial-commit`
when starting from an older installed source; `--expected-commit` must identify
the final candidate. An explicitly identified older source may start with the pre-fix
`control-group` unit; the candidate must install effective mixed termination. This
supports testing an actual pre-fix package, not just reinstalling a corrected one.
The ordinary interruption cases require exactly `cancelled` and a `(cancelled)`
check-attempt diagnostic, zero remaining locks, unchanged dated integrity evidence, no new
hold, and a successful subsequent full check. The report distinguishes a version-changing package upgrade from reinstalling
the same version; both exercise the package's service restart path. The
report and package logs remain private under `/root/stacklab-stop-RUN_ID`.
The script removes its drop-in and restarts the ordinary service even on failure;
it preserves failed repository/container fixtures for diagnosis. On success it
removes its destination, repository and container and revokes its API session.

`--early-lock` adds three restarts immediately after lock publication per version.
These target the short creation window rather than the normal two-second delay;
scheduling can still move an individual interruption past that window. Restic 0.14
may leave one lock. The harness verifies it belongs to its captured child PID and
hostname, verifies process exit, records the limitation, and uses ordinary native
unlock on its exclusive disposable repository before continuing. This is explicit
fixture cleanup, not Stacklab automatic unlock. Other versions must leave no lock.

The harness records and restores the original APT hold after package installation
and in cleanup; it does not permanently hold an initially unheld package.

Temporarily stop the active `apt-daily.timer` and `apt-daily-upgrade.timer` during
exclusive installed validation, after confirming no package operation is running.
Record their original state and restore it afterward. `dpkg -i` can clear the
package hold, leaving a window before `apt-mark hold` in which unattended-upgrades
may replace the candidate. Preserve the original hold after every install and compare source,
version and binary identity before and after the exercise; a replaced candidate
invalidates the claimed installed test.

## Native diagnostic housekeeping and vanished metadata

`TestNativeCheckHousekeeping` runs when restic/rclone are installed. It creates an
owned repository, obstructs a local cache directory, and creates an invalid-name
symlink that native rclone reports as NOTICE. Healthy checks must establish proof
without a hold and publish fixed warning events. A symlink named as a valid object
ID must still create a hold. No subprocess output is fabricated.

Set `STACKLAB_TEST_NATIVE_MISSING_LOCK=1` to additionally run native `cat lock` on
a missing ID for local and REST/rclone. With `STACKLAB_TEST_SFTP_RECOVERY=1`, the
same check also runs in the owned SFTP recovery fixture. These cases can take over
a minute on 0.14 because native restic retries missing objects. Select an older
binary through a private PATH directory whose executable is named `restic`, for
example `PATH=/private/fixture/restic-0.14:$PATH`; do not replace the host binary.
