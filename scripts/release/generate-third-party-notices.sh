#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: generate-third-party-notices.sh [--check]

Generate the repository-root THIRD_PARTY_NOTICES.md from the dependencies used
by Stacklab's distributed Linux runtime. With --check, verify that the tracked
file is current without modifying it.
EOF
}

check_mode=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --check)
      check_mode=1
      shift
      ;;
    --help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
notices_path="${repo_root}/THIRD_PARTY_NOTICES.md"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/stacklab-third-party-notices.XXXXXX")"

cleanup() {
  rm -rf "${tmp_dir}"
}
trap cleanup EXIT

go_modules_path="${tmp_dir}/go-modules.tsv"
generated_path="${tmp_dir}/THIRD_PARTY_NOTICES.md"

(
  cd "${repo_root}"
  for goarch in amd64 arm64; do
    GOOS=linux GOARCH="${goarch}" CGO_ENABLED=0 \
      go list -deps \
        -f $'{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}' \
        ./cmd/...
  done
) > "${go_modules_path}"

node - "${repo_root}" "${go_modules_path}" "${generated_path}" <<'NODE'
const fs = require("node:fs");
const path = require("node:path");
const { TextDecoder } = require("node:util");

const [, , repoRoot, goModulesPath, outputPath] = process.argv;
const decoder = new TextDecoder("utf-8", { fatal: true });

function compareText(left, right) {
  return left < right ? -1 : left > right ? 1 : 0;
}

function fail(message) {
  throw new Error(message);
}

function licenseFiles(directory, dependencyLabel) {
  if (!fs.existsSync(directory) || !fs.statSync(directory).isDirectory()) {
    fail(`dependency directory is missing for ${dependencyLabel}`);
  }

  const files = fs
    .readdirSync(directory)
    .filter((filename) => /^(LICENSE|COPYING|NOTICE)/i.test(filename))
    .filter((filename) => fs.statSync(path.join(directory, filename)).isFile())
    .sort(compareText);

  if (files.length === 0) {
    fail(`no LICENSE*, COPYING*, or NOTICE* file found for ${dependencyLabel}`);
  }

  return files.map((filename) => {
    const filePath = path.join(directory, filename);
    let contents;
    try {
      contents = decoder.decode(fs.readFileSync(filePath));
    } catch (error) {
      fail(`license file is not valid UTF-8 for ${dependencyLabel}: ${filename} (${error.message})`);
    }

    contents = contents.replace(/\r\n?/g, "\n").replace(/\n*$/, "\n");
    if (contents.trim().length === 0) {
      fail(`license file is empty for ${dependencyLabel}: ${filename}`);
    }

    return { filename, contents };
  });
}

function markdownCode(value) {
  return `\`${String(value).replace(/`/g, "\\`")}\``;
}

