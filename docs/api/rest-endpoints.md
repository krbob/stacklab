# REST API Conventions

## Purpose and authority

This document is the maintained companion to the machine-readable REST
contract. It records cross-cutting behavior and product semantics that are
awkward or impossible to express completely in OpenAPI.

[openapi.yaml](openapi.yaml) is the canonical source for:

- paths, methods, parameters, and operation IDs;
- authentication requirements and public-operation overrides;
- request and response bodies;
- status codes, error responses, enums, and validation constraints.

Do not add a second endpoint catalog or copy schemas into this file. Focused
documents linked from the [API index](README.md) may explain intent, safety
boundaries, or UI behavior, but they do not override OpenAPI. Executable code
and tests establish whether the implementation satisfies the published
contract.

## Transport conventions

### Base path and media type

REST operations are served below `/api`. JSON requests and responses use
`application/json` unless an operation in OpenAPI explicitly says otherwise.
The WebSocket upgrade at `/api/ws` is documented separately in
[websocket-protocol.md](websocket-protocol.md).

### Time and identifiers

- Timestamps use RFC 3339 in UTC, for example `2026-04-03T18:42:00Z`.
- Clients must treat job IDs, cursors, and request IDs as opaque values.
- Stack IDs are operator-visible stable identifiers; their accepted syntax is
  defined by the relevant OpenAPI parameter and schema.
- A missing optional field and a field explicitly allowing `null` are not
  interchangeable. Clients should follow the generated type.

### Request correlation

Every HTTP response includes `X-Request-ID`. A client or reverse proxy may send
the same header when its value matches the safe format in OpenAPI; otherwise
Stacklab creates a new identifier.

The identifier is included in structured request logs and persisted on jobs
started by the request. The UI should include it in actionable API errors so an
operator can correlate a failure with the service journal.

```bash
journalctl -u stacklab --output=json | jq 'select(.request_id == "req_...")'
```

## Authentication and session semantics

OpenAPI applies the `sessionCookie` security scheme globally, transported in
the `stacklab_session` cookie by default. Operations with `security: []` are
public exceptions.

The cookie is opaque and HTTP-only. Session behavior is deliberately stricter
than cookie presence:

- successful authenticated responses refresh `Set-Cookie`, capped by the
  immutable absolute session deadline;
- REST activity extends the logical idle lease, while SQLite persists activity
  at a bounded frequency;
- REST and WebSocket activity never extend the absolute lifetime;
- missing, revoked, password-version-invalid, idle-expired, and
  absolute-expired sessions return `401` and clear the cookie;
- authentication-storage failures return `500 internal_error` and do not clear
  an otherwise unknown cookie;
- changing the password revokes existing sessions according to the settings
  operation's documented policy.

Clients must accept refreshed cookies and treat `401` as loss of the current
session, not as an ordinary retryable request failure.

## Errors and bounded input

Non-success responses use the shared `ErrorResponse` schema from OpenAPI. The
stable application-facing fields are:

```json
{
  "error": {
    "code": "stack_locked",
    "message": "A mutating job is already running for this stack.",
    "details": {
      "stack_id": "nextcloud",
      "job_id": "job_01hr..."
    }
  }
}
```

- `code` is the programmatic discriminator.
- `message` is safe operator-facing context, not a stable value for control
  flow.
- `details` is optional and code-specific.
- `X-Request-ID` remains available on errors for correlation.

The exact codes and status codes accepted by an operation belong in OpenAPI.
Clients should preserve unknown codes as a generic error so additive server
changes do not break the UI.

Inputs and generated command output are bounded before untrusted data can be
accumulated indefinitely. A `413 request_too_large` response applies to an
oversized HTTP body; `413 content_too_large` applies to a bounded domain field
or generated output. The response includes the applicable limit in
`error.details.max_bytes` where available. Callers must not automatically retry
the same oversized payload.

## Long-running operations and jobs

Operational mutations commonly return a job envelope. Acceptance of such a
request is not confirmation that the operation succeeded:

