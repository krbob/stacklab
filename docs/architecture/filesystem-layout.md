# Filesystem Layout

## Terminology

`<STACKLAB_ROOT>` means the effective managed workspace configured by
`STACKLAB_ROOT`. It is the source-of-truth root for stacks, shared config,
operator-managed data, templates, and optional Git metadata. It is not a fixed
installation prefix and it is not the private application state directory.

`STACKLAB_DATA_DIR` is the separate private runtime-state directory. Both
supported production profiles use `/var/lib/stacklab` by default.

## Supported Deployment Profiles

| Profile | `<STACKLAB_ROOT>` | Application files | Runtime state | Configuration |
| --- | --- | --- | --- | --- |
| Debian/APT package | `/srv/stacklab` | `/usr/lib/stacklab` | `/var/lib/stacklab` | `/etc/stacklab/stacklab.env` |
| Manual tarball | `/opt/stacklab` | `/opt/stacklab/app` | `/var/lib/stacklab` | `/etc/stacklab/stacklab.env` |

The Debian package runs `/usr/lib/stacklab/bin/stacklab`. The tarball installer
stores versioned releases under `/opt/stacklab/app/releases/` and points
`/opt/stacklab/app/current` at the active release. Although the tarball
application directory is physically below `/opt/stacklab`, it is deployment
content, not part of the managed stack workspace and should not be committed to
the workspace Git repository.

Repository development uses `$PWD/.local/stacklab` and
`$PWD/.local/var/lib/stacklab` by default. Those relative fallback values are
for local development, not a third production layout.

Custom paths are possible through the service environment. A production
`STACKLAB_ROOT` override must also be reflected in the service working
directory, systemd write allowlist, ownership, and privileged workspace-helper
configuration. `STACKLAB_DATA_DIR`, `STACKLAB_DATABASE_PATH`, and the frontend
path are independent settings and do not move with the workspace implicitly.
Changing any path does not migrate existing content automatically.

See [ADR 0004](../adr/0004-deployment-aware-filesystem-layout.md) for the
decision behind the deployment-aware root.

## Managed Workspace

The portable workspace layout is:

```text
<STACKLAB_ROOT>/
├── stacks/
│   └── <stack>/
│       ├── compose.yaml
│       ├── .env                 # optional
│       └── ...                  # optional stack-local workspace files
├── config/
│   └── <stack>/
├── data/
│   └── <stack>/
├── templates/                   # optional operator templates
│   └── <template>/
│       ├── compose.yaml
│       └── template.yaml        # optional metadata and variables
└── .git/                        # optional; Git root must equal STACKLAB_ROOT
```

### Stack Definitions

- `<STACKLAB_ROOT>/stacks/<stack>/compose.yaml` is required for a discovered
  stack.
- `<STACKLAB_ROOT>/stacks/<stack>/.env` is optional and supported by editor,
  validation, and Compose operations.
- Other files below the stack directory form the stack-local workspace. The
  dedicated definition editor remains responsible for `compose.yaml` and
  `.env`.

### Shared Stack Configuration

`<STACKLAB_ROOT>/config/<stack>/` contains operator-managed configuration that
may be mounted into services. It may contain service-specific subdirectories
and may be versioned with the workspace.

### Stack Payload Data

`<STACKLAB_ROOT>/data/<stack>/` is for durable, operator-managed service data.
It is distinct from `STACKLAB_DATA_DIR`, should normally be excluded from Git,
and is never included in a default definition-removal flow. Deleting it
requires an explicit destructive option.

### Templates

`<STACKLAB_ROOT>/templates/<template>/compose.yaml` defines an operator
template. An optional `template.yaml` provides its display metadata and
variables. Stacklab falls back to built-in templates when the directory is
absent or contains no usable templates.

### Git Workspace

Git integration is available only when `<STACKLAB_ROOT>` itself is the
repository root. Stacklab does not search parent directories and does not treat
a nested repository as the managed workspace.

## Private Runtime State

The default `STACKLAB_DATA_DIR`, `/var/lib/stacklab`, contains application
state that must not be stored in the managed workspace:

- `stacklab.db` and its SQLite WAL/SHM files;
- the service account home directory;
- Docker registry client configuration when `DOCKER_CONFIG` uses the packaged
  default;
- Docker daemon configuration backups when that optional helper is enabled;
- temporary files used while validating unsaved environment content.

`STACKLAB_DATABASE_PATH` may override the database location; otherwise it is
`<STACKLAB_DATA_DIR>/stacklab.db`.

## Stack Identity And Discovery

- The directory name below `<STACKLAB_ROOT>/stacks/` is the canonical stack
  identifier.
- Valid identifiers match `^[a-z0-9]+(?:-[a-z0-9]+)*$`.
- A real directory is discovered only when it contains `compose.yaml`.
- A symlink cannot act as a stack root, and canonical path checks must keep all
  managed file operations inside the configured workspace.
- `compose.yml`, multiple Compose files, and profile-specific discovery are not
  supported discovery alternatives.

## Ownership And Safety

- Production services run as the dedicated non-root `stacklab` Unix account.
- That Unix account is the process identity, not a separate product user; the
  application exposes one logical `Local Operator` identity.
- Stacklab edits only paths allowed by the configured workspace and the
  narrower file-workspace policies.
- Destructive actions are explicit and scoped; stack payload data is retained
  unless separately selected.
- Atomic replacements follow the owner/group/mode, ACL/xattr, and durability
  rules in [Filesystem Metadata and Atomic Writes](filesystem-metadata-policy.md).
