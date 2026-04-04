#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  smoke-deb-install.sh [OPTIONS] DEB_PATH

Install a Stacklab .deb inside a disposable Debian container and run a basic smoke check.

Options:
  --platform PLATFORM   Docker platform, for example linux/amd64 or linux/arm64.
                        Default: linux/amd64
  --image IMAGE         Debian container image. Default: debian:bookworm-slim
  --help                Show this help.
EOF
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

main() {
  local platform="linux/amd64"
  local image="debian:bookworm-slim"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --platform)
        platform="$2"
        shift 2
        ;;
      --image)
        image="$2"
        shift 2
        ;;
      --help)
        usage
        exit 0
        ;;
      --*)
        echo "unknown option: $1" >&2
        usage
        exit 1
        ;;
      *)
        break
        ;;
    esac
  done

  [[ $# -eq 1 ]] || {
    usage
    exit 1
  }

  need_cmd docker

  local deb_path="$1"
  local deb_abs
  deb_abs="$(cd "$(dirname "${deb_path}")" && pwd)/$(basename "${deb_path}")"

  [[ -f "${deb_abs}" ]] || {
    echo "deb file not found: ${deb_abs}" >&2
    exit 1
  }

  docker run --rm \
    --platform "${platform}" \
    -v "${deb_abs}:/tmp/stacklab.deb:ro" \
    "${image}" \
    bash -lc '
      set -euo pipefail
      export DEBIAN_FRONTEND=noninteractive
      apt-get update >/dev/null
      apt-get install -y --no-install-recommends \
        adduser \
        ca-certificates \
        docker.io \
        docker-compose \
        git \
        systemd >/dev/null

      dpkg -i /tmp/stacklab.deb >/dev/null

      test -x /usr/lib/stacklab/bin/stacklab
      test -f /etc/stacklab/stacklab.env
      test -f /lib/systemd/system/stacklab.service
      test -d /srv/stacklab/stacks
      test -d /srv/stacklab/config
      test -d /srv/stacklab/data
      test -d /var/lib/stacklab/home
      test -d /var/lib/stacklab/docker
      getent passwd stacklab >/dev/null
      getent group stacklab >/dev/null
    '
}

main "$@"
