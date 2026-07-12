#!/usr/bin/env bash

set -euo pipefail

SOURCE_PACKAGE_VERSION="0~stacklab-systemd-smoke-a"

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

wait_for_stacklab() {
  local phase="$1"
  local timeout_seconds="${STACKLAB_SERVICE_TIMEOUT_SECONDS:-60}"
  local deadline=$((SECONDS + timeout_seconds))

  while (( SECONDS < deadline )); do
    if systemctl is-active --quiet stacklab.service \
      && curl -fsS http://127.0.0.1:8080/api/ready >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  systemctl status --no-pager --full stacklab.service >&2 || true
  journalctl --no-pager --boot -u stacklab.service -n 200 >&2 || true
  die "Stacklab did not become ready after ${phase} within ${timeout_seconds}s"
}

assert_service_identity() {
  local main_pid
  local process_user

  systemctl is-enabled --quiet stacklab.service || die "stacklab.service is not enabled"
  systemctl is-active --quiet stacklab.service || die "stacklab.service is not active"
  test "$(systemctl show stacklab.service --property=User --value)" = stacklab \
    || die "stacklab.service is not configured with User=stacklab"

  main_pid="$(systemctl show stacklab.service --property=MainPID --value)"
  [[ "${main_pid}" =~ ^[1-9][0-9]*$ ]] || die "stacklab.service has no main PID"
  process_user="$(ps -o user= -p "${main_pid}" | tr -d '[:space:]')"
  test "${process_user}" = stacklab \
    || die "Stacklab process runs as ${process_user:-unknown}, expected stacklab"
}

