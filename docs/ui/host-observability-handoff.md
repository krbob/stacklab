# Host Observability Handoff

This handoff documents the current `/host` UI shape for the host observability slice.

Backend contract:

- `docs/api/host-observability.md`

Current scope:

- Stacklab version display
- host overview
- native host dashboard
- Stacklab service log viewer

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

## Host Metrics Dashboard

Confirmed placement:

- inside the first `/host` section, below the overview cards
- logs stay below the host section

Confirmed metrics:

- CPU percent, core count, load average, and short history
- CPU temperature from Linux sysfs sensors when available, shown in the CPU card
- memory usage and short history
- swap usage in the Memory card, including an explicit disabled state when no
  swap is configured
- aggregate network RX/TX throughput and per-interface RX/TX
- disk read/write throughput in the Storage card, including the most active
  top-level block device
- mounted filesystems with percent, used/total bytes, mount point, device, and filesystem type
- duplicate bind mounts of the same physical filesystem are collapsed so
  `systemd` sandbox paths such as `/etc`, `/usr`, or the Stacklab root do not
  appear as separate disks
- the Stacklab root filesystem is marked as primary

Sampling behavior:

- the backend collector runs independently from browser sessions
- idle/background sampling interval: `30s`
- dashboard-active sampling interval: `1s`
- polling `GET /api/host/metrics` marks the collector active
- after the first full metrics load, the frontend polls with `since` and merges
  only new history samples into local state
- leaving `/host` or hiding the browser tab stops the frontend polling; after the active TTL expires, the backend returns to the background interval
- history is an in-memory `30m` ring buffer
- history is pruned by timestamp, so mixed `1s`/`30s` sampling still reports a real 30-minute window
- there is no SQLite persistence in v1

Dashdot parity decisions:

- always show percentages where a percent is available
- show host filesystems natively from `mountinfo`/`statfs`, deduped by physical
  mount identity; no dashdot-style container virtual mount mapping is needed for
  normal Stacklab installs
- skip network filesystems in v1 to avoid blocking dashboard sampling on an unavailable NAS/share
- show network interface throughput from byte counters
- filter Docker bridge/veth-style virtual interfaces from the primary dashboard view
- show CPU temperature/sensors directly from `/sys/class/hwmon` and
  `/sys/class/thermal` without requiring `lm-sensors`
- do not implement speedtest / Ookla EULA flow in v1
- do not implement public IP or GPU metrics in v1

Backlog candidates:

- GPU usage
- public IP display
- optional speedtest integration
- configurable filesystem include/exclude list if real deployments need it

Tracked engineering follow-ups:

- Memoize dashboard-derived series and sparkline inputs if `/host` becomes
  noticeably expensive on weak clients. Current active mode can re-render around
  once per metrics poll plus the uptime clock, which is acceptable for now.
- Dedupe the visibility/focus refresh path if returning to the tab proves noisy.
  The current behavior can issue one extra overview/metrics refresh when the
  browser fires both `focus` and `visibilitychange`; this is harmless but not
  perfectly tidy.
- Revisit hard timeout/isolation around `statfs` if real deployments show local
  or not-yet-classified filesystems blocking. Network filesystems are skipped in
  v1, which removes the main practical hang risk, but `statfs` still runs
  synchronously for accepted local filesystems.

## Stacklab Logs Panel

Confirmed placement:

- second section on the same `/host` page
- stacked vertically below the overview cards

Required capabilities:

- refresh
- follow mode via polling
- severity filter
- text filter
- HTTP access-log toggle; routine `msg="http request"` entries are hidden by
  default because dashboard polling and asset requests otherwise drown out
  actionable Stacklab events

Nice to have later:

- copy selected lines
- download current window

## UI States

Need to define:

- loading state for host overview
- loading state for host metrics
- loading state for logs
- empty logs state
- empty filesystem/network metric states
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
3. host resource presentation uses compact dashboard-style cards plus focused filesystem/interface lists, not a dense operator table
4. `/api/host/metrics` is polled only while the `/host` page is mounted
5. metrics use server-side adaptive sampling: `30s` idle, `1s` active

## Expected Backend/UI Sequence

1. backend implements `GET /api/host/overview`
2. backend implements polling-based `GET /api/host/stacklab-logs`
3. backend implements `GET /api/host/metrics`
4. UI renders overview cards, native metrics dashboard, and log viewer on `/host`
5. if log polling proves insufficient, revisit streaming later
