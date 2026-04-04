# Host Observability Handoff

This handoff covers the UI work for Milestone 1:

- Stacklab version display
- host overview
- Stacklab service log viewer

Backend contract draft:

- `docs/api/host-observability.md`

## Proposed Information Architecture

Recommended routes:

- `/host`
- keep `/settings` for app settings and password only

Recommended nav model:

- add **Host** to the global sidebar between `Stacks` and `Audit`

Reason:

- host overview and Stacklab logs are operational surfaces, not merely app preferences
- putting them into `Settings` would make troubleshooting harder to discover

## Screen Shape

## Host Overview Page

Recommended sections:

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

Recommended placement:

- second section on the same `/host` page
- tabbed or stacked below the overview, whichever fits better

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

## Questions For UI Developer

Please propose:

1. whether `/host` should be a dedicated page or a section inside `/settings`
2. whether logs should live on the same page or as a nested tab
3. whether host resource cards should be compact dashboard-style widgets or denser operator tables

## Expected Backend/UI Sequence

1. architecture confirms route placement
2. backend implements `GET /api/host/overview`
3. backend implements polling-based `GET /api/host/stacklab-logs`
4. UI implements host page and log viewer
5. if polling proves insufficient, revisit streaming later
