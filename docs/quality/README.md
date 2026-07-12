# Quality Docs

This section defines current testing strategy, CI policy, and acceptance
criteria. Git history retains superseded rollout plans and dated execution
records.

## Documents

- [acceptance-criteria.md](acceptance-criteria.md) — MVP acceptance criteria and non-acceptance conditions
- [ci.md](ci.md) — current GitHub Actions gates, local equivalents, advisory analysis, and release quality gate
- [developer-checks.md](developer-checks.md) — canonical Go/Node toolchain and the reproducible `make check` workflow
- [repository-hygiene.md](repository-hygiene.md) — enforced action/workflow, shell, secret, dependency, and zero-warning lint checks
- [dependency-updates.md](dependency-updates.md) — current Renovate grouping, automerge window, risk classes, and review policy
- [browser-e2e.md](browser-e2e.md) — Playwright harness, fixture root, failure artifacts, and CI gate
- [manual-test-plan.md](manual-test-plan.md) — current manual and exploratory scenarios for a Docker-capable Linux host
