# Incoming configuration changes

Operators can edit the homelab repository on another machine, push it, review
the incoming version in Stacklab, download that version, and apply selected
stacks. Local files remain the configuration source of truth.

## Operator flow

Config exposes a **Remote updates** view. Opening it checks the configured
upstream; an explicit refresh checks again. The view shows the checked time,
branch, incoming commit count, every changed repository path and bounded diffs.
Changes outside `stacks/` and `config/` are identified separately because a
Git fast-forward updates the whole checkout. They do not become editable through
the managed file workspace.

Downloading requires review of the exact local and upstream commits. It never
merges divergent histories, stashes local edits or resets the checkout. Tracked
and untracked local changes, an unfinished Git operation, and stale reviews
block the operation. Ignored files must not be silently overwritten. Git hooks
and hidden-index flags (`assume-unchanged` / `skip-worktree`) cannot bypass the
local-change guard. Submodules require an external update. Git hooks
and submodule recursion are disabled for this flow. Concurrent external Git
or filesystem writers must be stopped by the operator; Git is not a filesystem
transaction spanning other programs.

Download changes files without starting a Compose action. Applications that
watch mounted configuration may nevertheless react immediately. The operator
can then recreate individual affected stacks using the existing audited
`recreate` job. This applies Compose/environment changes and reloads mounted
configuration, with an explicit notice of interruption and possible startup of
stopped services. It is not an application-specific hot-reload protocol.
Deleted definitions, shared configuration and image build inputs require review
in the stack views; inferred stack IDs are hints, not exhaustive dependency maps.

The latest downloaded change remains available after a page/service restart.
It is historical download evidence, not a claim that every stack was deployed.
Existing stack jobs provide deployment progress, errors and history.

## Implementation and validation

Use native Git with the service account's existing authentication. Fetch and
checkout share the Git mutation lock. Checkout uses a pinned commit and
`merge --ff-only --no-autostash --no-overwrite-ignore`, with a job reservation
against concurrent host/stack operations. A private receipt is written before
checkout and reconciled against Git HEAD, so a disconnected request does not
erase the apply guidance. Raw transport errors and credential URLs are never
returned or logged.

Regression coverage must use a real bare remote and a second clone: preview
without writes, exact reviewed download, changed review, dirty files outside
managed roots, divergence, ignored-file collision, hooks, concurrent mutations,
and receipt recovery. API tests cover authentication, same-origin, audit/job
completion and deployment reservation. UI/browser checks cover changed-file
review, download failure, reload confirmation, progress and mobile layout.

Branch switching, clone, conflict resolution, automatic deployment, webhooks
and unattended background polling remain outside this increment.
