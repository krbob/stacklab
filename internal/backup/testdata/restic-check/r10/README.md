# Round-ten native diagnostics

Captured 2026-10-30 on the exclusive Debian 13 arm64 test VM with native restic
0.14.0 and distribution restic 0.18.0. Production diagnostic code is the
`2a67fc4` correction (also in candidate `e8f6c59`). The fixtures are
`TestNativeCheckHousekeeping` and `TestNativeSFTPInterruptedCheckRecovery`, with
`STACKLAB_TEST_NATIVE_MISSING_LOCK=1`. The 0.14 binary was selected through a
private PATH, preserving the host binary.

- `*-housekeeping.stderr`: successful full check followed by Stacklab's inventory
  refresh. Local cache is temporarily obstructed by a regular file; rclone's owned
  local remote contains `snapshots/zz-fixture-link`, an invalid object filename.
  These warnings establish neither damage nor a check hold. Job events use fixed
  text, never the native paths. A separate native check with a valid object-ID
  symlink verifies that such a NOTICE still creates a hold.
- `*-missing-lock.stderr`: actual `cat lock` of an absent full ID, with normal
  native retry behavior and the production 90-second command budget. Exit is 1;
  Stacklab refuses unlock with `unlock_refused`. No repository mutation occurs.
  The SFTP repository uses the fixture's independent OpenSSH server; rclone uses
  REST over stdio. All three backends passed on both versions.

Files preserve diagnostic text, generated fixture paths/IDs and retry timings.
Only the Go test logger's indentation is removed. No credential content is retained.
The corpus replays stderr/exit status through the production runner; it does not
reproduce native retry timing or create a real lock race. The native tests exercise
storage separately. `TestLockDiagnosticsRespectCommandAndEvidence` covers the
synthetic race mixed with unrelated unknown errors or damage.
