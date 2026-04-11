#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
fixture_root="${repo_root}/test/fixtures/e2e/root"

if [[ ! -d "${fixture_root}" ]]; then
  echo "E2E fixture root not found: ${fixture_root}" >&2
  exit 1
fi

workdir="${STACKLAB_E2E_WORKDIR:-$(mktemp -d "${TMPDIR:-/tmp}/stacklab-e2e.XXXXXX")}"
root_dir="${STACKLAB_ROOT:-${workdir}/root}"
data_dir="${STACKLAB_DATA_DIR:-${workdir}/var/lib/stacklab}"
database_path="${STACKLAB_DATABASE_PATH:-${data_dir}/stacklab.db}"
frontend_dist="${STACKLAB_FRONTEND_DIST:-${repo_root}/frontend/dist}"
http_addr="${STACKLAB_HTTP_ADDR:-127.0.0.1:18081}"
bootstrap_password="${STACKLAB_BOOTSTRAP_PASSWORD:-stacklab-e2e}"
enable_workspace_repair="${STACKLAB_E2E_ENABLE_WORKSPACE_REPAIR:-0}"
workspace_helper_path="${STACKLAB_WORKSPACE_ADMIN_HELPER_PATH:-${workdir}/bin/stacklab-workspace-admin-helper}"

mkdir -p "${root_dir}" "${data_dir}"
rm -rf "${root_dir}"
mkdir -p "${root_dir}"
cp -R "${fixture_root}/." "${root_dir}/"

mkdir -p "${root_dir}/config/blocked-fixture"
printf 'secret=blocked\n' > "${root_dir}/config/blocked-fixture/blocked.env"
chmod 000 "${root_dir}/config/blocked-fixture/blocked.env"

if [[ "${enable_workspace_repair}" == "1" ]]; then
  mkdir -p "$(dirname "${workspace_helper_path}")"
  (
    cd "${repo_root}"
    go build -o "${workspace_helper_path}" ./cmd/stacklab-workspace-admin-helper
  )
  export STACKLAB_WORKSPACE_ADMIN_HELPER_PATH="${workspace_helper_path}"
  export STACKLAB_WORKSPACE_ADMIN_USE_SUDO="true"
fi

export STACKLAB_ROOT="${root_dir}"
export STACKLAB_DATA_DIR="${data_dir}"
export STACKLAB_DATABASE_PATH="${database_path}"
export STACKLAB_FRONTEND_DIST="${frontend_dist}"
export STACKLAB_HTTP_ADDR="${http_addr}"
export STACKLAB_BOOTSTRAP_PASSWORD="${bootstrap_password}"
export STACKLAB_LOG_LEVEL="${STACKLAB_LOG_LEVEL:-debug}"

cat >&2 <<EOF
Starting Stacklab E2E backend
  fixture root: ${fixture_root}
  workdir: ${workdir}
  STACKLAB_ROOT: ${STACKLAB_ROOT}
  STACKLAB_DATA_DIR: ${STACKLAB_DATA_DIR}
  STACKLAB_DATABASE_PATH: ${STACKLAB_DATABASE_PATH}
  STACKLAB_FRONTEND_DIST: ${STACKLAB_FRONTEND_DIST}
  STACKLAB_HTTP_ADDR: ${STACKLAB_HTTP_ADDR}
  STACKLAB_BOOTSTRAP_PASSWORD: ${STACKLAB_BOOTSTRAP_PASSWORD}
  STACKLAB_E2E_ENABLE_WORKSPACE_REPAIR: ${enable_workspace_repair}
  STACKLAB_WORKSPACE_ADMIN_HELPER_PATH: ${STACKLAB_WORKSPACE_ADMIN_HELPER_PATH:-}
  STACKLAB_WORKSPACE_ADMIN_USE_SUDO: ${STACKLAB_WORKSPACE_ADMIN_USE_SUDO:-false}
EOF

cd "${repo_root}"
exec go run ./cmd/stacklab
