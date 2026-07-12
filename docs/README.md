# Documentation Map

This directory contains project documentation for Stacklab.

## Source of truth and lifecycle

Use the narrowest current contract for implementation decisions:

1. executable code, tests, and `api/openapi.yaml`;
2. current API, domain, data, architecture, and operations references;
3. the active remediation plan and roadmap;
4. dated reviews, test runs, handoffs, and proposals as historical context.

Documents described as a snapshot, proposal, handoff, review, test run, or
historical plan are retained for rationale. They are not authoritative when
they conflict with current code or contracts. A document whose title contains
`plan` can still be current when its own status says it defines an active or
steady-state policy.

## Sections

- `adr/` architectural decision records
- `product/` scope, MVP, roadmap inputs, and feature strategy
- `architecture/` system-level design
- `domain/` domain models and operational rules
- `api/` REST and WebSocket contracts
- `data/` persistence model and migrations
- `ui/` screen contracts for the UI developer
- `ops/` local development and deployment
- `quality/` active remediation, testing policy, acceptance criteria, and dated evidence

## Status

Documentation started contract-first and is now being reconciled against implementation and Linux staging validation.

The most current operator-facing entry points are:

- `../README.md`
- `ops/install-from-apt.md`
- `ops/install-from-tarball.md`
- `ops/release-plan.md`
- `ops/upgrade-validation-checklist.md`

Repository policies:

- `../CONTRIBUTING.md`
- `../SECURITY.md`
- `../LICENSE` and `../NOTICE`

Recommended product entry points:

- `roadmap.md`
- `product/scope.md`
- `product/feature-strategy.md`

Historical entry points retained for context:

- `product/development-plan.md` — milestone-era planning record
- `quality/github-actions-workflow-plan.md` — partly implemented workflow plan
- `quality/test-rollout-plan.md` — superseded rollout baseline
- `quality/review-2026-07-09.md` and `quality/test-run-*.md` — dated evidence
- `ui/visual-revamp-proposal*.md` and `ui/ui-review-2026-07-and-final-design.md` — design exploration and review snapshots
