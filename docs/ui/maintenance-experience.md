# Maintenance Experience

The `/maintenance` workspace groups update, inventory, and cleanup workflows
without eagerly loading every Docker inventory endpoint.

## Tab lifecycle

- Update is the initial mounted tab.
- Images, Networks, Volumes, and Cleanup mount on first activation.
- A visited tab remains mounted while the user switches views so filters and a
  running cleanup job are not lost.
- Arrow keys, Home, and End move between tabs using the same activation path as
  pointer input.

## Inventory search

Image, network, and volume search fields update immediately on screen, while
API queries receive the latest value after a 250ms debounce. Usage and origin
filters remain immediate. This avoids a Docker inventory request for every
keystroke without making the input feel delayed.

## Update workspace

Selected-stack rows show a visible runtime-state label and running/defined
service count in addition to the status dot. The idle progress panel summarizes
the current target scope and enabled workflow steps, so it acts as a review
rather than an empty placeholder.

The five most recent durable `update_stacks` and `prune` audit records appear
below the current workflow. Records with a job ID open the shared job-detail
drawer. Unrelated audit actions are not shown.
