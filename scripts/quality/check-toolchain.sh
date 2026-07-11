#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${repo_root}"
mode="${1:-all}"

case "${mode}" in
  all | go | node) ;;
  *)
    echo "Usage: $0 [all|go|node]" >&2
    exit 2
    ;;
esac

require_command() {
  local command_name="$1"
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Required command is not available: ${command_name}" >&2
    exit 1
  fi
}

check_equal() {
  local label="$1"
  local expected="$2"
  local actual="$3"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "${label} must be ${expected}; found ${actual:-missing}." >&2
    return 1
  fi
}

require_command git
go_mod_version="$(awk '$1 == "go" { print $2; exit }' go.mod)"
nvm_node_version="$(tr -d '[:space:]' < .nvmrc)"
tool_versions_go="$(awk '$1 == "golang" { print $2; exit }' .tool-versions)"
tool_versions_node="$(awk '$1 == "nodejs" { print $2; exit }' .tool-versions)"

status=0
if [[ "${mode}" == "all" || "${mode}" == "go" ]]; then
  require_command go
  actual_go_version="$(go env GOVERSION)"
  actual_go_version="${actual_go_version#go}"
  check_equal "Go in .tool-versions" "${go_mod_version}" "${tool_versions_go}" || status=1
  check_equal "Active Go" "${go_mod_version}" "${actual_go_version}" || status=1
fi

if [[ "${mode}" == "all" || "${mode}" == "node" ]]; then
  require_command node
  package_node_version="$(node -p "require('./frontend/package.json').engines.node")"
  lock_node_version="$(node -p "require('./frontend/package-lock.json').packages[''].engines.node")"
  actual_node_version="$(node --version)"
  actual_node_version="${actual_node_version#v}"
  check_equal "Node in .tool-versions" "${nvm_node_version}" "${tool_versions_node}" || status=1
  check_equal "Node in frontend/package.json engines" "${nvm_node_version}" "${package_node_version}" || status=1
  check_equal "Node in frontend/package-lock.json engines" "${nvm_node_version}" "${lock_node_version}" || status=1
  check_equal "Active Node" "${nvm_node_version}" "${actual_node_version}" || status=1
fi

while IFS= read -r workflow; do
  if grep -q '^[[:space:]]*node-version:' "${workflow}"; then
    echo "${workflow} must not declare node-version directly; use node-version-file: .nvmrc." >&2
    status=1
  fi
  setup_node_count="$(grep -c 'uses: actions/setup-node@' "${workflow}" || true)"
  node_file_count="$(grep -c 'node-version-file: .nvmrc' "${workflow}" || true)"
  if [[ "${setup_node_count}" != "${node_file_count}" ]]; then
    echo "${workflow} must configure every setup-node step with node-version-file: .nvmrc." >&2
    status=1
  fi

  if grep -q '^[[:space:]]*go-version:' "${workflow}"; then
    echo "${workflow} must not declare go-version directly; use go-version-file: go.mod." >&2
    status=1
  fi
  setup_go_count="$(grep -c 'uses: actions/setup-go@' "${workflow}" || true)"
  go_file_count="$(grep -c 'go-version-file: go.mod' "${workflow}" || true)"
  if [[ "${setup_go_count}" != "${go_file_count}" ]]; then
    echo "${workflow} must configure every setup-go step with go-version-file: go.mod." >&2
    status=1
  fi
done < <(git ls-files '.github/workflows/*.yml' '.github/workflows/*.yaml')

if [[ ${status} -ne 0 ]]; then
  exit "${status}"
fi

case "${mode}" in
  all) printf 'Go %s; Node %s\n' "${go_mod_version}" "${nvm_node_version}" ;;
  go) printf 'Go %s\n' "${go_mod_version}" ;;
  node) printf 'Node %s\n' "${nvm_node_version}" ;;
esac
