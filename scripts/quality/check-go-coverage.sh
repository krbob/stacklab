#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
coverage_dir="${1:-coverage}"
if [[ "${coverage_dir}" != /* ]]; then
  coverage_dir="${repo_root}/${coverage_dir}"
fi

mkdir -p "${coverage_dir}"
profile="${coverage_dir}/backend.out"
functions_report="${coverage_dir}/functions.txt"
package_report="${coverage_dir}/packages.tsv"
markdown_report="${coverage_dir}/summary.md"
html_report="${coverage_dir}/coverage.html"
rm -f "${profile}" "${functions_report}" "${package_report}" "${markdown_report}" "${html_report}"

cd "${repo_root}"
go test -covermode=atomic -coverprofile="${profile}" ./cmd/... ./internal/...
go tool cover -func="${profile}" > "${functions_report}"
go tool cover -html="${profile}" -o "${html_report}"

module_path="$(go list -m)"
package_specs=(
  "audit:90.0"
  "retention:95.0"
  "fsmeta:95.0"
  "httpapi:50.0"
  "selfupdate:70.0"
  "jobs:85.0"
  "store:75.0"
)

global_coverage="$(awk '/^total:/ { value = $NF; sub(/%$/, "", value); print value }' "${functions_report}")"
printf 'package\tcoverage_percent\tminimum_percent\tresult\n' > "${package_report}"
{
  printf '# Backend coverage\n\n'
  printf 'Global statement coverage: **%s%%** (reported as a trend; no global gate).\n\n' "${global_coverage}"
  printf '| Package | Coverage | Minimum | Result |\n'
  printf '| --- | ---: | ---: | --- |\n'
} > "${markdown_report}"

failed=0
printf 'Backend package coverage (global %s%%; no global threshold):\n' "${global_coverage}"
for spec in "${package_specs[@]}"; do
  package_name="${spec%%:*}"
  minimum="${spec#*:}"
  package_prefix="${module_path}/internal/${package_name}/"
  coverage="$(awk -v prefix="${package_prefix}" '
    NR == 1 { next }
    index($1, prefix) == 1 {
      statements = $(NF - 1)
      total += statements
      if ($NF > 0) {
        covered += statements
      }
    }
    END {
      if (total == 0) {
        exit 2
      }
      printf "%.1f", (covered * 100) / total
    }
  ' "${profile}")"

  result="PASS"
  if ! awk -v actual="${coverage}" -v threshold="${minimum}" 'BEGIN { exit !(actual + 0 >= threshold + 0) }'; then
    result="FAIL"
    failed=1
  fi

  printf '  %-12s %6s%% (minimum %s%%) %s\n' "${package_name}" "${coverage}" "${minimum}" "${result}"
  printf '%s\t%s\t%s\t%s\n' "${package_name}" "${coverage}" "${minimum}" "${result}" >> "${package_report}"
  printf "| \`internal/%s\` | %s%% | %s%% | %s |\\n" "${package_name}" "${coverage}" "${minimum}" "${result}" >> "${markdown_report}"
done

if (( failed != 0 )); then
  printf 'One or more package coverage floors were not met.\n' >&2
  exit 1
fi
