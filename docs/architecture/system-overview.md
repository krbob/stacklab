# System Overview

## Purpose

This document describes the high-level architecture of Stacklab v1.

## High-Level Components

### Frontend SPA

Responsibilities:

- stack list and stack detail views
- logs, stats, and shell UI
- compose editor and resolved config preview
- user authentication screens

Communication:

- REST for request-response actions and resource fetches
- WebSocket for logs, terminal streams, stats, and job progress

The route-level Settings and Host pages are composition shells. Settings
delegates operator tasks to dedicated host, password, notification,
maintenance, self-update, and about sections. Stateful host and self-update
flows live in focused hooks, while schedule normalization is kept in a tested
model module.

Host observability follows the same boundary: one hook owns overview and
metrics polling, visibility/focus handling, no-overlap guards, and incremental
history merging. Presentational modules own overview cards, the metrics
dashboard and widgets, the process table, and Stacklab logs. This keeps polling
lifecycle independent from rendering while preserving page-local UI state such
as process sorting and public-IP visibility.

Characterization tests cover retry paths, polling cadence and overlap guards,
visibility/focus transitions, update refresh timing, persisted schedule values,
notification configuration, and log filtering/follow controls. Structural
refactors must keep those contracts stable.

### Backend API

Responsibilities:

- authenticate the operator
- expose REST resources and mutating actions
- validate requests and enforce authorization
- coordinate jobs and per-stack locks

### Orchestrator

Responsibilities:

- run `docker compose` commands
- normalize command output into structured job events
- serialize mutating actions per stack

### Docker Runtime Adapter

Responsibilities:

- inspect containers
- fetch stats
- detect runtime state
- support container exec sessions when CLI is not the best source

### Persistence Layer

Backed by SQLite.

Stores:

- application settings
- sessions and auth state
- scheduled jobs
- audit entries
- cached stack metadata where useful

### Filesystem Layer

Primary source of truth for stack definitions and related directories under `/opt/stacklab`.

## Runtime Composition and Ownership

`cmd/stacklab` is the process composition root. Its `application` container
creates one instance of every service and adapter, wires shared instances into
all consumers, and injects a complete `httpapi.Dependencies` value into the HTTP
handler. In particular, the stack reader, host observability service,
maintenance runner, notification service, job service, and SQLite store are not
recreated inside the transport layer.

Service constructors do not start process-level goroutines. `application.Start`
explicitly admits these long-lived workers to one lifecycle manager:

- stack statistics sampling;
- host metrics sampling;
- notification self-health;
- maintenance scheduler;
- self-update reconciliation;
- operational retention.

Detached jobs started by HTTP requests and notifications for terminal job
states use that same lifecycle manager. WebSocket child goroutines remain
connection-scoped and are tracked by the connection/handler that closes them.

Shutdown is ordered by the composition root:

1. stop accepting HTTP requests;
2. cancel the shared runtime and reject new asynchronous work;
3. close and drain WebSocket connections;
4. stop auth session timers and privileged terminal sessions;
5. wait for every admitted process worker;
6. close SQLite only after the runtime has drained successfully.

The HTTP handler owns HTTP routing and hijacked connection state only. It does
not construct services, start background samplers, cancel the shared runtime,
or close injected persistence.

## HTTP Transport Layout

`internal/httpapi.Handler` is the shared transport shell: it stores injected
dependencies, applies middleware, serves the composed router, and owns
WebSocket connection lifecycle. Domain request handling and route registration
are split across seven controllers:

- auth;
- system;
- workspace;
- maintenance;
- stacks;
- operations (jobs and audit);
- settings.

Controllers share the process-owned services through the handler pointer, so a
test or the composition root still injects each dependency once. Transport
mechanisms that own connection state, including health probes, service metrics,
and WebSockets, remain on the handler and are registered by the system
controller.

`routes_contract_test.go` locks all explicit method/path patterns, their
authentication policy, and the API/frontend fallbacks. The split does not
change the REST or WebSocket contract.

## Primary Data Flow

### Read Flow

1. backend scans filesystem-defined stacks
2. backend maps runtime state from Docker
3. backend returns normalized stack view models to the UI

### Mutating Flow

1. UI sends action request
2. backend validates request and acquires stack lock
3. orchestrator executes compose command
4. backend streams progress over WebSocket
5. backend refreshes stack state and writes audit entry

### Live Diagnostic Flow

1. UI opens WebSocket channel
2. backend authenticates channel and initializes stream
3. logs, stats, or terminal bytes are streamed to the browser
4. backend closes the channel on disconnect, timeout, or revocation

## Architectural Rules

- filesystem definitions outrank cached database state
- Docker runtime augments stack state but does not redefine stack identity
- every mutating stack action is represented as a job
- every mutating stack job acquires a per-stack lock
- terminal features are isolated and treated as privileged subsystems
- process services are composed once outside the HTTP transport
- background workers have an explicit start and shutdown owner
