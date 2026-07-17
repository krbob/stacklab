# Dashboard Read Model, Structured Progress, and Activity Push

This is the delivery and architecture record for the API slices introduced for
the "Amber Console v2" design and richer operation progress. Most slices are
now implemented; the status section records the remaining follow-ups.

This document explains intent and implementation decisions. Exact REST shapes
belong to [openapi.yaml](openapi.yaml), and exact socket frames belong to
[websocket-protocol.md](websocket-protocol.md). The changes described here were
designed to be additive: existing fields did not change shape or meaning.

## Slice A — `GET /api/stacks` dashboard extensions

The stacks tile grid needs three per-item additions. Existing fields
(`runtime_state`, `config_state`, `health_summary`, `service_count`,
`last_action`) are already sufficient for status edges, drift and health
badges — the frontend simply does not consume them yet.

### A1. Live resource snapshot

```json
"stats": {
  "cpu_percent": 2.1,
  "memory_bytes": 1288490188,
  "sampled_at": "2026-07-04T14:31:50Z"
}
```

- `null` when the stack has no running containers or the sample is stale (>30s).
- Source: a single host-level collector goroutine sampling the Docker stats API
  for all running containers on a fixed 10s interval, aggregated per Compose
  project label and cached in memory. List requests never call Docker directly.
- Not persisted. No history (stats history stays frontend-only per the
  retention decision).

### A2. Stack metadata (icon + links)

```json
"metadata": {
  "icon": "jellyfin",
  "links": [
    { "label": "Web UI", "url": "https://jellyfin.bobinski.net" }
  ]
}
```

- Sourced from an `x-stacklab` extension block in `compose.yaml` — Compose-first,
  transparent, git-versioned, no side-channel files:

```yaml
x-stacklab:
  icon: jellyfin            # simple-icons slug or built-in fallback glyph
  links:
    - label: Web UI
      url: https://jellyfin.bobinski.net
```

- Parsed during the existing compose read; invalid blocks degrade to `null`
  metadata plus a validation warning (never an error).
- `icon` renders from a bundled subset of simple-icons (no network fetch;
  LAN-only constraint). Unknown slug → monogram fallback.

### A3. Update availability rollup (depends on Slice B)

```json
"updates": {
  "state": "available",        // available | up_to_date | unknown
  "services_with_updates": 2,
  "checked_at": "2026-07-04T06:02:11Z"
}
```

## Slice B — image update checks

Promotes the backlog item "image update checks that can notify when a registry
tag resolves to a new digest".

- New job action `check_image_updates` (workflow, host-scoped): for each unique
  `image_ref` used by managed stacks, resolve the remote manifest digest through
  an anonymous registry HEAD request and compare it with the local digest.
  Requests require HTTPS. Public registry and token endpoints are resolved and
  checked again on every redirect and dial. A private endpoint is allowed only
  when its exact `host:port` appears in `image_ref`; its token challenge cannot
  widen that access to another private host or port. Registries requiring
  credentials or a separate private auth endpoint report `unknown`.
- Persistence: SQLite table `image_update_status`
  (`image_ref`, `local_digest`, `remote_digest`, `state`, `checked_at`),
  bounded by existing retention machinery.
