# Backup Setup And Recovery

## Prepare The Host

The backup panel reports the detected restic version (or explicitly says it is unknown).
Versions 0.14–0.16 have less precise backend diagnostics; the panel explains what
to do when an inconclusive check persists. Versions below 0.14 trigger an explicit
compatibility warning. Version detection never opens a repository.

Restic 0.14 can leave its own fresh lock when cancellation arrives during lock
creation, including manual cancellation, restart and package upgrade. This also
occurs in native restic without Stacklab; the installed tests did not reproduce it
with 0.18. Prefer a validated newer restic for normal operation. Stacklab still
refuses locks younger than 30 minutes: PID, hostname and time alone cannot prove
ownership across PID namespaces or host restarts. Investigate remaining locks
using the procedure below; no automatic fresh-lock removal is performed. An
unreachable backend can also prevent any restic version from removing its lock.

Install restic 0.14 or newer, rclone 1.60 or newer, GNU tar, OpenSSH client and
CA certificates. Debian packages depend on the tools and recommend `ca-certificates`.
Private CA certificates installed in the system trust store are supported. Tarball operators
install them through their distribution. Use the service account's environment
when testing; a command working in an administrator's shell does not establish
that Stacklab can read the source or access a repository.

The backend stays non-root. For protected managed paths and local Docker named
volumes, install the release's `stacklab-backup-helper`, copy and review the
shipped sudoers example at
`/usr/share/stacklab/examples/stacklab-backup.sudoers.example` with `visudo`, then set:

```ini
STACKLAB_BACKUP_HELPER_PATH=/usr/lib/stacklab/bin/stacklab-backup-helper
STACKLAB_BACKUP_USE_SUDO=true
```

The optional root-owned `STACKLAB_BACKUP_ALLOWED_PATHS` setting accepts a
comma-separated list of additional trusted bind-source directories; it is not a
browser-configurable host filesystem grant. Keep it narrow.

