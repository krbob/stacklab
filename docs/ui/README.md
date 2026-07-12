# UI Docs

This section defines the implemented information architecture, screen behavior,
state presentation, and accessibility policy for Stacklab. API and domain
contracts remain authoritative for payloads and state enumerations; these
documents own how the product presents and navigates them.

## Documents

- [information-architecture.md](information-architecture.md) — navigation model, screen inventory, URL structure, metadata, and responsive breakpoints
- [screen-specs.md](screen-specs.md) — per-screen behavior and operator workflow specifications
- [states-and-empty-cases.md](states-and-empty-cases.md) — stack, service, operation, loading, error, blocked-file, and confirmation states
- [accessibility-dynamic-status.md](accessibility-dynamic-status.md) — ARIA and keyboard contract for dynamic status, progress, tabs, toggles, and the command palette
- [readability-and-motion.md](readability-and-motion.md) — text-size floor, AA token contrast, local fonts, reduced motion, and texture limits
- [maintenance-experience.md](maintenance-experience.md) — lazy tab lifecycle, debounced inventory search, stack status, idle review, and recent runs

Browser automation is documented with the other quality gates in
[Browser End-to-End Tests](../quality/browser-e2e.md).

## Frontend Stack

- React 19 with TypeScript and Vite
- Tailwind CSS and shadcn/ui primitives
- CodeMirror 6 for Compose editing
- XTerm.js for terminal sessions
- uPlot for runtime charts

## Responsive Policy

- desktop (`>= 1280px`): full layout and optional side panels;
- tablet (`768px–1279px`): collapsed navigation with the complete feature set;
- mobile (`< 768px`): core read and operate flows remain supported, while
  dense editor and terminal work is optimized for larger screens.

## Contract Ownership

- REST payloads and status codes: `docs/api/openapi.yaml` and the focused API
  guides under `docs/api/`;
- WebSocket streams: `docs/api/websocket-protocol.md`;
- stack and job states: documents under `docs/domain/`;
- route placement, responsive navigation, and screen hierarchy:
  [Information Architecture](information-architecture.md);
- presentation of asynchronous, empty, blocked, and error states:
  [States, Badges, and Empty Cases](states-and-empty-cases.md) and
  [Dynamic Interface Accessibility](accessibility-dynamic-status.md).
