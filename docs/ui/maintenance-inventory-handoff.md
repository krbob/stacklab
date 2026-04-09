# Maintenance Inventory Handoff

This handoff covers the first UI slice of Milestone 7:

- image inventory
- prune preview
- prune execution

Backend contract draft:

- `docs/api/maintenance-inventory.md`

## Product Intent

This is not a Portainer-style Docker inventory.

The UI should help operators answer:

- what images are on the host
- which ones are relevant to managed stacks
- what cleanup would remove
- how to run cleanup deliberately

The UI should avoid feeling like:

- generic Docker CRUD
- low-level object administration

## Route Recommendation

Keep this inside:

- `/maintenance`

Do not create separate global routes for:

- `/images`
- `/volumes`
- `/networks`

Reason:

- maintenance remains one operator workflow area
- image inventory and cleanup are tightly related to update and prune actions

## Recommended IA

Recommended top-level maintenance navigation inside `/maintenance`:

- `Update`
- `Images`
- `Networks`
- `Volumes`
- `Cleanup`

Recommended first implementation:

- tabs or segmented control at the top of the page
- keep current `Update` screen intact
- add `Images`, `Networks`, `Volumes`, and `Cleanup` without moving them into the sidebar
- later safe actions should stay inside `Networks` and `Volumes`, not become separate pages

## `Images` View

Recommended content:

- inventory table or compact list
- filters:
  - search
  - all / used / unused
  - stack-managed / external
- columns/fields:
  - image reference
  - size
  - created date
  - usage count
  - stacks using it
  - unused / dangling indicators

Recommended visual priorities:

- make `unused` easy to spot
- make stack relationships obvious
- keep row actions minimal in v1

Not recommended in the first version:

- destructive image actions from the table
- registry pulls from the table
- complicated nested object views

## `Networks` View

Recommended content:

- searchable list or table
- same filters as `Images`:
  - search
  - all / used / unused
  - stack-managed / external
- columns/fields:
  - name
  - driver
  - scope
  - attached container count
  - stacks using it
  - internal / ingress indicators when true

Recommended visual priorities:

- make external-but-used networks easy to spot
- keep `stacks_using` obvious
- do not add network create/delete actions in this slice

### Safe External Network Actions

Follow-up scope:

- create external network by name
- remove unused external network

Important UX constraints:

- built-in networks are never removable
- stack-managed or in-use networks must look blocked, not merely disabled without explanation
- create should stay intentionally narrow:
  - name only in v1
  - no advanced driver/options panel yet

## `Volumes` View

Recommended content:

- searchable list or table
- same filters as `Images`:
  - search
  - all / used / unused
  - stack-managed / external
- columns/fields:
  - name
  - driver
  - mountpoint
  - attached container count
  - stacks using it
  - options count

Recommended visual priorities:

- make unused volumes easy to spot without implying deletion is safe by default
- keep external-but-used volumes obvious
- do not add volume create/delete actions in this slice

### Safe External Volume Actions

Follow-up scope:

- create external named volume by name
- remove unused external named volume

Important UX constraints:

- stack-managed or in-use volumes must look blocked, not merely disabled without explanation
- create should stay intentionally narrow:
  - name only in v1
  - no driver/options editor yet

## `Cleanup` View

Recommended content:

- explicit cleanup scope toggles:
  - images
  - build cache
  - stopped containers
  - volumes
- preview summary before execution
- strong warning presentation for `volumes`
- one primary `Run cleanup` action

Recommended flow:

1. operator chooses scope
2. UI fetches prune preview
3. UI shows estimated impact
4. operator confirms
5. UI opens job progress panel / step list

## Progress UX

Cleanup execution should reuse the mental model from the existing maintenance update workflow:

- one global job
- chronological step list
- raw output/result panel

Recommended step labels:

- `Prune images`
- `Prune build cache`
- `Prune stopped containers`
- `Prune volumes`

## Important UX Constraints

- `volumes` must look meaningfully riskier than the other toggles
- preview should be visible before execution whenever backend data is available
- if preview is coarse or partially unavailable, the UI should say so explicitly
- image inventory and cleanup should feel maintenance-oriented, not exploratory-for-its-own-sake

## Open Design Questions For UI

These should be answered intentionally once the backend model is implemented:

1. Should `Images` use:
   - dense table
   - card list
   - or hybrid list with expandable stack usage?
2. Should `Cleanup` show:
   - inline preview cards
   - or a confirmation dialog after preview?
3. Should cleanup result history stay only in the job panel,
   - or also get a compact "last cleanup" summary on the page?

## Recommended First Version

To keep scope tight:

- `Images` = searchable table/list
- `Networks` = searchable list with stack usage
- `Volumes` = searchable list with stack usage
- `Cleanup` = toggle form + preview summary + confirm button
- no image row actions yet
- no historical cleanup summary yet

That is enough to replace manual shell cleanup habits without overbuilding the maintenance area.
