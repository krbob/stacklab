# Debian Package and APT Model

## Purpose

This document records the current Debian packaging strategy for Stacklab.

Current status:

- `.deb` artifacts are implemented
- the published APT repository is implemented
- Debian-family package-managed installs are the primary supported production path

Support policy for the package:

- the `.deb` targets Debian-family hosts running `systemd`
- Debian systems booted with another init system are out of scope for the first package version

## Why This Makes Sense

Stacklab has a very opinionated production target:

- single host
- Debian-family Linux
- host-native runtime
- `systemd`
- Docker Engine plus Compose v2

That makes Debian packaging a good fit.

Benefits:

- explicit package dependencies
- standard install and upgrade path
- easier first-time setup on Debian
- easier long-term maintenance on a homelab host
- cleaner documentation for operators

## Support Boundaries

Primary install mode:

- Debian-family hosts via `.deb` and the published APT repository

Secondary install mode:

- generic Linux hosts via manual tarball install

Unsupported transitions:

- tarball to `.deb`
- `.deb` to tarball
- in-place migration between `/opt/stacklab` and `/srv/stacklab`

## Package Scope

The Debian package should install the application runtime, not user-managed stacks.

Package-managed content:

- backend binary
- frontend bundle
- `systemd` unit
- default environment file template
- documentation snippets or examples as needed

Operator-managed content that must remain outside package ownership:

- `/srv/stacklab/stacks`
- `/srv/stacklab/config`
- `/srv/stacklab/data`
- most of `/var/lib/stacklab` runtime state

This rule is critical:

- package install and upgrade must never take ownership away from the filesystem-first Compose model

## Package Layout

The `.deb` uses a Debian-native split between:

- package-managed immutable application files
- operator-managed workspace files
- runtime state owned by Stacklab itself

Recommended package layout:

```text
/usr/lib/stacklab/
  bin/stacklab
  frontend/dist/
/etc/stacklab/
  stacklab.env
/srv/stacklab/
  stacks/
  config/
  data/
/var/lib/stacklab/
```

Meaning:

- `/usr/lib/stacklab` holds versioned application payload installed by `dpkg`
- `/etc/stacklab/stacklab.env` holds service configuration
- `/srv/stacklab` is the operator-managed workspace and Git root
- `/var/lib/stacklab` holds SQLite, service home, Docker config, and other runtime state

Why not keep `/opt/stacklab` for the package:

- `/opt` is reasonable for tarball/manual installs of third-party software
- for a native Debian package, `/usr/lib` + `/etc` + `/var/lib` is more conventional
- operator-managed stack/config/data content fits better under `/srv` than under `/var/lib`
- this keeps package-owned files and operator-owned files clearly separate

Practical recommendation:

- use `/usr/lib/stacklab` + `/srv/stacklab` + `/var/lib/stacklab` for Debian-family installs
- keep `/opt/stacklab` only for the separate tarball install mode

## Package Dependencies

The package should declare runtime dependencies clearly.

Expected Debian dependency shape:

- `systemd`
- `docker.io | docker-ce | moby-engine`
- `docker-compose | docker-compose-plugin`
- `git`

Reasoning:

- `systemd` is an intentional product dependency for the package, not an accidental implementation detail
- Docker may come from Debian packages or vendor packages
- Compose may exist as the standalone `docker-compose` binary or the plugin-backed `docker compose`
- Git is now part of the supported operator workflow, not an optional extra

Recommended `Recommends`:

- `ca-certificates`

Notes:

- stock Debian may provide Compose via standalone `docker-compose`
- some hosts may use Docker CE packages instead of distro Docker
- Stacklab already supports both `docker compose` and standalone `docker-compose`
- tarball installs may still rely on host tooling such as `curl`, `wget`, and `tar`, but those do not need to become hard runtime dependencies of the package itself

## Package Responsibilities

The package should:

- install Stacklab binaries and frontend assets
- install a `systemd` unit
- create or install `/etc/stacklab/stacklab.env`
- create a dedicated service account such as `stacklab`
- ensure ownership and permissions are sane
- enable the service only if that behavior is explicitly chosen in packaging policy

The package should not:

- overwrite operator stack definitions
- destroy `config/` or `data/`
- silently reset the password store
- assume reverse proxy configuration

## Maintainer Script Responsibilities

Likely maintainer scripts:

- `postinst`
- `prerm`
- possibly `postrm`

Expected `postinst` responsibilities:

- create service account if missing
- create required directories if missing
- install default env file if absent
- ensure the service account is in:
  - the Docker socket-owning group when present, typically `docker`
  - `systemd-journal` when present, so `/host` Stacklab logs can be read
- reload `systemd`
- optionally enable or restart the service

Expected `prerm` responsibilities:

- stop or restart service in a predictable way during upgrade/remove
- avoid deleting operator-managed state

Important rule:

- maintainer scripts must be idempotent
- maintainer scripts must handle upgrade and reinstall paths cleanly

## APT Repository Model

The published APT repository should be:

- signed
- static and reproducible
- generated by CI from already-built `.deb` artifacts

Reasonable hosting options:

- GitHub Pages
- another static HTTP host

Reasonable generation options:

- `aptly`
- `reprepro`
- a smaller static repository generator if it stays understandable

Recommended channel model:

- `stable`
- `nightly`

Meaning:

- `stable` contains monthly releases and hotfixes
- `nightly` contains prerelease builds from the default branch

## Release Validation Expectations

The release flow for the package-managed path should look like this:

1. build and test backend/frontend
2. build Linux `amd64` package artifact
3. test package install on clean Debian VM
4. test package upgrade from previous version
5. test service start under `systemd`
6. test login, dashboard, actions, logs, stats, terminal
7. publish `.deb` to GitHub Releases and APT

## Validation Requirements

Before trusting Debian packaging, the following should pass on Debian `amd64`:

- fresh install
- upgrade install
- reinstall
- service restart
- package removal without destructive data loss
- package purge policy clearly defined

Specific checks:

- `docker-compose-plugin` hosts work
- standalone `docker-compose` hosts work
- service account has Docker access
- `ProtectHome=true` plus `HOME` and `DOCKER_CONFIG` paths still work

## Remaining Gaps Worth Closing

- keep upgrade validation current as new product surfaces are added
- keep helper-backed privileged flows tested on real Debian hosts
- keep tarball smoke green so the secondary install mode does not drift behind the Debian package path
