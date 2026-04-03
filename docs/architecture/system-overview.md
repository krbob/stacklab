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
