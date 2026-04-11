#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  publish-apt-repo.sh --channel CHANNEL --pages-dir DIR --debs-dir DIR [OPTIONS]

Generate or update a signed static APT repository tree.

Options:
  --channel CHANNEL       stable | nightly
  --pages-dir DIR         Checkout root for published Pages content
  --debs-dir DIR          Directory containing .deb files to publish
  --repo-path PATH        Subdirectory under pages root. Default: apt
  --component NAME        APT component. Default: main
  --origin NAME           Release Origin. Default: Stacklab
  --label NAME            Release Label. Default: Stacklab
  --keep-versions COUNT   Retain newest COUNT package versions in the channel.
                          Defaults: stable=6, nightly=7. Use 0 to keep all.
  --help                  Show this help.

Environment:
  APT_GPG_PRIVATE_KEY_BASE64   Base64-encoded ASCII-armored private key
  APT_GPG_PASSPHRASE           Optional passphrase
  APT_GPG_KEY_ID               Optional key id to use when signing
EOF
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

channel=""
pages_dir=""
debs_dir=""
repo_path="apt"
component="main"
origin="Stacklab"
label="Stacklab"
keep_versions=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --channel)
      channel="$2"
      shift 2
      ;;
    --pages-dir)
      pages_dir="$2"
      shift 2
      ;;
    --debs-dir)
      debs_dir="$2"
      shift 2
      ;;
    --repo-path)
      repo_path="$2"
      shift 2
      ;;
    --component)
      component="$2"
      shift 2
      ;;
    --origin)
      origin="$2"
      shift 2
      ;;
    --label)
      label="$2"
      shift 2
      ;;
    --keep-versions)
      keep_versions="$2"
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
[[ -d "${pages_dir}" ]] || { echo "pages dir not found: ${pages_dir}" >&2; exit 1; }
[[ -d "${debs_dir}" ]] || { echo "debs dir not found: ${debs_dir}" >&2; exit 1; }
[[ -n "${APT_GPG_PRIVATE_KEY_BASE64:-}" ]] || {
  echo "APT_GPG_PRIVATE_KEY_BASE64 is required" >&2
  exit 1
}
if [[ -z "${keep_versions}" ]]; then
  case "${channel}" in
    stable) keep_versions=6 ;;
    nightly) keep_versions=7 ;;
  esac
fi
if [[ ! "${keep_versions}" =~ ^[0-9]+$ ]]; then
  echo "--keep-versions must be a non-negative integer" >&2
  exit 1
fi

need_cmd gpg
need_cmd dpkg-deb
need_cmd dpkg
need_cmd dpkg-scanpackages
need_cmd apt-ftparchive

repo_root="${pages_dir}/${repo_path}"
dist_dir="${repo_root}/dists/${channel}"
pool_dir="${repo_root}/pool/${channel}/${component}/s/stacklab"

mkdir -p "${pool_dir}" \
  "${dist_dir}/${component}/binary-amd64" \
  "${dist_dir}/${component}/binary-arm64"

find "${debs_dir}" -maxdepth 1 -name '*.deb' -type f -exec cp {} "${pool_dir}/" \;

sort_debian_versions_desc() {
  local sorted=()
  local version
  while IFS= read -r version; do
    [[ -n "${version}" ]] || continue
    local inserted=0
    local index
    for index in "${!sorted[@]}"; do
      if dpkg --compare-versions "${version}" gt "${sorted[$index]}"; then
        sorted=("${sorted[@]:0:${index}}" "${version}" "${sorted[@]:${index}}")
        inserted=1
        break
      fi
    done
    if [[ "${inserted}" == "0" ]]; then
      sorted+=("${version}")
    fi
  done
  printf '%s\n' "${sorted[@]}"
}

