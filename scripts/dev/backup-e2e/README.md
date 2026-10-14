# Backup integration on an isolated Linux host

`run-linux.sh` builds the current working tree for Linux, copies Go test binaries
and a dedicated helper to a disposable Docker host, and records source revision,
working-tree patch, toolchain, host versions, binary hashes and raw test logs.
The host needs GNU tar, restic, Python 3, Docker, `debian:trixie-slim` and
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
- each privileged recovery action's protocol probe;
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