For tarball installs the binary is under
`/opt/stacklab/app/current/bin/stacklab-backup-helper`. Keep the helper and
`/etc/stacklab/stacklab.env` root-owned. Restart Stacklab after changing service
configuration. A custom systemd sandbox must allow access to the selected mounts
and private state, and retain the [host PID
namespace](#interrupted-remote-operations-and-stale-locks). `ProtectHome=true`
intentionally blocks home-directory
sources; use managed workspace paths. Ensure private staging has space for
capture, retained retries and a recovery exercise. Helper-backed recovery also
uses root-owned disk storage at `/var/lib/stacklab-backup-helper`; custom units
need this writable path and `NoNewPrivileges=false` for sudo. Do not put this
storage on an undersized tmpfs. Debian installation creates it automatically.
Tarball deployment through `host-tools/upgrade.sh` also creates it. For custom
deployments only, create it before enabling recovery:

```sh
sudo install -d -o root -g root -m 0700 /var/lib/stacklab-backup-helper
```

After every helper upgrade, compare `/etc/sudoers.d/stacklab-backup` with the
current packaged example. The package preserves operator-managed sudoers files;
older rules may permit capture but omit prepared recovery actions. Install the
reviewed rule root:root with mode 0440, run `visudo -cf` on it, and verify **In-place
recovery helper** in Backups. Stacklab probes every required action at startup,
before review and periodically; an incompatible helper or denied action reports
how to repair the installation. Healthy readiness is cached for 15 minutes, failed
readiness for one minute; the periodic observer can take nearly two minutes
plus probe execution to notice a repaired helper. Opening a recovery review
performs a fresh check. An
unconfigured helper is informational. Probes do not repair permissions or delete
old preparations. Do not replace the allowlist with unrestricted sudo.
Prepared recovery protocol 2 binds capture scope as well as mode and target;
old, unused preparations must be prepared again. Interrupted writes still require
inspection and their retained evidence must not be removed during upgrade.

## Configure Copies

Open **Backups → Destinations**, follow connection, credentials and verification,
and store the repository password in a
password manager independent of this host. Also retain endpoint/bucket/path,
transport credentials, SSH host keys, or the authorized Drive configuration.
The repository cannot unlock itself using a password stored only inside it.

Selecting **Stacklab recovery** includes credentials for **all destinations** and
`stacklab.env`, and copies them to **every destination selected for that plan**.
Use a separate state-recovery plan pointed at consciously trusted destinations.
Repository encryption protects these secrets in storage; possession of that
repository password grants access to the full recovered state.

- Local disk or mounted NAS: enter the repository directory, detect and review
  its filesystem identity. Verify the intended disk/share is actually mounted.
  The device identity is checked before every access; after device renumbering,
  verify the mount before updating it. Prefer a separate physical disk.
- S3/B2: use `s3:https://endpoint/bucket/prefix`, access key, secret and optional
  region. Stacklab does not provision cloud accounts or buckets. Restrict the
  provider key to the intended bucket/prefix where supported.
- SFTP NAS: an absolute directory uses `sftp://user@host//srv/repository`
  (two slashes after the host). `sftp://user@host/path` is relative to the SSH
  home directory. The wizard preserves this distinction; existing addresses
  are never rewritten automatically. Use a dedicated SSH key and pinned `known_hosts` data.
  Verify the host key through a trusted channel. Interactive password prompts
  are not supported.
- Google Drive: authorize a `drive` remote using native `rclone config` on a
  trusted machine, supply the authorized configuration and use
  `rclone:remote:folder`. See [rclone Drive setup](https://rclone.org/drive/).
  Authorization is performed through rclone; Stacklab stores the resulting
  private configuration.

Testing access and initializing a new repository are separate actions. Never
initialize merely because an access test failed: the reason may be an absent
mount, a network outage or incorrect credentials. Attaching an existing
repository does not authorize pruning unrelated snapshots.

Create a plan, explicitly select sources and exclusions, and choose file or stop
mode. An active database directory copied in file mode is not an
application-consistent backup. Select prepared database dumps or stop its
writers. Avoid excludes on physical database directories: excluding one required
file can make even a stopped capture unusable. Prefer a complete database source,
and separate cache/configuration sources if they need filters. Review newly added service mounts in the stack's **Backups** tab.
Assign destinations with independent schedules and retention. Run the plan
manually before relying on its schedule and perform an isolated restore test.

**Protection** is the default view. Check expected versus current copies and
last capture time, then inspect uncovered sources separately. A configured plan
alone does not establish protection. Manual plans show age; scheduled plans
show the next occurrence and overdue copies. Changing the selected data requires
a fresh capture. Use **History** to filter by plan, destination, outcome or date
and inspect capture, upload, verification and retention separately. Before a
manual run, review the current affected services, including shared consumers.
Stop-mode services resume after capture and archive validation; file indexing
and uploads then continue against the prepared bytes without blocking unrelated
stack operations. Upload file counters count
recovery objects, while the file browser describes application data.

Choose every N hours (1–168), daily or weekly schedules. Hourly intervals are
anchored in UTC; calendar schedules use the chosen timezone. The editor previews
actual upcoming times. Configure repository integrity checks separately from
backup cadence and choose metadata, 10% sample or full data read. Data reads can
incur provider transfer charges. Independent prune schedules move pack cleanup
out of the backup job; without one, retention keeps the existing immediate
forget-and-prune behavior. Automatic isolated restore tests remain future work
until their disk quota and cleanup policy are defined.
Saving a new or changed schedule waits for its next future occurrence; use
**Run now** for an immediate backup. An existing unchanged schedule catches up
one missed occurrence after restart, resume or a long-running job.
Changing an assignment's schedule resets its outstanding deadline and grace period;
the previous overdue condition no longer applies. Saving an unchanged schedule
preserves the existing overdue history.

## Failures And Retrying

Review the parent job and each destination result. Upload, verification and
retention are separate stages; one successful copy does not hide another failed
copy. A completed verified copy stays usable when retention fails; its maintenance
error appears separately. Retry maintenance reuses that snapshot without uploading
or verifying the copy again, even after staging has been removed or expired.
It uses the current enabled plan and destination assignment, not the retention
policy captured by the old run. Removing or disabling either blocks retry;
changing to keep-all resolves the old maintenance attempt without deleting data.
An old retention retry cannot supersede the outcome of a newer upload.
Retrying a failed transfer still requires the original prepared recovery set
within its configured lifetime; otherwise start a new capture. A failed repository check
needs investigation even if upload succeeded. Metadata verification and sampled
reads do not clear known data corruption. After repairing the repository, run a
successful full-data check to clear that failure. Automatic forget/prune stays
blocked while integrity has failed or a check hold is unresolved. Manual prune
requires acknowledgement bound to the current failure/hold in the dialog and API;
changed evidence invalidates the review. Prune is not a repair operation.

### Interrupted Remote Operations And Stale Locks

A lost SFTP connection can leave a lock after its owner exits. Restore connectivity,
wait for legitimate work to finish, and keep all external clients stopped. **Remove
stale locks** queues ordinary `restic unlock` under Stacklab's reservation. It only
considers local-host locks at least **30 minutes old** whose PID is absent. A live
PID (including EPERM), future timestamp, malformed metadata, uncertain probe or
foreign hostname produces `unlock_refused`. The lock list must also be unchanged
immediately before unlock. Ownership review is bounded to 128 locks. Unreadable or
vanished lock metadata also refuses the preflight, without declaring repository
corruption. Recognized access, credential and cancellation failures retain their
own diagnostics rather than becoming ownership refusals. Jobs record the remaining lock count. `unlock_incomplete` means ordinary
unlock ran but locks remain or the postflight inventory could not be read; it is different
from a preflight refusal. If the unlock command itself fails, its access/cancellation
diagnostic is retained; an unknown command outcome becomes `unlock_incomplete`,
not an inconclusive repository check. Some locks may already have been removed. Refresh the lock
inventory before deciding what to retry.

Run Stacklab and its restic subprocesses in the host PID namespace. Do not add
`PrivatePIDs=yes`, a private PID namespace through a wrapper, or a container with a
shared hostname but isolated processes. A hostname match or `/proc/1` comparison
cannot prove process identity across namespaces. The age threshold mitigates fresh
namespace locks; it does not prove an old owner is dead. PID reuse can conservatively
refuse a safe unlock. Independent clients can still create/refresh locks after the
last inventory; the preflight is not an atomic distributed lock. Keep clocks synchronized
on every client (for example, verify `timedatectl status` and its NTP synchronization);
clock skew makes age-based decisions unreliable.

For a foreign-host lock, inspect `restic list locks --no-lock` and
`restic cat lock LOCK_ID --no-lock` with the service credentials described below.
Confirm the owner has stopped on the actual host and PID namespace, then perform
ordinary unlock there. If that host is permanently lost, fence it and its scheduled
clients before any manual removal. After inspecting ownership and waiting until the lock
is at least 30 minutes old, try ordinary `restic unlock` as the service user with the
environment below, then re-list locks. Restic can remove aged foreign-host locks even
though the UI deliberately refuses them. Ordinary CLI unlock can also remove a
same-hostname lock of any age when its PID is absent in the CLI process namespace.
Stop every client, including containers sharing that hostname, before manual unlock;
waiting 30 minutes alone does not make a mismatched PID namespace safe. An unreadable lock first requires checking
credentials, storage access and the lock bytes; do not equate unreadability with
staleness. Preserve an independent repository copy and lock inventory. Only an
operator who has established that **every** client is stopped may use the installed
restic's documented `unlock --remove-all` as a last resort; it removes live locks
as well and bypasses the UI safeguards. Stacklab never executes that option.

After successful unlock, refresh inventory or run the required check. The previous
failed attempt remains history. Unlock itself does not establish integrity or clear
an existing failure/hold. A fresh-lock refusal requires waiting and investigating
ownership, not repeatedly pressing unlock.

### Check Outcome And Next Action

| Latest outcome | Meaning and next action |
| --- | --- |
| `unavailable`, `permission_denied`, `credentials_rejected` | Fix transport, access or credentials, then repeat the intended check. Dated integrity proof remains historical; this failed attempt provides no new proof. |
| `cancelled`, `timeout` | Inspect why work stopped; restore normal service and repeat the intended check. Coordinate any remaining lock owners before unlock. |
| `repository_locked` | Wait for legitimate work; inspect owners if the lock persists. Follow the stale-lock procedure above. |
| `storage_full` | Restore free space or quota without deleting recovery material blindly, then repeat the check. |
| `repository_missing` | Verify the address and mount/backend; do not initialize over a repository expected to contain existing copies. |
| `operation_failed` | Inspect the native operation privately with the same identity and credentials. This generic failure does not prove corruption. |
| `unlock_refused` | No unlock was attempted. Refresh vanished/changed metadata or resolve the specific ownership problem; do not infer storage corruption. |
| `unlock_incomplete` | Unlock was attempted, but its result or remaining inventory is uncertain. Refresh the lock inventory and inspect owners before retrying. |
| `check_inconclusive` / check hold | Preserve independent copies and run the scope required by the hold. If it recurs, record the panel's restic version, reproduce the native check privately with the same identity/credentials, and inspect the unknown diagnostics. Test a supported restic upgrade on an independent copy when legacy diagnostics prevent a reliable result. Do not repeatedly prune, repair or clear state to dismiss the warning. |
| `integrity_failed` | Preserve a complete independent copy, diagnose the specific damage, then follow the matching repair procedure below. A successful full check is required for data failures. |

Configuration edits can return HTTP 409 while snapshot/catalog browsing, comparison,
restore validation or background inventory holds a read reservation, even without a
visible job. Normal inventory runs hourly; failed inventory retries every five minutes
and can hold configuration for up to 90 seconds per snapshots request. Wait for the
read to finish and retry. Cancelling visible jobs does not cancel background work or
another browser's read. The API currently uses the general `conflict` code. Reservations
protect configuration globally; finer destination reservations remain roadmap work.

### Repository Integrity Failure

Distinguish an unsuccessful check attempt from an explicit data-integrity failure.
Network, permission and cancellation errors do not establish corruption; previous
dated evidence remains visible alongside the failed attempt. Upgrades preserve
ambiguous historical failures, but recognize stored non-integrity codes, including
already migrated unknown-scope failures, as unsuccessful attempts instead.
Unexplained repository diagnostics create a persistent **check hold**: dated proof
remains visible, but current protection is unverified and automatic deletion stops.
A successful full check clears any hold; a metadata check clears only a metadata
hold. A later transport error cannot clear it. Known data corruption still needs
a full successful check, even after a successful metadata or sampled check. An operational
record can explain a generic damage summary, but cannot explain an independent unknown
object error or a decode error merely containing EOF. After a connection failure, rerun
the check before prune or a retention change; the failed attempt establishes no fresh
proof.

Checks stop after exhausted backend reads or listing failures, independently of a
provider's wording. Explicit corruption inside a retried read remains corruption.
A pending listing retry remains uncertain until recovery, a terminal cause, or
progress into the next check phase establishes its outcome. Restic emits failed
listing causes before pack comparisons. A later snapshot failure cannot turn an
earlier missing pack into a listing outage merely by repeating its error text.
Starting data listing also establishes that index loading completed; an exhausted
listing cannot retroactively turn an earlier recovered index retry into failure.
Unrecognized intervening diagnostics preserve uncertainty. Unrelated download
failures do not invalidate a completed listing. Bounded retry-correlation eviction
is separate from evidence loss: hundreds of recovered legacy retries do not create
a hold when the check completes successfully.

| Restic version | Backend diagnostics used by Stacklab |
| --- | --- |
| 0.14–0.15 | Retry records and repeated terminal causes; legacy `pack … failed to download`. |
| 0.16 | Also reports successful retries. |
| 0.17 and newer | Also reports terminal `List/Load/Stat(…) failed:` records. |

Each reported failed operation has a ten-minute budget, checked on continuing
failure reports. Its success record clears the budget. An operation without reports
for more than six minutes expires independently of retries on other objects; a new
failure starts a fresh window. At most 128 operation keys are monitored at once;
excess keys do not share a synthetic deadline. Diagnostic evidence has separate
bounded state whose overflow conservatively prevents verification. Silence never
triggers this monitor. Quiet healthy checks retain the overall job limit; completely
silent backend hangs still depend on request timeouts and that limit. Restic 0.14
can remain silent for minutes after losing SFTP access.
If a check appears stuck, inspect backend health and native process activity before
cancelling and repeating it. Silence alone does not distinguish a large healthy
check from a hang; there is no universal safe number of idle minutes. Overall jobs
remain bounded at 24 hours. Consider upgrading legacy restic when this recurs.

On service stop or upgrade, Stacklab cancels workers before draining HTTP. The
packaged unit and migrated tarball service use `KillMode=mixed`: systemd initially signals Stacklab alone, so
restic can clean up its locks through a still-running SSH process. Cancellation
allows restic five seconds for cleanup before terminating its process group;
systemd retains its final service stop deadline. Stacklab waits up to 25 seconds for
workers; once its main process exits, systemd can immediately kill remaining children.
The tarball upgrader installs and verifies its shutdown-policy drop-in before restart;
see [tarball service migration](install-from-tarball.md#service-file-notes).
An automatic backend-failure stop preserves previously observed unexplained diagnostics and
corruption. Later cancellation residue cannot create a hold or establish missing
objects from interrupted inventory. Explicit corruption is retained even if emitted
after the stop request. Manual cancellation and service shutdown preserve concrete
damage but do not create a new hold from earlier unknown diagnostics.

Recognized cache failures and lock-cleanup warnings produce fixed job events without
invalidating an otherwise successful check. A cleanup warning can mean a lock remains;
inspect ownership before the next operation. Recognized rclone notices are limited to
default-config fallback and skipped symlinks with non-object filenames in repository
object directories. Skipping a valid object ID or an unknown NOTICE/INFO remains
inconclusive. Unknown diagnostics still create a hold even with exit zero. A JSON
diagnostic following a newline-free warning is interpreted separately.

During ordinary lock acquisition, a vanished foreign lock does not stop the check.
During unlock preflight, vanished metadata refuses unlock and requires an inventory
refresh. Local, S3, SFTP and REST/rclone formats are recognized. On restic 0.14, native
missing-object retries can approach the 90-second preflight command budget; a timeout
also refuses the operation safely. Refresh inventory before retrying.

For confirmed damage, use the following preparation and choose only the repair
matching the diagnosis. Stacklab intentionally does not automate repair.

1. Pause schedules, wait for jobs to finish, then `sudo systemctl stop stacklab`.
   Pausing alone still permits manual jobs. Stop every other repository client,
   including schedulers on other hosts. Resolve unreliable storage/transport first.
2. Make an **independent full repository copy**, including config, keys, snapshots,
   indexes and encrypted data packs, before mutating repair. A copy of only indexes
   and snapshots cannot undo changes to packs. Use a separate storage location and
   verify the copy completed; hard links to mutable files are not an independent
   copy. For remote storage, copy all raw repository objects to an independent bucket/path
   or use a server-side filesystem copy with all clients stopped; `restic copy` repacks
   selected snapshots and is not a byte-preserving repair backup. Keep the independent
   copy until repository and application recovery have been validated.
3. On an intact host, reuse the saved service credentials in
   `/var/lib/stacklab/backup/credentials/DESTINATION_ID/` (adjust for a custom data
   directory). The destination ID is available through the backup API. This directory
   contains `password`, `ssh_key`, `known_hosts`, `rclone.conf` and `credentials.json`; do
   not rotate or copy them unnecessarily. On a fresh host, use the saved [recovery
   credential bundle](#recovery-kit-and-fresh-host-ui).
   Run restic as the installation's service user (normally `stacklab`), not root.
   Root-created index files can be unreadable to the service. Do not recursively
   chown a shared repository; correct only identified repair-created objects after
   checking the intended backend ownership/ACL policy. For a local repository, inspect
   unexpected ownership with `sudo find /path/to/repository ! -user stacklab -ls`; review
   each result before changing it.
4. Record `restic version` and private check diagnostics. The version map below
   describes command availability, not a recommendation to use every command.
   Test a reviewed tool upgrade on the independent copy when the installed version
   cannot perform the needed repair. The supported Stacklab minimum remains 0.14.

A local-backend example with explicitly prepared, service-readable credentials:

```bash
sudo install -d -o stacklab -g stacklab -m 0700 /var/lib/stacklab-repair
sudo install -o stacklab -g stacklab -m 0600 /secure/repository-password /var/lib/stacklab-repair/password
sudo -u stacklab env RESTIC_REPOSITORY=/mnt/backup/stacklab \
  RESTIC_PASSWORD_FILE=/var/lib/stacklab-repair/password restic --no-cache check --read-data
```

Use the same identity and environment for the selected repair command. The examples
use `--no-cache` to avoid creating a second cache. Alternatively, set
`RESTIC_CACHE_DIR=/var/lib/stacklab/backup/cache/DESTINATION_ID` while Stacklab is
stopped. Preserve the exact saved repository URI, including the S3 endpoint.

For S3 and rclone, first create the private repair directory as in the local example.
On a fresh host, install the saved password there and set `RESTIC_PASSWORD_FILE`
to `/var/lib/stacklab-repair/password`. For S3, prepare a service-owned mode-0600 **shell environment file** at
`/var/lib/stacklab-repair/s3.env` with a private editor. Do not paste real secrets
into shell commands. Quote values using POSIX shell syntax; this trusted file is
sourced as code. On an intact host, obtain the three AWS values from that
destination's `credentials.json` fields `access_key_id`, `secret_access_key` and
`session_token`. Obtain the address and region from the destination API fields
`repository` and `region`.
The following is the file's shape, not a command to enter with real credentials:

```sh
RESTIC_REPOSITORY='s3:https://storage.example/bucket/repository'
RESTIC_PASSWORD_FILE='/var/lib/stacklab/backup/credentials/DESTINATION_ID/password'
AWS_ACCESS_KEY_ID='REPLACE_IN_PRIVATE_EDITOR'
AWS_SECRET_ACCESS_KEY='REPLACE_IN_PRIVATE_EDITOR'
AWS_SESSION_TOKEN=''
AWS_DEFAULT_REGION='us-east-1'
# When configured for the service, copy the same CA file/directory settings:
# SSL_CERT_FILE='/etc/ssl/certs/private-storage-ca.pem'
# SSL_CERT_DIR='/etc/ssl/certs'
```

Use the actual configured region, including an empty value when appropriate. Copy
custom CA paths from the effective service environment (`/etc/stacklab/stacklab.env`
and any systemd drop-ins; inspect `systemctl show stacklab -p EnvironmentFiles -p Environment`);
`sudo` does not reliably preserve them. Keep TLS verification enabled. Execute:

```bash
sudo -u stacklab sh -c 'set -a; . /var/lib/stacklab-repair/s3.env; set +a; exec restic --no-cache -o s3.region="$AWS_DEFAULT_REGION" check --read-data'
```

After coordinating stopped clients as described above, use the same environment
for lock inspection and ordinary unlock:

```bash
sudo -u stacklab sh -c 'set -a; . /var/lib/stacklab-repair/s3.env; set +a; exec restic --no-cache list locks --no-lock'
sudo -u stacklab sh -c 'set -a; . /var/lib/stacklab-repair/s3.env; set +a; exec restic --no-cache cat lock LOCK_ID --no-lock'
sudo -u stacklab sh -c 'set -a; . /var/lib/stacklab-repair/s3.env; set +a; exec restic --no-cache unlock'
```

AWS secrets are loaded into the subprocess environment, not its command arguments
or shell history. Keep the environment file private and do not use shell tracing.
For rclone, prepare `/var/lib/stacklab-repair/rclone.env` the same way, containing
`RESTIC_REPOSITORY='rclone:remote:path'`, `RESTIC_PASSWORD_FILE` and
`RCLONE_CONFIG='/var/lib/stacklab/backup/credentials/DESTINATION_ID/rclone.conf'`.
Include the same custom CA settings where needed. The config must belong to the
service user with mode 0600; on a fresh host install the saved config with
`sudo install -o stacklab -g stacklab -m 0600 /secure/rclone.conf /var/lib/stacklab-repair/rclone.conf`
and point `RCLONE_CONFIG` there. Execute:

```bash
sudo -u stacklab sh -c 'set -a; . /var/lib/stacklab-repair/rclone.env; set +a; exec restic --no-cache check --read-data'
```

An administrator's SSH agent/config is not inherited by the service. For SFTP on
an intact host, use the existing service-owned key and pinned hosts file. On a
fresh host, install each saved file with owner/group `stacklab` and mode 0600 in
the mode-0700 repair directory, including the password file. For example:

```bash
sudo install -o stacklab -g stacklab -m 0600 /secure/ssh_key /var/lib/stacklab-repair/ssh_key
sudo install -o stacklab -g stacklab -m 0600 /secure/known_hosts /var/lib/stacklab-repair/known_hosts
sudo -u stacklab env RESTIC_REPOSITORY=sftp://user@nas:22//backup/stacklab \
  RESTIC_PASSWORD_FILE=/var/lib/stacklab-repair/password \
  restic --no-cache -o 'sftp.command=ssh -F /dev/null -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/var/lib/stacklab-repair/known_hosts -i /var/lib/stacklab-repair/ssh_key -p 22 user@nas -s sftp' check --read-data
```

For lock review, keep the same backend options and substitute `list locks --no-lock`,
then `cat lock LOCK_ID --no-lock`. Only after the ownership procedure above, run
ordinary `unlock` and re-list. Local and rclone examples using the prepared files:

```bash
sudo -u stacklab env RESTIC_REPOSITORY=/mnt/backup/stacklab RESTIC_PASSWORD_FILE=/var/lib/stacklab-repair/password restic --no-cache list locks --no-lock
sudo -u stacklab env RESTIC_REPOSITORY=/mnt/backup/stacklab RESTIC_PASSWORD_FILE=/var/lib/stacklab-repair/password restic --no-cache cat lock LOCK_ID --no-lock
sudo -u stacklab env RESTIC_REPOSITORY=/mnt/backup/stacklab RESTIC_PASSWORD_FILE=/var/lib/stacklab-repair/password restic --no-cache unlock
sudo -u stacklab sh -c 'set -a; . /var/lib/stacklab-repair/rclone.env; set +a; exec restic --no-cache list locks --no-lock'
sudo -u stacklab sh -c 'set -a; . /var/lib/stacklab-repair/rclone.env; set +a; exec restic --no-cache cat lock LOCK_ID --no-lock'
sudo -u stacklab sh -c 'set -a; . /var/lib/stacklab-repair/rclone.env; set +a; exec restic --no-cache unlock'
```

For SFTP, keep the explicitly pinned SSH command on **each** invocation:

```bash
sudo -u stacklab env RESTIC_REPOSITORY=sftp://user@nas:22//backup/stacklab RESTIC_PASSWORD_FILE=/var/lib/stacklab-repair/password restic --no-cache -o 'sftp.command=ssh -F /dev/null -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/var/lib/stacklab-repair/known_hosts -i /var/lib/stacklab-repair/ssh_key -p 22 user@nas -s sftp' list locks --no-lock
sudo -u stacklab env RESTIC_REPOSITORY=sftp://user@nas:22//backup/stacklab RESTIC_PASSWORD_FILE=/var/lib/stacklab-repair/password restic --no-cache -o 'sftp.command=ssh -F /dev/null -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/var/lib/stacklab-repair/known_hosts -i /var/lib/stacklab-repair/ssh_key -p 22 user@nas -s sftp' cat lock LOCK_ID --no-lock
sudo -u stacklab env RESTIC_REPOSITORY=sftp://user@nas:22//backup/stacklab RESTIC_PASSWORD_FILE=/var/lib/stacklab-repair/password restic --no-cache -o 'sftp.command=ssh -F /dev/null -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/var/lib/stacklab-repair/known_hosts -i /var/lib/stacklab-repair/ssh_key -p 22 user@nas -s sftp' unlock
```

Host, user and port in `sftp.command` must match the saved URL; the double slash
marks an absolute remote path. Keep secret values out of arguments, history and
shared logs. Remove temporary credential copies/environment files after the
procedure; retain the independent recovery bundle securely.

| Installed restic | Repair commands and version considerations |
| --- | --- |
| 0.14–0.15 | `rebuild-index`; no modern snapshot/pack repair command family. |
| 0.16.0 | `repair index` replaces `rebuild-index`; adds `repair snapshots`. |
| 0.16.1–0.16.x | Adds experimental `repair packs`; check the exact version's limitations. |
| 0.17–0.18 | Pack repair supports more truncation cases; inspect release notes before repairs, particularly for older format-v1 repositories. |
| 0.19 | Includes fixes for split/incomplete index entries and pack salvage (#21820); still choose the command from the actual diagnosis. |

This map follows the [versioned restic changelog](https://github.com/restic/restic/blob/v0.19.1/CHANGELOG.md).
Check each installed command's `--help` before use.

- **Missing/damaged index with intact packs:** run `repair index` (0.14–0.15:
  `rebuild-index`), then a full check. This rebuilds references, not missing payloads.
- **Damaged packs:** follow the check's specific `repair packs PACK_ID...` advice
  using a suitable restic version. Pack repair also updates the index; a second
  index repair is not a mandatory next step.
- **Missing snapshot data after repair:** recover what remains or create a fresh
  backup from intact sources. `repair snapshots` salvages snapshots by removing
  inaccessible content; it creates new snapshot IDs. `--forget` deletes the original
  snapshots, so defer it until the salvaged content has been reviewed and the
  independent repository copy is safe. Keeping damaged originals can keep the full
  repository check failing. See the [upstream troubleshooting
guide](https://restic.readthedocs.io/en/stable/077_troubleshooting.html).

Restic repair cannot recreate a missing Stacklab source archive or make an
incomplete tar payload complete. It cannot rewrite the manifest's capture truth.
Review recovery provenance and extract/test selected sources; do not equate a newly
consistent restic graph with a complete application backup.

Run a full check as the service user, then restart with `sudo systemctl start
stacklab` while schedules remain paused. Run a full **Stacklab** check to update
persisted evidence and an actual recovery test of the needed snapshot/sources.
Confirm application data, source completeness and repository access before resuming
schedules. Full reading may download the entire repository and incur transfer costs.

While integrity remains failed, new copies can still succeed but automatic
retention is blocked, so repository storage can grow. Monitor capacity and resolve
the condition; repeated lighter checks do not establish that all data was repaired.

In notification settings, enable **Job failed** for failed backups and backup
timeouts and overdue-copy alerts, including scheduled runs. Overdue alerts include
plan/destination names, respect the configured grace period (60 minutes by default),
and are sent on transition rather than every poll. The same channel reports
scheduling pauses/resumption and recovery from overdue protection. Observation
continues while execution is paused; a system pause never silently disables alerts.
Queued time counts toward the scheduled deadline. Enable **Maintenance succeeded** for
successful restore and restore-test results. These use the existing configured
webhook or Telegram channels; cancelled jobs do not trigger failure alerts.

Do not manually run restic prune or destructive maintenance concurrently with
Stacklab. External tools do not participate in Stacklab job locks. Stacklab
respects restic repository locks and does not automatically force-unlock them.
Stop Stacklab jobs and investigate the lock owner before using native recovery
commands. Keep captured data and restored data private: encryption protects the
repository, not the local staging directories.

## Reviewed Online Recovery

Open **Recovery**, select a destination and snapshot, and browse its sources and
files. Compare snapshots when investigating a changed file. Restore the whole
set, selected sources or selected regular files into an isolated result. The
current format downloads each containing source archive; review the displayed
transfer estimate even when selecting one small file. Older snapshots without
indexes may build a local index after isolated restore. Indexes support up to
100,000 entries and 12 MiB of metadata per source; exceeding a bound preserves
the backup and reports that browsing/comparison is unavailable. The local fallback
has the same limits. The whole archive remains recoverable. Search includes descendants, and snapshot comparison covers
the complete source or chosen subtree, including content and metadata changes.

The completed job opens its registered result in Recovery. Download individual
files directly or choose **Review stack recovery** for complete source restores;
partial-file results are inspection only. Select source archives and map them to explicit directories
or existing local Docker volumes. Review affected services and overwritten
content. Confirm by typing `RESTORE`; the confirmation is single-use and expires
after ten minutes. Services remain stopped by default. Resume them only after
reviewing application prerequisites; previously stopped services stay stopped.

Complete-directory apply defaults to **Replace**: files absent from the backup
are removed. **Merge** is an explicit option that leaves unrelated/newer files,
and is unsuitable for restoring a database directory to an exact earlier state.
A single-file source only replaces its basename; it never clears neighboring
configuration files. Sources with backup exclusions cannot use Replace: excluded
live files have no copy in the archive. Explicit Merge remains available for
appropriate file data; it does not promise exact database recovery. Managed
stacks/config/data roots, configured path boundaries and their ancestors cannot
be replacement targets. Select the individual source directory or an inspected
local volume. Review the target and all-content deletion scope before confirming.

For offline recovery of a filtered source, stop all writers and extract into a
**new, empty, private target**, preserving archived metadata. Check the database's
own completeness and recovery requirements before changing any mount. An empty
target avoids mixing old/new files but cannot reconstruct excluded required files;
if those are missing, use a complete backup or a valid database dump instead.
Validate ownership, ACLs, modes and application data with the database's tools,
then explicitly switch the Compose bind mount or volume to the recovered target.
Keep the original target for rollback and start services only after validation.
Do not treat a successful tar extraction as a database integrity check.

The helper prepares immutable input before downtime, retains the target mountpoint,
rejects nested mounts, and keeps prior contents for rollback during replacement.
This is not an atomic multi-source transaction. If writes fail or Stacklab is
killed mid-restore, services remain stopped across restart. Inspect the target,
job and retained helper preparation/rollback directories before acknowledging a
hold in Recovery diagnostics. Acknowledgement does not start services or remove
rollback files. Corrupt journals require direct host inspection; never delete a
journal merely to clear a warning. Previously recorded containers may have been
removed or recreated. Diagnostics show recorded intent separately from current
Docker observations. When every unfinished original container is confirmed
absent, inspect replacements and shared data, type `INSPECTED` and record a
reason to acknowledge the missing originals. This writes a durable audit record
under `backup/recovery-history` before clearing the journal; it never starts a
replacement. For mixed present/missing originals retry recovery first. Unresolved
or unreadable journals retain global capture/apply exclusion because their shared
source scope cannot always be established safely.

Sources with restricted metadata (for example absolute symlinks, FIFO or setuid
files) are preserved as archives. Normal isolated recovery makes those archives
available for download and shows the restriction instead of unsafe extraction.
An automatic full restore test cannot succeed for such a source. Extract it only
through a reviewed offline procedure appropriate to the data and destination.

Use **Delete isolated restore** for old Stacklab-owned results. For custom
locations, **Forget result** removes only the library entry; directories remain
operator-owned and require manual cleanup. The library stops
accepting new restores at 200 registrations; it does not discard old ownership
records and leave invisible directories. Private Stacklab state uses offline DR.

## Capacity, Scheduling And Monitoring

Every run creates a full, unencrypted local tar copy of each selected source,
including file mode. Restic deduplicates repository blocks, but staging still
requires full source reads/writes. Defaults are a 10 GiB capture budget, 24-hour
retry lifetime and 300-second maximum stopped capture period. The byte budget is
an upper limit, not a requirement to reserve all 10 GiB before a small capture;
a 64 MiB filesystem reserve remains enforced. Allow space for concurrent retained
failed sets, isolated recovery and helper preparation/rollback. The helper spool
is separate from the plan's capture budget. Fully successful staging is removed
promptly; failed copies retain staging for retry until expiry.

Jobs have a 24-hour execution limit, independently of the retry lifetime.
Ordinary `snapshots`, `cat`, `dump`, `ls`, `stats` and `init` commands have a
90-second deadline. Copy verification and retry lookup allow 15 minutes per
command; a timeout preserves an unverified candidate for retry instead of
classifying it as incomplete or uploading a duplicate. Retention has a 30-minute
budget. Other check/prune/restore work remains within the job execution limit.
Backup upload aborts after five minutes without counter progress while reading;
the final flush has a bounded 30-minute no-progress window once all known input
bytes have been processed. Repeated unchanged status events do not extend it.
A large initial upload that cannot fit in
24 hours needs a smaller plan or faster transport; repository deduplication can
reuse committed blocks, but a new capture is required after staging expires.
Cancellation is graceful first; never assume a manually removed restic lock was
unused merely because a Stacklab job ended.

Scheduled runs, maintenance and inventory have bounded independent workers;
a slow repository does not hold the scheduler loop. Finished copies release their
repository reservations after retention even while a sibling copy is still busy.

Retention selects only verified complete snapshots of this instance and plan.
Daily, weekly and monthly buckets use UTC (weeks use ISO week numbering), retaining
the union and protecting the current checkpoint. Unknown, incomplete, future-dated
and legacy snapshots remain conservatively preserved; they cannot displace a
verified copy. At most 64 unknown candidates are inspected per retention run;
retained unclassified objects can require deliberate operator cleanup. Zeroes
for all three retention counts continue to mean keep all.

Backup settings provide a global automatic-scheduling pause that also suppresses
retention. Effective pause reasons are shown separately from the user's preference.
Manual backup remains allowed during a user pause, but host review, invalid
policy/state or unresolved recovery blocks unsafe capture with an actionable
conflict. Inventory and overdue observation continue during pauses. After restoring state
to a host with a different machine identity,
review is required before resuming. A clone retaining the same machine identity
cannot be detected automatically: pause before migration and keep the previous
host disabled. Host changes do not silently adopt old snapshot retention ownership.
Existing plans without saved host identity also pause once after upgrading;
review them before resuming because legacy recovery and in-place upgrade cannot
be distinguished automatically.

Optionally configure an external HTTP(S) heartbeat URL in backup settings. Stacklab
pings it at most once a minute while all enabled scheduled copies have acceptable
current protection evidence (allowing the overdue grace). It sends nothing when
paused, when no scheduled copies exist, or when protection is unverified/failed.
The external monitor detects a stopped Stacklab process; configure its deadline
and maintenance periods accordingly. The URL is stored as a write-only secret.
Repository availability, last check, snapshot presence and restore exercise age
remain separate evidence; a green freshness timestamp alone is insufficient.

## Recover Without The Original Stacklab Database

These steps work on a fresh host using ordinary restic and GNU tar. Read the
manifest before placing anything over application data.

1. Install the tools and prepare the independently saved access credentials.
   Use protected files and environment variables, not passwords in command-line
   arguments or shell history. Match the repository backend configuration.
2. Browse snapshots and restore the selected full snapshot into a new directory:

   ```bash
   export RESTIC_REPOSITORY=/mnt/backup/stacklab
   export RESTIC_PASSWORD_FILE=/secure/repository-password
   restic snapshots --tag stacklab
   restic check --read-data-subset=10%
   mkdir -m 0700 /srv/recovery-inspection
   restic restore SNAPSHOT_ID --target /srv/recovery-inspection --verify
   find /srv/recovery-inspection -name manifest.json -print
   ```

   For S3, supply the independent AWS credentials; for Drive supply the
   authorized `RCLONE_CONFIG`; for SFTP install the key and pinned host keys.
   A full `check --read-data` reads the whole repository and may incur substantial
   cloud transfer costs. A sample checks only the sampled data and does not replace
   a full check or application recovery exercise.
   Follow the upstream [restic repository guide](https://restic.readthedocs.io/en/stable/030_preparing_a_new_repo.html)
   for backend-specific environment configuration.
3. Inspect `manifest.json`. It maps each saved archive to its logical source,
   original location, shared writers, exclusions and capture method. Confirm
   available space, ownership, desired path/volume remapping, image availability,
   external networks, secret files and build contexts. List each archive with
   `tar -tvf` before extraction. A recorded image reference is not an image copy.
4. Extract selected archives into new empty directories. On Linux, a reviewed
   administrative extraction with GNU tar can preserve numeric owners, ACLs and
   xattrs:

   ```bash
   sudo tar --extract --file=SOURCE.tar --directory=/srv/recovered-source \
     --numeric-owner --same-owner --same-permissions --acls --xattrs
   ```

   Use the actual archive path from the manifest. A file source archive contains
   its basename; a directory source contains that directory's contents. Inspect
   links and contents before moving anything into a live destination. Do not
   extract an untrusted archive as root.
5. For Stacklab state, stop Stacklab, restore the standalone `stacklab.db` to
   the configured database path and private state to `STACKLAB_DATA_DIR`, using
   the saved `layout.json` as a reference. Review `stacklab.env` and destination
   credentials before enabling schedules on a new machine. Never replace a live
   SQLite database or its WAL. Ensure service ownership and private permissions.
6. Restore stack definitions and selected config/data into reviewed locations,
   recreate local volumes and map their archive contents, validate with
   `docker compose config`, then start services explicitly when ready. Verify
   actual application records, permissions and health. Record the exercise;
   repository integrity and an application restore test answer different questions.

The [upstream restore guide](https://restic.readthedocs.io/en/stable/050_restore.html)
explains selection and verification. Stacklab adds a versioned manifest and
source archives, not a proprietary repository format. Whole-host imaging,
operating-system recovery and database-specific point-in-time recovery remain
outside this workflow.


## Recovery Kit And Fresh-Host UI

Keep the following outside the protected host and outside the repository it unlocks:
repository URI/backend, repository password, S3 credentials/region or SSH key and
pinned host keys or the authorized rclone configuration, Stacklab/restic versions,
and a record of plan IDs/names, stack mappings and which repositories contain
Stacklab state. Include effective `stacklab.env`, the heartbeat monitor reference
and the independent credentials needed to restore monitoring.
Retain refreshed provider credentials when tokens rotate. A state backup includes
**all destination credentials and service configuration**; anyone who obtains that
state repository's decryption key may recover credentials for other destinations.
This is intentional full-state escrow, not independent isolation between those
secrets. A storage provider without the decryption key cannot read them.

For UI recovery on a fresh installation, add the existing destination, choose
**Test access** (never Initialize an existing repository), then select the snapshot
in Recovery and restore into an isolated directory. Review/download definitions,
create the required local Docker volumes, and map complete sources to their new
locations. Do not start application containers until their data and prerequisites
are restored. If recovering `stacklab.db` instead, stop Stacklab, restore its private
state offline, verify owner/mode and service configuration, then review the global
backup pause before enabling automatic work. Power off or fence the old host and its
clients before recovery, especially before **Remove stale locks** on a replacement with
the same hostname. Stopping one scheduler does not prevent the old host from writing
again.
A fresh database creates a new instance identity: attached historical snapshots
remain foreign to automatic retention until explicitly managed outside Stacklab.

Stack-definition capture includes the whole selected stack directory, including
relative `./data` subdirectories unless excluded. Select data explicitly and review
exclusions to avoid unintended live database copies or duplicate nested content.
Image references are saved; image digests and image layers are not currently backed
up. Retention `0/0/0` keeps all snapshots. With daily-only retention, several hourly
captures can be reduced to the retained point for each day.
