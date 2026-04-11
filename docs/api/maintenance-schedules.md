# Maintenance Schedules

Purpose:

- add explicit opt-in scheduled maintenance policies without turning Stacklab into a generic cron UI
- reuse the existing `update_stacks` and `prune` workflows, job model, audit trail, global activity, and notifications

Scope in this milestone:

- one scheduled `update_stacks` policy
- one scheduled `prune` policy
- host-local time only
- cadence:
  - `daily`
  - `weekly`
- background dispatcher inside Stacklab

Non-goals in this milestone:

- arbitrary cron expressions
- multiple schedules per action
- per-schedule notification overrides
- automatic prune bundled silently into update policies
- retry queues for skipped/conflicting schedules

## `GET /api/settings/maintenance-schedules`

Purpose:

- fetch current scheduler policies and their runtime status

Response:

```json
{
  "timezone": "host_local",
  "update": {
    "enabled": false,
    "frequency": "weekly",
    "time": "03:30",
    "weekdays": ["sat"],
    "target": {
      "mode": "all"
    },
    "options": {
      "pull_images": true,
      "build_images": true,
      "remove_orphans": true,
      "prune_after": false,
      "include_volumes": false
    },
    "status": {
      "next_run_at": "2026-04-11T03:30:00Z"
    }
  },
  "prune": {
    "enabled": false,
    "frequency": "weekly",
    "time": "04:30",
    "weekdays": ["sun"],
    "scope": {
      "images": true,
      "build_cache": true,
      "stopped_containers": true,
      "volumes": false
    },
    "status": {
      "next_run_at": "2026-04-12T04:30:00Z"
    }
  }
}
```

`status` semantics:

- `next_run_at`: next planned run based on host local time
- `last_triggered_at`: when Stacklab actually started evaluating this run slot
- `last_scheduled_for`: the slot time that was executed or skipped
- `last_result`:
  - `running`
  - `succeeded`
  - `failed`
  - `skipped`
- `last_message`: short operator-facing explanation for failed or skipped runs
- `last_job_id`: present when a real job was started

## `PUT /api/settings/maintenance-schedules`

Purpose:

- persist scheduler policies in SQLite

Request:

```json
{
  "update": {
    "enabled": true,
    "frequency": "weekly",
    "time": "03:30",
    "weekdays": ["sat"],
    "target": {
      "mode": "selected",
      "stack_ids": ["demo", "traefik"]
    },
    "options": {
      "pull_images": true,
      "build_images": true,
      "remove_orphans": true,
      "prune_after": false,
      "include_volumes": false
    }
  },
  "prune": {
    "enabled": true,
    "frequency": "weekly",
    "time": "04:30",
    "weekdays": ["sun"],
    "scope": {
      "images": true,
      "build_cache": true,
      "stopped_containers": true,
      "volumes": false
    }
  }
}
```

Validation:

- `time` must be `HH:MM`
- `frequency` must be `daily` or `weekly`
- weekly schedules require at least one weekday
- update target mode must be `all` or `selected`
- selected update targets must contain stack IDs
- `include_volumes = true` requires `prune_after = true`
- prune scope must enable at least one category

Behavior:

- schedules run in host local time
- first milestone stores exactly one `update` policy and one `prune` policy
- due runs are deduplicated by their scheduled slot
- if another maintenance job already holds the needed locks:
  - the scheduled run is marked `skipped`
  - no retry loop is started automatically
- scheduled jobs reuse the same:
  - job model
  - audit trail
  - notifications
  - global activity

Audit / job details:

- started jobs are recorded with `requested_by = scheduler`
- job audit details include:
  - `trigger = scheduled`
  - `schedule_key = update|prune`
- skipped runs without a started job are recorded as system audit events

Recommended first UI direction:

- configuration surface can live either:
  - under `/settings` as policy/configuration
  - or in `/maintenance` as a `Schedules` tab
- the data model intentionally supports either placement
