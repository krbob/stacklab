#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  smoke-tarball-install.sh [OPTIONS] TARBALL_PATH

Install and upgrade a Stacklab release tarball inside a disposable Debian
container and run a smoke check against the host-side upgrade script.

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

  local tarball_path="$1"
  local tarball_abs
  tarball_abs="$(cd "$(dirname "${tarball_path}")" && pwd)/$(basename "${tarball_path}")"

  [[ -f "${tarball_abs}" ]] || {
    echo "tarball not found: ${tarball_abs}" >&2
    exit 1
  }

  docker run --rm \
    --platform "${platform}" \
    -v "${tarball_abs}:/tmp/stacklab.tar.gz:ro" \
    "${image}" \
    bash -lc '
      set -euo pipefail
      export DEBIAN_FRONTEND=noninteractive

      apt-get update >/dev/null
      apt-get install -y --no-install-recommends \
        adduser \
        ca-certificates \
        curl \
        passwd \
        perl \
        tar >/dev/null

      cat >/usr/local/bin/systemctl <<'"'"'EOF'"'"'
#!/usr/bin/env bash
set -euo pipefail
printf "%s\n" "$*" >> /var/tmp/stacklab-systemctl.log
if [[ "${1:-}" == "restart" && "${SYSTEMCTL_FAIL_RESTART:-0}" == "1" ]]; then
  exit 1
fi
exit 0
EOF
      chmod +x /usr/local/bin/systemctl

      mkdir -p /tmp/artifact-a /tmp/artifact-b /tmp/artifact-c
      tar -xzf /tmp/stacklab.tar.gz -C /tmp/artifact-a
      artifact_a="$(find /tmp/artifact-a -mindepth 1 -maxdepth 1 -type d | head -n1)"

      test -n "${artifact_a}"
      test -x "${artifact_a}/host-tools/upgrade.sh"
      test -f "${artifact_a}/systemd/stacklab.service.example"
      test -f "${artifact_a}/systemd/stacklab.env.example"

      STACKLAB_BOOTSTRAP_PASSWORD=smoke-pass \
        "${artifact_a}/host-tools/upgrade.sh" --install-unit --no-health-check

      release_a="$(readlink -f /opt/stacklab/app/current)"
      test -n "${release_a}"
      test -d "${release_a}"
      test -f /etc/systemd/system/stacklab.service
      test -f /etc/stacklab/stacklab.env
      grep -q "^STACKLAB_ROOT=/opt/stacklab$" /etc/stacklab/stacklab.env
      grep -q "^STACKLAB_DATA_DIR=/var/lib/stacklab$" /etc/stacklab/stacklab.env
      grep -q "^STACKLAB_BOOTSTRAP_PASSWORD=smoke-pass$" /etc/stacklab/stacklab.env
      getent passwd stacklab >/dev/null
      getent group stacklab >/dev/null
      test -d /var/lib/stacklab/home
      test -d /var/lib/stacklab/docker
      grep -q "^daemon-reload$" /var/tmp/stacklab-systemctl.log
      grep -q "^enable stacklab$" /var/tmp/stacklab-systemctl.log
      grep -q "^restart stacklab$" /var/tmp/stacklab-systemctl.log

      mkdir -p /opt/stacklab/stacks/demo /opt/stacklab/config/demo /opt/stacklab/data/demo
      printf "services:\n" >/opt/stacklab/stacks/demo/compose.yaml
      printf "hello=world\n" >/opt/stacklab/config/demo/app.env
      printf "keep\n" >/opt/stacklab/data/demo/sentinel.txt

      cp -R "${artifact_a}" /tmp/artifact-b/stacklab-upgrade-b
      printf "0.0.0-smoke-upgrade\n" >/tmp/artifact-b/stacklab-upgrade-b/metadata/version.txt
      /tmp/artifact-b/stacklab-upgrade-b/host-tools/upgrade.sh --no-health-check

      release_b="$(readlink -f /opt/stacklab/app/current)"
      test -n "${release_b}"
      test -d "${release_b}"
      test "${release_b}" != "${release_a}"
      test -d "${release_a}"
      test -f /opt/stacklab/stacks/demo/compose.yaml
      test -f /opt/stacklab/config/demo/app.env
      test -f /opt/stacklab/data/demo/sentinel.txt

      cp -R "${artifact_a}" /tmp/artifact-c/stacklab-upgrade-c
      printf "0.0.0-smoke-rollback\n" >/tmp/artifact-c/stacklab-upgrade-c/metadata/version.txt
      if SYSTEMCTL_FAIL_RESTART=1 /tmp/artifact-c/stacklab-upgrade-c/host-tools/upgrade.sh --no-health-check; then
        echo "expected restart failure to trigger rollback" >&2
        exit 1
      fi

      test "$(readlink -f /opt/stacklab/app/current)" = "${release_b}"
      restart_count="$(grep -c "^restart stacklab$" /var/tmp/stacklab-systemctl.log)"
      test "${restart_count}" -ge 4
    '
}

main "$@"
