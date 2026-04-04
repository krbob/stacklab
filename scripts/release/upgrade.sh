#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  upgrade.sh [OPTIONS] [ARTIFACT]

Install or upgrade Stacklab from a release artifact.

ARTIFACT can be:
  - a local .tar.gz release artifact
  - an extracted artifact directory
  - an HTTPS URL pointing to the release artifact

If ARTIFACT is omitted, the script assumes it is being run from an extracted
artifact copy located under <artifact-root>/host-tools/upgrade.sh.

Options:
  --install-unit         Install the example systemd unit and env file if missing.
  --service-name NAME    systemd service name. Default: stacklab
  --service-user USER    systemd service user. Default: stacklab
  --service-group GROUP  systemd service group. Default: stacklab
  --app-root PATH        Application root. Default: /opt/stacklab/app
  --stacklab-root PATH   Managed Stacklab root. Default: /opt/stacklab
  --data-dir PATH        Runtime data directory. Default: /var/lib/stacklab
  --health-url URL       Health check URL. Default: http://127.0.0.1:8080/api/health
  --no-health-check      Skip the post-restart health check.
  --help                 Show this help.

Environment:
  STACKLAB_BOOTSTRAP_PASSWORD
      If set and /etc/stacklab/stacklab.env is created by this script, the value
      is written into that file as the first-run bootstrap password.
EOF
}

log() {
  printf '[stacklab-upgrade] %s\n' "$*"
}

