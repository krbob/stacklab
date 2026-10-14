# Backup integration on an isolated Linux host

`run-linux.sh` builds the current working tree for Linux, copies Go test binaries
and a dedicated helper to a disposable Docker host, and records source revision,
working-tree patch, toolchain, host versions, binary hashes and raw test logs.
The host needs GNU tar, restic, POSIX ACL/xattr tools (`acl`, `attr`), Python 3, Docker, `debian:trixie-slim` and
`postgres:17-alpine`. It does not need Go. Run the native backup suite as a
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

## Installed package upgrade

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
repository, manual plan and recovery results. It verifies six successful jobs:
repository initialization, capture, isolated full restore, full restore test,
selected-file recovery and reviewed replacement. Full recovery and reviewed apply
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
