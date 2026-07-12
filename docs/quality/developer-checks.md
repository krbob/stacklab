# Reproducible Developer Checks

## Canonical Toolchain

Stacklab uses one exact toolchain for local development and CI:

| Tool | Version | Canonical declaration | Mirrors and consumers |
| --- | --- | --- | --- |
| Go | `1.26.5` | `go.mod` | `.tool-versions`, every `actions/setup-go` step |
| Node.js | `24.18.0` | `.nvmrc` | `.tool-versions`, `frontend/package.json` engines, every `actions/setup-node` step |

`frontend/.npmrc` enables `engine-strict`, so direct npm commands also reject an
unsupported Node version. `.tool-versions` supports asdf/mise users, while
`.nvmrc` works with nvm and is the version source used by GitHub Actions.
Dated `test-run-*` reports retain the versions observed during those historical
runs and are not active toolchain declarations.

## Full Local Gate

From the repository root, run:

```bash
make check
```

The command fails before installing dependencies when the active Go or Node
version differs from the repository declarations. It then runs, in order:

1. backend tests for `./cmd/...` and `./internal/...`
2. backend formatting and `go vet` for the same explicit scope
3. `npm ci`, frontend tests, typecheck, and production build
4. the existing repository hygiene gate: actionlint, ShellCheck, secret scans,
   production npm audit, and zero-warning ESLint

The explicit Go package scope is intentional. It prevents the backend check
from traversing `frontend/node_modules` or treating frontend tooling as Go
source.

For a focused iteration, use:

```bash
make check-backend
make check-frontend
make check-hygiene
```

These targets enforce the relevant portion of the toolchain before running.
The full `make check` remains the handoff command before a commit or pull
request.

Docker-backed integration and browser E2E remain separate CI workflows because
they require runtime services beyond the reproducible source-tree baseline.

## OpenAPI Contract Generation

The tracked frontend contract is generated from `docs/api/openapi.yaml`. After
changing the REST API specification, regenerate it with the canonical Node
toolchain:

```bash
cd frontend
npm run generate:api
```

Commit `src/lib/api-contract.generated.ts` together with the OpenAPI change.
Do not edit the generated file by hand. `frontend/src/lib/api-types.ts` is the
stable application-facing facade over generated `components` and `operations`;
WebSocket-only models remain in `frontend/src/lib/ws-types.ts`.

To check generation without accepting drift, run from the repository root:

```bash
make frontend-api-contract
```

The target regenerates the contract and fails when the result differs from the
committed file. It is also part of `make check-frontend`, `make check`, and the
GitHub Actions frontend gate.
