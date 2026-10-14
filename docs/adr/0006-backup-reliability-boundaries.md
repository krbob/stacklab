# ADR 0006: Backup Representation And Reliability Boundaries

## Status

Accepted, 2026-10-14. Implementation is tracked in the
[backup reliability plan](../product/backup-reliability.md).

## Context

The initial implementation prepares GNU tar archives, then copies one immutable
recovery set to independent restic repositories. This preserves owners, ACLs and
xattrs without running repository processes as root and resumes applications
before network transfer. Review exposed excessive coupling between capture,
inspection, privileged extraction, global job locking and protection evidence.

Every run currently reads and stages all selected data. Restic still deduplicates
blocks, but cannot use original-file metadata to skip unchanged file content.
Full staging, retained runs, indexes and isolated restore may consume multiple
copies of source data locally. Archive-based single-file recovery transfers the
containing archive. These are explicit costs, not transparent incremental capture.

## Decision

Retain the portable archive representation during reliability hardening. Separate
four contracts: faithful capture, optional bounded inspection, complete repository
copy, and policy-controlled extraction. An inspection limit is not a backup
failure; an incomplete repository snapshot is not a successful copy.

Keep the non-root backend and narrow privileged helper. Prepare immutable inputs
before downtime and retain strict checks at mutation time. Preserve running-state
intent durably and expose unresolved recovery through an available panel. Exact
restore and merge have different semantics; neither a multi-source transaction
nor universal atomic directory replacement is promised.

Restrict lifecycle exclusion to phases touching live sources/targets. Repository
reservations protect real repository identities and credentials independently;
native restic locks remain authoritative. Graceful cancellation and bounded
network waits precede any forced process termination. Do not force-unlock.

Treat protection as timestamped evidence: source selection, capture freshness,
complete snapshot presence, availability, integrity policy and restore exercise
scope. Derive summaries from those dimensions without equating an operation
failure with repository corruption or a saved checkpoint with current existence.

Retain explicit full-state secret escrow and require independently retained
recovery credentials. A state backup's decryption key grants access to secrets
contained in that state; repository storage administrators without the key do
not automatically gain those secrets. Document this boundary in setup and DR.

## Alternatives Considered

- **Direct restic backup of each source:** avoids full local staging and enables
  file-level change detection and native browsing. It changes privilege handling
  and reading live data independently for each destination loses the single-set
  guarantee. Requires its own prototype and benchmarks.
- **Filesystem snapshots:** can reduce downtime and staging, but depend on host
  filesystem/storage configuration. They are not a universal Debian baseline.
- **Backrest as runtime:** mature generic backup controls, but would duplicate
  configuration, jobs and identity, and would not resolve Compose writer recovery.
- **Unbounded or silently truncated indexes:** rejected; memory/storage exhaustion
  and misleading search results are unacceptable.
- **Universal directory rename or automatic journal removal:** rejected; mounted
  volumes and unfinished Docker operations need explicit recovery semantics.
- **Automatic credential removal or force-unlock:** rejected; either can break
  recovery or concurrent repository operations without operator awareness.

## Consequences

Archive and `backup.v1` compatibility remain. Full staging still needs explicit
capacity planning; successful sets can be removed promptly and failed copies
retain a bounded retry window. Some archived metadata requires offline handling
instead of automatic privileged extraction. Reliability tests must include actual
restic partial success, daemon-side stop races, clock changes and unavailable
destinations, rather than relying only on happy-path restore fixtures.
