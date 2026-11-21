# Incoming configuration changes

Operators can edit the homelab repository on another machine, push it, review
the incoming version in Stacklab, download that version, and apply selected
stacks. Local files remain the configuration source of truth.

## Operator flow

Config checks the upstream on entry and when returning to the browser tab,
with a one-minute cooldown for automatic checks. A persistent status banner
shows incoming/local commits, the last successful check, or an unverified remote
when the check fails. Fetch never replaces editor contents. Local editing remains
available offline. Commit and push check again; an incoming version requires an
explicit choice to preserve local edits in a commit for subsequent reconciliation.

Config exposes a **Remote updates** view. Opening it checks the configured
upstream; an explicit refresh checks again. The view shows the checked time,
branch, incoming commit count, every changed repository path and bounded diffs.
Changes outside `stacks/` and `config/` are identified separately because a
Git fast-forward updates the whole checkout. They do not become editable through
the managed file workspace.

Downloading requires review of the exact local and upstream commits. It never
merges divergent histories, stashes local edits or resets the checkout. Tracked
and untracked local changes, an unfinished Git operation, and stale reviews
block the operation. Ignored files must not be silently overwritten. Checkouts
with hidden-index flags (`assume-unchanged` / `skip-worktree`) or submodules
require an external update or resolution of those flags. Git hooks
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

## Reconciliation of local and remote changes

Uncommitted changes are first saved explicitly using the existing Changes commit
flow. The synchronization feature never silently commits untracked files or
stashes edits. For diverged commits, **Reconcile changes** prepares a merge in a
private, independent repository under the application data directory. Preparation
does not change the managed checkout, its index, branch, or running containers.

The draft pins both commits. Native Git combines non-conflicting changes. The UI
shows each conflict's base, local and remote version; users can select a side,
delete the file, or enter a text result for an ordinary managed file. Binary and
non-regular files allow selection of an existing version, not text conversion.
The complete resulting diff is available before approval. Local commits remain
in history through a two-parent merge commit; pushing the result is separate.

One durable draft is retained across page/service restarts, with explicit discard.
Draft revisions prevent one browser from overwriting another's resolutions. Draft
creation and updates are serialized, bounded and never invoke configured external
merge drivers or Git hooks. Limits and unsupported Git arrangements return a
clear error, preserving the checkout and previous draft.

Approval rechecks the branch, fetched upstream, HEAD, entire working tree, index
flags and unfinished operations. It imports the prepared commit and performs a
native fast-forward under the same workspace/job locks as a normal download.
Ignored-file collisions remain protected. No reset, automatic push or container
recreation occurs. A durable receipt supports recovery after request interruption.
New local edits or newly fetched upstream commits invalidate approval; the draft
must be recreated after preserving/resolving those changes. External Git/filesystem
writers still require coordination. A fetch does not reserve the remote branch;
an ordinary rejected push remains possible if another writer pushes afterwards.

Branch switching, initial workspace cloning, automatic deployment, webhooks and
unattended background polling remain outside this increment.

Execution evidence is recorded in [Git sync validation](../quality/git-sync-validation.md).
