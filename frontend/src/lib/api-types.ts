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

export type NotificationEventToggles = APISchemas['NotificationEventToggles']
export type NotificationEventTogglesRequest = APISchemas['NotificationEventTogglesRequest']
export type NotificationWebhookChannel = APISchemas['NotificationWebhookChannel']
export type NotificationTelegramChannel = APISchemas['NotificationTelegramChannel']
export type NotificationChannels = APISchemas['NotificationChannelsResponse']
export type NotificationChannelRequest = APISchemas['NotificationChannelsRequest']
export type NotificationSettingsResponse = APISchemas['NotificationSettingsResponse']
export type NotificationSettingsUpdateRequest = APISchemas['NotificationSettingsRequest']
export type NotificationTestRequest = APISchemas['NotificationTestRequest']
export type NotificationTestResponse = APISchemas['NotificationTestResponse']
export type ScheduleFrequency = APISchemas['MaintenanceScheduleFrequency']
export type ScheduleWeekday = APISchemas['MaintenanceScheduleWeekday']
export type MaintenanceScheduleStatus = APISchemas['MaintenanceScheduleStatus']
export type MaintenanceUpdateScheduleConfig = APISchemas['MaintenanceUpdateScheduleConfig']
export type MaintenancePruneScheduleConfig = APISchemas['MaintenancePruneScheduleConfig']
export type MaintenanceSchedulesResponse = APISchemas['MaintenanceSchedulesResponse']
export type MaintenanceSchedulesUpdateRequest = APISchemas['MaintenanceSchedulesRequest']

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

export type MaintenanceUpdateStacksRequest = APISchemas['MaintenanceUpdateStacksRequest']

export type MaintenanceImageUsage = 'all' | 'used' | 'unused'
export type MaintenanceImageOrigin = 'all' | 'stack_managed' | 'external'
export type MaintenanceImageSource = APISchemas['MaintenanceImageItem']['source']
export type MaintenanceImageStackUsage = APISchemas['MaintenanceImageStackUsage']
export type MaintenanceImageItem = APISchemas['MaintenanceImageItem']
export type MaintenanceImagesResponse = APISchemas['MaintenanceImagesResponse']
export type MaintenanceNetworkSource = APISchemas['MaintenanceNetworkItem']['source']
export type MaintenanceNetworkItem = APISchemas['MaintenanceNetworkItem']
export type MaintenanceNetworksResponse = APISchemas['MaintenanceNetworksResponse']
export type MaintenanceCreateNetworkRequest = APISchemas['MaintenanceCreateNetworkRequest']
export type MaintenanceCreateNetworkResponse = APISchemas['MaintenanceCreateNetworkResponse']
export type MaintenanceDeleteNetworkResponse = APISchemas['MaintenanceDeleteNetworkResponse']
export type MaintenanceVolumeSource = APISchemas['MaintenanceVolumeItem']['source']
export type MaintenanceVolumeItem = APISchemas['MaintenanceVolumeItem']
export type MaintenanceVolumesResponse = APISchemas['MaintenanceVolumesResponse']
export type MaintenanceCreateVolumeRequest = APISchemas['MaintenanceCreateVolumeRequest']
export type MaintenanceCreateVolumeResponse = APISchemas['MaintenanceCreateVolumeResponse']
export type MaintenanceDeleteVolumeResponse = APISchemas['MaintenanceDeleteVolumeResponse']
export type MaintenancePrunePreviewItem = APISchemas['MaintenancePrunePreviewItem']
export type MaintenancePrunePreviewCategory = APISchemas['MaintenancePrunePreviewCategory']
export type MaintenancePrunePreviewResponse = APISchemas['MaintenancePrunePreviewResponse']
export type MaintenancePruneRequest = APISchemas['MaintenancePruneRequest']

export interface ApiError {
  error: {
    code: string
    message: string
    details?: Record<string, unknown>
  }
}
