# Stacklab

Stacklab is a host-native control panel for running Docker Compose stacks on a
single Linux server. Compose files stay on disk and remain usable from the CLI;
Stacklab adds a focused web UI for daily operation, troubleshooting, and safe
maintenance without taking ownership of the stack model.

![Stacklab stacks overview](docs/images/readme/stacks-overview.png)

## What it covers

- **Compose lifecycle** — discover, create, edit, validate, start, stop, pull,
  rebuild, and remove stacks while keeping `compose.yaml` and `.env` as the
  source of truth.
- **Diagnostics** — inspect services and containers, follow logs, view live
  stats, open a container terminal, and review retained job progress.
- **Files and Git** — edit stack and config workspace files, diagnose
  permissions, review diffs, and make selective commits and pushes.
- **Maintenance** — review images, networks, and volumes; preview cleanup;
  update stacks in bulk; and schedule selected maintenance workflows.
- **Host operations** — inspect host health and Stacklab service logs, manage a
  constrained set of Docker daemon settings and registry credentials, and
  update APT-managed installations.
- **Operational guardrails** — authentication, session revocation, per-stack
  locking, audit history, explicit destructive-action review, and webhook or
  Telegram notifications.

## Is Stacklab for you?

Stacklab fits a homelab or similarly trusted environment with:

- one Linux host running Docker Engine and Compose;
- one logical local operator;
- stack definitions that should remain plain files on the host;
- access over a trusted LAN, an SSH tunnel, or an HTTPS reverse proxy.

Linux `amd64` is the primary target and Linux `arm64` is supported. Stacklab is
not a multi-host control plane, a multi-user/RBAC system, a GitOps reconciler,
or a general-purpose replacement for every Docker tool. See the full
[product scope](docs/product/scope.md) and [non-goals](docs/product/non-goals.md).

### Security boundary

Access to Docker and container terminals is highly privileged: control of the
Docker socket is effectively control of the host. Do not expose Stacklab
directly to the public internet. Prefer HTTPS even on a LAN, keep privileged
helpers opt-in and narrowly allowlisted, and follow the
[security model](docs/architecture/security-model.md) before deployment.

## Install

### Debian or Ubuntu: APT (recommended)

Install the repository key and stable channel:

```bash
sudo mkdir -p /usr/share/keyrings
curl -fsSL https://krbob.github.io/stacklab/apt/stacklab-archive-keyring.gpg \
  | sudo tee /usr/share/keyrings/stacklab-archive-keyring.gpg >/dev/null

arch="$(dpkg --print-architecture)"
echo "deb [arch=${arch} signed-by=/usr/share/keyrings/stacklab-archive-keyring.gpg] https://krbob.github.io/stacklab/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/stacklab.list

sudo apt-get update
sudo apt-get install stacklab
```

Continue with [First Run After an APT Install](docs/ops/first-run.md) to choose
the access path, initialize the operator password, and verify readiness. The
[APT guide](docs/ops/install-from-apt.md) covers channels, upgrades, and
optional privileged helpers.

### Other Linux distributions: release tarball

Release artifacts and SHA-256 checksums for supported architectures are
available from [GitHub Releases](https://github.com/krbob/stacklab/releases).
Follow the
[tarball install and upgrade guide](docs/ops/install-from-tarball.md); moving
an existing installation between the tarball and package-managed layouts is
not supported.

## Product tour

| Compose editing | Maintenance review |
| --- | --- |
| ![Compose editor with resolved configuration](docs/images/readme/stack-editor.png) | ![Maintenance update workflow](docs/images/readme/maintenance-update.png) |

## Develop and test

The canonical Go, Node.js, and tool versions live in the repository manifests.
Start the backend:

```bash
STACKLAB_BOOTSTRAP_PASSWORD=replace-with-a-long-random-password go run ./cmd/stacklab
```

Then start the frontend in another terminal:

```bash
npm --prefix frontend ci
npm --prefix frontend run dev:host
```

Run the reproducible repository baseline from the project root:

```bash
make check
```

See [local development](docs/ops/local-dev.md),
[developer checks](docs/quality/developer-checks.md), and
[CONTRIBUTING.md](CONTRIBUTING.md) for focused workflows and change
expectations.

## Documentation

Choose an entry point by what you are trying to do:

- **Operators:** [installation and operations](docs/ops/README.md),
  [first run](docs/ops/first-run.md), and
  [upgrade validation](docs/ops/upgrade-validation-checklist.md).
- **Contributors:** [documentation map](docs/README.md),
  [architecture](docs/architecture/README.md),
  [API contracts](docs/api/README.md), and
  [quality checks](docs/quality/README.md).
- **Product and UI:** [product direction](docs/product/README.md),
  [roadmap](docs/roadmap.md), and
  [information architecture](docs/ui/information-architecture.md).

## Releases, security, and license

- Installable artifacts and release notes: [GitHub Releases](https://github.com/krbob/stacklab/releases)
- Private vulnerability reporting: [Security Policy](SECURITY.md)
- Project license: [Apache License 2.0](LICENSE)
- Distributed dependency notices: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)
