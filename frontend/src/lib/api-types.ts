// Domain types derived from docs/api/rest-endpoints.md and docs/domain/stack-model.md

// --- Enums ---

export type RuntimeState = 'defined' | 'running' | 'partial' | 'stopped' | 'error' | 'orphaned'
export type ConfigState = 'unknown' | 'in_sync' | 'drifted' | 'invalid'
export type ActivityState = 'idle' | 'locked'
export type DisplayState = RuntimeState

export type ServiceMode = 'image' | 'build' | 'hybrid'

export type ContainerStatus = 'created' | 'running' | 'restarting' | 'paused' | 'exited' | 'dead'
export type HealthStatus = 'healthy' | 'unhealthy' | 'starting' | null

export type JobState = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancel_requested' | 'cancelled' | 'timed_out'
export type JobEventType =
  | 'job_started'
  | 'job_step_started'
  | 'job_step_finished'
  | 'job_progress'
  | 'job_log'
  | 'job_warning'
  | 'job_error'
  | 'job_finished'

export type StackAction =
  | 'validate'
  | 'up'
  | 'down'
  | 'stop'
  | 'restart'
  | 'pull'
  | 'build'
  | 'recreate'
  | 'save_definition'
  | 'create_stack'
  | 'remove_stack_definition'

export type TerminalExitReason = 'process_exit' | 'idle_timeout' | 'server_cleanup' | 'connection_replaced' | 'client_close'

// --- REST response shapes ---

export interface HealthResponse {
  status: string
  version: string
}

export interface SessionResponse {
  authenticated: boolean
  user: {
    id: string
    display_name: string
  }
  features: {
    host_shell: boolean
  }
}

export interface MetaResponse {
  app: {
    name: string
    version: string
  }
  environment: {
    stack_root: string
    platform: string
  }
  docker: {
    engine_version: string
    compose_version: string
  }
  features: {
    host_shell: boolean
  }
}

export interface PortMapping {
  published: number
  target: number
  protocol: string
}

export interface VolumeMount {
  source: string
  target: string
}

export interface HealthSummary {
  healthy_container_count: number
  unhealthy_container_count: number
  unknown_health_container_count: number
}

export interface ServiceCount {
  defined: number
  running: number
}

export interface LastAction {
  action: string
  result: string
  finished_at: string
}

export interface StackCapabilities {
  can_edit_definition: boolean
  can_view_logs: boolean
  can_view_stats: boolean
  can_open_terminal: boolean
}

export interface Service {
  name: string
  mode: ServiceMode
  image_ref: string | null
  build_context: string | null
  dockerfile_path: string | null
  ports: PortMapping[]
  volumes: VolumeMount[]
  depends_on: string[]
  healthcheck_present: boolean
}

export interface Container {
  id: string
  name: string
  service_name: string
  status: ContainerStatus
  health_status: HealthStatus
  started_at: string | null
  image_id: string
  image_ref: string
  ports: PortMapping[]
  networks: string[]
}

export interface StackListItem {
  id: string
  name: string
  display_state: DisplayState
  runtime_state: RuntimeState
  config_state: ConfigState
  activity_state: ActivityState
  health_summary: HealthSummary
  service_count: ServiceCount
  last_action: LastAction | null
}

export interface StackListSummary {
  stack_count: number
  running_count: number
  stopped_count: number
  error_count: number
  container_count: {
    running: number
    total: number
  }
}

export interface StackListResponse {
  items: StackListItem[]
  summary: StackListSummary
}

export interface StackDetailResponse {
  stack: {
    id: string
    name: string
    root_path: string
    compose_file_path: string
    env_file_path: string | null
    config_path: string
    data_path: string
    display_state: DisplayState
    runtime_state: RuntimeState
    config_state: ConfigState
    activity_state: ActivityState
    health_summary: HealthSummary
    capabilities: StackCapabilities
    available_actions: StackAction[]
    services: Service[]
    containers: Container[]
    last_deployed_at: string | null
    last_action: LastAction | null
  }
}