die() {
  printf '[stacklab-upgrade] ERROR: %s\n' "$*" >&2
  exit 1
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    die "this script must be run as root"
  fi
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

download_http() {
  local source="$1"
  local target="$2"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "${source}" -o "${target}"
    return 0
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -qO "${target}" "${source}"
    return 0
  fi

  die "missing required command: curl or wget"
}

check_health_once() {
  local health_url="$1"

  if command -v curl >/dev/null 2>&1; then
    curl -fsS "${health_url}" >/dev/null 2>&1
    return $?
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -qO /dev/null "${health_url}"
    return $?
  fi

  die "missing required command: curl or wget"
}

fetch_to_temp() {
  local source="$1"
  local target="$2"

  if [[ "${source}" =~ ^https?:// ]]; then
    log "Downloading artifact from ${source}"
    download_http "${source}" "${target}"
  else
    cp "${source}" "${target}"
  fi
}

extract_artifact() {
  local source="$1"
  local work_dir="$2"
  local tarball="${work_dir}/artifact.tar.gz"

  fetch_to_temp "${source}" "${tarball}"
  tar -xzf "${tarball}" -C "${work_dir}"

  local artifact_root
  artifact_root="$(find "${work_dir}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
  [[ -n "${artifact_root}" ]] || die "artifact archive did not contain a top-level directory"
  printf '%s\n' "${artifact_root}"
}

infer_embedded_artifact_root() {
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  local artifact_root
  artifact_root="$(cd "${script_dir}/.." && pwd)"

  if [[ -f "${artifact_root}/bin/stacklab" && -d "${artifact_root}/frontend/dist" ]]; then
    printf '%s\n' "${artifact_root}"
    return 0
  fi

  return 1
}

wait_for_health() {
  local health_url="$1"
  local attempts=20
  local delay=2

  for _ in $(seq 1 "${attempts}"); do
    if check_health_once "${health_url}"; then
      return 0
    fi
    sleep "${delay}"
  done

  return 1
}

install_env_file() {
  local source_env="$1"
  local env_path="$2"
  local stacklab_root="$3"
  local data_dir="$4"

  install -d -m 0755 "$(dirname "${env_path}")"

  if [[ -f "${env_path}" ]]; then
    return 0
  fi

  cp "${source_env}" "${env_path}"
  perl -0pi -e "s|STACKLAB_ROOT=/opt/stacklab|STACKLAB_ROOT=${stacklab_root}|g; s|STACKLAB_DATA_DIR=/var/lib/stacklab|STACKLAB_DATA_DIR=${data_dir}|g" "${env_path}"

  if [[ -n "${STACKLAB_BOOTSTRAP_PASSWORD:-}" ]]; then
    printf '\nSTACKLAB_BOOTSTRAP_PASSWORD=%s\n' "${STACKLAB_BOOTSTRAP_PASSWORD}" >> "${env_path}"
  fi
}

install_service_unit() {
  local source_unit="$1"
  local unit_path="$2"

  install -d -m 0755 "$(dirname "${unit_path}")"

  if [[ ! -f "${unit_path}" ]]; then
    cp "${source_unit}" "${unit_path}"
  fi
}

ensure_service_account() {
  local service_user="$1"
  local service_group="$2"
  local stacklab_root="$3"
  local app_root="$4"
  local data_dir="$5"

  if ! getent group "${service_group}" >/dev/null 2>&1; then
    groupadd --system "${service_group}"
  fi

  if ! id -u "${service_user}" >/dev/null 2>&1; then
    useradd \
      --system \
      --gid "${service_group}" \
      --home-dir "${data_dir}/home" \
      --create-home \
      --shell /usr/sbin/nologin \
      "${service_user}"
  fi

  if getent group docker >/dev/null 2>&1; then
    usermod -a -G docker "${service_user}" || true
  fi

  install -d -m 0755 "${data_dir}/home" "${data_dir}/docker"
  chown -R "${service_user}:${service_group}" "${app_root}" "${data_dir}"
  chown "${service_user}:${service_group}" "${stacklab_root}" "${stacklab_root}/stacks" "${stacklab_root}/config" "${stacklab_root}/data"
}

main() {
  local artifact_arg=""
  local install_unit=0
  local service_name="stacklab"
  local service_user="stacklab"
  local service_group="stacklab"
  local app_root="/opt/stacklab/app"
  local stacklab_root="/opt/stacklab"
  local data_dir="/var/lib/stacklab"
  local health_url="http://127.0.0.1:8080/api/health"
  local do_health_check=1

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --install-unit)
        install_unit=1
        shift
        ;;
      --service-name)
        service_name="$2"
        shift 2
        ;;
      --service-user)
        service_user="$2"
        shift 2
        ;;
      --service-group)
        service_group="$2"
        shift 2
        ;;
      --app-root)
        app_root="$2"
        shift 2
        ;;
      --stacklab-root)
        stacklab_root="$2"
        shift 2
        ;;
      --data-dir)
        data_dir="$2"
        shift 2
        ;;
      --health-url)
        health_url="$2"
        shift 2
        ;;
      --no-health-check)
        do_health_check=0
        shift
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      --*)
        die "unknown option: $1"
        ;;
      *)
        if [[ -n "${artifact_arg}" ]]; then
          die "only one artifact argument is supported"
        fi
        artifact_arg="$1"
        shift
        ;;
    esac
  done

  require_root
  need_cmd tar
  need_cmd systemctl

  local work_dir
  work_dir="$(mktemp -d)"
  trap "rm -rf -- '${work_dir}'" EXIT

  local artifact_root=""
  if [[ -n "${artifact_arg}" ]]; then
    if [[ -d "${artifact_arg}" ]]; then
      artifact_root="$(cd "${artifact_arg}" && pwd)"
    else
      artifact_root="$(extract_artifact "${artifact_arg}" "${work_dir}")"
    fi
  else
    artifact_root="$(infer_embedded_artifact_root)" || die "no artifact argument provided and could not infer extracted artifact root"
  fi

  [[ -f "${artifact_root}/bin/stacklab" ]] || die "artifact is missing bin/stacklab"
  [[ -d "${artifact_root}/frontend/dist" ]] || die "artifact is missing frontend/dist"
  [[ -f "${artifact_root}/metadata/version.txt" ]] || die "artifact is missing metadata/version.txt"
  [[ -f "${artifact_root}/systemd/stacklab.service.example" ]] || die "artifact is missing systemd/stacklab.service.example"
  [[ -f "${artifact_root}/systemd/stacklab.env.example" ]] || die "artifact is missing systemd/stacklab.env.example"

  local release_name
  release_name="$(basename "${artifact_root}")"
  local releases_dir="${app_root}/releases"
  local current_link="${app_root}/current"
  local install_dir="${releases_dir}/${release_name}"
  local previous_target=""
  local unit_path="/etc/systemd/system/${service_name}.service"
  local env_path="/etc/stacklab/stacklab.env"

  [[ ! -e "${install_dir}" ]] || die "release already exists at ${install_dir}"

  install -d -m 0755 "${releases_dir}" "${stacklab_root}/stacks" "${stacklab_root}/config" "${stacklab_root}/data" "${data_dir}"

  log "Installing release ${release_name} into ${install_dir}"
  cp -R "${artifact_root}" "${install_dir}"

  if [[ "${install_unit}" -eq 1 ]]; then
    log "Installing systemd template files if missing"
    ensure_service_account "${service_user}" "${service_group}" "${stacklab_root}" "${app_root}" "${data_dir}"
    install_env_file "${install_dir}/systemd/stacklab.env.example" "${env_path}" "${stacklab_root}" "${data_dir}"
    install_service_unit "${install_dir}/systemd/stacklab.service.example" "${unit_path}"
    systemctl daemon-reload
    systemctl enable "${service_name}" >/dev/null 2>&1 || true
  fi

  if [[ -L "${current_link}" || -e "${current_link}" ]]; then
    previous_target="$(readlink -f "${current_link}" || true)"
  fi

  local tmp_link="${app_root}/.current.new"
  rm -f "${tmp_link}"
  ln -s "${install_dir}" "${tmp_link}"
  mv -Tf "${tmp_link}" "${current_link}"

  log "Restarting ${service_name}.service"
  if ! systemctl restart "${service_name}"; then
    if [[ -n "${previous_target}" && -e "${previous_target}" ]]; then
      log "Restart failed, rolling back symlink"
      ln -s "${previous_target}" "${tmp_link}"
      mv -Tf "${tmp_link}" "${current_link}"
      systemctl restart "${service_name}" || true
    fi
    die "systemctl restart ${service_name} failed"
  fi

  if [[ "${do_health_check}" -eq 1 ]]; then
    log "Waiting for health check ${health_url}"
    if ! wait_for_health "${health_url}"; then
      if [[ -n "${previous_target}" && -e "${previous_target}" ]]; then
        log "Health check failed, rolling back to ${previous_target}"
        ln -s "${previous_target}" "${tmp_link}"
        mv -Tf "${tmp_link}" "${current_link}"
        systemctl restart "${service_name}" || true
      fi
      die "health check failed after upgrade"
    fi
  fi

  local version
  version="$(tr -d '\n' < "${install_dir}/metadata/version.txt")"
  local commit
  commit="$(tr -d '\n' < "${install_dir}/metadata/commit.txt")"

  log "Upgrade complete"
  log "Version: ${version}"
  log "Commit: ${commit}"
  log "Current release: ${install_dir}"
}

main "$@"
