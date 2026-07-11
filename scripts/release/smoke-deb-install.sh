#!/usr/bin/env bash

set -euo pipefail

SMOKE_IMAGE_TAG=""
SMOKE_CONTAINER_NAME=""
SMOKE_CONTAINER_STARTED=0

usage() {
  cat <<'EOF'
Usage:
  smoke-deb-install.sh [OPTIONS] DEB_PATH

Install and upgrade a Stacklab package inside a disposable privileged Debian
container running real systemd as PID 1.

Options:
  --platform PLATFORM   Docker platform, for example linux/amd64 or linux/arm64.
                        Default: linux/amd64
  --image IMAGE         Base image used for the systemd smoke container.
                        Default: debian:bookworm-slim
  --timeout SECONDS     Total timeout for the in-container smoke driver.
                        Default: 240
  --help                Show this help.
EOF
}

log() {
  printf '[stacklab-systemd-smoke] %s\n' "$*"
}

die() {
  printf '[stacklab-systemd-smoke] ERROR: %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

is_positive_integer() {
  [[ "$1" =~ ^[1-9][0-9]*$ ]]
}

main() {
  local platform="linux/amd64"
  local base_image="debian:bookworm-slim"
  local timeout_seconds=240

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --platform)
        [[ $# -ge 2 ]] || die "--platform requires a value"
        platform="$2"
        shift 2
        ;;
      --image)
        [[ $# -ge 2 ]] || die "--image requires a value"
        base_image="$2"
        shift 2
        ;;
      --timeout)
        [[ $# -ge 2 ]] || die "--timeout requires a value"
        is_positive_integer "$2" || die "--timeout must be a positive integer"
        timeout_seconds="$2"
        shift 2
        ;;
      --help)
        usage
        exit 0
        ;;
      --*)
        die "unknown option: $1"
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

  local script_dir
  local dockerfile
  local driver
  local deb_path="$1"
  local deb_abs
  local run_id

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  dockerfile="${script_dir}/systemd-smoke.Dockerfile"
  driver="${script_dir}/run-systemd-deb-smoke.sh"
  deb_abs="$(cd "$(dirname "${deb_path}")" && pwd)/$(basename "${deb_path}")"

  [[ -f "${deb_abs}" ]] || die "deb file not found: ${deb_abs}"
  [[ -f "${dockerfile}" ]] || die "systemd smoke Dockerfile not found: ${dockerfile}"
  [[ -x "${driver}" ]] || die "systemd smoke driver is not executable: ${driver}"

  run_id="${$}-${RANDOM}"
  SMOKE_IMAGE_TAG="stacklab-systemd-smoke:${run_id}"
  SMOKE_CONTAINER_NAME="stacklab-systemd-smoke-${run_id}"

  diagnostics() {
    if [[ "${SMOKE_CONTAINER_STARTED}" -ne 1 ]]; then
      return 0
    fi
    printf '\n[stacklab-systemd-smoke] systemd state\n' >&2
    docker exec "${SMOKE_CONTAINER_NAME}" systemctl is-system-running >&2 || true
    printf '\n[stacklab-systemd-smoke] stacklab.service status\n' >&2
    docker exec "${SMOKE_CONTAINER_NAME}" \
      systemctl status --no-pager --full stacklab.service >&2 || true
    printf '\n[stacklab-systemd-smoke] stacklab.service journal\n' >&2
    docker exec "${SMOKE_CONTAINER_NAME}" \
      journalctl --no-pager --boot -u stacklab.service -n 250 >&2 || true
    printf '\n[stacklab-systemd-smoke] container log\n' >&2
    docker logs "${SMOKE_CONTAINER_NAME}" >&2 || true
  }

  cleanup() {
    local exit_code=$?
    trap - EXIT
    if [[ "${exit_code}" -ne 0 ]]; then
      diagnostics
    fi
    docker rm --force "${SMOKE_CONTAINER_NAME}" >/dev/null 2>&1 || true
    docker image rm --force "${SMOKE_IMAGE_TAG}" >/dev/null 2>&1 || true
    exit "${exit_code}"
  }
  trap cleanup EXIT

  log "Building isolated systemd image from ${base_image}"
  docker build \
    --platform "${platform}" \
    --build-arg "BASE_IMAGE=${base_image}" \
    --tag "${SMOKE_IMAGE_TAG}" \
    --file "${dockerfile}" \
    "${script_dir}"

  log "Starting privileged Debian container with systemd as PID 1"
  docker run \
    --detach \
    --name "${SMOKE_CONTAINER_NAME}" \
    --hostname stacklab-systemd-smoke \
    --platform "${platform}" \
    --privileged \
    --cgroupns private \
    --network none \
    --tmpfs /run \
    --tmpfs /run/lock \
    --volume "${deb_abs}:/artifacts/stacklab-b.deb:ro" \
    --volume "${driver}:/usr/local/bin/run-systemd-deb-smoke:ro" \
    "${SMOKE_IMAGE_TAG}" >/dev/null
  SMOKE_CONTAINER_STARTED=1

  local systemd_deadline=$((SECONDS + 30))
  while (( SECONDS < systemd_deadline )); do
    if docker exec "${SMOKE_CONTAINER_NAME}" \
      sh -c 'test "$(cat /proc/1/comm)" = systemd && systemctl show --property=SystemState --value >/dev/null'; then
      break
    fi
    sleep 1
  done
  docker exec "${SMOKE_CONTAINER_NAME}" \
    sh -c 'test "$(cat /proc/1/comm)" = systemd && systemctl show --property=SystemState --value >/dev/null' \
    || die "systemd did not become available as PID 1 within 30s"

  log "Running real-systemd package install and upgrade smoke"
  docker exec \
    --env STACKLAB_SMOKE_PASSWORD=stacklab-systemd-smoke \
    --env STACKLAB_SERVICE_TIMEOUT_SECONDS=60 \
    "${SMOKE_CONTAINER_NAME}" \
    timeout --signal=TERM --kill-after=15 "${timeout_seconds}" \
    /usr/local/bin/run-systemd-deb-smoke /artifacts/stacklab-b.deb
}

main "$@"
