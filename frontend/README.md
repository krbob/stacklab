# Stacklab Frontend

React + Vite frontend for the Stacklab web UI.

## Scripts

- `npm run dev` starts Vite with the default host.
- `npm run dev:host` binds to `127.0.0.1:5173` and proxies `/api` to the Go backend on `127.0.0.1:8080`.
- `npm run typecheck` runs the TypeScript project build without emitting files.
- `npm run lint` runs ESLint.
- `npm run build` produces the production bundle in `dist/`.
- `npm test` runs the Vitest suite.

## Current Scope

- React Router application aligned with `docs/ui/information-architecture.md`
- auth flow and route guarding
- stack list, stack detail, editor, logs, stats, terminal, history, login, settings, and create-stack flows
- REST client and multiplexed WebSocket client
- Vitest coverage for selected UI and client modules

## Notes

- the frontend is optimized for desktop-first homelab usage, with tablet-tolerant layouts where practical
- the production bundle is served by the Go backend from `frontend/dist`
- Vite dev mode proxies `/api` and WebSocket traffic to the backend
