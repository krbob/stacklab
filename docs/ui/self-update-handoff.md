# Stacklab Self-Update Handoff

This handoff covers the first UI slice for Stacklab self-update.

Backend contract draft:

- `docs/api/self-update.md`

## Product Intent

This surface exists to let the operator:

- see the current Stacklab version
- see whether a newer package is available
- trigger the Stacklab package upgrade without leaving the UI

It is not a generic package manager.

## Recommended Information Architecture

Recommended placement:

- `/settings`

Recommended section order:

- Password
- Notifications
- Maintenance schedules
- Stacklab update
- About

Rationale:

- this is application administration, not host observability
- it belongs near notifications and schedules, not inside `/host`
- it is global and not tied to any single stack

## Screen Shape

Recommended v1 shape:

1. Stacklab update card
   - current version
   - install mode
   - configured channel
   - installed package version
   - candidate package version
   - update available badge
2. capability / unsupported state
   - APT-only explanation for tarball installs
   - helper/sudoers capability reason when write is disabled
3. runtime status
   - currently running self-update job
   - most recent result
   - link to shared job detail drawer
4. update action
   - `Update Stacklab`
   - optional `Refresh package index` toggle if we decide to expose it in v1

## Required UI States

- loading overview
- APT supported and update available
- APT supported but already up to date
- tarball unsupported
- helper unavailable / preview-only capability disabled
- self-update currently running
- last update succeeded
- last update failed

## UX Guidance

- treat this as a deliberate, high-trust admin action
- show current and candidate version clearly before the operator clicks update
- if `write_capability.supported = false`, show the reason inline instead of hiding the feature
- self-update should reuse:
  - global activity
  - job detail drawer

Recommended copy for unsupported tarball installs:

- `Self-update is only available for APT installs.`

Recommended copy when no update is available:

- `Stacklab is already up to date.`

Recommended copy when the helper is not configured:

- use `write_capability.reason` verbatim

## Important Technical Notes

- `POST /api/stacklab/update/apply` starts a global job and may restart Stacklab
- the HTTP request itself should be treated as "fire and monitor", not "wait for final success"
- final status should come from:
  - global activity
  - shared job detail drawer
  - refreshed `/api/stacklab/update/overview`

## Questions To Confirm With UI

The backend is ready. The next UI decisions are:

1. Should `Stacklab update` live as:
   - one card inside `/settings`
   - or a dedicated route such as `/stacklab`
2. Should the action area be:
   - one primary `Update Stacklab` button
   - or `Check again` + `Update Stacklab`
3. Should `refresh_package_index` be:
   - implicit and hidden in v1
   - or shown as an advanced checkbox

Recommended answers:

- one card inside `/settings`
- one primary `Update Stacklab` button
- keep `refresh_package_index` hidden in v1 and default it on
