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

// --- Config workspace ---

export type ConfigEntryType = 'directory' | 'text_file' | 'binary_file' | 'unknown_file'

export interface ConfigTreeEntry {
  name: string
  path: string
  type: ConfigEntryType
  size_bytes: number
  modified_at: string
  stack_id: string | null
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
  writable: boolean
}

export interface ConfigFileSaveResponse {
  saved: boolean
  path: string
  modified_at: string
  audit_action: string
}

// --- Git workspace ---

export type GitFileStatus = 'modified' | 'added' | 'deleted' | 'renamed' | 'untracked' | 'conflicted'

export interface GitStatusItem {
  path: string
  scope: 'stacks' | 'config'
  stack_id: string | null
  status: GitFileStatus
  old_path: string | null
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
  scope: string
  stack_id: string | null
  status: GitFileStatus
  old_path: string | null
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

export interface ApiError {
  error: {
    code: string
    message: string
    details?: Record<string, unknown>
  }
}
