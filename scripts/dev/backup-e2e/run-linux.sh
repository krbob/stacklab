#!/usr/bin/env bash
set -euo pipefail

# SSH credentials are supplied through an agent/key or SSHPASS plus sshpass.
# The target account needs Docker administration on a disposable Linux host.
# Existing application configuration and containers are never modified.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
host="${1:?Usage: run-linux.sh user@host unique-run-id artifact-directory [arm64|amd64]}"
run_id="${2:?Supply a unique run ID}"
artifacts="${3:?Supply a local artifact directory}"
architecture="${4:-arm64}"
[[ "${host}" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.@:-]*$ ]] || { echo 'Invalid SSH target' >&2; exit 1; }
[[ "${run_id}" =~ ^[a-z0-9-]{1,48}$ ]] || { echo 'Invalid run ID' >&2; exit 1; }
[[ "${architecture}" == arm64 || "${architecture}" == amd64 ]] || { echo 'Unsupported architecture' >&2; exit 1; }
[[ ! -e "${artifacts}" ]] || { echo 'Artifact directory already exists; choose a new one' >&2; exit 1; }
mkdir -p "${artifacts}"
artifacts="$(cd "${artifacts}" && pwd)"
remote_tmp="/tmp/stacklab-backup-e2e-${run_id}"
remote_work="/var/lib/stacklab-backup-e2e-${run_id}"
ssh_command=(ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 -o NumberOfPasswordPrompts=1 -o ControlMaster=auto -o ControlPersist=600 -o "ControlPath=${artifacts}/ssh-control")
scp_command=(scp -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 -o NumberOfPasswordPrompts=1 -o ControlMaster=auto -o ControlPersist=600 -o "ControlPath=${artifacts}/ssh-control")
if [[ -n "${SSHPASS:-}" ]]; then
  ssh_command=(sshpass -e "${ssh_command[@]}" -o PreferredAuthentications=password -o PubkeyAuthentication=no)
  scp_command=(sshpass -e "${scp_command[@]}" -o PreferredAuthentications=password -o PubkeyAuthentication=no)
fi
# Establish the connection before any remote mutation. Retrying authentication
# cannot repeat a destructive command; all subsequent calls reuse this session.
connected=0
for attempt in 1 2 3; do
  if "${ssh_command[@]}" -N -f "${host}"; then connected=1; break; fi
  printf 'SSH connection attempt %s failed.\n' "${attempt}" >&2
done
[[ "${connected}" == 1 ]] || exit 1
cleanup_connection() { "${ssh_command[@]}" -O exit "${host}" >/dev/null 2>&1 || true; }
trap cleanup_connection EXIT
cd "${repo_root}"
git rev-parse HEAD >"${artifacts}/revision.txt"
git diff --binary >"${artifacts}/working-tree.patch"
git ls-files -z --cached --others --exclude-standard -- '*.go' go.mod go.sum 'scripts/dev/backup-e2e/*' 'internal/backup/testdata/**' | tar -czf "${artifacts}/source.tar.gz" --null -T -
go version >"${artifacts}/toolchain.txt"
GOOS=linux GOARCH="${architecture}" CGO_ENABLED=0 go test -c -o "${artifacts}/backup.test" ./internal/backup
GOOS=linux GOARCH="${architecture}" CGO_ENABLED=0 go test -c -o "${artifacts}/helper.test" ./cmd/stacklab-backup-helper
GOOS=linux GOARCH="${architecture}" CGO_ENABLED=0 go build \
  -ldflags "-X main.configurationPath=${remote_work}/stacklab.env -X main.restoreSpoolRoot=${remote_work}/helper-spool" \
  -o "${artifacts}/fixture-helper" ./cmd/stacklab-backup-helper
shasum -a 256 "${artifacts}/backup.test" "${artifacts}/helper.test" "${artifacts}/fixture-helper" >"${artifacts}/sha256.txt"
"${ssh_command[@]}" "${host}" "mkdir -m 700 '${remote_tmp}'"
"${scp_command[@]}" "${artifacts}/backup.test" "${artifacts}/helper.test" "${artifacts}/fixture-helper" \
  scripts/dev/backup-e2e/isolated-linux.py "${host}:${remote_tmp}/"
# Expand id only on the remote host.
# shellcheck disable=SC2016
"${ssh_command[@]}" "${host}" 'set -e; test "$(id -u)" -ne 0; command -v restic >/dev/null; command -v python3 >/dev/null; docker image inspect debian:trixie-slim postgres:17-alpine >/dev/null; uname -a; restic version; tar --version | head -1; docker version --format "{{.Server.Version}}"; dpkg-query -W stacklab restic' >"${artifacts}/host.txt"
"${ssh_command[@]}" "${host}" "'${remote_tmp}/backup.test' -test.v -test.timeout=15m" 2>&1 | tee "${artifacts}/backup-tests.log"
"${ssh_command[@]}" "${host}" "docker run --rm --pid=host -v /:/host debian:trixie-slim chroot /host '${remote_tmp}/helper.test' -test.v -test.timeout=10m" 2>&1 | tee "${artifacts}/helper-tests.log"
"${ssh_command[@]}" "${host}" "docker run --rm --pid=host -v /:/host debian:trixie-slim chroot /host python3 '${remote_tmp}/isolated-linux.py' --work-dir '${remote_work}' --helper '${remote_tmp}/fixture-helper' --result '${remote_tmp}/integration.json' --keep-on-failure" 2>&1 | tee "${artifacts}/integration.log"
"${scp_command[@]}" "${host}:${remote_tmp}/integration.json" "${artifacts}/integration.json"
"${ssh_command[@]}" "${host}" "rm -rf '${remote_tmp}'"
printf 'Evidence and binary hashes: %s\n' "${artifacts}"
