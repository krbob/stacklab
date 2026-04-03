# UI Docs

This section contains information architecture, screens, and UI-facing contracts for Stacklab.

## Documents

- [information-architecture.md](information-architecture.md) — navigation model, screen inventory, URL structure, responsive breakpoints
- [screen-specs.md](screen-specs.md) — per-screen specifications with wireframes
- [states-and-empty-cases.md](states-and-empty-cases.md) — stack/service/operation state model, badges, empty states, loading, errors, confirmation dialogs
- [editor-progress-integration.md](editor-progress-integration.md) — backend-backed integration notes for the editor, mutating actions, and progress panel

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
