#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
generator="${repo_root}/scripts/release/generate-release-notes.sh"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/stacklab-release-notes-test.XXXXXX")"

cleanup() {
  rm -rf "${tmp_dir}"
}
trap cleanup EXIT

assert_contains() {
  local output="$1"
  local expected="$2"
  if [[ "${output}" != *"${expected}"* ]]; then
    echo "Expected release notes to contain: ${expected}" >&2
    exit 1
  fi
}

assert_not_contains() {
  local output="$1"
  local unexpected="$2"
  if [[ "${output}" == *"${unexpected}"* ]]; then
    echo "Expected release notes not to contain: ${unexpected}" >&2
    exit 1
  fi
}

git -C "${tmp_dir}" init --quiet
git -C "${tmp_dir}" config user.name "Stacklab release test"
git -C "${tmp_dir}" config user.email "release-test@stacklab.invalid"
git -C "${tmp_dir}" commit --quiet --allow-empty -m "feat: establish baseline"
from_commit="$(git -C "${tmp_dir}" rev-parse HEAD)"
git -C "${tmp_dir}" commit --quiet --allow-empty -m "fix: include target commit"
to_commit="$(git -C "${tmp_dir}" rev-parse HEAD)"
to_short="$(git -C "${tmp_dir}" rev-parse --short HEAD)"

output="$({
  cd "${tmp_dir}"
  "${generator}" \
    --version test \
    --channel nightly \
    --from "${from_commit}" \
    --repo https://github.com/krbob/stacklab
})"

assert_contains "${output}" "- \`${to_short}\` fix: include target commit"
assert_contains "${output}" "https://github.com/krbob/stacklab/compare/${from_commit}...${to_commit}"
assert_not_contains "${output}" "...HEAD"
assert_not_contains "${output}" "_No changes detected in the selected range._"

echo "Release notes generator tests passed."
