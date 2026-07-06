#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  smoke-apt-repo-install.sh [OPTIONS]

Validate installation from a published Stacklab APT repository using a disposable
Debian container.

Options:
  --repo-url URL         Repository root URL. Default: https://krbob.github.io/stacklab/apt
  --channel CHANNEL      stable | nightly. Default: stable
  --arch ARCH            amd64 | arm64. Default: amd64
  --key-url URL          Override public key URL
  --expected-version V   Require this exact stacklab package version
  --help                 Show this help
EOF
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

repo_url="https://krbob.github.io/stacklab/apt"
channel="stable"
arch="amd64"
key_url=""
expected_version=""

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
    --key-url)
      key_url="$2"
      shift 2
      ;;
    --expected-version)
      expected_version="$2"
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
if [[ -n "${expected_version}" && ! "${expected_version}" =~ ^[0-9A-Za-z.+:~_-]+$ ]]; then
  echo "--expected-version contains unsupported characters" >&2
  exit 1
fi

need_cmd docker

if [[ -z "${key_url}" ]]; then
  key_url="${repo_url}/stacklab-archive-keyring.gpg"
fi

workdir="$(mktemp -d "${TMPDIR:-/tmp}/stacklab-apt-smoke.XXXXXX")"
cleanup() {
  rm -rf "${workdir}"
}
trap cleanup EXIT

cat > "${workdir}/Dockerfile" <<EOF
FROM --platform=linux/${arch} debian:bookworm-slim

ARG EXPECTED_VERSION=""

RUN apt-get update && apt-get install -y ca-certificates curl gnupg
RUN mkdir -p /usr/share/keyrings \\
 && curl -fsSL "${key_url}" -o /usr/share/keyrings/stacklab-archive-keyring.gpg \\
 && echo "deb [arch=${arch} signed-by=/usr/share/keyrings/stacklab-archive-keyring.gpg] ${repo_url} ${channel} main" > /etc/apt/sources.list.d/stacklab.list \\
 && apt-get update \\
 && apt-cache policy stacklab \\
 && if [ -n "\${EXPECTED_VERSION}" ]; then \\
      candidate="\$(apt-cache policy stacklab | awk '/Candidate:/ { print \$2; exit }')"; \\
      if [ "\${candidate}" != "\${EXPECTED_VERSION}" ]; then \\
        echo "expected stacklab \${EXPECTED_VERSION}, got candidate \${candidate}" >&2; \\
        exit 1; \\
      fi; \\
      apt-get install -y --no-install-recommends "stacklab=\${EXPECTED_VERSION}"; \\
    else \\
      apt-get install -y --no-install-recommends stacklab; \\
    fi
EOF

docker build \
  --platform="linux/${arch}" \
  --build-arg "EXPECTED_VERSION=${expected_version}" \
  -t "stacklab-apt-smoke-${channel}-${arch}" \
  "${workdir}"
