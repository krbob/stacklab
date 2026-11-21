# Incoming Git configuration validation

Validation date: 2026-11-21. Scope: the
[incoming configuration workflow](../product/git-incoming-changes.md) and
[Git API contract](../api/git-workspace.md#incoming-changes-and-configuration-reload).

## Automated checks

- Native Git tests use a bare remote, a separate author clone and the managed
  checkout. Preview leaves HEAD/files unchanged; download applies the reviewed
  commit even if a newer remote commit has since been pushed. A subsequent fetch
  discovers that newer update.
- Stale reviews, divergence, tracked/staged/untracked local changes outside and
  inside managed roots, unfinished merges, submodules and hidden-index flags
  cannot result in a download. Ignored-file collisions and existing index locks
  preserve local bytes and HEAD. Post-merge hooks are not executed.
- A fresh service instance reads the saved receipt; a prepared receipt is
  recovered against HEAD. Diff paths are exact, including literal metacharacters,
  and transport diagnostics do not reveal credential URLs.
- The complete `internal/gitworkspace` and `internal/httpapi` suites pass with
  `-race`. API tests cover authentication, same-origin, stale state, job/audit
  completion, competing deployments and synchronous workspace edits. OpenAPI
  route and response checks pass.
- The full frontend suite passed (702 tests before the final StrictMode regression);
  the final focused config/router suites passed 64 tests. Typecheck, zero-warning
  lint, production build, external-font asset check and documentation checks pass.
- Four Chromium UI scenarios pass, including complete review/download/reload at
  1440, 390 and 320 pixels, history after page refresh, cancellation of a reload
  review, visible stale-review errors and no implicit deployment. These browser
  scenarios use mocked APIs; desktop and mobile screenshots were inspected.

## Native Linux and Docker exercise

A separately built arm64 backend ran as the unprivileged `stacklab` account on
the dedicated Debian test VM, with isolated workspace, database, frontend and
loopback HTTP port. Tools: Git 2.47.3, Docker 26.1.5 and Compose 2.26.1.

The fixture used a local bare repository and a second clone, plus a unique
Compose project using the cached `busybox:1.36` image. The container copied a
mounted configuration value to an internal file at startup.

| Operation | Observed result |
| --- | --- |
| Initial `up` job | Container read `before-push`. |
| Commit/push `after-push` from the author clone | Incoming review showed one changed config file and its diff. |
| Reviewed API download | Host configuration changed; container ID and startup value remained unchanged. |
| Explicit `recreate` job | Container ID changed and the startup value became `after-push`. |
| Refresh incoming state | No incoming commits; the completed download receipt remained available. |
| Audit | Both `git_pull` and `recreate` had succeeded entries. |

The fixture backend and Compose project were stopped after the exercise.
No production checkout or application container was updated by this validation.

## Retained boundaries

Checks now also run when entering Config or returning to its browser tab, with a
one-minute automatic cooldown; commit/push check explicitly. No unattended fetch
or deployment is implied. Shared configuration dependencies and image
build inputs require operator review. Compose/environment drift is not presented
as proof that arbitrary mounted configuration has been reloaded. External Git
and filesystem writers require coordination outside Stacklab's internal locks.

Native checkout behavior follows the documented
[Git merge fast-forward and ignored-file options](https://git-scm.com/docs/git-merge).

## Proactive checks and reconciliation — 2026-11-21

The complete frontend suite passed 713 tests. Coverage includes entry/focus fetch
coalescing and cooldown, offline/unverified state, cache revocation after logout,
pre-commit acknowledgement, blocked push when incoming changes exist, persisted
conflict drafts, manual text resolution, stale approval, discarded late responses
and protection of unsaved resolution text.

The full Git workspace and HTTP API suites passed with `-race`. Native Git
fixtures verify clean automatic merge, three-way text conflicts, exact binary
version selection, deletion, preservation of both parent histories, persistent
draft/resolutions after a new service instance, and checkout/index preservation
before approval. Approval rejects new local edits, staged/untracked files,
changed commits, hidden index flags, native locks and unfinished operations;
ignored-file collisions retain local bytes. Additional race-tested regressions
verify that a refused checkout leaves the draft editable and configured merge
drivers/post-merge hooks are not executed. API tests cover authentication,
same-origin, deployment/editor exclusion and `git_reconcile` audit completion.

Eight Chromium scenarios passed: the prior fast-forward/reload flow and the new
Config-entry/reconcile flow at 1440, 390 and 320 pixels, plus stale-operation
errors. Draft resolutions survive page reload; approved synchronization never
implicitly invokes a stack action. Desktop and mobile layouts were inspected.
Browser scenarios use mocked APIs; native API/Git execution is tested separately.

A second isolated Debian VM exercise used a real Compose project and divergent
commits in the author and managed clones. The base configuration was
`before-push`; local and remote versions conflicted. The API saved a manual
`merged-value` resolution. Restarting the backend retained the draft ID, revision
and readiness. Applying the merge changed the host file while preserving the
container ID and startup value. Explicit recreation changed the container ID and
the observed value to `merged-value`. Audit recorded successful `git_reconcile`
and `recreate`; the synchronization receipt remained after refresh. The fixture
backend and Compose project were stopped. No production service was changed.

Retained limits: uncommitted files must first be deliberately committed; this
workflow never silently includes untracked files. Custom conflict text is limited
to ordinary managed UTF-8 files. Native merge failures/unsupported arrangements
leave the checkout untouched during preparation. Apply still requires coordination
with external writers and uses native Git checkout protections, not a multi-file
filesystem transaction. Push and container reload remain explicit operations.
