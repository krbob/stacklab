# Backup Setup And Recovery

## Prepare The Host

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
and private state. `ProtectHome=true` intentionally blocks home-directory
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

A lost SFTP connection or restarted NAS can leave a restic lock after the owning
process exits. Wait for legitimate work to finish, restore connectivity, then use
**Remove stale locks** on the destination. This queues ordinary `restic unlock`
under Stacklab's repository reservation and records the result in Jobs. Before
unlock, Stacklab reads lock metadata and probes local PIDs without signalling them.
A live process, including a root process inaccessible to the service user, blocks
unlock even if its lock is old. Malformed metadata, an uncertain probe or another
hostname also blocks removal. PID reuse may cause a conservative refusal. Resolve
remote-host locks on their owning host after confirming all clients have stopped.

Keep external clients stopped throughout this operation. The preflight is not
atomic with external restic commands, and identical hostnames across PID namespaces
cannot establish ownership reliably. Stacklab never uses `--remove-all` or runs
restic as root. Jobs report remaining locks; remaining or unreadable locks make the
operation unsuccessful. Retry the failed operation after reviewing the result.
Unlock does not clear integrity evidence or imply successful verification.

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
a full successful check, even after a successful metadata or sampled check.

For confirmed damage:

1. Pause schedules, wait for Stacklab jobs to finish, and stop the Stacklab service
   with `sudo systemctl stop stacklab` before manual repair. Pausing schedules alone
   still permits manual jobs. Stop or coordinate every other repository client.
   Investigate storage, hardware and transport reliability first.
2. Retrieve the independently saved repository password and transport credentials
   from the [recovery credential bundle](#recovery-kit-and-fresh-host-ui). Use a
   private password file or secure environment setup for restic; never paste secrets
   into shell history or shared job logs. Stacklab read APIs do not export secrets.
3. Preserve a separate copy of the repository when possible, at least its index and
   snapshots, before repair. Keep diagnostic output private until reviewed.
4. Run restic checks with the installed version and follow its diagnosis-specific
   instructions. Consult the [upstream recovery guide](https://restic.readthedocs.io/en/stable/077_troubleshooting.html).
   Restic 0.14 uses `rebuild-index`; restic 0.18 offers `repair index`, `repair packs`
   and `repair snapshots`.
   Check `restic version` and each command's `--help` against that version's guide;
   these are diagnosis-specific tools, not a generic sequence. Older installations
   may need a separately reviewed restic upgrade for a particular repair. Stacklab's
   supported minimum remains 0.14; backup/check support is not a promise that every
   modern repair command exists in that version.
   Snapshot repair can remove inaccessible data, and prune/forget can remove
   material needed for recovery. Stacklab intentionally does not automate repair.
5. After repair, restart Stacklab with `sudo systemctl start stacklab`, keep schedules
   paused and run a full data check in Stacklab. Only successful full reading
   clears known data failure. This may download the entire repository and incur
   transfer charges. Review protection and maintenance outcomes before resuming.

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
conflict. Inventory and overdue observation continue during pauses. After restoring state to a host with a different machine identity,
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
backup pause before enabling automatic work. Keep the old host's scheduler stopped.
A fresh database creates a new instance identity: attached historical snapshots
remain foreign to automatic retention until explicitly managed outside Stacklab.

Stack-definition capture includes the whole selected stack directory, including
relative `./data` subdirectories unless excluded. Select data explicitly and review
exclusions to avoid unintended live database copies or duplicate nested content.
Image references are saved; image digests and image layers are not currently backed
up. Retention `0/0/0` keeps all snapshots. With daily-only retention, several hourly
captures can be reduced to the retained point for each day.
