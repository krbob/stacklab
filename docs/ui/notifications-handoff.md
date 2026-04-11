# Notifications Handoff

Route:

- stays inside `/settings`
- do not create a standalone notifications page in v1

Surface:

- a dedicated `Notifications` section below password and above about/version info

V1 scope:

- one webhook URL
- three event toggles:
  - failed jobs
  - succeeded with warnings
  - maintenance succeeded
- `Save`
- `Send test`

Expected API:

- `GET /api/settings/notifications`
- `PUT /api/settings/notifications`
- `POST /api/settings/notifications/test`

Required states:

- loading existing settings
- dirty form
- save success
- save validation error
- test success
- test delivery failure

Recommended form shape:

- master `Enable notifications` toggle
- webhook URL input
- checkbox list for event toggles
- secondary `Send test` button
- primary `Save` button

UX rules:

- `Send test` should use the current draft form values, not only the last saved settings
- `Send test` should be allowed even if notifications are not yet enabled, as long as the URL is valid
- save and test feedback should be inline, not toast-only
- do not add advanced headers, auth tokens, or templating in v1

Error mapping:

- `400 validation_failed` -> inline form error
- `502 delivery_failed` -> inline delivery error near the test action
- `401 unauthorized` -> existing auth handling

Copy guidance:

- make it clear that notifications are outgoing webhooks
- make it clear that v1 is best-effort and does not retry
- do not promise instant delivery guarantees
