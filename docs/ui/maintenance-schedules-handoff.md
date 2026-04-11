# Maintenance Schedules Handoff

Related API contract:

- `docs/api/maintenance-schedules.md`

Goal:

- expose explicit opt-in scheduled maintenance without making operators leave Stacklab for cron or systemd timers

Scope in this slice:

- exactly two policies:
  - scheduled update
  - scheduled prune
- host local time only
- cadence:
  - daily
  - weekly

Not in scope:

- arbitrary cron syntax
- multiple schedules per action
- retries after skipped/conflicting runs
- per-schedule notification routing

Backend shape:

- `GET /api/settings/maintenance-schedules`
- `PUT /api/settings/maintenance-schedules`

Runtime status already provided by backend:

- `next_run_at`
- `last_triggered_at`
- `last_scheduled_for`
- `last_result`
- `last_message`
- `last_job_id`

Important product semantics:

- schedules reuse the same workflows as manual `Update` and `Cleanup`
- scheduled jobs show up in:
  - global activity
  - job detail drawer
  - audit
  - notifications
- conflicts do not auto-retry in v1:
  - they surface as `skipped`

Open UI decisions needed:

1. placement:
   - `/settings` section
   - or `/maintenance` tab
2. layout:
   - two fixed cards (`Update schedule`, `Cleanup schedule`)
   - or a more policy-list-like treatment
3. cadence control:
   - simple segmented `Daily / Weekly`
   - plus weekday chips for weekly

Strong recommendation:

- use two fixed cards
- use segmented `Daily / Weekly`
- surface "Runs in host local time" as plain copy, not as a timezone selector

Why:

- the backend model is intentionally narrow
- a generic scheduler UI would over-signal flexibility we do not yet support

Minimum useful UX:

- enable toggle per card
- time input
- daily/weekly selector
- weekday picker for weekly
- update target:
  - all stacks
  - selected stacks
- scheduled prune scope toggles
- status footer:
  - next run
  - last run result
  - link to job detail if `last_job_id` exists
