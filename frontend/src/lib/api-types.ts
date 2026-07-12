import type { components } from './api-contract.generated'

type APISchemas = components['schemas']

// Stable application-facing aliases for the generated OpenAPI contract.

// --- Enums ---

export type RuntimeState = 'defined' | 'running' | 'partial' | 'stopped' | 'error' | 'orphaned'
export type ConfigState = 'unknown' | 'in_sync' | 'drifted' | 'invalid'
export type ActivityState = 'idle' | 'locked'
export type DisplayState = RuntimeState

export type ServiceMode = 'image' | 'build' | 'hybrid'

export type ContainerStatus = 'created' | 'running' | 'restarting' | 'paused' | 'exited' | 'dead'
export type HealthStatus = 'healthy' | 'unhealthy' | 'starting' | null

export type JobState = APISchemas['JobState']
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

export type HealthResponse = APISchemas['HealthResponse']
export type LivenessResponse = APISchemas['LivenessResponse']
export type SessionResponse = APISchemas['SessionResponse']
export type MetaResponse = APISchemas['MetaResponse']

export interface NotificationEventToggles {
  job_failed: boolean
  job_succeeded_with_warnings: boolean
  maintenance_succeeded: boolean
  post_update_recovery_failed?: boolean
  stacklab_service_error?: boolean
  runtime_health_degraded?: boolean
  runtime_log_error_burst?: boolean
}

export interface NotificationWebhookChannel {
  enabled: boolean
  configured: boolean
  url: string
}

export interface NotificationTelegramChannel {
  enabled: boolean
  configured: boolean
  bot_token_configured: boolean
  chat_id: string
}

export interface NotificationChannels {
  webhook: NotificationWebhookChannel
  telegram: NotificationTelegramChannel
}

export interface NotificationSettingsResponse {
  enabled: boolean
  configured: boolean
  webhook_url: string
  events: NotificationEventToggles
  channels?: NotificationChannels
}

export interface NotificationChannelRequest {
  webhook?: {
    enabled: boolean
    url: string
  }
  telegram?: {
    enabled: boolean
    bot_token: string
    chat_id: string
  }
}

export interface NotificationSettingsUpdateRequest {
  enabled: boolean
  webhook_url: string
  events: NotificationEventToggles
  channels?: NotificationChannelRequest
}

export interface NotificationTestRequest {
  channel?: 'webhook' | 'telegram'
  enabled: boolean
  webhook_url: string
  events: NotificationEventToggles
  channels?: NotificationChannelRequest
}

export interface NotificationTestResponse {
  sent: boolean
  channel?: 'webhook' | 'telegram'
}

export type ScheduleFrequency = 'daily' | 'weekly'
export type ScheduleWeekday = 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun'

export interface MaintenanceScheduleStatus {
  next_run_at?: string
  last_triggered_at?: string
  last_scheduled_for?: string
  last_result?: string
  last_message?: string
  last_job_id?: string
}

export interface MaintenanceUpdateScheduleConfig {
  enabled: boolean
  frequency: ScheduleFrequency
  time: string
  weekdays?: ScheduleWeekday[]
  target: {
    mode: 'selected' | 'all'
    stack_ids?: string[]
    excluded_services?: Record<string, string[]>
  }
  options: {
    pull_images: boolean
    build_images: boolean
    remove_orphans: boolean
    prune_after: boolean
    include_volumes: boolean
  }
}

export interface MaintenancePruneScheduleConfig {
  enabled: boolean
  frequency: ScheduleFrequency
  time: string
  weekdays?: ScheduleWeekday[]
  scope: {
    images: boolean
    build_cache: boolean
    stopped_containers: boolean
    volumes: boolean
  }
}

export interface MaintenanceSchedulesResponse {
  timezone: string
  update: MaintenanceUpdateScheduleConfig & { status: MaintenanceScheduleStatus }
  prune: MaintenancePruneScheduleConfig & { status: MaintenanceScheduleStatus }
}