export interface DefinitionResponse {
  stack_id: string
  files: {
    compose_yaml: {
      path: string
      content: string
    }
    env: {
      path: string
      content: string
      exists: boolean
    }
  }
  config_state: ConfigState
}

export interface ResolvedConfigResponse {
  stack_id: string
  valid: boolean
  content?: string
  error?: {
    code: string
    message: string
    details?: {
      line?: number
      column?: number
    }
  }
}

export interface JobRef {
  id: string
  stack_id: string | null
  action: string
  state: JobState
  workflow?: {
    steps: { action: string; state: string; target_stack_id?: string }[]
  }
}

export interface JobDetail {
  id: string
  stack_id: string | null
  action: string
  state: JobState
  requested_at: string
  started_at: string | null
  finished_at: string | null
  workflow?: {
    steps: { action: string; state: string; target_stack_id?: string }[]
  } | null
}

export interface ActiveJobStep {
  index: number
  total: number
  action: string
  target_stack_id?: string
}

export interface ActiveJobLatestEvent {
  event: JobEventType | string
  message?: string
  data?: string | null
  timestamp: string
  step?: ActiveJobStep | null
}

export interface ActiveJobItem {
  id: string
  stack_id: string | null
  action: string
  state: JobState
  requested_at: string
  started_at: string | null
  workflow?: {
    steps: { action: string; state: string; target_stack_id?: string }[]
  } | null
  current_step?: ActiveJobStep | null
  latest_event?: ActiveJobLatestEvent | null
}

export interface ActiveJobsResponse {
  items: ActiveJobItem[]
  summary: {
    active_count: number
    running_count: number
    queued_count: number
    cancel_requested_count: number
  }
}

export interface AuditEntry {
  id: string
  stack_id: string | null
  job_id: string | null
  action: string
  requested_by: string
  result: string
  requested_at: string
  finished_at: string | null
  duration_ms: number | null
}

export interface AuditResponse {
  items: AuditEntry[]
  next_cursor: string | null
}

// --- Host observability ---

export interface HostOverviewResponse {
  host: {
    hostname: string
    os_name: string
    kernel_version: string
    architecture: string
    uptime_seconds: number
  }
  stacklab: {
    version: string
    commit: string
    started_at: string
  }
  docker: {
    engine_version: string
    compose_version: string
  }
  resources: {
    cpu: {
      core_count: number
      load_average: [number, number, number]
      usage_percent: number
    }
    memory: {
      total_bytes: number
      used_bytes: number
      available_bytes: number
      usage_percent: number
    }
    disk: {
      path: string
      total_bytes: number
      used_bytes: number
      available_bytes: number
      usage_percent: number
    }
  }
}

export interface StacklabLogEntry {
  timestamp: string
  level: string
  message: string
  cursor: string
}

export interface StacklabLogsResponse {
  items: StacklabLogEntry[]
  next_cursor: string | null
  has_more: boolean
}

export interface DockerServiceStatus {
  manager: string
  supported: boolean
  unit_name: string
  load_state: string
  active_state: string
  sub_state: string
  unit_file_state: string
  fragment_path: string
  started_at?: string | null
  message?: string | null
}

export interface DockerEngineStatus {
  available: boolean
  version: string
  api_version: string
  compose_version: string
  root_dir: string
  driver: string
  logging_driver: string
  cgroup_driver: string
  message?: string | null
}

export interface DockerDaemonConfigSummary {
  dns: string[]
  registry_mirrors: string[]
  insecure_registries: string[]
  log_driver: string
  data_root: string
  live_restore?: boolean | null
}

export interface DockerDaemonConfigMeta {
  path: string
  exists: boolean
  permissions?: FilePermissions | null
  size_bytes?: number | null
  modified_at?: string | null
  valid_json: boolean
  parse_error?: string | null
  configured_keys: string[]
  summary: DockerDaemonConfigSummary
  write_capability: DockerDaemonWriteCapability
}

