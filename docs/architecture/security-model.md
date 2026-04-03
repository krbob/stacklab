# Security Model

## Purpose

This document defines the v1 security model for Stacklab.

It focuses on realistic controls for a single-host, LAN-only, single-operator system without pretending the risk is low just because the system is not internet-exposed.

## Threat Model

Stacklab must assume the following risks are real:

- an authenticated browser session may be left open on a trusted LAN machine
- `docker.sock` access is effectively privileged host access
- web terminals can amplify XSS and output-injection risks
- filesystem writes can damage or delete stack definitions and data
- reverse proxy exposure on LAN may still allow lateral movement from another device

Stacklab v1 does **not** try to solve:

- hostile multi-tenant access
- public internet exposure as a primary model
- fine-grained RBAC

## Security Principles

- default to host-local trust boundaries, but do not skip authentication
- keep the backend host-native and avoid a privileged management container
- treat terminal access as privileged even in MVP
- prefer explicit allowlists over generic shell execution
- keep filesystem definitions as the source of truth
- avoid logging secrets or raw sensitive file contents

## Authentication

## User Model

v1 supports a single local operator account.

The UI may display a friendly label such as `Local Operator`, but there is no multi-user identity model in v1.

## Password Storage

The application password must be stored as a password hash, not plaintext.

Recommended algorithm:

- `argon2id`

Stored parameters should be explicit so they can be migrated later.

## Session Model

Authenticated browser access uses a signed server-side session represented by a cookie.

Session cookie properties:

- `HttpOnly`
- `SameSite=Strict`
- `Path=/`
- `Secure` when served over HTTPS

Recommended v1 session limits:

- app session idle timeout: `12h`
- app session absolute lifetime: `7d`

Rules:

- session expiry should force re-login
- terminal session lifetime is separate from app session lifetime

## Authorization

Authorization is coarse-grained in v1:

- authenticated user may use all supported features
- feature flags may disable selected capabilities globally

Current feature flags:

- `host_shell`

## Transport And Origin Rules

## REST

Rules:

- CORS should be disabled by default
- mutating requests must require authentication
- backend should reject suspicious cross-origin requests using `Origin` and `Host` checks where available
- requests must use JSON for mutating endpoints unless explicitly documented otherwise

## WebSocket

Rules:

- WebSocket upgrade requires an authenticated session
- backend must validate `Origin`
- backend must reject unauthorized upgrades before establishing the connection
- server heartbeat must close dead connections rather than leaving them half-open indefinitely

## TLS And Reverse Proxying

v1 may operate over plain HTTP on a trusted LAN, but HTTPS is strongly recommended when accessed through a reverse proxy and hostname such as `stacklab.example.lan`.

Recommended deployment posture:

- Stacklab backend listens on a local or private host port
- reverse proxy terminates TLS
- reverse proxy forwards only to Stacklab's internal address

## Terminal Security

## Scope

Container shell is in MVP.

Host shell is **not** in MVP.

## Container Shell Authorization

In v1, container shell does **not** require a separate re-auth prompt beyond the authenticated app session.

Rationale:

- single-user LAN system
- avoids unnecessary friction in MVP
- keeps the terminal model aligned with the rest of the app

Follow-up for post-MVP host shell:

- require fresh re-auth
- require an explicit elevated mode
- require stronger warnings and stricter limits

## Container Shell Limits

Recommended v1 defaults:

- max concurrent container terminal sessions per authenticated user session: `5`
- terminal idle timeout: `30m`
- terminal attach grace period after socket loss: implementation-defined, but should be short and bounded

Rules:

- when the terminal idle timeout is reached, backend terminates the PTY and emits `terminal.exited` with `reason = idle_timeout`
- when the concurrent session limit is reached, backend rejects new terminal opens
- terminal sessions are tied to stack-owned containers only

## Shell Allowlist

The terminal open request may not execute arbitrary commands.

Allowed shell binaries in v1:

- `/bin/sh`
- `/bin/bash` only if present in the container

Rules:

- backend selects only from an allowlist
- frontend may offer a shell selector, but backend remains authoritative

## Terminal Output Handling

Rules:

- terminal output is rendered only through XTerm.js or equivalent terminal emulator
- terminal output must never be injected into the DOM as raw HTML
- ANSI sequences are treated as terminal data, not markup
- scrollback is a client feature, not an audit feature

## Terminal Auditing

Audit the existence of the session, not every command.

Record at least:

- session opened
- session attached
- session closed
- close reason
- target stack and container

Do **not** log:

- every keystroke
- full command history
- raw terminal output by default

## Web Terminal Failure Semantics

Rules:

- failure before session establishment returns a WebSocket `error`
- failure after session establishment returns `terminal.exited`
- reconnect does not guarantee PTY resume

## Command Execution Safety

## Compose Operations

Mutating stack operations must be executed through explicit backend code paths, not through generic shell command construction from user input.

Rules:

- use absolute stack paths
- validate `stack_id` against the canonical regex
- never interpolate untrusted user strings into a shell command line
- prefer direct process execution APIs over shell invocation

## File Operations

Rules:

- all file paths must be resolved under the configured Stacklab root
- reject path traversal and symlink escape attempts
- destructive operations must be explicit and separately flagged
- deleting `/opt/stacklab/data/<stack>` is never the default path

## Docker Access

Because Docker access is privileged in practice:

- Stacklab should run under a service account with only the host permissions it actually needs
- Docker socket access must be treated as privileged access
- backend must never expose arbitrary Docker API pass-through to the browser

## Secret Handling

Secrets may appear in:

- `.env`
- `compose.yaml`
- mounted config files

Rules:

- audit entries must not store full file contents
- job logs should avoid persisting sensitive output beyond what is needed for diagnosis
- resolved config views may reveal interpolated values; access is therefore restricted to authenticated sessions
- future encrypted-secret support remains a separate feature, not part of v1

## UI-Facing Security Rules

Frontend must assume:

- session expiry can happen at any time
- REST and WebSocket auth failures should force a login redirect or re-auth flow
- mutating actions may be rejected due to lock state or invalid state
- terminal session resume is opportunistic, not guaranteed

Required UI behavior:

- disable mutating controls while `activity_state = locked`
- show distinct messages for `idle_timeout`, `server_cleanup`, `process_exit`, and `connection_replaced`
- do not render terminal or logs via raw HTML insertion

## Recommended Security Headers

Recommended defaults when Stacklab serves HTTP directly or behind a proxy:

- `Content-Security-Policy`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: same-origin`
- `X-Frame-Options: DENY` or equivalent CSP `frame-ancestors 'none'`

Suggested CSP posture:

- default-src 'self'
- connect-src 'self'
- img-src 'self' data:
- style-src 'self' 'unsafe-inline' only if needed by the chosen UI stack
- script-src 'self'

## Audit Requirements

Mutating actions must produce audit entries containing:

- action
- stack_id
- requested_by
- requested_at
- result
- duration_ms

Terminal audit should include metadata events:

- opened
- attached
- closed
- reason

## Future Hardening

Post-MVP hardening candidates:

- fresh-auth requirement for host shell
- stronger rate limiting for login attempts
- optional TOTP or client-certificate access on LAN
- encrypted secret references
- stricter per-feature policy toggles

