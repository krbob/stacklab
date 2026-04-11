# Host Observability Handoff

This handoff is a historical UI planning snapshot for the host observability slice.

The route and feature are already implemented; use `docs/api/host-observability.md`
and the running product as the current source of truth.

Original scope:

- Stacklab version display
- host overview
- Stacklab service log viewer

Backend contract draft:

- `docs/api/host-observability.md`

## Confirmed Information Architecture

Confirmed route:

- `/host`
- keep `/settings` for app settings and password only

Confirmed nav model:

- add **Host** to the global sidebar between `Stacks` and `Audit`

Rationale:

- host overview and Stacklab logs are operational surfaces, not merely app preferences
- putting them into `Settings` would make troubleshooting harder to discover

## Confirmed Screen Shape

## Host Overview Page

Confirmed sections:

1. Stacklab
   - version
   - commit/build metadata
   - process start time
2. Host
   - hostname
   - OS
   - kernel
   - uptime
   - architecture
3. Docker
   - engine version
   - compose version
4. Resources
   - CPU
   - memory
   - disk

Optional later:

- compact status cards on the dashboard

## Stacklab Logs Panel

Confirmed placement:

- second section on the same `/host` page
- stacked vertically below the overview cards

Required capabilities:

- refresh
- follow mode via polling
- severity filter
- text filter

Nice to have later:

- copy selected lines
- download current window

## UI States

Need to define:

- loading state for host overview
- loading state for logs
- empty logs state
- permission/unavailable state when `journald` is not readable
- degraded state if host metrics load but logs do not

## Local Development Caveat

When `/host` is exercised on macOS during development, the page may look incomplete compared to a real Linux host.

Expected degraded behaviors on macOS:

- Stacklab logs may show an unavailable state because `journald` is not present
- host resource numbers may be partial or less trustworthy than on Linux
- the page should still render cleanly and communicate degraded availability instead of looking broken

This page should be judged primarily on Linux staging and production-like hosts, not only on macOS screenshots.

## UI Decisions Captured

1. `/host` is a dedicated page in the main sidebar
2. Stacklab logs are stacked under the overview on the same page
3. host resource presentation uses compact dashboard-style cards, not a dense operator table

## Expected Backend/UI Sequence

1. architecture confirms route placement
2. backend implements `GET /api/host/overview`
3. backend implements polling-based `GET /api/host/stacklab-logs`
4. UI implements host page and log viewer
5. if polling proves insufficient, revisit streaming later
