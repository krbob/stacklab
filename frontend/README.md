# Stacklab Frontend

React, TypeScript, and Vite application for the Stacklab web UI. In production
the Go backend serves the bundle from `frontend/dist`; in development Vite
proxies API and WebSocket traffic to the backend.

Install the pinned dependencies before running a script:

```bash
npm ci
```

## Scripts

| Command | Purpose |
| --- | --- |
| `npm run dev` | Start the Vite development server. |
| `npm run dev:host` | Bind to `127.0.0.1:5173` and proxy to the backend on `127.0.0.1:8080`. |
| `npm run generate` / `npm run generate:api` | Regenerate tracked REST types from [`docs/api/openapi.yaml`](../docs/api/openapi.yaml). |
| `npm run typecheck` | Run the TypeScript project build without emitting files. |
| `npm run lint` | Run ESLint with zero warnings allowed. |
| `npm test` | Run the Vitest suite once. |
| `npm run test:watch` | Run Vitest in watch mode. |
| `npm run test:e2e` | Run the Playwright browser suite. |
| `npm run build` | Type-check and build the production bundle in `dist/`. |
| `npm run preview` | Serve the production bundle locally with Vite Preview. |
| `npm run screenshots:readme` | Refresh tracked README screenshots against a running Stacklab instance. |

Do not edit `src/lib/api-contract.generated.ts` by hand. Update OpenAPI, run
the generator, and commit both changes together; `src/lib/api-types.ts` is the
stable application-facing facade.

See [local development](../docs/ops/local-dev.md),
[developer checks](../docs/quality/developer-checks.md), and the
[UI documentation](../docs/ui/README.md) for runtime configuration, repository
gates, and interaction contracts.
