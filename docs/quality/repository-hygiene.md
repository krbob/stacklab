# Repository Hygiene Checks

## Enforcement

The required `repository-hygiene` job is part of `.github/workflows/pr-quality.yml`.
It runs for pull requests and pushes to `main`; release workflows inherit it
through the reusable release quality gate and validate the requested source SHA.

The job enforces:

- actionlint `v1.7.12` for every GitHub Actions workflow
- ShellCheck `v0.11.0` for tracked shell scripts and Debian maintainer scripts
- Gitleaks `v8.30.0` with the default rule set against the complete Git history and normalized current lockfile contents
- `npm audit --omit=dev` for production frontend dependencies
- ESLint with `--max-warnings=0`

GitHub Actions are pinned to full commit SHAs. Go-based tools are invoked at
explicit module versions. CI downloads the ShellCheck release archive at an
explicit version and verifies its pinned SHA-256 before extraction.

## Local Command

Install frontend dependencies first:

```bash
npm --prefix frontend ci
```

Then run the same checks as CI from the repository root:

```bash
./scripts/quality/check-repository-hygiene.sh
```

The command requires the repository Go and Node toolchains plus ShellCheck
`0.11.0`. actionlint and Gitleaks are resolved by Go at the versions embedded
in the script.

## Secret Scan Exceptions

`.gitleaks.toml` extends the tool's default rules. It does not exclude fixture
directories or test files wholesale. A fixture exception applies only to the
`generic-api-key` rule and must match both a narrow path expression and an
exact deterministic value; stronger token rules remain active in the same
file.

Gitleaks' built-in default config excludes dependency lockfiles by path. The
local command compensates with a second stdin scan of their current contents.
It removes only complete npm `integrity` properties and valid three-field Go
checksum values while preserving module names and versions. Any credential in
the remaining lockfile content, or appended to an integrity record, remains
visible.

Do not add commit-wide baselines or path-only exceptions to make CI green.
Investigate a finding first; if it is a false positive, constrain an exception
to the smallest path and exact non-secret value shape.

## Updating Tool Pins

Renovate discovers the versions embedded in the local script. When updating
ShellCheck, also download the new official Linux x86_64 archive, calculate its
SHA-256, and update `SHELLCHECK_ARCHIVE_SHA256` in `pr-quality.yml`. A version
change with the old digest intentionally fails closed.
