# Contributing to Stacklab

Thank you for improving Stacklab. The project targets a single Linux host and
keeps Docker Compose files on disk as the source of truth. Changes should
preserve that scope unless a proposal explicitly updates the product and
architecture documentation first.

## Before opening a change

1. Search existing issues and pull requests for overlapping work.
2. For a large feature or contract change, open a design issue before writing
   the implementation.
3. Do not include credentials, host inventories, private image names, `.env`
   contents, database files, or production logs with sensitive values.

Security vulnerabilities follow [SECURITY.md](SECURITY.md), not the public
issue tracker.

## Development setup

The canonical versions are declared in `.tool-versions`, `.nvmrc`, `go.mod`,
and `frontend/package.json`. Install frontend dependencies with `npm ci`.

Run the complete local baseline from the repository root:

```bash
make check
```

Focused commands are available when iterating:

```bash
make check-backend
make check-frontend
make check-hygiene
```

See [developer checks](docs/quality/developer-checks.md) and
[local development](docs/ops/local-dev.md) for prerequisites and runtime
configuration.

## Change expectations

- keep commits focused and use an imperative Conventional Commit-style subject;
- add regression tests for behavior changes;
- update the canonical OpenAPI or WebSocket contract for public API changes,
  plus a focused semantics document when the behavior needs one;
- update operational documentation for install, upgrade, permission, or
  lifecycle changes;
- preserve unrelated local changes and generated artifacts;
- keep UI behavior keyboard accessible and test important loading, error, and
  empty states.

Pull requests should explain the problem, the chosen behavior, risk and
rollback considerations, and the verification performed. Screenshots or short
recordings are useful for visible UI changes.

## Generated and release files

Frontend REST types are generated from `docs/api/openapi.yaml` and tracked in
the repository. After changing the REST contract, run:

```bash
npm --prefix frontend run generate:api
```

Commit `frontend/src/lib/api-contract.generated.ts` with the OpenAPI change.
Do not edit the generated file by hand; use
`frontend/src/lib/api-types.ts` as the stable application-facing facade.

Release artifacts, package repository metadata, and screenshots are produced
by their documented workflows. A normal feature pull request must not publish
them.

## Licensing contributions

Stacklab is licensed under the [Apache License 2.0](LICENSE). Unless explicitly
stated otherwise, every contribution intentionally submitted for inclusion in
Stacklab is provided under that license without additional terms or conditions.
Contributors must identify third-party code or assets and preserve their
copyright, license, and attribution notices.
