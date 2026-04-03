# ADR 0002: Compose-First Domain Model

## Status

Accepted

## Context

The product goal is not to manage arbitrary Docker resources. It is to manage Compose stacks on one host while preserving manual CLI compatibility.

There are two competing models:

- Docker-first: list containers first and group them later
- Compose-first: list filesystem-defined stacks first and map runtime state onto them

## Decision

Stacklab will be modeled as a Compose-first system.

The primary source of truth is the filesystem under `/opt/homelab/stacks`. Runtime state from Docker augments the model but does not replace it.

## Rationale

- matches operator mental model
- preserves compatibility with manual `docker compose` workflows
- keeps stack definitions human-readable and Git-friendly
- prevents the database from becoming an opaque source of truth
- makes Stacklab meaningfully different from generic Docker dashboards

## Consequences

### Positive

- stable relationship between stack directories and UI objects
- easy recovery if the application database is lost
- simpler reasoning about config editing and deployment

### Negative

- drift between filesystem and runtime must be detected and presented clearly
- single containers outside Compose are explicitly unsupported
- stack discovery depends on directory conventions remaining stable

## Follow-Up

- define canonical stack directory layout
- define stack discovery rules
- define drift states between filesystem and runtime

