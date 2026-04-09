#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"

version="${STACKLAB_RELEASE_VERSION:-0.0.0-dev}"
commit="${STACKLAB_RELEASE_COMMIT:-$(git -C "${repo_root}" rev-parse --short=12 HEAD)}"
goos="${STACKLAB_RELEASE_GOOS:-linux}"
goarch="${STACKLAB_RELEASE_GOARCH:-amd64}"
platform="${goos}-${goarch}"
artifact_name="stacklab-${version}-${platform}"
output_dir="${STACKLAB_RELEASE_OUTPUT_DIR:-${repo_root}/dist/release}"
stage_dir="${output_dir}/${artifact_name}"
tarball_path="${output_dir}/${artifact_name}.tar.gz"
sha_path="${tarball_path}.sha256"
build_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
skip_frontend_build="${STACKLAB_SKIP_FRONTEND_BUILD:-0}"

sha256_file() {
  local target="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${target}"
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${target}"
    return 0
  fi
  echo "missing required command: sha256sum or shasum" >&2
  exit 1
}

mkdir -p "${output_dir}"
rm -rf "${stage_dir}" "${tarball_path}" "${sha_path}"
mkdir -p "${stage_dir}/bin" "${stage_dir}/frontend" "${stage_dir}/metadata" "${stage_dir}/systemd" "${stage_dir}/host-tools"

if [[ "${skip_frontend_build}" != "1" ]]; then
  echo "Building frontend assets..."
  (
    cd "${repo_root}/frontend"
    npm run build
  )
fi

echo "Building backend binary for ${platform}..."
(
  cd "${repo_root}"
  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 \
    go build \
      -ldflags "-s -w -X 'stacklab/internal/stacks.AppVersion=${version}' -X 'stacklab/internal/stacks.AppCommit=${commit}'" \
      -o "${stage_dir}/bin/stacklab" \
      ./cmd/stacklab

  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 \
    go build \
      -ldflags "-s -w" \
      -o "${stage_dir}/bin/stacklab-docker-admin-helper" \
      ./cmd/stacklab-docker-admin-helper

  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 \
    go build \
      -ldflags "-s -w" \
      -o "${stage_dir}/bin/stacklab-workspace-admin-helper" \
      ./cmd/stacklab-workspace-admin-helper
)

cp -R "${repo_root}/frontend/dist" "${stage_dir}/frontend/dist"
cp "${repo_root}/packaging/systemd/stacklab.service.example" "${stage_dir}/systemd/stacklab.service.example"
cp "${repo_root}/packaging/systemd/stacklab.env.example" "${stage_dir}/systemd/stacklab.env.example"
cp "${repo_root}/packaging/systemd/stacklab-docker-admin.sudoers.example" "${stage_dir}/systemd/stacklab-docker-admin.sudoers.example"
cp "${repo_root}/packaging/systemd/stacklab-workspace-admin.sudoers.example" "${stage_dir}/systemd/stacklab-workspace-admin.sudoers.example"
cp "${repo_root}/scripts/release/upgrade.sh" "${stage_dir}/host-tools/upgrade.sh"
chmod +x "${stage_dir}/host-tools/upgrade.sh"

if command -v xattr >/dev/null 2>&1; then
  xattr -cr "${stage_dir}" 2>/dev/null || true
fi

printf '%s\n' "${version}" > "${stage_dir}/metadata/version.txt"
printf '%s\n' "${commit}" > "${stage_dir}/metadata/commit.txt"
printf '%s\n' "${build_time}" > "${stage_dir}/metadata/build_time.txt"
printf '%s\n' "${platform}" > "${stage_dir}/metadata/platform.txt"

(
  cd "${output_dir}"
  tar_cmd=(tar)
  if tar --help 2>/dev/null | grep -q -- '--no-mac-metadata'; then
    tar_cmd+=(--no-mac-metadata)
  fi
  COPYFILE_DISABLE=1 "${tar_cmd[@]}" -czf "${tarball_path}" "${artifact_name}"
)

(
  cd "${output_dir}"
  sha256_file "$(basename "${tarball_path}")" > "$(basename "${sha_path}")"
)

echo "Release artifact created:"
echo "  ${tarball_path}"
echo "  ${sha_path}"
