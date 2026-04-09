# UI Docs

This section contains information architecture, screens, and UI-facing contracts for Stacklab.

## Documents

- [information-architecture.md](information-architecture.md) — navigation model, screen inventory, URL structure, responsive breakpoints
- [screen-specs.md](screen-specs.md) — per-screen specifications with wireframes
- [states-and-empty-cases.md](states-and-empty-cases.md) — stack/service/operation state model, badges, empty states, loading, errors, confirmation dialogs
- [editor-progress-integration.md](editor-progress-integration.md) — backend-backed integration notes for the editor, mutating actions, and progress panel
- [host-observability-handoff.md](host-observability-handoff.md) — route and screen guidance for host overview and Stacklab service logs
- [docker-admin-handoff.md](docker-admin-handoff.md) — route and screen guidance for read-only Docker daemon administration
- [config-workspace-handoff.md](config-workspace-handoff.md) — route and screen guidance for `/opt/stacklab/config` browsing and editing
- [git-workspace-handoff.md](git-workspace-handoff.md) — route and screen guidance for local Git change visibility inside `/config`
- [workspace-permissions-handoff.md](workspace-permissions-handoff.md) — blocked-file semantics for config and Git views when containers create unreadable files
- [maintenance-workflows-handoff.md](maintenance-workflows-handoff.md) — route and screen guidance for bulk stack update workflows
- [maintenance-inventory-handoff.md](maintenance-inventory-handoff.md) — route and screen guidance for image inventory and prune flows inside `/maintenance`
- [browser-e2e-handoff.md](browser-e2e-handoff.md) — backend harness, fixture root, and CI shape for browser E2E smoke

## Frontend Stack

Confirmed:

- React 19 + TypeScript + Vite
- Tailwind CSS + shadcn/ui
- CodeMirror 6 (Compose editor)
- XTerm.js (terminal)
- uPlot (stats charts)

## Responsive Policy

- Desktop-first (>= 1280px): full experience
- Tablet (768px - 1279px): dashboard and details usable, terminal and editor show "best on desktop" hint
- Mobile (< 768px): not a target, basic readability only

## Dependencies on Backend Contracts

UI implementation of the following screens depends on backend contracts and runtime behavior:

- Terminal, Logs, Stats → `docs/api/websocket-protocol.md`
- All data views → `docs/api/rest-endpoints.md`
- Badge system → `docs/domain/stack-model.md` (state enumerations)
- Audit views → audit log schema and API
