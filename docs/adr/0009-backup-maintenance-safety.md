# ADR 0009: Backup Maintenance Safety

## Status

Accepted, 2026-10-15. Supplements and amends [ADR 0008](0008-backup-integrity-evidence.md)
with the [repository safety contract](../product/backup-repository-safety.md).

Amended 2026-10-21 by the
[sixth-round contract](../product/backup-diagnostics-and-unlock.md): preserve diagnostic
record context; require at least 30 minutes of lock age in addition to an absent
local owner; recheck lock identities before ordinary unlock. These safeguards reduce
PID-namespace and inventory races without promising atomicity with external clients.

Amended 2026-10-27 by the seventh-review extension of that contract: classify backend
failures using operation context, retain diagnostic evidence while streaming, bound
sustained check failures, and keep background review refresh separate from initial
freshness gating. An operational record explains itself and generic summaries; it
cannot explain an independent object diagnostic. Unknown object evidence wins over
transport elsewhere in the same attempt. Pure transport plus a generic damage summary
remains an unsuccessful availability check, without creating a new integrity hold.

## Context

A historical successful check is useful proof, but cannot authorize deletion after
an unresolved repository diagnostic. Retrying old maintenance with its captured
policy can delete snapshots the current policy would preserve. Native restic unlock
can consider a live process stale when its UID prevents the restic process probe;
repository reservation only coordinates Stacklab jobs, not external clients.

## Decision

Persist a scope-aware check hold independently of historical integrity evidence.
Classify explicit corruption before operational failure, but do not infer corruption
from generic summaries. A hold blocks automatic destructive maintenance and makes
current evidence unusable without erasing dated proof. Clearing requires successful
coverage of the uncertain scope. Migrate only known inconclusive attempts.

Use the captured plan for capture/transfer identity and the current active plan for
retention. Revalidate under the repository reservation before deletion. Keep-all
performs no destructive operation. Store maintenance chronology independently so an
old retry cannot supersede a newer copy failure.

Bind manual prune review to a derived identity of all unresolved evidence. Validate
that identity on admission and execution. The token is a review revision, not an
access credential. UI confirmation must reset when its evidence changes.

Preflight ordinary unlock conservatively using lock metadata and a non-signalling
local process probe. A live or uncertain owner prevents removal; EPERM means the
process exists. Do not elevate restic or use force unlock. Report remaining locks.
External writers must be quiescent: preflight and unlock are not an atomic operation,
and remote hosts or PID namespaces cannot be proven idle from this process. Manual
repair requires stopping Stacklab and coordinating all other clients.

## Alternatives And Consequences

Turning every failed check into corruption destroys the distinction between an
unavailable backend and damaged data. Preserving old proof without a hold permits
unsafe maintenance. A separate durable hold retains both facts and requires another
check, potentially a costly full read, before automatic deletion resumes.

Capturing deletion policy with a run is reproducible but contradicts later operator
changes. Current policy is authoritative for deletion; immutable run provenance is
still retained. Additional maintenance observations require conservative migration.

Plain restic unlock is insufficient for the observed cross-UID case. Running it as
root broadens privileges and can signal unrelated processes. A conservative probe
can refuse a safe unlock (for example PID reuse), which is preferable to removing a
live lock. It cannot guarantee safety against external clients starting in the gap;
that limit remains explicit in product and operator guidance.
