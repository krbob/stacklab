# ADR 0004: Deployment-Aware Filesystem Layout

## Status

Accepted

## Context

The original host-native and Compose-first decisions used `/opt/stacklab` as a
single canonical prefix. That remains correct for the manual tarball installer,
but the Debian package now follows a package-native split:

- immutable application files under `/usr/lib/stacklab`;
- operator-managed workspace content under `/srv/stacklab`;
- private runtime state under `/var/lib/stacklab`;
- root-owned service configuration under `/etc/stacklab`.

Treating `/opt/stacklab` as universal makes the architecture contradict the
package, systemd unit, and migration guidance. It also conflates three
different concerns: managed Compose content, application artifacts, and
private application state.

## Decision

Architecture and domain contracts will use `<STACKLAB_ROOT>` for the configured
managed workspace. Its required portable structure is:

- `<STACKLAB_ROOT>/stacks`;
- `<STACKLAB_ROOT>/config`;
- `<STACKLAB_ROOT>/data`;
- optional `<STACKLAB_ROOT>/templates` and workspace-root Git metadata.

The supported production defaults are:

| Profile | `<STACKLAB_ROOT>` | Application files | `STACKLAB_DATA_DIR` |
| --- | --- | --- | --- |
| Debian/APT package | `/srv/stacklab` | `/usr/lib/stacklab` | `/var/lib/stacklab` |
| Manual tarball | `/opt/stacklab` | `/opt/stacklab/app` | `/var/lib/stacklab` |

Both profiles use `/etc/stacklab/stacklab.env` for service configuration. The
tarball application directory is colocated below its root for compatibility
with the release/symlink installer, but it is not managed stack content.

References to `/opt/stacklab/stacks`, `/opt/stacklab/config`, or
`/opt/stacklab/data` in ADR 0001 and ADR 0002 describe the tarball profile. The
host-native deployment and Compose-first source-of-truth decisions remain in
force; only their universal path assumption is superseded by this ADR.

Custom roots are configuration, not another default profile. They require
coordinated service environment, ownership, systemd sandbox, working-directory,
and privileged-helper changes. Stacklab does not automatically migrate content
between roots.

## Rationale

- preserves one portable domain model across both release channels;
- matches the paths installed and validated by the repository;
- keeps package-owned artifacts separate from operator-managed content;
- distinguishes stack payload data from private SQLite and credential state;
- allows documentation and tests to express path-independent behavior.

## Consequences

### Positive

- APT and tarball documentation no longer compete over a universal root.
- Architecture references can be reused for custom roots and local fixtures.
- Backup, permissions, Git, and migration boundaries become explicit.

### Negative

- Examples must name their deployment profile or use `<STACKLAB_ROOT>`.
- The tarball profile still physically nests application artifacts below its
  workspace root and must exclude them from workspace Git tracking.
- Operators changing defaults must update the full service sandbox and helper
  configuration themselves.

## Follow-Up

- keep package and tarball smoke tests asserting their respective defaults;
- use `<STACKLAB_ROOT>` in portable product, domain, API, and quality contracts;
- document migration explicitly instead of silently moving workspace content.
