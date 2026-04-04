#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"

version="${STACKLAB_RELEASE_VERSION:-0.0.0-dev}"
deb_version="${STACKLAB_DEB_VERSION:-${version}}"
goarch="${STACKLAB_RELEASE_GOARCH:-amd64}"
package_name="${STACKLAB_DEB_PACKAGE_NAME:-stacklab}"
output_dir="${STACKLAB_RELEASE_OUTPUT_DIR:-${repo_root}/dist/release}"
artifact_name="stacklab-${version}-linux-${goarch}"
artifact_dir="${STACKLAB_RELEASE_STAGE_DIR:-${output_dir}/${artifact_name}}"

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

case "${goarch}" in
  amd64) deb_arch="amd64" ;;
  arm64) deb_arch="arm64" ;;
  *)
    echo "Unsupported GOARCH for Debian package: ${goarch}" >&2
    exit 1
    ;;
esac

if ! command -v dpkg-deb >/dev/null 2>&1; then
  echo "missing required command: dpkg-deb" >&2
  exit 1
fi

if [[ ! -d "${artifact_dir}" ]]; then
  echo "release artifact directory not found: ${artifact_dir}" >&2
  echo "run scripts/release/build-artifact.sh first" >&2
  exit 1
fi

pkg_root="${output_dir}/${package_name}_${deb_version}_${deb_arch}.pkg"
deb_path="${output_dir}/${package_name}_${deb_version}_${deb_arch}.deb"
sha_path="${deb_path}.sha256"

rm -rf "${pkg_root}" "${deb_path}" "${sha_path}"
mkdir -p \
  "${pkg_root}/DEBIAN" \
  "${pkg_root}/usr/lib/stacklab/bin" \
  "${pkg_root}/usr/lib/stacklab/frontend" \
  "${pkg_root}/usr/lib/stacklab/metadata" \
  "${pkg_root}/lib/systemd/system" \
  "${pkg_root}/etc/stacklab"

install -m 0755 "${artifact_dir}/bin/stacklab" "${pkg_root}/usr/lib/stacklab/bin/stacklab"
cp -R "${artifact_dir}/frontend/dist" "${pkg_root}/usr/lib/stacklab/frontend/dist"
cp -R "${artifact_dir}/metadata/." "${pkg_root}/usr/lib/stacklab/metadata/"
install -m 0644 "${repo_root}/packaging/debian/stacklab.service" "${pkg_root}/lib/systemd/system/stacklab.service"
install -m 0644 "${repo_root}/packaging/debian/stacklab.env" "${pkg_root}/etc/stacklab/stacklab.env"

cat > "${pkg_root}/DEBIAN/control" <<EOF
Package: ${package_name}
Version: ${deb_version}
Section: admin
Priority: optional
Architecture: ${deb_arch}
Maintainer: Stacklab Maintainers <maintainers@stacklab.invalid>
Depends: systemd, docker.io | docker-ce | moby-engine, docker-compose | docker-compose-plugin, git
Recommends: ca-certificates
Description: Compose-first control panel for Docker Compose homelabs
 Stacklab is a host-native, filesystem-first control panel for Docker Compose
 homelabs running on a single Linux host.
EOF

cat > "${pkg_root}/DEBIAN/conffiles" <<'EOF'
/etc/stacklab/stacklab.env
EOF

install -m 0755 "${repo_root}/packaging/debian/postinst" "${pkg_root}/DEBIAN/postinst"
install -m 0755 "${repo_root}/packaging/debian/prerm" "${pkg_root}/DEBIAN/prerm"

dpkg-deb --build --root-owner-group "${pkg_root}" "${deb_path}"
(
  cd "${output_dir}"
  sha256_file "$(basename "${deb_path}")" > "$(basename "${sha_path}")"
)

echo "Debian package created:"
echo "  ${deb_path}"
echo "  ${sha_path}"
