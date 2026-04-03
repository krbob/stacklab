# Filesystem Layout

## Canonical Root

Stacklab assumes the following canonical root:

- `/opt/stacklab`

Alternative roots may be supported later through configuration, but v1 documentation assumes this layout.

## Canonical Directories

### Application

- `/opt/stacklab/app/`

Rules:

- contains Stacklab source code, deployment assets, or both
- is separate from managed stack definitions

### Stack Definitions

- `/opt/stacklab/stacks/<stack>/compose.yaml`
- `/opt/stacklab/stacks/<stack>/.env`

Rules:

- each stack gets its own directory
- `compose.yaml` is required for a valid stack
- `.env` is optional but supported

### Stack Configuration

- `/opt/stacklab/config/<stack>/`

Rules:

- intended for versioned configuration mounted into services
- may contain service-specific subdirectories

### Stack Data

- `/opt/stacklab/data/<stack>/`

Rules:

- intended for durable runtime data
- excluded from Git by default
- never deleted automatically by default stack removal flows

### Stacklab Runtime

- `/var/lib/stacklab/`

Contains:

- SQLite database
- temporary runtime files
- cache
- lock artifacts if file-backed locks are used

## Stack Naming Rules

- stack directory name is the canonical stack identifier
- names should be lowercase ASCII with dashes
- names must be filesystem-safe and URL-safe

## Discovery Rules

A directory under `/opt/stacklab/stacks/` is considered a Stacklab stack when:

- it is a directory
- it contains `compose.yaml`

Optional support later:

- `compose.yml`
- multiple Compose files
- profile-aware layouts

## Safety Rules

- Stacklab edits files only inside the configured Stacklab root
- destructive actions must be explicit and scoped
- data directories are opt-out for deletion, not opt-in
