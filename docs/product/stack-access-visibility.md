# Stack Access Visibility

## Status And Scope

Discussion conclusions recorded on 2026-10-30. This is a candidate implementation
plan, not an implemented feature or a scheduled delivery commitment.

The operator already accesses Stacklab through LAN or VPN. Some managed
containers intentionally need host data under `/mnt`. Preserve those workflows:
do not restrict stack mount paths, migrate data, introduce permission prompts,
or stop existing containers as part of this work.

The proposed feature explains what each stack can access and how a Compose edit
would change that access. Enforcement, a separate execution service, rootless
Docker migration, MFA and additional network controls remain separate decisions.

## Conclusions From The Security Discussion

- Stacklab already creates definitions under
  `<STACKLAB_ROOT>/stacks/<stack>/` and restricts workspace editing. This does
  not restrict the host paths Docker can mount into a container.
- The host-native backend has administrative Docker access. Compromise of that
  process, or of an operator session that can deploy arbitrary Compose, must be
  treated as a possible host compromise. A host shell or container escape is
  not required to reach that outcome.
- Compromise of an ordinary application container does not automatically give
  the same access. Its mounts, devices, capabilities, namespaces and access to
  administrative services determine the available routes beyond the container.
- `/mnt` is a legitimate location for disks, media and NAS shares. An external
  path alone is not evidence of host administration capability. Explain the
  exact resource and read/write mode instead of treating every external mount
  as equally dangerous.
- Allowing all of `<STACKLAB_ROOT>` would expose other stacks' data, secrets,
  definitions and shared Git metadata. A future restriction policy would need
  per-stack scope and explicit shared resources, not one broad path prefix.
- Backend validation and UI warnings cannot contain arbitrary code executing
  in a backend that retains unrestricted Docker access. A stronger boundary
  would need independently enforced policy and a review of privileged helpers.

