#!/usr/bin/env bash
set -euo pipefail

# renovate: datasource=go depName=github.com/rhysd/actionlint
readonly ACTIONLINT_VERSION="v1.7.12"
# renovate: datasource=go depName=github.com/zricethezav/gitleaks/v8
readonly GITLEAKS_VERSION="v8.30.0"
# renovate: datasource=github-releases depName=koalaman/shellcheck
readonly SHELLCHECK_VERSION="0.11.0"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${repo_root}"

require_command() {
  local command_name="$1"
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Required command is not available: ${command_name}" >&2
    exit 1
  fi
}

require_command git
require_command go
require_command node
require_command npm
require_command shellcheck

actual_shellcheck_version="$(shellcheck --version | awk '$1 == "version:" { print $2 }')"
if [[ "${actual_shellcheck_version}" != "${SHELLCHECK_VERSION}" ]]; then
  echo "ShellCheck ${SHELLCHECK_VERSION} is required; found ${actual_shellcheck_version:-unknown}." >&2
  exit 1
fi

echo "==> actionlint ${ACTIONLINT_VERSION}"
go run "github.com/rhysd/actionlint/cmd/actionlint@${ACTIONLINT_VERSION}" -color=false

shell_files=()
while IFS= read -r -d '' file; do
  shell_files+=("${file}")
done < <(
  git ls-files -z -- \
    '*.sh' \
    'packaging/debian/preinst' \
    'packaging/debian/postinst' \
    'packaging/debian/prerm' \
    'packaging/debian/postrm'
)
if [[ ${#shell_files[@]} -eq 0 ]]; then
  echo "No tracked shell scripts found." >&2
  exit 1
fi

echo "==> ShellCheck v${SHELLCHECK_VERSION}"
shellcheck "${shell_files[@]}"

echo "==> Gitleaks ${GITLEAKS_VERSION} (complete Git history)"
go run "github.com/zricethezav/gitleaks/v8@${GITLEAKS_VERSION}" git \
  --config .gitleaks.toml \
  --exit-code 1 \
  --no-banner \
  --redact \
  --verbose

# Gitleaks' default global allowlist skips dependency lockfiles by path. Scan
# their current contents separately after removing only syntactically complete
# integrity values; all package names, URLs, and surrounding content remain.
echo "==> Gitleaks ${GITLEAKS_VERSION} (normalized lockfile contents)"
{
  sed -E \
    '/^[[:space:]]*"integrity"[[:space:]]*:[[:space:]]*"sha(256|384|512)-[A-Za-z0-9+\/]+={0,2}",?[[:space:]]*$/d' \
    frontend/package-lock.json
  awk 'NF == 3 && $3 ~ /^h1:[A-Za-z0-9+\/]+=$/ { print $1, $2; next } { print }' go.sum
} | go run "github.com/zricethezav/gitleaks/v8@${GITLEAKS_VERSION}" stdin \
  --config .gitleaks.toml \
  --exit-code 1 \
  --no-banner \
  --redact \
  --verbose

if [[ ! -x frontend/node_modules/.bin/eslint ]]; then
  echo "Frontend dependencies are missing; run 'npm --prefix frontend ci' first." >&2
  exit 1
fi

echo "==> Third-party notices"
scripts/release/generate-third-party-notices.sh --check

echo "==> npm production audit"
npm --prefix frontend audit --omit=dev

echo "==> ESLint (zero warnings)"
npm --prefix frontend run lint
