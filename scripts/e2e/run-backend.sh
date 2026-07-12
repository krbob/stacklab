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
workspace_helper_env_path="${STACKLAB_E2E_WORKSPACE_HELPER_ENV_PATH:-${workdir}/stacklab.env}"
git_origin_dir="${workdir}/git-origin.git"

mkdir -p "${root_dir}" "${data_dir}"
rm -rf "${root_dir}"
mkdir -p "${root_dir}"
cp -R "${fixture_root}/." "${root_dir}/"

rm -rf -- "${git_origin_dir}"
git init --bare --initial-branch=main "${git_origin_dir}"
(
  cd "${root_dir}"
  git init --initial-branch=main
  git config user.name "Stacklab E2E"
  git config user.email "stacklab-e2e@example.invalid"
  git config commit.gpgsign false
  git add -f -- stacks config
  git commit --no-gpg-sign -m "Initialize E2E workspace"
  git remote add origin "${git_origin_dir}"
  git push --set-upstream origin main
  printf '/data/\n/config/blocked-fixture/\n' >> .git/info/exclude
)

mkdir -p "${root_dir}/config/blocked-fixture"
printf 'secret=blocked\n' > "${root_dir}/config/blocked-fixture/blocked.env"
chmod 000 "${root_dir}/config/blocked-fixture/blocked.env"

if [[ "${enable_workspace_repair}" == "1" ]]; then
  mkdir -p "$(dirname "${workspace_helper_path}")"
  printf 'STACKLAB_ROOT=%s\n' "${root_dir}" > "${workspace_helper_env_path}"
  (
    cd "${repo_root}"
    go build -ldflags "-X main.stacklabEnvFilePath=${workspace_helper_env_path}" -o "${workspace_helper_path}" ./cmd/stacklab-workspace-admin-helper
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
  STACKLAB_E2E_GIT_ORIGIN: ${git_origin_dir}
  STACKLAB_WORKSPACE_ADMIN_HELPER_PATH: ${STACKLAB_WORKSPACE_ADMIN_HELPER_PATH:-}
  STACKLAB_WORKSPACE_ADMIN_USE_SUDO: ${STACKLAB_WORKSPACE_ADMIN_USE_SUDO:-false}
  STACKLAB_E2E_WORKSPACE_HELPER_ENV_PATH: ${workspace_helper_env_path}
EOF

cd "${repo_root}"
exec go run ./cmd/stacklab