assert_health_and_frontend() {
  local phase="$1"
  local work_dir="$2"
  local health_file="${work_dir}/health-${phase}.json"
  local index_file="${work_dir}/index-${phase}.html"
  local asset_path
  local asset_url

  curl -fsS http://127.0.0.1:8080/api/ready >"${health_file}"
  grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"' "${health_file}" \
    || die "health response after ${phase} did not report status=ok"

  curl -fsS http://127.0.0.1:8080/ >"${index_file}"
  grep -q 'id="root"' "${index_file}" \
    || die "frontend index after ${phase} is missing the root element"
  asset_path="$(grep -o 'src="[^"]*\.js"' "${index_file}" | head -n 1 | cut -d '"' -f 2)"
  [[ -n "${asset_path}" ]] || die "frontend index after ${phase} contains no JavaScript asset"
  if [[ "${asset_path}" = /* ]]; then
    asset_url="http://127.0.0.1:8080${asset_path}"
  else
    asset_url="http://127.0.0.1:8080/${asset_path#./}"
  fi
  curl -fsS "${asset_url}" >/dev/null
}

login() {
  local phase="$1"
  local work_dir="$2"
  local cookie_jar="$3"
  local response_file="${work_dir}/login-${phase}.json"
  local status

  status="$(curl -sS \
    --output "${response_file}" \
    --write-out '%{http_code}' \
    --cookie-jar "${cookie_jar}" \
    --header 'Content-Type: application/json' \
    --header 'Origin: http://127.0.0.1:8080' \
    --data "{\"password\":\"${STACKLAB_SMOKE_PASSWORD}\"}" \
    http://127.0.0.1:8080/api/auth/login)"
  test "${status}" = 200 \
    || die "login after ${phase} returned HTTP ${status}: $(tr -d '\n' <"${response_file}")"
  grep -Eq '"authenticated"[[:space:]]*:[[:space:]]*true' "${response_file}" \
    || die "login response after ${phase} was not authenticated"

  curl -fsS --cookie "${cookie_jar}" http://127.0.0.1:8080/api/session \
    | grep -Eq '"authenticated"[[:space:]]*:[[:space:]]*true' \
    || die "authenticated session check after ${phase} failed"
}

build_source_package() {
  local target_deb="$1"
  local source_deb="$2"
  local work_dir="$3"
  local package_root="${work_dir}/package-a"
  local target_version

  target_version="$(dpkg-deb --field "${target_deb}" Version)"
  dpkg --compare-versions "${SOURCE_PACKAGE_VERSION}" lt "${target_version}" \
    || die "generated source version ${SOURCE_PACKAGE_VERSION} is not older than ${target_version}"

  # Derive A from the exact target payload so the smoke is deterministic and
  # exercises Debian's upgrade lifecycle without downloading an older release.
  dpkg-deb --raw-extract "${target_deb}" "${package_root}"
  sed -i "s/^Version: .*/Version: ${SOURCE_PACKAGE_VERSION}/" "${package_root}/DEBIAN/control"
  printf '%s\n' "${SOURCE_PACKAGE_VERSION}" >"${package_root}/usr/lib/stacklab/metadata/version.txt"
  dpkg-deb --build --root-owner-group "${package_root}" "${source_deb}" >/dev/null
  chmod 0644 "${source_deb}"
}

configure_smoke_auth() {
  local env_file="/etc/stacklab/stacklab.env"

  sed -i 's/^STACKLAB_COOKIE_SECURE=.*/STACKLAB_COOKIE_SECURE=false/' "${env_file}"
  if grep -q '^STACKLAB_BOOTSTRAP_PASSWORD=' "${env_file}"; then
    sed -i "s/^STACKLAB_BOOTSTRAP_PASSWORD=.*/STACKLAB_BOOTSTRAP_PASSWORD=${STACKLAB_SMOKE_PASSWORD}/" "${env_file}"
  else
    printf '\nSTACKLAB_BOOTSTRAP_PASSWORD=%s\n' "${STACKLAB_SMOKE_PASSWORD}" >>"${env_file}"
  fi
  printf '# stacklab-systemd-smoke-preserve\n' >>"${env_file}"
  chown root:root "${env_file}"
  chmod 0600 "${env_file}"
}

create_persistent_fixtures() {
  install -d -o stacklab -g stacklab -m 0755 \
    /srv/stacklab/stacks/systemd-smoke \
    /srv/stacklab/config/systemd-smoke \
    /srv/stacklab/data/systemd-smoke

  printf 'services:\n  app:\n    image: busybox:stable\n' \
    >/srv/stacklab/stacks/systemd-smoke/compose.yaml
  printf 'SMOKE_SECRET=preserved\n' \
    >/srv/stacklab/stacks/systemd-smoke/.env
  printf 'config=preserved\n' \
    >/srv/stacklab/config/systemd-smoke/app.env
  printf 'data=preserved\n' \
    >/srv/stacklab/data/systemd-smoke/sentinel.txt
  printf 'runtime=preserved\n' \
    >/var/lib/stacklab/systemd-smoke-state.txt

  chown -R stacklab:stacklab \
    /srv/stacklab/stacks/systemd-smoke \
    /srv/stacklab/config/systemd-smoke \
    /srv/stacklab/data/systemd-smoke \
    /var/lib/stacklab/systemd-smoke-state.txt
  chmod 0600 /srv/stacklab/stacks/systemd-smoke/.env

  install -d -o root -g root -m 0755 /srv/stacklab/stacks/systemd-smoke-external
  printf 'services:\n  app:\n    image: busybox:stable\n' \
    >/srv/stacklab/stacks/systemd-smoke-external/compose.yaml
  printf 'EXTERNAL_SECRET=preserved\n' \
    >/srv/stacklab/stacks/systemd-smoke-external/.env
  chown root:root /srv/stacklab/stacks/systemd-smoke-external/compose.yaml \
    /srv/stacklab/stacks/systemd-smoke-external/.env
  chmod 0644 /srv/stacklab/stacks/systemd-smoke-external/.env
}

assert_persistent_fixtures() {
  dpkg-query -W -f='${Description}\n' stacklab \
    | grep -Fq 'Host-native web control panel for Docker Compose stacks'
  grep -q '^# stacklab-systemd-smoke-preserve$' /etc/stacklab/stacklab.env \
    || die "package upgrade replaced the operator environment config"
  test "$(stat -c %a /etc/stacklab/stacklab.env)" = 600
  grep -q '^STACKLAB_COOKIE_SECURE=false$' /etc/stacklab/stacklab.env
  grep -q '^STACKLAB_BOOTSTRAP_PASSWORD=' /etc/stacklab/stacklab.env
  grep -q '^services:$' /srv/stacklab/stacks/systemd-smoke/compose.yaml
  grep -q '^SMOKE_SECRET=preserved$' /srv/stacklab/stacks/systemd-smoke/.env
  grep -q '^config=preserved$' /srv/stacklab/config/systemd-smoke/app.env
  grep -q '^data=preserved$' /srv/stacklab/data/systemd-smoke/sentinel.txt
  grep -q '^runtime=preserved$' /var/lib/stacklab/systemd-smoke-state.txt
  test "$(stat -c %U:%G /srv/stacklab/stacks/systemd-smoke/compose.yaml)" = stacklab:stacklab
  test "$(stat -c %a /srv/stacklab/stacks/systemd-smoke/.env)" = 600
  grep -q '^EXTERNAL_SECRET=preserved$' /srv/stacklab/stacks/systemd-smoke-external/.env
  test "$(stat -c %U:%G /srv/stacklab/stacks/systemd-smoke-external/.env)" = root:stacklab
  test "$(stat -c %a /srv/stacklab/stacks/systemd-smoke-external/.env)" = 640
}

assert_legal_documentation() {
  local copyright_file="/usr/share/doc/stacklab/copyright"
  local notice_file="/usr/share/doc/stacklab/NOTICE"
  local font_notices_file="/usr/lib/stacklab/frontend/dist/font-licenses.txt"

  test -f "${copyright_file}" || die "package copyright file is missing"
  test ! -L "${copyright_file}" || die "package copyright file must not be a symlink"
  test "$(stat -c %a "${copyright_file}")" = 644 \
    || die "package copyright file must have mode 0644"
  test -f "${notice_file}" || die "package NOTICE file is missing"
  test "$(stat -c %a "${notice_file}")" = 644 \
    || die "package NOTICE file must have mode 0644"
  test -f /usr/share/common-licenses/Apache-2.0 \
    || die "Debian Apache-2.0 common license is missing"
  grep -Fq "/usr/share/common-licenses/Apache-2.0" "${copyright_file}" \
    || die "package copyright file does not reference Apache-2.0"
  grep -Fq "modernc.org/libc" "${copyright_file}" \
    || die "package copyright file is missing third-party notices"
  grep -Fq "Copyright 2011-2016 Canonical Ltd." "${notice_file}" \
    || die "package NOTICE file is missing the Canonical attribution"
  test -f "${font_notices_file}" || die "bundled font notices are missing"
  grep -Fq "SIL Open Font License, Version 1.1" "${font_notices_file}" \
    || die "bundled font notices are incomplete"
}

main() {
  [[ $# -eq 1 ]] || die "usage: run-systemd-deb-smoke.sh TARGET_DEB"
  [[ "$(cat /proc/1/comm)" = systemd ]] || die "systemd is not PID 1"
  [[ -n "${STACKLAB_SMOKE_PASSWORD:-}" ]] || die "STACKLAB_SMOKE_PASSWORD is required"
  [[ "${STACKLAB_SERVICE_TIMEOUT_SECONDS:-60}" =~ ^[1-9][0-9]*$ ]] \
    || die "STACKLAB_SERVICE_TIMEOUT_SECONDS must be a positive integer"

  need_cmd curl
  need_cmd dpkg
  need_cmd dpkg-deb
  need_cmd journalctl
  need_cmd ps
  need_cmd systemctl
  need_cmd tar

  local target_deb="$1"
  local target_version
  local work_dir
  local source_deb
  local cookie_jar
  local database_inode
  local service_pid_a
  local target_application_version

  [[ -f "${target_deb}" ]] || die "target package not found: ${target_deb}"
  target_version="$(dpkg-deb --field "${target_deb}" Version)"
  target_application_version="$(dpkg-deb --fsys-tarfile "${target_deb}" \
    | tar -xOf - ./usr/lib/stacklab/metadata/version.txt \
    | tr -d '\n')"
  [[ -n "${target_application_version}" ]] || die "target package has no application version metadata"
  work_dir="$(mktemp -d)"
  source_deb="${work_dir}/stacklab-a.deb"
  cookie_jar="${work_dir}/cookies.txt"
  # Capture the local path while main is still active.
  # shellcheck disable=SC2064
  trap "rm -rf -- '${work_dir}'" EXIT

  log "Building source package A from target package ${target_version}"
  build_source_package "${target_deb}" "${source_deb}" "${work_dir}"

  log "Installing package A under real systemd"
  apt-get install -y --no-install-recommends "${source_deb}" >/dev/null
  configure_smoke_auth
  systemctl restart stacklab.service
  wait_for_stacklab "install A"
  test "$(dpkg-query --show --showformat='${Version}' stacklab)" = "${SOURCE_PACKAGE_VERSION}" \
    || die "package A version was not installed"
  test "$(cat /usr/lib/stacklab/metadata/version.txt)" = "${SOURCE_PACKAGE_VERSION}" \
    || die "package A payload was not installed"
  assert_legal_documentation
  assert_service_identity
  assert_health_and_frontend "install-a" "${work_dir}"
  login "install-a" "${work_dir}" "${cookie_jar}"

  create_persistent_fixtures
  [[ -f /var/lib/stacklab/stacklab.db ]] || die "runtime database was not created"
  database_inode="$(stat -c %i /var/lib/stacklab/stacklab.db)"
  service_pid_a="$(systemctl show stacklab.service --property=MainPID --value)"

  log "Upgrading package A to package B (${target_version})"
  apt-get install -y --no-install-recommends "${target_deb}" >/dev/null
  wait_for_stacklab "upgrade B"

  test "$(dpkg-query --show --showformat='${Version}' stacklab)" = "${target_version}" \
    || die "installed package version does not match target ${target_version}"
  test "$(cat /usr/lib/stacklab/metadata/version.txt)" = "${target_application_version}" \
    || die "package B payload did not replace package A"
  test "$(stat -c %i /var/lib/stacklab/stacklab.db)" = "${database_inode}" \
    || die "runtime database was replaced during upgrade"
  test "$(stat -c %U:%G /var/lib/stacklab/stacklab.db)" = stacklab:stacklab
  test "$(stat -c %a /var/lib/stacklab/stacklab.db)" = 600
  test "$(systemctl show stacklab.service --property=MainPID --value)" != "${service_pid_a}" \
    || die "package upgrade did not restart stacklab.service"
  assert_legal_documentation
  assert_service_identity
  assert_health_and_frontend "upgrade-b" "${work_dir}"
  assert_persistent_fixtures

  curl -fsS --cookie "${cookie_jar}" http://127.0.0.1:8080/api/session \
    | grep -Eq '"authenticated"[[:space:]]*:[[:space:]]*true' \
    || die "session persisted before upgrade is no longer authenticated"
  login "upgrade-b" "${work_dir}" "${work_dir}/cookies-after-upgrade.txt"

  log "Real-systemd install and A-to-B upgrade smoke passed"
}

main "$@"