The [security model](../architecture/security-model.md) defines the current trust
boundary. [Filesystem layout](../architecture/filesystem-layout.md) distinguishes
managed workload files from Stacklab's private runtime state. Backup source grants
and repository destinations retain their existing, separate
[operational rules](../ops/backup-and-recovery.md#prepare-the-host).

## Proposed Operator Experience

The operator should be able to identify which service has access to which host
resource, whether it can change that resource, and whether the available evidence
is current and complete.

- Add a compact access summary to stack cards and a detailed **Host access**
  section in stack details, with findings grouped by service and resource.
- Show host source, container target and read/write mode for mounts; distinguish
  own data/configuration, shared resources, external data and sensitive host
  resources. Private Stacklab state and writable deployment definitions deserve
  explicit findings even when a path is inside a configured workspace root.
- Identify administrative Docker socket access, `privileged`, host namespaces,
  added capabilities, device access and disabled security profiles. Explain the
  consequence of each finding; do not imply that host networking alone grants
  root or that every added capability has the same impact.
- Keep ordinary named volumes visible without treating Docker's storage location
  outside `<STACKLAB_ROOT>` as an arbitrary host mount. Inspect existing volume
  driver/options; named volumes can also wrap host bind mounts or network shares.
- Explain that a read-only mount limits file writes, but can still expose secrets.
  Mounting a Docker socket with `:ro` does not make its API read-only; a socket
  proxy's authorization policy cannot be inferred from its pathname.
- In the Compose editor, show access added, removed or changed by the draft.
  Compare with the saved definition and separately show differences from the
  observed runtime. Keep all findings advisory: no deploy rejection, additional
  confirmation or automatic rewrite in this scope.
- Keep incomplete analysis visible alongside known findings. Avoid a blanket
  "safe" badge or numerical security score. "No findings in analyzed settings"
  must never imply protection against kernel vulnerabilities or backend compromise.

Use the existing visual system, details navigation and editor validation flow.
Retain keyboard access, readable mobile layouts and focus during refresh. Do not
add a separate security dashboard for the first delivery.

## Evidence And Analysis Boundaries

Use one backend assessment model for the stack summary, details and draft diff;
the frontend must not independently classify YAML. Each finding needs a stable
rule identifier, affected service/resource, consequence and supporting evidence.
Keep impact separate from evidence completeness.

Combine resolved Compose with Docker container and volume inspection. Record
whether evidence describes the draft, saved configuration or observed runtime,
with definition revision and observation time. A stopped or undeployed service
may have configuration evidence without runtime evidence. Recreated containers,
missing volumes, unsupported fields or failed inspection must not silently clear
previously known exposure or produce a complete assessment.

Handle short and long mount syntax, interpolation, relative paths and symlink
resolution. Check actual resource identity where accessible; spelling a path
under `/mnt` or `<STACKLAB_ROOT>` is not proof of its contents or isolation.
Inspect shared/external volumes and inherited mounts where possible, otherwise
report the limitation. Distinguish requested Compose settings from effective
runtime settings and do not assume one Docker daemon security configuration.

The initial analysis covers container access plus Compose inputs that read host
files, such as build contexts, `env_file`, configs and secrets. Mark unsupported
resolution paths as incomplete. Resolve only through supported existing flows;
analysis must not build images, start containers, run hooks or probe resources by
writing to them. Bound subprocess time, output, resource counts and caching.

Expose only the structured evidence needed by the authenticated operator. Do not
return or log full resolved Compose, environment values, secret contents or raw
Docker inspection payloads for this feature. Assessments are observations, not
authorization tokens or promises that runtime configuration cannot change.

## Candidate Delivery Sequence

1. **Assessment model and rules.** Inventory the existing Compose/runtime readers,
   define impact and completeness semantics, and add bounded extraction with
   representative fixtures. Specify limitations and resource identity rules.
2. **Read API and stack views.** Define the OpenAPI contract and generated types,
   integrate authenticated assessment reads, and add summaries/details with
   partial, stale, loading and unavailable states. Start with on-demand reads;
   avoid a new scheduler or persistent finding history unless demonstrated needs
   justify them.
3. **Compose change preview.** Reuse the assessment model for drafts, bind results
   to their revision, and compare draft/saved/runtime evidence without conflating
   them. Discard responses for superseded drafts and preserve editing focus.
4. **Compatibility and validation.** Exercise representative stacks on the
   disposable Linux host, document actual coverage, and update API/UI/security
   references to describe the delivered behavior. Keep any enforcement proposal
   outside this implementation.

## Acceptance Scenarios

- A container using `/mnt/media:ro` remains deployable and shows external data
  access with its exact source, target and mode. Changing it to writable produces
  a clear change in the preview without rejecting the deployment.
- Ordinary databases on local named volumes retain their behavior. A volume
  backed by a host directory is assessed using its inspected configuration;
  missing or inaccessible volume metadata produces incomplete evidence.
- Docker socket access, including a read-only socket mount, broad host mounts,
  writable definitions and access to another stack's data produce distinct,
  actionable findings. Privileged mode, host namespaces and devices remain visible
  even when no suspicious bind mount is present.
- Relative paths, symlinks, interpolated sources and alternate mount syntax do
  not produce a misleading own-data classification. Unresolvable inputs remain
  explicit, and configuration/runtime divergence is shown independently.
- Docker outages, stale results and rapid draft edits cannot display old findings
  as a current complete assessment. Analysis errors do not block existing stack
  actions or claim that access has been removed.
- API/log fixtures contain no secret values or full inspection payloads. Browser
  tests cover keyboard/mobile use and refresh focus. Linux validation confirms
  analysis performs no mutations and leaves lifecycle and backup behavior intact.

## References

- [Docker Engine security](https://docs.docker.com/engine/security/)
- [Docker bind mounts](https://docs.docker.com/engine/storage/bind-mounts/)
- [Compose volume drivers and options](https://docs.docker.com/reference/compose-file/volumes/)