export interface DockerAdminOverviewResponse {
  service: DockerServiceStatus
  engine: DockerEngineStatus
  daemon_config: DockerDaemonConfigMeta
  write_capability: DockerDaemonWriteCapability
}

export interface DockerDaemonConfigResponse extends DockerDaemonConfigMeta {
  content?: string | null
}

export interface DockerDaemonWriteCapability {
  supported: boolean
  reason?: string | null
  managed_keys: string[]
}

export interface DockerManagedSettings {
  dns?: string[]
  registry_mirrors?: string[]
  insecure_registries?: string[]
  live_restore?: boolean
}

export interface DockerDaemonValidateRequest {
  settings: DockerManagedSettings
  remove_keys?: string[]
}

export type DockerDaemonApplyRequest = DockerDaemonValidateRequest

export interface DockerDaemonConfigPreview {
  path: string
  content: string
  configured_keys: string[]
  summary: DockerDaemonConfigSummary
}

export interface DockerDaemonValidateResponse {
  write_capability: DockerDaemonWriteCapability
  changed_keys: string[]
  requires_restart: boolean
  warnings: string[]
  preview: DockerDaemonConfigPreview
}

export interface FilePermissions {
  owner_uid: number | null
  owner_name: string | null
  group_gid: number | null
  group_name: string | null
  mode: string
  readable: boolean
  writable: boolean
}

// --- Config workspace ---

export type ConfigEntryType = 'directory' | 'text_file' | 'binary_file' | 'unknown_file'

export interface ConfigTreeEntry {
  name: string
  path: string
  type: ConfigEntryType
  size_bytes: number
  modified_at: string
  stack_id: string | null
  permissions: FilePermissions
}

export interface ConfigTreeResponse {
  workspace_root: string
  current_path: string
  parent_path: string | null
  items: ConfigTreeEntry[]
}

export interface ConfigFileResponse {
  path: string
  name: string
  type: ConfigEntryType
  stack_id: string | null
  content: string | null
  encoding: string | null
  size_bytes: number
  modified_at: string
  readable: boolean
  writable: boolean
  blocked_reason: string | null
  permissions: FilePermissions
  repair_capability: WorkspaceRepairCapability
}

export interface ConfigFileSaveResponse {
  saved: boolean
  path: string
  modified_at: string
  audit_action: string
}

export interface WorkspaceRepairCapability {
  supported: boolean
  reason?: string
  recursive: boolean
}

export interface ConfigRepairPermissionsRequest {
  path: string
  recursive?: boolean
}

export interface ConfigRepairPermissionsResponse {
  repaired: boolean
  path: string
  recursive: boolean
  changed_items: number
  warnings?: string[]
  target_permissions_before: FilePermissions
  target_permissions_after: FilePermissions
  audit_action: string
  repair_capability: WorkspaceRepairCapability
}

// --- Stack workspace ---

export type StackWorkspaceEntryType = ConfigEntryType

export interface StackWorkspaceTreeEntry {
  name: string
  path: string
  type: StackWorkspaceEntryType
  size_bytes: number
  modified_at: string
  permissions: FilePermissions
}

export interface StackWorkspaceTreeResponse {
  stack_id: string
  workspace_root: string
  current_path: string
  parent_path: string | null
  items: StackWorkspaceTreeEntry[]
}

export interface StackWorkspaceFileResponse {
  stack_id: string
  path: string
  name: string
  type: StackWorkspaceEntryType
  content: string | null
  encoding: string | null
  size_bytes: number
  modified_at: string
  readable: boolean
  writable: boolean
  blocked_reason: string | null
  permissions: FilePermissions
  repair_capability: WorkspaceRepairCapability
}

export interface StackWorkspaceFileSaveResponse {
  saved: boolean
  stack_id: string
  path: string
  modified_at: string
  audit_action: string
}

