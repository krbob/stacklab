#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  generate-release-notes.sh --version VERSION [OPTIONS]

Generate Markdown release notes from commit messages.

Options:
  --version VERSION     Required. Release version shown in the title.
  --channel NAME        stable | hotfix | nightly. Default: stable
  --from REF            Lower bound (exclusive) git ref. Optional.
  --to REF              Upper bound (inclusive) git ref. Default: HEAD
  --repo URL            Optional repository URL used to render compare links.
  --help                Show this help.
EOF
}

version=""
channel="stable"
from_ref=""
to_ref="HEAD"
repo_url=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="$2"
      shift 2
      ;;
    --channel)
      channel="$2"
      shift 2
      ;;
    --from)
      from_ref="$2"
      shift 2
      ;;
    --to)
      to_ref="$2"
      shift 2
      ;;
    --repo)
      repo_url="$2"
      shift 2
      ;;
    --help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

[[ -n "${version}" ]] || {
  echo "--version is required" >&2
  exit 1
}

range_spec="${to_ref}"
if [[ -n "${from_ref}" ]]; then
  range_spec="${from_ref}..${to_ref}"
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/stacklab-release-notes.XXXXXX")"
cleanup() {
  rm -rf "${tmp_dir}"
}
trap cleanup EXIT

features_file="${tmp_dir}/features.txt"
fixes_file="${tmp_dir}/fixes.txt"
docs_file="${tmp_dir}/docs.txt"
tests_file="${tmp_dir}/tests.txt"
maintenance_file="${tmp_dir}/maintenance.txt"
other_file="${tmp_dir}/other.txt"

touch "${features_file}" "${fixes_file}" "${docs_file}" "${tests_file}" "${maintenance_file}" "${other_file}"

while IFS=$'\t' read -r short_sha subject; do
  [[ -n "${subject}" ]] || continue
  line="- \`${short_sha}\` ${subject}"
  case "${subject}" in
    feat:*|feat\(*)
      printf '%s\n' "${line}" >> "${features_file}"
      ;;
    fix:*|fix\(*)
      printf '%s\n' "${line}" >> "${fixes_file}"
      ;;
    docs:*|docs\(*)
      printf '%s\n' "${line}" >> "${docs_file}"
      ;;
    test:*|test\(*)
      printf '%s\n' "${line}" >> "${tests_file}"
      ;;
    chore:*|chore\(*|ci:*|ci\(*|build:*|build\(*|refactor:*|refactor\(*)
      printf '%s\n' "${line}" >> "${maintenance_file}"
      ;;
    *)
      printf '%s\n' "${line}" >> "${other_file}"
      ;;
  esac
done < <(git log --reverse --pretty=format:'%h%x09%s' "${range_spec}")

echo "# Stacklab ${version}"
echo
echo "Channel: \`${channel}\`"
echo

if [[ -n "${from_ref}" && -n "${repo_url}" ]]; then
  echo "Changes since \`${from_ref}\`."
  echo
  echo "Compare: ${repo_url}/compare/${from_ref}...${to_ref}"
  echo
fi

render_section() {
  local title="$1"
  local file="$2"
  if [[ -s "${file}" ]]; then
    echo "## ${title}"
    echo
    cat "${file}"
    echo
  fi
}

render_section "Features" "${features_file}"
render_section "Fixes" "${fixes_file}"
render_section "Docs" "${docs_file}"
render_section "Tests" "${tests_file}"
render_section "Maintenance" "${maintenance_file}"
render_section "Other Changes" "${other_file}"

if [[ ! -s "${features_file}" && ! -s "${fixes_file}" && ! -s "${docs_file}" && ! -s "${tests_file}" && ! -s "${maintenance_file}" && ! -s "${other_file}" ]]; then
  echo "_No changes detected in the selected range._"
fi
