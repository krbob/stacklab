# Product Documentation

These documents define who Stacklab is for, which problems it solves, and where
the product is heading.

## Current documents

- [Backup reliability and recovery hardening](backup-reliability.md) — accepted
  repair plan and release gate following the backup review.

- [Scope](scope.md) — product statement, target environment, operator model,
  principles, source of truth, and success criteria.
- [Non-goals](non-goals.md) — deliberate boundaries that keep Stacklab focused.
- [MVP baseline](mvp.md) — implemented first-generation capability baseline
  and the work intentionally deferred beyond it.
- [Roadmap](../roadmap.md) — current priorities after the MVP baseline.
- [Native backup and recovery](backup-and-recovery.md) — implemented product direction
  for source selection, multiple destinations, consistency, and restore;
  native restic/rclone orchestration, reviewed restore and offline state recovery.
- [Backup protection and recovery experience](backup-experience.md) — accepted
  implementation plan for the protection dashboard, file browser, continuous
  recovery, execution history, guided setup and maintenance schedules.

For implementation boundaries, continue with the
[domain model](../domain/README.md),
[system architecture](../architecture/README.md), and
[information architecture](../ui/information-architecture.md).
