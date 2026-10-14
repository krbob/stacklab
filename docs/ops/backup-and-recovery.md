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
`/usr/share/doc/stacklab/examples/stacklab-backup.sudoers.example` with `visudo`, then set:

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
storage on an undersized tmpfs.

## Configure Copies

Open **Backups → Destinations**, follow connection, credentials and verification,
and store the repository password in a
password manager independent of this host. Also retain endpoint/bucket/path,
transport credentials, SSH host keys, or the authorized Drive configuration.
The repository cannot unlock itself using a password stored only inside it.

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
writers. Review newly added service mounts in the stack's **Backups** tab.
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
one missed occurrence after restart.

## Failures And Retrying

Review the parent job and each destination result. Upload, verification and
retention are separate stages; one successful copy does not hide another failed
copy. Retry while the prepared recovery set remains within its configured
lifetime. When staging expires, start a new capture. A failed repository check
needs investigation even if upload succeeded.

In notification settings, enable **Job failed** for failed backups and backup
timeouts and overdue-copy alerts, including scheduled runs. Overdue alerts include
plan/destination names, respect the configured grace period (60 minutes by default),
and are sent on transition rather than every poll. Enable **Maintenance succeeded** for
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
configuration files. Review the deletion scope before confirming.

The helper prepares immutable input before downtime, retains the target mountpoint,
rejects nested mounts, and keeps prior contents for rollback during replacement.
This is not an atomic multi-source transaction. If writes fail or Stacklab is
killed mid-restore, services remain stopped across restart. Inspect the target,
job and retained helper preparation/rollback directories before acknowledging a
hold in Recovery diagnostics. Acknowledgement does not start services or remove
rollback files. Corrupt journals require direct host inspection; never delete a
journal merely to clear a warning. Previously recorded containers may have been
removed or recreated and must be identified before operator recovery.

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
Repository metadata commands are bounded to 90 seconds, and backup upload aborts
after five minutes without progress. A large initial upload that cannot fit in
24 hours needs a smaller plan or faster transport; repository deduplication can
reuse committed blocks, but a new capture is required after staging expires.
Cancellation is graceful first; never assume a manually removed restic lock was
unused merely because a Stacklab job ended.

Backup settings provide a global automatic-scheduling pause that also suppresses
retention. After restoring state to a host with a different machine identity,
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
   restic snapshots
   restic check --read-data
   mkdir -m 0700 /srv/recovery-inspection
   restic restore SNAPSHOT_ID --target /srv/recovery-inspection --verify
   find /srv/recovery-inspection -name manifest.json -print
   ```

   For S3, supply the independent AWS credentials; for Drive supply the
   authorized `RCLONE_CONFIG`; for SFTP install the key and pinned host keys.
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
and a record of which plan contains the Stacklab state and required stack sources.
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

Stack-definition capture includes the whole selected stack directory, including
relative `./data` subdirectories unless excluded. Select data explicitly and review
exclusions to avoid unintended live database copies or duplicate nested content.
Image references are saved; image digests and image layers are not currently backed
up. Retention `0/0/0` keeps all snapshots. With daily-only retention, several hourly
captures can be reduced to the retained point for each day.