export interface MaintenanceSchedulesUpdateRequest {
  update: MaintenanceUpdateScheduleConfig
  prune: MaintenancePruneScheduleConfig
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

export interface StackMetadata {
  icon?: string
  links?: StackMetaLink[]
}

export interface StackMetaLink {
  label: string
  url: string
}

export interface StackStats {
  cpu_percent: number
  memory_bytes: number
  sampled_at: string
}

export interface StackTemplate {
  id: string
  name: string
  description?: string
  icon?: string
  compose_yaml: string
  built_in: boolean
  variables?: StackTemplateVariable[]
}

export interface StackTemplateVariable {
  name: string
  label?: string
  description?: string
  default?: string
  required: boolean
}

export interface StackUpdates {
  state: 'available' | 'up_to_date' | 'unknown'
  services_with_updates: number
  checked_at: string
}

export interface ImageUpdateStatus {
  image_ref: string
  local_digest?: string
  remote_digest?: string
  state: 'available' | 'up_to_date' | 'unknown'
  checked_at: string
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
  metadata?: StackMetadata | null
  stats?: StackStats | null
  updates?: StackUpdates | null
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
    updates?: StackUpdates | null
  }
}

export interface DefinitionResponse {
  stack_id: string
  files: {
    compose_yaml: {
      path: string
      content: string
      modified_at: string
    }
    env: {
      path: string
      content: string
      exists: boolean
      modified_at: string | null
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
  warnings?: ComposeWarning[]
}

export interface ComposeWarning {
  code: string
  service?: string
  message: string
}

export type JobRef = APISchemas['Job']
export type JobDetail = APISchemas['Job']
export type JobEventStep = APISchemas['ActiveJobStep']
export type JobHistoryEvent = APISchemas['JobEventRecord']
export type JobEventsResponse = APISchemas['JobEventsResponse']
export type ActiveJobStep = APISchemas['ActiveJobStep']
export type ActiveJobLatestEvent = APISchemas['ActiveJobLatestEvent']
export type ActiveJobItem = APISchemas['ActiveJobItem']
export type ActiveJobsResponse = APISchemas['ActiveJobsResponse']
export type AuditEntry = APISchemas['AuditEntry']
export type AuditResponse = APISchemas['AuditListResponse']

// --- Host observability ---

export type HostOverviewResponse = APISchemas['HostOverviewResponse']
export type HostMetricsResponse = APISchemas['HostMetricsResponse']
export type HostSettingsResponse = APISchemas['HostSettingsResponse']
export type HostSettingsUpdateRequest = APISchemas['HostSettingsRequest']
export type HostMetricSample = APISchemas['HostMetricSample']
export type HostSwapUsage = APISchemas['HostSwap']
export type HostTemperatureUsage = APISchemas['HostTemperatures']
export type HostTemperatureSensor = APISchemas['HostTemperatureSensor']
export type HostFilesystemUsage = APISchemas['HostFilesystem']
export type HostDiskIOUsage = APISchemas['HostDiskIO']
export type HostDiskIODeviceUsage = APISchemas['HostDiskIODevice']
export type HostNetworkUsage = APISchemas['HostNetwork']
export type HostNetworkInterfaceUsage = APISchemas['HostNetworkInterface']
export type HostProcessUsage = APISchemas['HostProcesses']
export type HostProcessInfo = APISchemas['HostProcess']
export type HostProcessContainerInfo = APISchemas['HostProcessContainer']
export type StacklabLogEntry = APISchemas['StacklabLogEntry']
export type StacklabLogsResponse = APISchemas['StacklabLogsResponse']

export type DockerServiceStatus = APISchemas['DockerServiceStatus']
export type DockerEngineStatus = APISchemas['DockerEngineStatus']
export type DockerDaemonConfigSummary = APISchemas['DockerDaemonConfigSummary']
export type DockerManagedKey = APISchemas['DockerManagedKey']
export type DockerDaemonConfigMeta = APISchemas['DockerDaemonConfigMeta']
export type DockerAdminOverviewResponse = APISchemas['DockerAdminOverviewResponse']
export type DockerDaemonConfigResponse = APISchemas['DockerDaemonConfigResponse']
export type DockerDaemonWriteCapability = APISchemas['DockerDaemonWriteCapability']
export type DockerManagedSettings = APISchemas['DockerManagedSettings']
export type DockerDaemonValidateRequest = APISchemas['DockerDaemonValidateRequest']
export type DockerDaemonApplyRequest = APISchemas['DockerDaemonValidateRequest']
export type DockerDaemonConfigPreview = APISchemas['DockerDaemonConfigPreview']
export type DockerDaemonValidateResponse = APISchemas['DockerDaemonValidateResponse']
export type DockerRegistryEntry = APISchemas['DockerRegistryEntry']
export type DockerRegistryStatusResponse = APISchemas['DockerRegistryStatusResponse']
export type DockerRegistryLoginRequest = APISchemas['DockerRegistryLoginRequest']
export type DockerRegistryLogoutRequest = APISchemas['DockerRegistryLogoutRequest']
export type StacklabUpdatePackageStatus = APISchemas['StacklabUpdatePackageStatus']
export type StacklabUpdateWriteCapability = APISchemas['StacklabUpdateWriteCapability']
export type StacklabUpdateRuntimeStatus = APISchemas['StacklabUpdateRuntimeStatus']
export type StacklabUpdateOverviewResponse = APISchemas['StacklabUpdateOverviewResponse']
export type StacklabUpdateApplyRequest = APISchemas['StacklabUpdateApplyRequest']
export type StacklabUpdateApplyResponse = APISchemas['StacklabUpdateApplyResponse']

export type FilePermissions = APISchemas['FilePermissions']

// --- Config workspace ---

export type ConfigEntryType = APISchemas['ConfigEntryType']
export type ConfigTreeEntry = APISchemas['ConfigTreeEntry']
export type ConfigTreeResponse = APISchemas['ConfigTreeResponse']
export type ConfigFileResponse = APISchemas['ConfigFileResponse']
export type ConfigFileSaveResponse = APISchemas['ConfigFileSaveResponse']
export type WorkspaceRepairCapability = APISchemas['WorkspaceRepairCapability']
export type ConfigRepairPermissionsRequest = APISchemas['ConfigRepairPermissionsRequest']
export type ConfigRepairPermissionsResponse = APISchemas['ConfigRepairPermissionsResponse']

// --- Stack workspace ---

export type StackWorkspaceEntryType = ConfigEntryType
export type StackWorkspaceTreeEntry = APISchemas['StackWorkspaceTreeEntry']
export type StackWorkspaceTreeResponse = APISchemas['StackWorkspaceTreeResponse']
export type StackWorkspaceFileResponse = APISchemas['StackWorkspaceFileResponse']
export type StackWorkspaceFileSaveResponse = APISchemas['StackWorkspaceFileSaveResponse']
export type StackRepairPermissionsRequest = APISchemas['StackRepairPermissionsRequest']
export type StackRepairPermissionsResponse = APISchemas['StackRepairPermissionsResponse']

// --- Git workspace ---

export type GitFileStatus = APISchemas['GitFileStatus']
export type GitStatusItem = APISchemas['GitStatusItem']
export type GitWorkspaceStatusResponse = APISchemas['GitWorkspaceStatusResponse']
export type GitDiffResponse = APISchemas['GitDiffResponse']
export type GitCommitRequest = APISchemas['GitCommitRequest']
export type GitCommitResponse = APISchemas['GitCommitResponse']
export type GitPushResponse = APISchemas['GitPushResponse']

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

export type MaintenanceNetworkSource = 'stack_managed' | 'external'

export interface MaintenanceNetworkItem {
  id: string
  name: string
  driver: string
  scope: string
  internal: boolean
  attachable: boolean
  ingress: boolean
  containers_using: number
  stacks_using: MaintenanceImageStackUsage[]
  is_unused: boolean
  source: MaintenanceNetworkSource
}

export interface MaintenanceNetworksResponse {
  items: MaintenanceNetworkItem[]
}

export interface MaintenanceCreateNetworkRequest {
  name: string
}

export interface MaintenanceCreateNetworkResponse {
  created: boolean
  name: string
}

export interface MaintenanceDeleteNetworkResponse {
  deleted: boolean
  name: string
}

export type MaintenanceVolumeSource = 'stack_managed' | 'external'

export interface MaintenanceVolumeItem {
  name: string
  driver: string
  mountpoint: string
  scope: string
  size_bytes: number
  options_count: number
  containers_using: number
  stacks_using: MaintenanceImageStackUsage[]
  is_unused: boolean
  source: MaintenanceVolumeSource
}

export interface MaintenanceVolumesResponse {
  items: MaintenanceVolumeItem[]
}

export interface MaintenanceCreateVolumeRequest {
  name: string
}

export interface MaintenanceCreateVolumeResponse {
  created: boolean
  name: string
}

export interface MaintenanceDeleteVolumeResponse {
  deleted: boolean
  name: string
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
