#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  wait-for-apt-repo-version.sh --expected-version VERSION [OPTIONS]

Wait until the published Stacklab APT repository exposes an exact package
version through GitHub Pages.

Options:
  --repo-url URL         Repository root URL. Default: https://krbob.github.io/stacklab/apt
  --channel CHANNEL      stable | nightly. Default: stable
  --arch ARCH            amd64 | arm64. Default: amd64
  --expected-version V   Required stacklab package version
  --attempts N           Package-version attempts. Default: 60
  --sleep-seconds N      Delay between attempts. Default: 10
  --help                 Show this help
EOF
}

repo_url="https://krbob.github.io/stacklab/apt"
channel="stable"
arch="amd64"
expected_version=""
attempts=60
sleep_seconds=10

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo-url)
      repo_url="$2"
      shift 2
      ;;
    --channel)
      channel="$2"
      shift 2
      ;;
    --arch)
      arch="$2"
      shift 2
      ;;
    --expected-version)
      expected_version="$2"
      shift 2
      ;;
    --attempts)
      attempts="$2"
      shift 2
      ;;
    --sleep-seconds)
      sleep_seconds="$2"
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

[[ "${channel}" == "stable" || "${channel}" == "nightly" ]] || {
  echo "--channel must be stable or nightly" >&2
  exit 1
}
[[ "${arch}" == "amd64" || "${arch}" == "arm64" ]] || {
  echo "--arch must be amd64 or arm64" >&2
  exit 1
}
[[ -n "${expected_version}" ]] || {
  echo "--expected-version is required" >&2
  exit 1
}
if [[ ! "${expected_version}" =~ ^[0-9A-Za-z.+:~_-]+$ ]]; then
  echo "--expected-version contains unsupported characters" >&2
  exit 1
fi
if [[ ! "${attempts}" =~ ^[1-9][0-9]*$ ]]; then
  echo "--attempts must be a positive integer" >&2
  exit 1
fi
if [[ ! "${sleep_seconds}" =~ ^[1-9][0-9]*$ ]]; then
  echo "--sleep-seconds must be a positive integer" >&2
  exit 1
fi

wait_for_url() {
  local url="$1"
  local attempt=1
  while (( attempt <= 30 )); do
    if curl -fsI "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep "${sleep_seconds}"
    attempt=$((attempt + 1))
  done
  echo "timed out waiting for ${url} after 30 attempts" >&2
  return 1
}

wait_for_package_version() {
  local url="$1"
  local attempt=1
  while (( attempt <= attempts )); do
    if curl -fsSL "${url}" 2>/dev/null | grep -Fxq "Version: ${expected_version}"; then
      return 0
    fi
    sleep "${sleep_seconds}"
    attempt=$((attempt + 1))
  done
  echo "timed out waiting for ${url} to expose stacklab ${expected_version} after ${attempts} attempts" >&2
  curl -fsSL "${url}" | awk '/^Version:/ { print }' >&2 || true
  return 1
}

wait_for_url "${repo_url}/stacklab-archive-keyring.gpg"
wait_for_url "${repo_url}/dists/${channel}/InRelease"
wait_for_package_version "${repo_url}/dists/${channel}/main/binary-${arch}/Packages"
