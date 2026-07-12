import type { components } from './api-contract.generated'

type APISchemas = components['schemas']

// Stable application-facing aliases for the generated OpenAPI contract.

// --- Enums ---

export type RuntimeState = APISchemas['RuntimeState']
export type ConfigState = APISchemas['ConfigState']
export type ActivityState = APISchemas['ActivityState']
export type DisplayState = APISchemas['DisplayState']
export type ServiceMode = APISchemas['ServiceMode']
export type ContainerStatus = APISchemas['ContainerStatus']
export type HealthStatus = APISchemas['Container']['health_status']

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

export type AvailableStackAction = APISchemas['AvailableStackAction']
export type StackAction = AvailableStackAction

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

export type PortMapping = APISchemas['PortMapping']
export type VolumeMount = APISchemas['VolumeMount']
export type HealthSummary = APISchemas['HealthSummary']
export type ServiceCount = APISchemas['StackListItem']['service_count']
export type LastAction = NonNullable<APISchemas['LastAction']>
export type StackCapabilities = APISchemas['StackCapabilities']
export type Service = APISchemas['Service']
export type Container = APISchemas['Container']
export type StackMetadata = NonNullable<APISchemas['StackMetadata']>
export type StackMetaLink = NonNullable<StackMetadata['links']>[number]
export type StackStats = NonNullable<APISchemas['StackStats']>
export type StackTemplate = APISchemas['StackTemplate']
export type StackTemplateVariable = APISchemas['TemplateVariable']
export type StackUpdates = NonNullable<APISchemas['StackUpdates']>
export type ImageUpdateStatus = APISchemas['ImageUpdateStatus']
export type StackListItem = APISchemas['StackListItem']
export type StackListSummary = APISchemas['StackListSummary']
export type StackListResponse = APISchemas['StackListResponse']
export type StackDetailResponse = APISchemas['StackDetailResponse']
export type DefinitionResponse = APISchemas['StackDefinitionResponse']
export type ResolvedConfigResponse = APISchemas['ResolvedConfigResponse']
export type ComposeWarning = APISchemas['ComposeWarning']

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
