# Restic check diagnostics

These files preserve subprocess stderr, including JSON escaping and multiline
messages. They contain only disposable fixture IDs and public restic links.

`restic-0.19.1-local-*.stderr` was captured on 2026-10-15 on macOS arm64 using
`restic 0.19.1 compiled with go1.27.1`. Each command returned exit status 1.

1. Initialise a fresh local repository and back up a directory containing one
   1 MiB random file.
2. Save and remove every file in `index/`; run `check --read-data --no-cache`,
   separately with and without `--json`.
3. Restore the index bytes, truncate the largest pack to half its original size,
   and repeat both checks.

`TestNativeRepositoryMissingIndexAndTruncatedPack` reproduces these mutations
against a fresh repository using the installed restic. The ordinary corpus test
runs even when restic is absent. Synthetic tests are kept separately and labelled
as such; they cover mixed diagnostics, retry warnings and future error wording.

`restic-0.18.0-sftp-truncated-pack.json.stderr` was captured on 2026-10-15 on
Debian 13 arm64 (`restic 0.18.0`, Go 1.24.4), by
`TestNativeSFTPInterruptedCheckRecovery`. After interruption/unlock recovery, the
fixture truncated its largest encrypted pack to 64 bytes and ran a full check over
SFTP. Exit status was 1; Stacklab classified the actual mixed size/EOF diagnostics
as integrity failure. Restoring those exact bytes and a full check recovered it.

`restic-0.18.0-sftp-inventory-outage.json.stderr` was captured on 2026-10-21 on
Debian 13 arm64 by `TestNativeSFTPInterruptedInventoryRecovery`. The owned SSH
subsystem gates the actual SFTP OPENDIR request for `repository/data`, before
forwarding it to OpenSSH's sftp-server. The fixture then stops its sshd container.
This deterministically interrupts inventory before pack reads, with no fabricated
restic output. Restic exits 1 with SSH status 255, apparent missing packs and
circuit-breaker errors. Expected classification: unavailable, historical proof
retained, no new hold. The separate TCP-byte gate still covers pack-read outage.

The SFTP inventory-outage transcripts include the generic damaged-repository summary and lock-removal warning. These exercise operational summary interpretation and lock-warning isolation. The native SFTP recovery tests additionally assert retained check evidence and absence of a new hold.

The three `restic-0.19.1-s3-*-outage.stderr` transcripts were captured on
2026-10-28 on macOS arm64 by `TestNativeS3TruncatedPackIsIntegrityFailure` with
`STACKLAB_TEST_NATIVE_CHECK_RETRY_EXHAUSTION=1`. An owned TLS proxy returned real
S3 500 InternalError responses for listing or pack GET, or closed pack bodies
halfway through the advertised size. Restic used its ten-retry compatibility
policy; Stacklab used the production ten-minute budget. Terminal List/Load failures
were observed before Stacklab requested SIGINT. These are native stderr, with only
terminal control characters and trailing terminal whitespace removed; generated object IDs remain unchanged.
Expected result: unavailable, previous proof retained, no new hold. The modern
listing transcript includes actual missing-pack and snapshot-cancellation residue.
Synthetic cancellation-boundary tests separately establish its runtime provenance.

The three `restic-0.14.0-s3-*-outage.stderr` files were captured on Debian 13
arm64 with the explicitly selected restic 0.14.0 binary and the same isolated
fixture/production budget. Listing and partial-read transcripts came from passing
checks; pack-read stderr came from the first failing regression run and is retained
unchanged apart from terminal-control removal. It exposed the legacy tree
`ReadFull(<data/…>): cause` wrapper before `pack … failed to download: StreamPack: cause`.
The corpus replays the production watchdog's stop boundary for all S3 transcripts;
otherwise post-SIGINT output would incorrectly lose its cancellation provenance.

The equivalent `restic-0.18.0-s3-*-outage.stderr` files were captured on the same
disposable Debian host using its original stable-distribution restic 0.18.0.
All three faults, the corruption/transport mixture, and final healthy recovery
passed. This run used the same explicit retry-exhaustion policy and real TLS proxy;
only terminal control characters were removed from the retained stderr.
