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
  local checksum_abs="${tarball_abs}.sha256"

  [[ -f "${tarball_abs}" ]] || {
    echo "tarball not found: ${tarball_abs}" >&2
    exit 1
  }
  [[ -f "${checksum_abs}" ]] || {
    echo "checksum not found: ${checksum_abs}" >&2
    exit 1
  }

  docker run --rm \
    --platform "${platform}" \
    -v "${tarball_abs}:/tmp/stacklab.tar.gz:ro" \
    -v "${checksum_abs}:/tmp/stacklab.tar.gz.sha256:ro" \
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
        python3 \
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

      expected_a="$(awk "NR == 1 { print \$1 }" /tmp/stacklab.tar.gz.sha256)"
      actual_a="$(sha256sum /tmp/stacklab.tar.gz)"
      actual_a="${actual_a%% *}"
      test "${actual_a}" = "${expected_a}"

      mkdir -p /tmp/artifact-a /tmp/release-assets
      tar -xzf /tmp/stacklab.tar.gz -C /tmp/artifact-a
      artifact_a="$(find /tmp/artifact-a -mindepth 1 -maxdepth 1 -type d | head -n1)"

      test -n "${artifact_a}"
      test -x "${artifact_a}/host-tools/upgrade.sh"
      test -f "${artifact_a}/systemd/stacklab.service.example"
      test -f "${artifact_a}/systemd/stacklab.env.example"
      test -f "${artifact_a}/LICENSE"
      test -f "${artifact_a}/NOTICE"
      test -f "${artifact_a}/THIRD_PARTY_NOTICES.md"
      test -f "${artifact_a}/frontend/dist/font-licenses.txt"
      grep -Fq "Apache License" "${artifact_a}/LICENSE"
      grep -Fq "Copyright 2011-2016 Canonical Ltd." "${artifact_a}/NOTICE"
      grep -Fq "modernc.org/libc" "${artifact_a}/THIRD_PARTY_NOTICES.md"
      grep -Fq "SIL Open Font License, Version 1.1" "${artifact_a}/frontend/dist/font-licenses.txt"

      STACKLAB_BOOTSTRAP_PASSWORD=smoke-pass \
        "${artifact_a}/host-tools/upgrade.sh" --install-unit --no-health-check

      release_a="$(readlink -f /opt/stacklab/app/current)"
      test -n "${release_a}"
      test -d "${release_a}"
      test -f "${release_a}/LICENSE"
      test -f "${release_a}/NOTICE"
      test -f "${release_a}/THIRD_PARTY_NOTICES.md"
      test -f /etc/systemd/system/stacklab.service
      test -f /etc/stacklab/stacklab.env
      test "$(stat -c %a /etc/stacklab/stacklab.env)" = "600"
      grep -q "^STACKLAB_ROOT=/opt/stacklab$" /etc/stacklab/stacklab.env
      grep -q "^STACKLAB_DATA_DIR=/var/lib/stacklab$" /etc/stacklab/stacklab.env
      grep -q "^STACKLAB_BOOTSTRAP_PASSWORD=smoke-pass$" /etc/stacklab/stacklab.env
      getent passwd stacklab >/dev/null
      getent group stacklab >/dev/null
      test -d /var/lib/stacklab/home
      test -d /var/lib/stacklab/docker
      test "$(stat -c %a /var/lib/stacklab)" = "700"
      grep -q "^daemon-reload$" /var/tmp/stacklab-systemctl.log
      grep -q "^enable stacklab$" /var/tmp/stacklab-systemctl.log
      grep -q "^restart stacklab$" /var/tmp/stacklab-systemctl.log

      mkdir -p /opt/stacklab/stacks/demo /opt/stacklab/config/demo /opt/stacklab/data/demo
      printf "services:\n" >/opt/stacklab/stacks/demo/compose.yaml
      printf "SECRET=smoke\n" >/opt/stacklab/stacks/demo/.env
      chmod 0644 /opt/stacklab/stacks/demo/.env
      printf "hello=world\n" >/opt/stacklab/config/demo/app.env
      printf "keep\n" >/opt/stacklab/data/demo/sentinel.txt

      make_archive() {
        local release_name="$1"
        local version="$2"
        local build_dir="/tmp/build-${release_name}"
        local archive_name="${release_name}.tar.gz"

        mkdir -p "${build_dir}"
        cp -R "${artifact_a}" "${build_dir}/${release_name}"
        printf "%s\n" "${version}" >"${build_dir}/${release_name}/metadata/version.txt"
        tar -czf "/tmp/release-assets/${archive_name}" -C "${build_dir}" "${release_name}"
        (
          cd /tmp/release-assets
          sha256sum "${archive_name}" >"${archive_name}.sha256"
        )
      }

      make_archive stacklab-upgrade-b 0.0.0-smoke-upgrade

      cp /tmp/release-assets/stacklab-upgrade-b.tar.gz /tmp/release-assets/stacklab-missing-checksum.tar.gz
      if "${artifact_a}/host-tools/upgrade.sh" --no-health-check /tmp/release-assets/stacklab-missing-checksum.tar.gz; then
        echo "expected a missing checksum to reject the archive" >&2
        exit 1
      fi
      test "$(readlink -f /opt/stacklab/app/current)" = "${release_a}"

      cp /tmp/release-assets/stacklab-upgrade-b.tar.gz /tmp/release-assets/stacklab-tampered.tar.gz
      cp /tmp/release-assets/stacklab-upgrade-b.tar.gz.sha256 /tmp/release-assets/stacklab-tampered.tar.gz.sha256
      printf "tampered\n" >>/tmp/release-assets/stacklab-tampered.tar.gz
      if "${artifact_a}/host-tools/upgrade.sh" --no-health-check /tmp/release-assets/stacklab-tampered.tar.gz; then
        echo "expected a checksum mismatch to reject the archive" >&2
        exit 1
      fi
      test "$(readlink -f /opt/stacklab/app/current)" = "${release_a}"

      "${artifact_a}/host-tools/upgrade.sh" --no-health-check /tmp/release-assets/stacklab-upgrade-b.tar.gz
      test "$(stat -c %U:%G /opt/stacklab/stacks/demo/.env)" = "root:stacklab"
      test "$(stat -c %a /opt/stacklab/stacks/demo/.env)" = "640"

      release_b="$(readlink -f /opt/stacklab/app/current)"
      test -n "${release_b}"
      test -d "${release_b}"
      test "${release_b}" != "${release_a}"
      test -d "${release_a}"
      test -f /opt/stacklab/stacks/demo/compose.yaml
      test -f /opt/stacklab/config/demo/app.env
      test -f /opt/stacklab/data/demo/sentinel.txt

      make_archive stacklab-upgrade-c 0.0.0-smoke-url
      python3 -m http.server 18080 --bind 127.0.0.1 --directory /tmp/release-assets >/tmp/stacklab-http.log 2>&1 &
      http_pid=$!
      for _ in $(seq 1 20); do
        if curl -fsS http://127.0.0.1:18080/stacklab-upgrade-c.tar.gz.sha256 >/dev/null; then
          break
        fi
        sleep 0.1
      done
      if ! curl -fsS http://127.0.0.1:18080/stacklab-upgrade-c.tar.gz.sha256 >/dev/null; then
        cat /tmp/stacklab-http.log >&2
        exit 1
      fi
      "${artifact_a}/host-tools/upgrade.sh" --no-health-check http://127.0.0.1:18080/stacklab-upgrade-c.tar.gz
      kill "${http_pid}"
      wait "${http_pid}" 2>/dev/null || true

      release_c="$(readlink -f /opt/stacklab/app/current)"
      test -n "${release_c}"
      test "${release_c}" != "${release_b}"

      make_archive stacklab-upgrade-d 0.0.0-smoke-rollback
      expected_d="$(awk "NR == 1 { print \$1 }" /tmp/release-assets/stacklab-upgrade-d.tar.gz.sha256)"
      if SYSTEMCTL_FAIL_RESTART=1 "${artifact_a}/host-tools/upgrade.sh" \
        --sha256 "${expected_d}" \
        --no-health-check \
        /tmp/release-assets/stacklab-upgrade-d.tar.gz; then
        echo "expected restart failure to trigger rollback" >&2
        exit 1
      fi

      test "$(readlink -f /opt/stacklab/app/current)" = "${release_c}"
      restart_count="$(grep -c "^restart stacklab$" /var/tmp/stacklab-systemctl.log)"
      test "${restart_count}" -ge 5
    '
}

main "$@"