prune_pool_versions() {
  local keep_count="$1"
  if [[ "${keep_count}" == "0" ]]; then
    return 0
  fi

  local manifest
  manifest="$(mktemp "${TMPDIR:-/tmp}/stacklab-apt-pool.XXXXXX")"
  find "${pool_dir}" -maxdepth 1 -name '*.deb' -type f -print0 | while IFS= read -r -d '' deb; do
    local package_name
    local version
    package_name="$(dpkg-deb -f "${deb}" Package)"
    version="$(dpkg-deb -f "${deb}" Version)"
    if [[ "${package_name}" == "stacklab" && -n "${version}" ]]; then
      printf '%s\t%s\n' "${version}" "${deb}"
    fi
  done > "${manifest}"

  if [[ ! -s "${manifest}" ]]; then
    rm -f "${manifest}"
    return 0
  fi

  local keep_file
  keep_file="$(mktemp "${TMPDIR:-/tmp}/stacklab-apt-keep.XXXXXX")"
  local -a sorted_versions
  mapfile -t sorted_versions < <(cut -f1 "${manifest}" | sort -u | sort_debian_versions_desc)
  : > "${keep_file}"
  local index
  for index in "${!sorted_versions[@]}"; do
    if (( index >= keep_count )); then
      break
    fi
    printf '%s\n' "${sorted_versions[$index]}" >> "${keep_file}"
  done

  while IFS=$'\t' read -r version deb; do
    if ! grep -Fxq "${version}" "${keep_file}"; then
      rm -f "${deb}" "${deb}.sha256"
    fi
  done < "${manifest}"

  rm -f "${manifest}" "${keep_file}"
}

prune_pool_versions "${keep_versions}"

(
  cd "${repo_root}"
  dpkg-scanpackages --multiversion -a amd64 "pool/${channel}/${component}/s/stacklab" /dev/null > "dists/${channel}/${component}/binary-amd64/Packages"
  dpkg-scanpackages --multiversion -a arm64 "pool/${channel}/${component}/s/stacklab" /dev/null > "dists/${channel}/${component}/binary-arm64/Packages"
  gzip -9c "dists/${channel}/${component}/binary-amd64/Packages" > "dists/${channel}/${component}/binary-amd64/Packages.gz"
  gzip -9c "dists/${channel}/${component}/binary-arm64/Packages" > "dists/${channel}/${component}/binary-arm64/Packages.gz"
)

apt-ftparchive \
  -o "APT::FTPArchive::Release::Origin=${origin}" \
  -o "APT::FTPArchive::Release::Label=${label}" \
  -o "APT::FTPArchive::Release::Suite=${channel}" \
  -o "APT::FTPArchive::Release::Codename=${channel}" \
  -o "APT::FTPArchive::Release::Architectures=amd64 arm64" \
  -o "APT::FTPArchive::Release::Components=${component}" \
  release "${dist_dir}" > "${dist_dir}/Release"

gpg_home="$(mktemp -d "${TMPDIR:-/tmp}/stacklab-apt-gpg.XXXXXX")"
cleanup() {
  rm -rf "${gpg_home}"
}
trap cleanup EXIT
chmod 700 "${gpg_home}"

printf '%s' "${APT_GPG_PRIVATE_KEY_BASE64}" | base64 --decode | gpg --batch --homedir "${gpg_home}" --import

selected_key="${APT_GPG_KEY_ID:-}"
if [[ -z "${selected_key}" ]]; then
  selected_key="$(
    gpg --batch --homedir "${gpg_home}" --list-secret-keys --with-colons \
      | awk -F: '/^fpr:/ { print $10; exit }'
  )"
fi
[[ -n "${selected_key}" ]] || {
  echo "could not determine signing key id" >&2
  exit 1
}

gpg_args=(--batch --yes --homedir "${gpg_home}" --pinentry-mode loopback --default-key "${selected_key}")
if [[ -n "${APT_GPG_PASSPHRASE:-}" ]]; then
  gpg_args+=(--passphrase "${APT_GPG_PASSPHRASE}")
fi

gpg --batch --yes --homedir "${gpg_home}" --armor --export "${selected_key}" > "${repo_root}/stacklab-archive-keyring.asc"
gpg --batch --yes --homedir "${gpg_home}" --export "${selected_key}" > "${repo_root}/stacklab-archive-keyring.gpg"

gpg "${gpg_args[@]}" --armor --detach-sign --output "${dist_dir}/Release.gpg" "${dist_dir}/Release"
gpg "${gpg_args[@]}" --clearsign --output "${dist_dir}/InRelease" "${dist_dir}/Release"