- Read model: `GET /api/maintenance/image-updates` → list with per-stack
  attribution (reuses inventory's image→stack mapping); feeds the A3 rollup.
- Schedulable via the existing maintenance schedules model (new workflow kind),
  default off.
- Notification event `image_updates_available` on transition
  `up_to_date → available` (existing channels; respects per-event toggles).
- Non-goals: no auto-update trigger from the check itself; build-mode services
  report `unknown`.

## Slice C — structured progress on job events

Today `job_progress` / `job_log` carry only `message` + raw `data`, which
forces the UI into log-dump step cards. Additive `progress` object on job
events and on step records:

```json
"progress": {
  "phase": "pull",             // pull | build | create | start | stop | remove | prune
  "completed": 7,
  "total": 12,
  "unit": "layers",            // layers | services | steps | bytes
  "detail": "0d6922a6b13e extracting 45.2MB / 120MB"
}
```

- Sources: `docker compose --progress=json` events for compose-driven actions;
  Docker API pull progress for direct pulls. The runner translates provider
  events into the neutral shape above — the WS contract never leaks
  compose-internal formats.
- Workflow rollup already exists (`step.index/total`); step records additionally
  carry their latest `progress` snapshot so late subscribers and the job detail
  drawer can render bars without replaying the full event stream.
- Raw output lines keep flowing as `job_log` (collapsed by default in UI);
  `progress` is for bars/counters, not a replacement for logs.
- Event rate: runner throttles `job_progress` emissions to max 2/s per step;
  terminal transitions always emitted.

## Slice D — activity push over WebSocket

Upgrades polled `GET /api/jobs/active` (kept as reconnect fallback) with a
logical stream on the existing multiplexed socket, consistent with
logs/stats/jobs streams:

- `activity.subscribe` → server acknowledges the subscription and replies with
  `activity.snapshot` (the full payload, with the same shape as the REST
  response), followed by `activity.update` messages on:
  - job lifecycle transitions (queued/running/step change/terminal),
  - coalesced progress and job events (at most one full update every 500 ms,
    latest state wins).
- Every `activity.update` carries a full `ActiveJobsResponse`, not a delta or a
  single `ActiveJobItem`. A terminal job disappears from `items`, and the
  refreshed summary reflects its removal.
- Server-side: the jobs service already centralizes state transitions; the
  publisher hooks there. No new persistence.
- UI consumers: host-strip live chip ("1 job · pull traefik 30/40"),
  background-job tray, per-stack activity badges on tiles.

## Slice E — editor support

- `POST /api/stacks/{stackId}/resolved-config` resolves a draft definition
  without saving it and returns
  `warnings: [{ code, message, service, line }]`. The initial lint rules cover
  a missing Compose-declared healthcheck, a missing restart policy, and
  `0.0.0.0` port binds. An image may still provide the effective runtime
  healthcheck.
  Warnings never block save or deploy.
- `GET /api/stacks/{stackId}/resolved-config?source=last_valid` resolves the persisted deploy
  baseline — powers the "diff vs last known good" editor view and completes
  drift detection surfacing.

## Slice F — stack templates (create flow)

Near-term roadmap goal 1, needed for the redesigned create-stack screen:

- `GET /api/templates` → local curated templates from
  `<root>/templates/<id>/` (`compose.yaml` + `template.yaml` with
  name/description/icon/variables).
- `POST /api/stacks` accepts optional `template_id` + `variables` for
  server-side substitution, plain `${VAR}` only, no remote catalogs.

## Current implementation status

- Slices A1/A2/C/D: implemented in the repository. Global activity uses one
  shared WebSocket subscription in the frontend and a 3-second REST polling
  fallback when push is unavailable or unsupported.
- Slice B: implemented — `POST /api/maintenance/image-updates/check` runs the
  `check_image_updates` job (anonymous registry digest checks; images needing
  credentials report `unknown`), `GET /api/maintenance/image-updates` lists
  per-image status, `updates` rollup ships in `GET /api/stacks`. Follow-ups:
  schedule integration and the `image_updates_available` notification.
- Slice C addendum: on compose without `--progress json` (< ~2.30) the runner
  falls back to `--progress plain` with heuristic layer/container parsing.
- Slice E: lint warnings implemented in resolved-config responses
  (`missing_healthcheck`, `missing_restart_policy`, `public_port_bind`);
  `source=last_valid` implemented from the persisted deploy baseline.
- Slice F: `GET /api/templates` implemented — operator templates from
  `<root>/templates/<id>/` (compose.yaml + template.yaml) with six built-in
  starters as fallback; create-stack supports template variables and server-side
  rendering via `template_id` + `variables`.

Each REST slice is represented in [openapi.yaml](openapi.yaml). Frontend REST
types are generated into `frontend/src/lib/api-contract.generated.ts`; the
application imports its stable aliases from `frontend/src/lib/api-types.ts`.
Run `npm --prefix frontend run generate:api` after a REST contract change.
WebSocket frame types remain hand-maintained against
[websocket-protocol.md](websocket-protocol.md).
