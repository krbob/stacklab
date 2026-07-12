# Operations Docs

This section defines local development, deployment, and operational procedures.

## Documents

- [local-dev.md](local-dev.md) — local development workflow for backend and frontend
- [first-run.md](first-run.md) — secure password initialization, access setup, readiness, and first login after an APT install
- [systemd.md](systemd.md) — recommended host-native deployment model for Linux, with `amd64` primary and `arm64` supported
- [release-and-validation.md](release-and-validation.md) — supported install modes, release artifacts, validation matrix, and rollback policy
- [install-from-apt.md](install-from-apt.md) — primary install and upgrade path for Debian-family hosts
- [install-from-tarball.md](install-from-tarball.md) — secondary manual install and upgrade path for generic Linux hosts
- [upgrade-validation-checklist.md](upgrade-validation-checklist.md) — operational checklist for validating both supported install modes on real Linux hosts
- [debian-package.md](debian-package.md) — current Debian package and APT operating model, helper assumptions, and remaining gaps
- [versioning-and-release-policy.md](versioning-and-release-policy.md) — active calendar versioning, nightly/stable/hotfix train, APT retention, and Renovate window
