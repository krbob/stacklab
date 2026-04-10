# Notifications V2 Handoff

Scope:

- keep the existing `Notifications` section inside `/settings`
- extend it from webhook-only to multi-channel configuration
- add the first mobile-friendly channel:
  - Telegram
- add one new event toggle:
  - `Post-update recovery failed`

Do not add yet:

- WhatsApp
- email
- notification inbox
- log anomaly rules
- Stacklab self-health alerts from `journald`

Backend state now available:

- current settings API is backward-compatible with v1 webhook clients
- nested `channels` shape is available for newer clients
- supported channels:
  - `webhook`
  - `telegram`
- `POST /api/settings/notifications/test` accepts an optional `channel`
- event toggles now include:
  - `job_failed`
  - `job_succeeded_with_warnings`
  - `maintenance_succeeded`
  - `post_update_recovery_failed`

Telegram model:

- `bot_token`
- `chat_id`
- explicit `Send test` targeting Telegram

Post-update recovery failed:

- this is not a generic runtime alert
- it is specifically for:
  - maintenance update completed
  - one or more target stacks did not recover to a healthy running state

Open UI decisions needed:

1. Channel layout inside `Settings`
- stacked cards per channel
- tabs inside the section
- or one compact list with expandable channel details

2. Secret handling for Telegram bot token
- show raw token in an editable input for the single authenticated operator
- or show masked/empty state and require re-entry on every edit

3. `Send test` affordance
- one button per channel
- or a shared button with a channel picker

4. Event toggle grouping
- whether `maintenance_succeeded` and `post_update_recovery_failed` should sit together under a “Maintenance” subheading

Recommended direction:

- keep the section on the same page
- render channel cards:
  - Webhook
  - Telegram
- keep separate `Send test` actions per channel
- group event toggles into:
  - Jobs
  - Maintenance

Copy guidance:

- `post_update_recovery_failed` should be explained in operator language, not internal job language
- preferred phrasing:
  - “Notify when an update finishes but a stack does not recover”

Follow-up after this slice:

- `stacklab_service_error`
- source:
  - `journalctl -u stacklab`
- intent:
  - notify when Stacklab itself starts logging new `error` / `fatal` entries
- this should be a later checkbox under a separate “Stacklab” or “Self-health” grouping, not mixed silently into the current v2 UI
