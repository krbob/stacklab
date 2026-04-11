# Feature Strategy

This document aligns Stacklab's functional direction with:

- the current implemented MVP baseline
- the realities of a single-host, Compose-first homelab product
- lessons taken from comparable tools without copying their broader scope blindly

## Product Direction

Stacklab should remain:

- Compose-first
- filesystem-first
- single-host-first
- operator-focused instead of team-platform-first
- conservative with destructive maintenance actions
- explicit about what it owns and what it merely observes

That means Stacklab should prefer:

- safe operational workflows around Compose stacks
- first-class replacements for ad-hoc shell scripts operators use today
- strong visibility into stack, host, and Stacklab health
- selective Docker maintenance features that directly support Compose operations

Stacklab should avoid drifting into:

- a full generic Docker control plane
- a remote multi-host management mesh
- an always-reconciling GitOps controller

## Comparable Products

### Dockge

[Dockge](https://github.com/louislam/dockge) is the closest philosophical match to Stacklab. Its README emphasizes:

- file-based `compose.yaml` ownership
- an interactive Compose editor
- web terminal
- real-time progress for stack operations
- optional multi-agent support
- `docker run` to Compose conversion

The latest release activity on GitHub also shows:

- active maintenance through at least release `1.5.0`
- translation work and multiple-agent support as ongoing themes

Implication for Stacklab:

- Dockge validates the Compose-first, file-based model
- real-time progress, editor, and terminal were the right initial priorities
- multi-agent support is useful in general, but not a near-term fit for Stacklab

### Dockhand

[Dockhand](https://dockhand.pro/) is broader and more operations-heavy. Its public site highlights:

- Git-backed stack deployment with webhook auto-sync
- scheduled deployments and updates
- live metrics, ANSI logs, and activity history
- images, volumes, networks, registry, schedules, and notifications
- vulnerability scanning

Implication for Stacklab:

- the most relevant ideas are around maintenance workflows, notifications, and Git-aware stack management
- the least relevant ideas are the broad "Docker platform" surfaces such as registry management and environment sprawl

### Arcane

[Arcane](https://getarcane.app/) is broader than Stacklab, but some features map well to our direction:

- `Projects Directory` as source of truth for Compose projects
- template registries and reusable templates
- notification providers for updates
- images, volumes, and networks as first-class pages
- custom project metadata like icons and external links
- translation support

Implication for Stacklab:

- Arcane reinforces the value of filesystem-backed projects
- template catalogs, project metadata, and notifications are good candidates for Stacklab
- remote environments, OIDC, and broader Docker management are not near-term priorities for us

## Assessment Of Candidate Features

### Strong Near-Term Fits

#### Stacklab service logs from journald

Recommendation: `yes`

Why:

- host-native deployment makes this a natural differentiator
- helps debug Stacklab itself without leaving the browser
- useful on headless hosts where `journalctl` is not always convenient

Suggested scope:

- read-only browser view of `journalctl -u stacklab`
- recent logs, severity filter, refresh / stream mode
- no generic full-host journal browser

#### Stacklab version display

Recommendation: `yes, immediately`

Why:

- the backend already exposes version metadata
- helps support, release validation, and bug reports
- almost zero product risk

Suggested scope:

- show version in settings, login footer, and host overview
- expose commit/build metadata on a details panel, not everywhere

#### Host system parameters

Recommendation: `yes`

Why:

- operators need to know whether problems come from the stack or the host
- CPU, RAM, disk, and uptime are high-value context during troubleshooting
- aligns with the maintenance and diagnostics role of Stacklab

Suggested scope:

- CPU usage
- memory usage
- disk usage
- uptime
- hostname, OS, kernel
- Docker and Compose version

#### Light mode and dark mode

Recommendation: `yes`

Why:

- user-facing control panel benefits from theme choice
- lower risk than internationalization
- good for broader public adoption once visuals stabilize

Suggested scope:

- system preference + explicit toggle
- one maintained light theme and one maintained dark theme

#### Manual prune workflows

Recommendation: `yes, but conservative`

Why:

- useful operationally
- pairs naturally with image lifecycle and cleanup
- dangerous if too broad or too automatic

Suggested scope:

- first-class maintenance flows that replace ad-hoc scripts such as `update_stacks.sh`
- update selected stacks or all stacks in one workflow
- make the workflow explicit:
  - pull
  - build when needed
  - `up -d --remove-orphans`
- keep prune as an explicit optional step, not an unavoidable side effect
- manual prune first
- explicit scope selection: dangling images, unused images, build cache, stopped containers
- show preview/impact where practical
- never silently delete named volumes by default

### Good Mid-Term Fits

#### Mobile notifications for important failures

Recommendation: `yes`

Why:

- Stacklab already has long-running jobs, job detail, audit, and global activity
- operators should not need to keep the UI open during maintenance windows
- Telegram is a pragmatic first mobile channel without requiring self-hosted notification infrastructure

Suggested scope order:

- generic webhook baseline first
- Telegram as the first native mobile channel
- post-update recovery failures before broader runtime alerts
- Stacklab self-health alerts from the `stacklab` journald unit after that

Important constraint:

- Stacklab service error alerts should not mirror raw journald lines one-to-one
- use debounce and dedupe, for example:
  - repeated `error` or `fatal` entries within a short window
  - suppression of identical messages during a cooldown period
- keep container log anomaly alerts for later, after Stacklab's own self-health alerts are stable

#### Git-aware stack management

Recommendation: `yes`

Why:

- valuable for homelab operators who keep the local Stacklab workspace in Git
- aligns with our filesystem-first model
- does not require Stacklab to become the source of truth

Suggested scope:

- read-only Git status and diff first
- workspace-level status for `/opt/stacklab/stacks` and `/opt/stacklab/config`
- commit and push workflow for local changes made through Stacklab
- per-file selection as the primary write model
- stack-scoped quick selection as a convenience:
  - select all files under `stacks/<stack_id>/**`
  - select all files under `config/<stack_id>/**`
- allow operators to keep unrelated changes untouched
- file-level diff for stack definitions and config files
- do not require versioning of arbitrary top-level repo content if Stacklab replaces old maintenance scripts directly

Not recommended yet:

- automatic always-on reconciliation from remote Git
- complex merge or branch management in the UI
- pull/rebase conflict resolution in the UI

#### Config browser and editor

Recommendation: `yes`

Why:

- `/opt/stacklab/config` is part of the operator-owned workspace
- many homelab setups keep supporting configuration there, not only `compose.yaml`
- it pairs naturally with a local Git-backed workflow

Suggested scope:

- tree view limited to `/opt/stacklab/config`
- read-only browsing first for unknown or binary files
- text editor for common text-based config files
- save + audit integration
- later: Git diff and commit support from the same view

Avoid:

- turning Stacklab into a general-purpose host file manager
- exposing arbitrary host paths outside the Stacklab workspace

#### Workspace permissions diagnostics and repair

Recommendation: `yes`

Why:

- containers sometimes create unreadable or root-owned files on bind mounts
- that can block Git status, diff, and commit workflows for a non-root operator
- running the whole web app as `root` would solve the symptom by widening the blast radius

Suggested scope:

- clearly surface unreadable or unwritable files in config and Git views
- show ownership and mode information when access is blocked
- base diagnostics on the current file metadata and effective access, not on assumed ACL inheritance
- recommend aligning container `uid:gid` or `PUID/PGID` where possible
- add an explicit helper-backed repair workflow restricted to managed roots
- keep repair scoped to explicit target paths inside `/config` and `/stacks/<id>`

Avoid:

- running Stacklab as `root` by default
- turning permission repair into a generic unrestricted host administration surface

#### Notifications

Recommendation: `yes`

Why:

- useful for update jobs, scheduled maintenance, and failures
- proven valuable in Arcane and Dockhand

Suggested scope:

- webhook notifications first
- Telegram next as the first native mobile channel
- `ntfy` and `Gotify` are good later candidates for homelab-first, self-hosted delivery
- email later if needed
- events:
  - job failed
  - job succeeded with warnings
  - scheduled maintenance summary
  - post-update stack recovery failure
  - later runtime health degradation such as unhealthy containers or restart loops

#### Template library / app catalog

Recommendation: `yes`

Why:

- good onboarding feature
- fits a Compose-first product if templates remain plain files
- Arcane's local/remote template model is a strong reference

Suggested scope:

- local templates first
- optional remote registry format later
- templates produce normal stack directories and remain editable on disk

#### Custom project metadata

Recommendation: `yes`

Why:

- low complexity
- meaningful UX gain
- Arcane shows a useful pattern with icons and external URLs

Suggested scope:

- project icon
- external links like docs, homepage, Git repo
- keep metadata Compose-adjacent and filesystem-visible

### Useful But Scope-Sensitive

#### Images management

Recommendation: `yes, selectively`

Why:

- tightly related to pull/build/update/prune workflows
- useful for troubleshooting disk usage and stale images

Suggested scope:

- read-only inventory first
- show which stacks/services use which images
- allow limited actions like pull or remove unused images

Avoid:

- full registry browser or registry administration as a near-term goal

#### Networks and volumes management

Recommendation: `yes, selectively`

Why:

- Compose stacks often depend on external networks and named volumes
- operators need to inspect dependencies and leftovers

Suggested scope:

- read-only inventory of referenced objects first
- show attachments, stack usage, and orphaned objects
- allow targeted create/remove where this directly supports Compose stacks

Avoid:

- turning Stacklab into a general-purpose Docker object CRUD console

### Later / Conditional

#### Internationalization

Recommendation: `later`

Why:

- valuable for adoption
- but expensive to maintain while UI copy is still moving
- adds process overhead across frontend, docs, and release QA

Suggested approach:

- prepare UI copy for future i18n now
- do not prioritize full translation work until core UX and terminology stabilize

#### Automatic prune as part of scheduled maintenance

Recommendation: `later, opt-in only`

Why:

- useful only after manual prune workflows and scheduler are mature
- risky if users do not understand what is being removed

Suggested scope:

- separate maintenance policies from deploy/update policies
- expose retention and safety knobs
- default to off

#### Vulnerability scanning

Recommendation: `later, optional module`

Why:

- useful but expands scope materially
- better after maintenance, notifications, and image inventory are mature

## Recommended Borrowed Features

The following are the most worthwhile "borrowed" ideas for Stacklab:

1. Stacklab self-observability
   From: our host-native model, reinforced by Dockhand/Arcane emphasis on diagnostics
2. Host overview and maintenance visibility
   From: Dockhand dashboards and Arcane analytics mindset
3. Git-aware stack workflows without full GitOps
   From: Dockhand Git-backed stacks, adapted to our safer local-workspace model
4. Notifications for maintenance and failures
   From: Dockhand and Arcane
5. Template library and starter catalog
   From: Arcane
6. Config browser/editor for the Stacklab workspace
   From: our filesystem-first model, strengthened by the local Git workflow
7. Custom metadata such as icons and useful links
   From: Arcane
8. Limited image/network/volume maintenance surfaces
   From: Dockhand and Arcane, but constrained to Compose-first use cases
9. Targeted Docker daemon administration
   From: practical homelab needs rather than a direct product clone, constrained to explicit Docker service operations instead of generic host control

## Recommended Product Priorities

### Priority A: Trust And Operations

- version display
- Stacklab journald log viewer
- host overview page
- clearer maintenance/result surfacing

### Priority B: Safe Maintenance

- selected/all stack update workflow replacing ad-hoc shell scripts
- manual prune
- image inventory
- Docker daemon administration for common operator needs such as DNS, with explicit backup/restart/rollback
- stronger background activity visibility for long-running operations across the whole app
- read-only network and volume inventory
- notifications, with Telegram as the first native mobile target after generic webhooks

### Priority C: Git And Templates

- Git status, diff, commit, and push for the local workspace
- per-file commit selection with stack-scoped quick selection
- config browser/editor for `/opt/stacklab/config`
- workspace permission diagnostics and later explicit repair flow
- template library
- custom metadata

### Priority D: Polish And Broader Adoption

- theme toggle
- i18n groundwork
- optional vulnerability scanning later

## Sources

- [Dockge repository](https://github.com/louislam/dockge)
- [Dockge releases](https://github.com/louislam/dockge/releases)
- [Dockhand website](https://dockhand.pro/)
- [Arcane documentation](https://getarcane.app/docs)
- [Arcane projects docs](https://getarcane.app/docs/features/projects)
- [Arcane notifications docs](https://getarcane.app/docs/configuration/notifications)
- [Arcane templates docs](https://getarcane.app/docs/templates)
- [Arcane custom metadata docs](https://getarcane.app/docs/guides/custom-metadata)
