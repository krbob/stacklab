# Dependency Update Policy

Renovate is the repository's dependency-update mechanism. The active policy is
defined in `.github/renovate.json5`; this document explains the operating model
and review expectations.

## Cadence

- Renovate creates new dependency branches and pull requests from 07:00 on the
  1st day of each month, after the stable workflow has started with its pinned
  source revision.
- GitHub platform automerge merges each update as soon as `ci-required` passes.
- A branch is rebased only when it conflicts with `main`.
- Existing branches may be refreshed outside the creation window so they remain
  current and real conflicts can be resolved.
- Nightly releases provide soak time between the post-stable update window and
  the next stable release.

The repository intentionally has no hourly or concurrent pull-request limit.
Grouping controls review volume, while the Dependency Dashboard keeps updates
visible between monthly creation windows.

Renovate's separate vulnerability-alert PR mechanism is disabled so vulnerable
dependencies follow the same creation and automerge windows as every other
update. They remain visible through normal update detection and the dashboard.

## Grouping

Renovate groups:

- frontend runtime dependencies;
- frontend development dependencies;
- non-high-risk Go modules;
- low-risk GitHub Actions (`checkout`, `setup-go`, and `setup-node`);
- all remaining GitHub Actions in a separate group.

Major updates remain separate. The following runtime-sensitive Go modules also
remain separate:

- `github.com/creack/pty`;
- `github.com/gorilla/websocket`;
- `modernc.org/sqlite`.

## Automerge And Review

Every dependency update is eligible for automerge after the required repository
checks pass, including major updates, GitHub Actions, and the PTY, WebSocket, and
SQLite modules listed above. Major and runtime-sensitive updates remain separate
so a failure is isolated and the resulting nightly can be diagnosed or reverted
without disentangling an unrelated group.

Eligible updates merge after the stable workflow has captured its source
revision. The next nightly is the first published artifact to include them and
runs the broader release quality gate before publication. A failed or missing
required check keeps a pull request open for manual intervention.

Renovate does not assign or request reviewers from `CODEOWNERS`. This avoids
automatic review-request subscriptions; it does not override a user's own
GitHub watch settings, mentions, comments, or existing pull-request
subscriptions.

## Validation

Renovate pull requests use the same CI as other pull requests. At minimum,
expect:

- frontend tests, generated API contract check, typecheck, and production
  build;
- backend tests and package coverage floors.

Repository hygiene, vulnerability analysis, Docker integration and browser E2E
remain release checks. They gate the nightly that first carries an automerged
dependency update rather than its pull request.

See [Continuous Integration](ci.md) for workflow ownership and local commands.

The following targeted checks guide diagnosis when PR CI or the nightly release
gate fails; they are not separate human approval gates:

| Dependency class | Targeted review or validation |
| --- | --- |
| PTY or terminal stack | terminal open/input/resize/close, disconnect, cleanup, and session limits |
| WebSocket stack | authentication, subscribe/replay, reconnect, backpressure, and shutdown |
| SQLite | migrations, lock contention, retention, backup/restore, and upgrade smoke |
| Docker/Compose integration | real lifecycle, pull/build, logs/stats, and failure cleanup |
| GitHub Actions | permissions, pinned SHA, action inputs, artifacts, and release behavior |
| Frontend runtime | affected user journeys in browser E2E and focused component tests |

A patch version alone does not make an update low-risk. If release notes or the
diff reveal a known regression, migration requirement, or unsafe behavior,
temporarily hold the affected update with a narrow Renovate rule.

## Manual Intervention

Intervene when:

- a required check fails or becomes flaky;
- an update needs a migration or source change;
- upstream reports a relevant regression or vulnerability;
- grouped changes obscure which dependency caused a failure;
- a release or incident requires temporarily holding an update.

Prefer a narrow configuration rule with an explanatory `description` over
closing the same class of pull request repeatedly. Do not bypass a failing gate
only to fit the monthly merge window.

## Maintaining The Configuration

When changing `.github/renovate.json5`:

1. keep high-risk separation rules more specific than broad grouping rules;
2. keep major and runtime-sensitive updates separate even though they automerge;
3. preserve `assigneesFromCodeOwners: false` and
   `reviewersFromCodeOwners: false` unless automatic notifications are wanted;
4. update custom-manager patterns together with the pinned tool declarations
   they track;
5. run repository hygiene and review Renovate's Dependency Dashboard after the
   change reaches `main`.