1. read the returned job ID;
2. observe live progress through the multiplexed WebSocket job stream;
3. recover or reconcile through the job REST read models;
4. use the terminal job state as the outcome.

Job history is durable within the configured retention window. Live WebSocket
events are ephemeral and must not be the sole source of truth after reconnect.
See [job-detail.md](job-detail.md),
[global-activity.md](global-activity.md), and
[websocket-protocol.md](websocket-protocol.md).

Cancellation is best effort. `cancel_requested` remains active until the
runner reaches a cancellation boundary and records a terminal state. A request
may race with normal completion; clients must render the state returned by the
subsequent job snapshot instead of assuming `cancelled`.

The domain-level operation lifecycle and retention rules live in
[operation-model.md](../domain/operation-model.md) and
[retention-and-audit.md](../data/retention-and-audit.md).

## Capability and concurrency semantics

Read models expose capabilities and state so the UI can disable impossible
actions before submission. The backend remains authoritative and repeats these
checks at execution time.

### Locked resources

Mutating jobs acquire typed resource locks. Conflicting work returns a
conflict response, normally `stack_locked` or `invalid_state`, with the
blocking job identified when available. Safe reads and live streams remain
available while a resource is locked.

Global, Docker daemon, registry, self-update, image-update, and stack-scoped
operations may overlap only when their declared resources do not conflict.
Clients must not infer concurrency solely from stack IDs.

### Orphaned stacks

An orphaned stack has runtime resources but no valid managed definition.
Detail, history, logs, stats, terminal access, and explicit cleanup can remain
available. Definition-dependent actions such as deploy, build, pull, or
recreate are unavailable. The response's capability fields are authoritative
for the current stack.

### Invalid configuration

Validation and editing remain available when configuration is invalid.
Deploy-oriented actions may be rejected until a valid definition is restored.
Saving a draft and successfully applying it are separate outcomes.

The full stack state model is described in
[stack-model.md](../domain/stack-model.md).

## Readiness and partial availability

Liveness answers whether the HTTP process can serve a request. Readiness
checks required dependencies and may return a non-success status with bounded,
per-component diagnostics. A deprecated compatibility endpoint, if present,
must remain an alias of the canonical readiness operation until it is removed
through an explicit compatibility change.

Feature-specific read models should prefer explicit unavailable or degraded
states over hiding an entire page when one optional host capability is absent.
Focused capability rules are documented in the relevant API document.

## Compatibility rules

Stacklab currently evolves the API without a version segment in the path.
Changes should therefore be additive whenever practical:

- add optional response fields rather than changing existing meanings;
- introduce new enum values only when clients preserve unknown values safely;
- do not reuse an error code for a different condition;
- deprecate an operation in OpenAPI before removing it;
- keep REST fallback reads when a live WebSocket stream is the primary UI path;
- update implementation, tests, OpenAPI, and generated consumers in the same
  change.

Breaking changes require an explicit migration plan rather than an unnoticed
edit to a prose example.

## Frontend contract generation

Frontend REST types are generated from [openapi.yaml](openapi.yaml) into
`frontend/src/lib/api-contract.generated.ts` with `openapi-typescript`.
`frontend/src/lib/api-types.ts` is the stable application-facing facade over
generated components and operations. WebSocket frame types remain separate
because the WebSocket protocol is not described by the REST OpenAPI document.

After changing the REST contract, run from the repository root:

```bash
npm --prefix frontend run generate:api
make frontend-api-contract
```

Commit the generated file with the OpenAPI change. Never edit the generated
file by hand. The Make target regenerates it and fails when the tracked output
has drifted.

## Contract change checklist

1. Change the backend behavior and its tests.
2. Update `openapi.yaml` for every affected REST operation and schema.
3. Regenerate the frontend contract and adapt the typed facade or client.
4. Update a focused document only when product semantics, safety boundaries,
   or operational guidance changed.
5. Run `make frontend-api-contract` and the relevant backend contract tests.
6. Check links from the [API index](README.md) and avoid adding another route
   inventory in prose.