function fencedText(contents) {
  const runs = contents.match(/`+/g) ?? [];
  const fenceLength = Math.max(3, ...runs.map((run) => run.length + 1));
  const fence = "`".repeat(fenceLength);
  return `${fence}text\n${contents}${fence}\n`;
}

function renderLicenseFiles(lines, files) {
  for (const file of files) {
    lines.push(`#### ${markdownCode(file.filename)}`, "", fencedText(file.contents));
  }
}

function readGoModules() {
  const modules = new Map();
  const rows = fs.readFileSync(goModulesPath, "utf8").split("\n");

  for (const row of rows) {
    if (row.length === 0) continue;

    const fields = row.split("\t");
    if (fields.length !== 3 || fields.some((field) => field.length === 0)) {
      fail(`invalid go list module record: ${JSON.stringify(row)}`);
    }

    const [modulePath, version, directory] = fields;
    const key = `${modulePath}\u0000${version}`;
    const existing = modules.get(key);
    if (existing && existing.directory !== directory) {
      fail(`Go module resolves to multiple directories: ${modulePath} ${version}`);
    }
    modules.set(key, { modulePath, version, directory });
  }

  if (modules.size === 0) {
    fail("go list returned no third-party modules for ./cmd/...");
  }

  return [...modules.values()]
    .sort((left, right) =>
      compareText(left.modulePath, right.modulePath) || compareText(left.version, right.version),
    )
    .map((module) => ({
      ...module,
      licenses: licenseFiles(module.directory, `${module.modulePath} ${module.version}`),
    }));
}

function packageNameFromPath(packagePath) {
  const marker = "node_modules/";
  const markerIndex = packagePath.lastIndexOf(marker);
  if (markerIndex === -1) return "";

  const packageParts = packagePath.slice(markerIndex + marker.length).split("/");
  if (packageParts[0].startsWith("@")) {
    return packageParts.length >= 2 ? `${packageParts[0]}/${packageParts[1]}` : "";
  }
  return packageParts[0] ?? "";
}

function readFrontendPackages() {
  const lockPath = path.join(repoRoot, "frontend", "package-lock.json");
  const lock = JSON.parse(fs.readFileSync(lockPath, "utf8"));
  if (!lock.packages || typeof lock.packages !== "object") {
    fail("frontend/package-lock.json does not contain a packages map");
  }

  const packages = [];
  for (const [packagePath, metadata] of Object.entries(lock.packages)) {
    if (packagePath === "" || metadata.dev === true) continue;

    const name = metadata.name || packageNameFromPath(packagePath);
    const version = metadata.version;
    const license = metadata.license;
    if (typeof name !== "string" || name.length === 0) {
      fail(`missing package name metadata for ${packagePath}`);
    }
    if (typeof version !== "string" || version.length === 0) {
      fail(`missing package version metadata for ${packagePath}`);
    }
    if (typeof license !== "string" || license.length === 0) {
      fail(`missing package license metadata for ${name} ${version}`);
    }

    const directory = path.join(repoRoot, "frontend", packagePath);
    packages.push({
      packagePath,
      name,
      version,
      license,
      licenses: licenseFiles(directory, `${name} ${version}`),
    });
  }

  if (packages.length === 0) {
    fail("frontend/package-lock.json contains no runtime packages");
  }

  return packages.sort((left, right) =>
    compareText(left.name, right.name) ||
    compareText(left.version, right.version) ||
    compareText(left.packagePath, right.packagePath),
  );
}

const goModules = readGoModules();
const frontendPackages = readFrontendPackages();
const lines = [
  "# Third-Party Notices",
  "",
  "Stacklab includes third-party software governed by the license terms reproduced below.",
  "This file is generated deterministically by `scripts/release/generate-third-party-notices.sh`; do not edit it manually.",
  "",
  "The dependency scope is:",
  "",
  "- Go modules linked into the distributed Linux amd64 or arm64 binaries built from `./cmd/...`.",
  "- Frontend packages marked as non-development dependencies in `frontend/package-lock.json`.",
  "",
  `## Go modules (${goModules.length})`,
  "",
];

for (const module of goModules) {
  lines.push(
    `### ${module.modulePath} ${module.version}`,
    "",
    `- Module: ${markdownCode(module.modulePath)}`,
    `- Version: ${markdownCode(module.version)}`,
    "",
  );
  renderLicenseFiles(lines, module.licenses);
}

lines.push(`## Frontend packages (${frontendPackages.length})`, "");
for (const frontendPackage of frontendPackages) {
  lines.push(
    `### ${frontendPackage.name} ${frontendPackage.version}`,
    "",
    `- Name: ${markdownCode(frontendPackage.name)}`,
    `- Version: ${markdownCode(frontendPackage.version)}`,
    `- Declared license: ${markdownCode(frontendPackage.license)}`,
    "",
  );
  renderLicenseFiles(lines, frontendPackage.licenses);
}

fs.writeFileSync(outputPath, `${lines.join("\n").replace(/\n+$/, "")}\n`, { mode: 0o644 });
NODE

if [[ "${check_mode}" == "1" ]]; then
  if [[ ! -f "${notices_path}" ]]; then
    echo "THIRD_PARTY_NOTICES.md is missing; run scripts/release/generate-third-party-notices.sh" >&2
    exit 1
  fi
  if ! cmp -s "${generated_path}" "${notices_path}"; then
    echo "THIRD_PARTY_NOTICES.md is stale; run scripts/release/generate-third-party-notices.sh" >&2
    exit 1
  fi
  echo "THIRD_PARTY_NOTICES.md is current"
  exit 0
fi

chmod 0644 "${generated_path}"
mv "${generated_path}" "${notices_path}"
echo "Generated THIRD_PARTY_NOTICES.md"
