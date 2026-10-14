# Backup Evidence And Recovery Corrections

## Status And Scope

Accepted for implementation, 2026-10-14, following the third independent review.
This plan supplements [reliability completion](backup-reliability-completion.md)
and [ADR 0008](../adr/0008-backup-integrity-evidence.md). The third review reopens
release acceptance for the sequences below; earlier successful tests remain
historical evidence, not proof that these additional cases pass.

## Implementation Contract

1. Restore read-only files and inherited POSIX ACLs using the existing unprivileged
   isolated extraction boundary. Preserve modes, ACLs, user xattrs and links; avoid
   applying POSIX ACLs twice through tar's separate ACL and generic xattr handlers.
   Exercise isolated recovery and the privileged replacement path on Linux.
2. Separate metadata checks, data checks and the latest check attempt. A metadata
   check or sampled data check cannot clear known data corruption. A successful
   full-data check can. Transport failure or cancellation is not proof of corrupt
   data. Migrate ambiguous historical failures conservatively. Automatic retention
   must not forget or prune while repository integrity is known to have failed.
3. Measure overdue protection from the first unsatisfied scheduled occurrence
   after the valid capture or schedule activation. New occurrences, process
   restart and disabled notifications must not grant repeated grace or emit false
   recovery/healthy heartbeats. Preserve the last completed copy error while a new
   attempt is queued or running.
4. Persist bounded, versioned snapshot classifications and a fair continuation
   cursor. Complete, incomplete, legacy, unsupported and invalid snapshots are
   distinct from temporary transport failures. Repeated retention must progress
   beyond the same unclassified prefix. Keep unknown/legacy/unsupported snapshots
   conservatively. Report maintenance failure separately from a successful verified
   copy, and retry failed maintenance without uploading that copy again.
5. Refresh the idle backup overview and on focus; invalidate recovery inventory
   when a relevant destination/copy changes, including another session and scheduled
   work. Keep snapshot and restored-library refreshes separate. Preserve a missing
   selected snapshot ID with an explicit status until the operator chooses another.
   Show retry progress, scope errors atomically and gate actions by the actual pause
   reason. Operator scheduling pause still permits manual capture.
6. Cache successful helper readiness for 15 minutes and failed readiness for one
   minute; operation review still performs a fresh probe. Run readiness outside the
   startup writer-recovery budget. Probe all six authorized actions without creating,
   changing or garbage-collecting the privileged spool. Distinguish unconfigured
   helper information from a configured helper failure. Warn on outdated installed
   sudoers examples and ship examples outside documentation-only package paths.
7. Warn about filtered database sources during configuration and recovery review.
   Document offline recovery into a new empty private target, completeness checks
   and explicit mount switch; an empty target does not recover excluded database
   files. Align validation bounds and emitted API fields, improve failure summaries
   and retain precise target wording in recovery events.

## Verification And Delivery

Commit this contract before implementation. Follow with focused implementation
commits and regression tests, generated API types, frontend checks and browser
recovery scenarios. Run native Linux ACL/xattr/link tests and helper tests, then
exercise a packaged upgrade on the disposable Debian host. Record exact source
revision and distinguish local artifacts from published CI evidence. CI should
cover rclone availability and race tests for backup, notifications and the helper;
never publish recovery archives or authentication material as test artifacts.
Push the resulting commits and check the CI run for the pushed revision.

Acceptance requires the ACL restore sequence; full-check failure followed by
metadata/subset/full checks; repeated overdue boundaries without a fresh capture;
retention progress across cached negative classifications; successful copy with
failed maintenance and maintenance retry; idle cross-session UI changes; missing
snapshot selection; helper readiness and upgrade behavior. Update the existing
[validation record](../quality/backup-validation.md) with actual outcomes and limits.

## Retained Limits

- Keep the existing long verification budget for cold/large repositories. Only
  introduce shorter transport-specific connection limits with protocol-specific
  evidence; a blanket timeout reduction would regress copy verification.
- Shared-repository admission still reserves all repositories needed by a plan.
  Splitting admission requires a separate capture/watermark contract.
- Do not automatically delete legacy, unsupported or invalid snapshots. Cleanup
  of proven partial snapshots is optional follow-up with explicit provenance and
  a verified successor, not an age-based deletion rule.
- Exact replacement of filtered sources remains refused. New database profiles,
  representation benchmarks and broad component refactors are outside these fixes.
