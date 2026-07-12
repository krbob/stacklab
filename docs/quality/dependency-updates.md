# Dependency Update Policy

Renovate is the repository's dependency-update mechanism. The active policy is
defined in `.github/renovate.json5`; this document explains the operating model
and review expectations.

## Cadence

- Renovate may create branches and pull requests whenever it detects updates.
- Routine updates may automerge only on the 2nd and 3rd day of each month,
  after the stable release scheduled for the 1st.
- `platformAutomerge` is disabled, so Renovate performs the merge itself using
  the configured rebase strategy.
- A branch is rebased when it falls behind `main`.
- Nightly releases provide soak time between the post-stable update window and
  the next stable release.

The repository intentionally has no hourly or concurrent pull-request limit.
Grouping controls review volume without delaying security and maintenance
visibility.

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

Routine grouped updates are eligible for automerge only after repository checks
pass. Automerge is disabled for:

- every major update;
- every GitHub Actions update;
- the PTY, WebSocket, and SQLite modules listed above.

Those pull requests require a human decision. Review the release notes and the
changed runtime seam, then merge only after the relevant automated and targeted
checks pass.

Renovate does not assign or request reviewers from `CODEOWNERS`. This avoids
automatic review-request subscriptions; it does not override a user's own
GitHub watch settings, mentions, comments, or existing pull-request
subscriptions.

## Validation

Renovate pull requests use the same CI as other pull requests. At minimum,
expect:

- frontend tests, generated API contract check, typecheck, and production
  build;
- backend tests and package coverage floors;
- Go formatting and `go vet`;
- repository hygiene, production npm audit, and zero-warning ESLint;
- Docker-backed integration tests;
- browser E2E.

See [Continuous Integration](ci.md) for workflow ownership and local commands.

Additional targeted checks are required when the update affects a sensitive
seam:

| Dependency class | Targeted review or validation |
| --- | --- |
| PTY or terminal stack | terminal open/input/resize/close, disconnect, cleanup, and session limits |
| WebSocket stack | authentication, subscribe/replay, reconnect, backpressure, and shutdown |
| SQLite | migrations, lock contention, retention, backup/restore, and upgrade smoke |
| Docker/Compose integration | real lifecycle, pull/build, logs/stats, and failure cleanup |
| GitHub Actions | permissions, pinned SHA, action inputs, artifacts, and release behavior |
| Frontend runtime | affected user journeys in browser E2E and focused component tests |

A patch version alone does not make an update low-risk. Escalate it to manual
review whenever release notes or the diff show behavior changes in process
execution, persistent state, authentication, privileged helpers, or release
publication.

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

1. keep high-risk exceptions more specific than broad grouping rules;
2. keep major and GitHub Actions updates review-only unless the risk model is
   deliberately changed;
3. preserve `assigneesFromCodeOwners: false` and
   `reviewersFromCodeOwners: false` unless automatic notifications are wanted;
4. update custom-manager patterns together with the pinned tool declarations
   they track;
5. run repository hygiene and review Renovate's Dependency Dashboard after the
   change reaches `main`.
