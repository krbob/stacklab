# Operations Docs

This section defines local development, deployment, and operational procedures.

## Documents

- [local-dev.md](local-dev.md) — local development workflow for backend and frontend
- [systemd.md](systemd.md) — recommended host-native deployment model for Linux, with `amd64` primary and `arm64` supported
- [release-plan.md](release-plan.md) — supported install modes, release artifacts, validation matrix, and rollback policy
- [release-automation-plan.md](release-automation-plan.md) — target nightly/stable/hotfix automation model, tags, and APT channel strategy
- [install-from-apt.md](install-from-apt.md) — primary install and upgrade path for Debian-family hosts
- [install-from-tarball.md](install-from-tarball.md) — secondary manual install and upgrade path for generic Linux hosts
- [upgrade-validation-checklist.md](upgrade-validation-checklist.md) — operational checklist for validating both supported install modes on real Linux hosts
- [debian-package-plan.md](debian-package-plan.md) — current Debian package and APT operating model, helper assumptions, and remaining gaps
- [versioning-and-release-policy.md](versioning-and-release-policy.md) — recommended versioning scheme, monthly release cadence, and reminder model
