# Non-Goals

The following are intentionally out of scope for the first product generation:

- full Docker management outside Compose
- full CRUD coverage for images, volumes, and networks unrelated to Compose stack workflows
- swarm or Kubernetes orchestration
- multi-node or multi-host topology
- full image registry management
- always-on GitOps reconciliation from remote repositories
- Git branch, merge, or PR workflow management inside the UI
- general-purpose editing of arbitrary host paths outside Stacklab's managed roots
- secrets management platform replacement
- long-term observability platform replacement
- whole-host disk imaging, operating-system recovery, or a general-purpose
  host backup product; application-level backup and restore are scoped
  in [Native Backup And Recovery](backup-and-recovery.md)
- public SaaS-style hosting model
- deep Git workflow enforcement
