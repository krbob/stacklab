# API Documentation

This directory documents Stacklab's REST and WebSocket interfaces. Use the
most specific authoritative source below instead of copying a contract between
documents.

## Contract hierarchy

1. [openapi.yaml](openapi.yaml) is the canonical published REST contract for
   paths, methods, parameters, security, status codes, and schemas.
2. [websocket-protocol.md](websocket-protocol.md) is the canonical WebSocket
   transport and frame contract.
3. Focused documents record product intent, safety boundaries, operational
   semantics, and implementation decisions that do not fit cleanly in the
   machine-readable contracts.
4. Code and executable tests verify that the implementation conforms to those
   published contracts. When they disagree, treat it as contract drift and fix
   the implementation or the canonical contract in the same change.

[rest-endpoints.md](rest-endpoints.md) deliberately does not enumerate every
REST operation. It is the maintained guide to cross-cutting conventions.

## Frontend types

REST types are generated from `openapi.yaml`:

```bash
npm --prefix frontend run generate:api
make frontend-api-contract
```

The generated output is
`frontend/src/lib/api-contract.generated.ts`; do not edit it manually.
`frontend/src/lib/api-types.ts` is the stable application-facing facade.
WebSocket types remain separate because the socket protocol is outside the
REST OpenAPI document.

## Complete index

| Document | Role | Status | Source of truth |
| --- | --- | --- | --- |
| [README.md](README.md) | Directory map and contract ownership rules | Current | This index for document routing |
| [openapi.yaml](openapi.yaml) | Machine-readable REST operations and schemas | Canonical, current | This file for the published REST contract |
| [rest-endpoints.md](rest-endpoints.md) | Cross-cutting REST conventions, compatibility, jobs, and capability semantics | Current companion | OpenAPI for operation details; this document for shared semantics |
| [websocket-protocol.md](websocket-protocol.md) | Multiplexed socket lifecycle, commands, events, limits, and recovery | Canonical, current | This file for WebSocket frames and transport behavior |
| [config-workspace.md](config-workspace.md) | Managed config browsing, editing, and permission-repair boundaries | Implemented focused contract | OpenAPI for REST shapes; this document for workspace and safety semantics |
| [dashboard-read-model-and-progress.md](dashboard-read-model-and-progress.md) | Delivery record for dashboard metadata, stats, updates, progress, activity, editor lint, and templates | Implemented design record with noted follow-ups | OpenAPI and WebSocket protocol for wire contracts; this document for rationale and status |
| [docker-admin.md](docker-admin.md) | Docker daemon overview, configuration validation, and helper-backed apply behavior | Implemented focused contract | OpenAPI for REST shapes; this document for capability and helper semantics |
| [docker-registry-auth.md](docker-registry-auth.md) | Registry login/logout and secret-handling boundaries | Implemented focused contract | OpenAPI for REST shapes; this document for credential and source-of-truth rules |
| [git-workspace.md](git-workspace.md) | Managed Git status, diff, commit, and push workflows | Implemented focused contract | OpenAPI for REST shapes; this document for repository boundary and workflow semantics |
| [global-activity.md](global-activity.md) | Cross-application active-job snapshot, push updates, and fallback behavior | Implemented, current | OpenAPI for the REST payload; WebSocket protocol for frames; this document for consumer semantics |
| [host-observability.md](host-observability.md) | Host overview, metrics, and bounded Stacklab service logs | Implemented focused contract | OpenAPI for REST shapes; this document for platform and degradation semantics |
| [job-detail.md](job-detail.md) | Durable job snapshot and retained event replay | Implemented focused contract | OpenAPI for REST shapes; this document for retention-aware UI semantics |
| [maintenance-inventory.md](maintenance-inventory.md) | Images, networks, volumes, prune preview, and cleanup safety | Implemented focused contract | OpenAPI for REST shapes; this document for inventory and destructive-action semantics |
| [maintenance-schedules.md](maintenance-schedules.md) | Opt-in update and prune schedules | Implemented focused contract | OpenAPI for REST shapes; this document for scheduling and collision semantics |
| [maintenance-workflows.md](maintenance-workflows.md) | Bulk stack update workflow and structured step behavior | Implemented focused contract | OpenAPI for REST shapes; this document for workflow semantics |
| [notifications.md](notifications.md) | Notification settings, channel tests, and delivery events | Implemented focused contract | OpenAPI for REST shapes; this document for delivery and secret-handling semantics |
| [self-update.md](self-update.md) | APT installation overview and helper-backed Stacklab update flow | Implemented focused contract | OpenAPI for REST shapes; this document for install-mode and recovery semantics |
| [service-metrics.md](service-metrics.md) | Bounded process-local HTTP, job, WebSocket, and readiness metrics | Implemented focused contract | OpenAPI for REST shape; this document for metric definitions and cardinality limits |
| [stack-workspace.md](stack-workspace.md) | Auxiliary per-stack file browsing, editing, and repair boundaries | Implemented focused contract | OpenAPI for REST shapes; this document for filesystem and safety semantics |

## Maintenance rule

New REST work belongs in `openapi.yaml` first. Add or update a focused document
only when the feature needs durable rationale, safety policy, degradation
behavior, or UI guidance. A route list, copied response model, or copied error
table is not a reason to create another document.