export interface StackRepairPermissionsRequest {
  path: string
  recursive?: boolean
}

export interface StackRepairPermissionsResponse {
  repaired: boolean
  stack_id: string
  path: string
  recursive: boolean
  changed_items: number
  warnings?: string[]
  target_permissions_before: FilePermissions
  target_permissions_after: FilePermissions
  audit_action: string
  repair_capability: WorkspaceRepairCapability
}

// --- Git workspace ---

export type GitFileStatus = 'modified' | 'added' | 'deleted' | 'renamed' | 'untracked' | 'conflicted'

export interface GitStatusItem {
  path: string
  scope: 'stacks' | 'config'
  stack_id: string | null
  status: GitFileStatus
  old_path: string | null
  permissions: FilePermissions | null
  diff_available: boolean
  commit_allowed: boolean
  blocked_reason: string | null
}

export interface GitWorkspaceStatusResponse {
  available: boolean
  repo_root: string
  managed_roots: string[]
  branch?: string
  head_commit?: string
  has_upstream?: boolean
  upstream_name?: string
  ahead_count?: number
  behind_count?: number
  clean?: boolean
  reason?: string
  items?: GitStatusItem[]
}

export interface GitDiffResponse {
  available: boolean
  path: string
  scope: 'stacks' | 'config'
  stack_id: string | null
  status: GitFileStatus
  old_path: string | null
  permissions: FilePermissions | null
  diff_available: boolean
  blocked_reason: string | null
  is_binary: boolean
  diff: string | null
  truncated: boolean
}

export interface GitCommitRequest {
  message: string
  paths: string[]
}

export interface GitCommitResponse {
  committed: boolean
  commit: string
  summary: string
  paths: string[]
  remaining_changes: number
}

export interface GitPushResponse {
  pushed: boolean
  remote: string
  branch: string
  upstream_name: string
  head_commit: string
  ahead_count: number
  behind_count: number
}

// --- Maintenance ---

export interface MaintenanceUpdateStacksRequest {
  target: {
    mode: 'selected' | 'all'
    stack_ids?: string[]
  }
  options?: {
    pull_images?: boolean
    build_images?: boolean
    remove_orphans?: boolean
    prune_after?: {
      enabled?: boolean
      include_volumes?: boolean
    }
  }
}

export type MaintenanceImageUsage = 'all' | 'used' | 'unused'
export type MaintenanceImageOrigin = 'all' | 'stack_managed' | 'external'
export type MaintenanceImageSource = 'stack_managed' | 'external'

export interface MaintenanceImageStackUsage {
  stack_id: string
  service_names: string[]
}

export interface MaintenanceImageItem {
  id: string
  repository: string
  tag: string
  reference: string
  size_bytes: number
  created_at: string
  containers_using: number
  stacks_using: MaintenanceImageStackUsage[]
  is_dangling: boolean
  is_unused: boolean
  source: MaintenanceImageSource
}

export interface MaintenanceImagesResponse {
  items: MaintenanceImageItem[]
}

export interface MaintenancePrunePreviewItem {
  reference: string
  size_bytes: number
  reason: string
}

export interface MaintenancePrunePreviewCategory {
  count: number
  reclaimable_bytes: number
  items?: MaintenancePrunePreviewItem[]
}

export interface MaintenancePrunePreviewResponse {
  preview: {
    images: MaintenancePrunePreviewCategory
    build_cache: MaintenancePrunePreviewCategory
    stopped_containers: MaintenancePrunePreviewCategory
    volumes: MaintenancePrunePreviewCategory
    total_reclaimable_bytes: number
  }
}

export interface MaintenancePruneRequest {
  scope: {
    images: boolean
    build_cache: boolean
    stopped_containers: boolean
    volumes: boolean
  }
}

export interface ApiError {
  error: {
    code: string
    message: string
    details?: Record<string, unknown>
  }
}
