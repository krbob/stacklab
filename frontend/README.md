# Stacklab Frontend

React and Vite scaffold for the Stacklab web UI.

## Scripts

- `npm run dev` starts Vite with the default host.
- `npm run dev:host` binds to `127.0.0.1:5173` and proxies `/api` to the Go backend on `127.0.0.1:8080`.
- `npm run typecheck` runs the TypeScript project build without emitting files.
- `npm run lint` runs ESLint.
- `npm run build` produces the production bundle in `dist/`.

## Current Scope

- React Router layout aligned with `docs/ui/information-architecture.md`
- dashboard, stack detail, editor, logs, stats, terminal, history, login, settings, and create-stack placeholder routes
- Tailwind v4 styling tokens aligned with the current visual direction

## Next Steps

- replace placeholder routes with real data flows from `docs/api/openapi.yaml`
- add shared API client, auth handling, and WebSocket provider
- layer in UI primitives for forms, badges, tables, dialogs, and terminal surfaces
