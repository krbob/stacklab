import type {
  ActiveJobsResponse,
  AuditResponse,
  ConfigFileResponse,
  ConfigFileSaveRequest,
  ConfigFileSaveResponse,
  ConfigRepairPermissionsRequest,
  ConfigRepairPermissionsResponse,
  ConfigTreeResponse,
  CreateStackRequest,
  DefinitionResponse,
  DeleteStackRequest,
  DockerAdminOverviewResponse,
  DockerDaemonConfigResponse,
  DockerDaemonApplyRequest,
  DockerDaemonValidateRequest,
  DockerDaemonValidateResponse,
  DockerRegistryLoginRequest,
  DockerRegistryLogoutRequest,
  DockerRegistryStatusResponse,
  StacklabUpdateApplyRequest,
  StacklabUpdateApplyResponse,
  StacklabUpdateOverviewResponse,
  GitCommitRequest,
  GitCommitResponse,
  GitDiffResponse,
  GitPushResponse,
  GitWorkspaceStatusResponse,
  HealthResponse,
  LivenessResponse,
  HostMetricsResponse,
  HostOverviewResponse,
  HostSettingsResponse,
  HostSettingsUpdateRequest,
  JobDetailResponse,
  JobEnvelopeResponse,
  JobEventsResponse,
  ImageUpdatesResponse,
  LoginRequest,
  LoginResponse,
  LogoutResponse,
  MaintenanceCreateNetworkRequest,
  MaintenanceCreateNetworkResponse,
  MaintenanceSchedulesResponse,
  MaintenanceSchedulesUpdateRequest,
  MaintenanceCreateVolumeRequest,
  MaintenanceCreateVolumeResponse,
  MaintenanceDeleteNetworkResponse,
  MaintenanceDeleteVolumeResponse,
  MaintenanceUpdateStacksRequest,
  MaintenanceImagesResponse,
  MaintenanceNetworksResponse,
  MaintenancePrunePreviewResponse,
  MaintenancePruneRequest,
  MaintenanceVolumesResponse,
  MetaResponse,
  NotificationSettingsResponse,
  NotificationSettingsUpdateRequest,
  NotificationTestRequest,
  NotificationTestResponse,
  ResolvedConfigPreviewRequest,
  ResolvedConfigResponse,
  SessionResponse,
  StackDetailResponse,
  StackAction,
  StackWorkspaceFileResponse,
  StackWorkspaceFileSaveRequest,
  StackWorkspaceFileSaveResponse,
  StackRepairPermissionsRequest,
  StackRepairPermissionsResponse,
  StackWorkspaceTreeResponse,
  StackListResponse,
  StacklabLogsResponse,
  TemplatesResponse,
  UpdateDefinitionRequest,
  UpdateDefinitionResponse,
  UpdatePasswordRequest,
  UpdatePasswordResponse,
} from '@/lib/api-types'
import type { operations } from './api-contract.generated'

type HostMetricsQueryParams = NonNullable<operations['getHostMetrics']['parameters']['query']>
type StacklabLogsQueryParams = NonNullable<operations['getStacklabLogs']['parameters']['query']>
type ConfigWorkspaceTreeQueryParams = NonNullable<operations['getConfigWorkspaceTree']['parameters']['query']>
type ConfigWorkspaceFileQueryParams = NonNullable<operations['getConfigWorkspaceFile']['parameters']['query']>
type GitWorkspaceDiffQueryParams = NonNullable<operations['getGitWorkspaceDiff']['parameters']['query']>
type StackListQueryParams = NonNullable<operations['listStacks']['parameters']['query']>
type StackWorkspaceTreeQueryParams = NonNullable<operations['getStackWorkspaceTree']['parameters']['query']>
type ResolvedConfigQueryParams = NonNullable<operations['getResolvedConfig']['parameters']['query']>
export type AuditQueryParams = NonNullable<operations['listStackAudit']['parameters']['query']>
type GlobalAuditQueryParams = NonNullable<operations['listAudit']['parameters']['query']>
type MaintenanceImagesQueryParams = NonNullable<operations['getMaintenanceImages']['parameters']['query']>
type MaintenanceNetworksQueryParams = NonNullable<operations['getMaintenanceNetworks']['parameters']['query']>
type MaintenanceVolumesQueryParams = NonNullable<operations['getMaintenanceVolumes']['parameters']['query']>
type MaintenancePrunePreviewQueryParams = NonNullable<operations['getMaintenancePrunePreview']['parameters']['query']>

class ApiClientError extends Error {
  status: number
  code: string
  details?: Record<string, unknown>
  requestId?: string

  constructor(
    status: number,
    code: string,
    message: string,
    details?: Record<string, unknown>,
    requestId?: string,
  ) {
    super(requestId ? `${message} [Request ID: ${requestId}]` : message)
    this.name = 'ApiClientError'
    this.status = status
    this.code = code
    this.details = details
    this.requestId = requestId
  }
}

interface RequestPolicy {
  acceptedStatuses?: readonly number[]
}

async function request<T>(path: string, init?: RequestInit, policy?: RequestPolicy): Promise<T> {
  const method = (init?.method ?? 'GET').toUpperCase()
  const res = await fetch(path, {
    ...init,
    cache: init?.cache ?? (method === 'GET' || method === 'HEAD' ? 'no-store' : undefined),
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      ...init?.headers,
    },
  })

  if (!res.ok && !policy?.acceptedStatuses?.includes(res.status)) {
    let code = 'unknown'
    let message = res.statusText || `Request failed with status ${res.status}`
    let details: Record<string, unknown> | undefined
    const requestId = res.headers?.get?.('X-Request-ID') || undefined

    try {
      const body = await res.json()
      if (body?.error) {
        code = body.error.code ?? code
        message = body.error.message ?? message
        details = body.error.details
      }
    } catch {
      // response body not JSON, use defaults
    }

    throw new ApiClientError(res.status, code, message, details, requestId)
  }

  return res.json() as Promise<T>
}

// --- Read endpoints ---

export function getHealth(signal?: AbortSignal): Promise<HealthResponse> {
  return request('/api/health', { signal }, { acceptedStatuses: [503] })
}

export function getLiveness(): Promise<LivenessResponse> {
  return request('/api/live')
}

export function getReadiness(signal?: AbortSignal): Promise<HealthResponse> {
  return request('/api/ready', { signal }, { acceptedStatuses: [503] })
}

export function getSession(): Promise<SessionResponse> {
  return request('/api/session')
}

export function getMeta(): Promise<MetaResponse> {
  return request('/api/meta')
}

export function getNotificationSettings(): Promise<NotificationSettingsResponse> {
  return request('/api/settings/notifications')
}

export function updateNotificationSettings(requestBody: NotificationSettingsUpdateRequest): Promise<NotificationSettingsResponse> {
  return request('/api/settings/notifications', {
    method: 'PUT',
    body: JSON.stringify(requestBody),
  })
}

export function sendNotificationTest(requestBody: NotificationTestRequest): Promise<NotificationTestResponse> {
  return request('/api/settings/notifications/test', {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

export function getMaintenanceSchedules(): Promise<MaintenanceSchedulesResponse> {
  return request('/api/settings/maintenance-schedules')
}

export function updateMaintenanceSchedules(requestBody: MaintenanceSchedulesUpdateRequest): Promise<MaintenanceSchedulesResponse> {
  return request('/api/settings/maintenance-schedules', {
    method: 'PUT',
    body: JSON.stringify(requestBody),
  })
}

export function getHostSettings(): Promise<HostSettingsResponse> {
  return request('/api/settings/host')
}

export function updateHostSettings(requestBody: HostSettingsUpdateRequest): Promise<HostSettingsResponse> {
  return request('/api/settings/host', {
    method: 'PUT',
    body: JSON.stringify(requestBody),
  })
}

export function getHostOverview(): Promise<HostOverviewResponse> {
  return request('/api/host/overview')
}

export function getHostMetrics(params?: HostMetricsQueryParams): Promise<HostMetricsResponse> {
  const search = new URLSearchParams()
  if (params?.since) search.set('since', params.since)
  const qs = search.toString()
  return request(`/api/host/metrics${qs ? `?${qs}` : ''}`)
}

export function getStacklabLogs(params?: StacklabLogsQueryParams): Promise<StacklabLogsResponse> {
  const search = new URLSearchParams()
  if (params?.limit) search.set('limit', String(params.limit))
  if (params?.cursor) search.set('cursor', params.cursor)
  if (params?.level) search.set('level', params.level)
  if (params?.q) search.set('q', params.q)
  if (params?.include_http) search.set('include_http', 'true')
  const qs = search.toString()
  return request(`/api/host/stacklab-logs${qs ? `?${qs}` : ''}`)
}

export function getDockerAdminOverview(): Promise<DockerAdminOverviewResponse> {
  return request('/api/docker/admin/overview')
}

export function getDockerDaemonConfig(): Promise<DockerDaemonConfigResponse> {
  return request('/api/docker/admin/daemon-config')
}

export function validateDockerDaemonConfig(requestBody: DockerDaemonValidateRequest): Promise<DockerDaemonValidateResponse> {
  return request('/api/docker/admin/daemon-config/validate', {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

export function applyDockerDaemonConfig(requestBody: DockerDaemonApplyRequest): Promise<JobEnvelopeResponse> {
  return request('/api/docker/admin/daemon-config/apply', {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

export function getDockerRegistryStatus(): Promise<DockerRegistryStatusResponse> {
  return request('/api/docker/registries')
}

export function loginDockerRegistry(requestBody: DockerRegistryLoginRequest): Promise<JobEnvelopeResponse> {
  return request('/api/docker/registries/login', {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

export function logoutDockerRegistry(requestBody: DockerRegistryLogoutRequest): Promise<JobEnvelopeResponse> {
  return request('/api/docker/registries/logout', {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

export function getStacklabUpdateOverview(): Promise<StacklabUpdateOverviewResponse> {
  return request('/api/stacklab/update/overview')
}

export function applyStacklabUpdate(requestBody: StacklabUpdateApplyRequest): Promise<StacklabUpdateApplyResponse> {
  return request('/api/stacklab/update/apply', {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

// --- Git workspace ---

export function getGitWorkspaceStatus(): Promise<GitWorkspaceStatusResponse> {
  return request('/api/git/workspace/status')
}

export function getGitWorkspaceDiff(path: GitWorkspaceDiffQueryParams['path']): Promise<GitDiffResponse> {
  return request(`/api/git/workspace/diff?path=${encodeURIComponent(path)}`)
}

export function commitGitWorkspace(requestBody: GitCommitRequest): Promise<GitCommitResponse> {
  return request('/api/git/workspace/commit', {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

export function pushGitWorkspace(): Promise<GitPushResponse> {
  return request('/api/git/workspace/push', {
    method: 'POST',
  })
}

// --- Config workspace ---

export function getConfigTree(path?: ConfigWorkspaceTreeQueryParams['path']): Promise<ConfigTreeResponse> {
  const search = new URLSearchParams()
  if (path) search.set('path', path)
  const qs = search.toString()
  return request(`/api/config/workspace/tree${qs ? `?${qs}` : ''}`)
}

export function getConfigFile(path: ConfigWorkspaceFileQueryParams['path']): Promise<ConfigFileResponse> {
  return request(`/api/config/workspace/file?path=${encodeURIComponent(path)}`)
}

export function saveConfigFile(path: string, content: string, createParentDirectories = false, expectedModifiedAt?: string): Promise<ConfigFileSaveResponse> {
  const requestBody: ConfigFileSaveRequest = {
    path,
    content,
    create_parent_directories: createParentDirectories,
    expected_modified_at: expectedModifiedAt,
  }
  return request('/api/config/workspace/file', {
    method: 'PUT',
    body: JSON.stringify(requestBody),
  })
}

export function repairConfigWorkspacePermissions(requestBody: ConfigRepairPermissionsRequest): Promise<ConfigRepairPermissionsResponse> {
  return request('/api/config/workspace/repair-permissions', {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

// --- Stack management ---

export function getStacks(params?: StackListQueryParams): Promise<StackListResponse> {
  const search = new URLSearchParams()
  if (params?.q) search.set('q', params.q)
  if (params?.sort) search.set('sort', params.sort)
  const qs = search.toString()
  return request(`/api/stacks${qs ? `?${qs}` : ''}`)
}

export function getTemplates(): Promise<TemplatesResponse> {
  return request('/api/templates')
}

export function getImageUpdates(): Promise<ImageUpdatesResponse> {
  return request('/api/maintenance/image-updates')
}

export function checkImageUpdates(): Promise<JobEnvelopeResponse> {
  return request('/api/maintenance/image-updates/check', { method: 'POST', body: JSON.stringify({}) })
}

export function getStack(stackId: string): Promise<StackDetailResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}`)
}

export function getDefinition(stackId: string): Promise<DefinitionResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/definition`)
}

export function getStackWorkspaceTree(stackId: string, path?: StackWorkspaceTreeQueryParams['path']): Promise<StackWorkspaceTreeResponse> {
  const search = new URLSearchParams()
  if (path) search.set('path', path)
  const qs = search.toString()
  return request(`/api/stacks/${encodeURIComponent(stackId)}/workspace/tree${qs ? `?${qs}` : ''}`)
}

export function getStackWorkspaceFile(stackId: string, path: string): Promise<StackWorkspaceFileResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/workspace/file?path=${encodeURIComponent(path)}`)
}

export function saveStackWorkspaceFile(
  stackId: string,
  path: string,
  content: string,
  createParentDirectories = false,
  expectedModifiedAt?: string,
): Promise<StackWorkspaceFileSaveResponse> {
  const requestBody: StackWorkspaceFileSaveRequest = {
    path,
    content,
    create_parent_directories: createParentDirectories,
    expected_modified_at: expectedModifiedAt,
  }
  return request(`/api/stacks/${encodeURIComponent(stackId)}/workspace/file`, {
    method: 'PUT',
    body: JSON.stringify(requestBody),
  })
}

export function repairStackWorkspacePermissions(
  stackId: string,
  requestBody: StackRepairPermissionsRequest,
): Promise<StackRepairPermissionsResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/workspace/repair-permissions`, {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

export function getResolvedConfig(stackId: string, source?: ResolvedConfigQueryParams['source']): Promise<ResolvedConfigResponse> {
  const search = new URLSearchParams()
  if (source) search.set('source', source)
  const qs = search.toString()
  return request(`/api/stacks/${encodeURIComponent(stackId)}/resolved-config${qs ? `?${qs}` : ''}`)
}

export function getStackAudit(stackId: string, params?: AuditQueryParams, signal?: AbortSignal): Promise<AuditResponse> {
  const search = new URLSearchParams()
  if (params?.cursor) search.set('cursor', params.cursor)
  if (params?.limit) search.set('limit', String(params.limit))
  if (params?.q) search.set('q', params.q)
  if (params?.result) search.set('result', params.result)
  if (params?.from) search.set('from', params.from)
  if (params?.to) search.set('to', params.to)
  const qs = search.toString()
  return request(`/api/stacks/${encodeURIComponent(stackId)}/audit${qs ? `?${qs}` : ''}`, { signal })
}

export function getGlobalAudit(params?: GlobalAuditQueryParams, signal?: AbortSignal): Promise<AuditResponse> {
  const search = new URLSearchParams()
  if (params?.stack_id) search.set('stack_id', params.stack_id)
  if (params?.cursor) search.set('cursor', params.cursor)
  if (params?.limit) search.set('limit', String(params.limit))
  if (params?.q) search.set('q', params.q)
  if (params?.result) search.set('result', params.result)
  if (params?.from) search.set('from', params.from)
  if (params?.to) search.set('to', params.to)
  const qs = search.toString()
  return request(`/api/audit${qs ? `?${qs}` : ''}`, { signal })
}

export function getJob(jobId: string): Promise<JobDetailResponse> {
  return request(`/api/jobs/${encodeURIComponent(jobId)}`)
}

export function getJobEvents(jobId: string): Promise<JobEventsResponse> {
  return request(`/api/jobs/${encodeURIComponent(jobId)}/events`)
}

export function getActiveJobs(): Promise<ActiveJobsResponse> {
  return request('/api/jobs/active')
}

// --- Mutating endpoints ---

export function login(password: string): Promise<LoginResponse> {
  const requestBody: LoginRequest = { password }
  return request('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

export function logout(): Promise<LogoutResponse> {
  return request('/api/auth/logout', { method: 'POST' })
}

export function createStack(payload: CreateStackRequest): Promise<JobEnvelopeResponse> {
  return request('/api/stacks', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function saveDefinition(stackId: string, payload: UpdateDefinitionRequest): Promise<UpdateDefinitionResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/definition`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function resolveConfigDraft(stackId: string, payload: ResolvedConfigPreviewRequest): Promise<ResolvedConfigResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/resolved-config`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function invokeAction(stackId: string, action: StackAction): Promise<JobEnvelopeResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/actions/${encodeURIComponent(action)}`, {
    method: 'POST',
    body: JSON.stringify({}),
  })
}

export function deleteStack(stackId: string, flags: DeleteStackRequest): Promise<JobEnvelopeResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}`, {
    method: 'DELETE',
    body: JSON.stringify(flags),
  })
}

export function updateStacksMaintenance(payload: MaintenanceUpdateStacksRequest): Promise<JobEnvelopeResponse> {
  return request('/api/maintenance/update-stacks', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getMaintenanceImages(params?: MaintenanceImagesQueryParams): Promise<MaintenanceImagesResponse> {
  const search = new URLSearchParams()
  if (params?.q) search.set('q', params.q)
  if (params?.usage) search.set('usage', params.usage)
  if (params?.origin) search.set('origin', params.origin)
  const qs = search.toString()
  return request(`/api/maintenance/images${qs ? `?${qs}` : ''}`)
}

export function getMaintenanceNetworks(params?: MaintenanceNetworksQueryParams): Promise<MaintenanceNetworksResponse> {
  const search = new URLSearchParams()
  if (params?.q) search.set('q', params.q)
  if (params?.usage) search.set('usage', params.usage)
  if (params?.origin) search.set('origin', params.origin)
  const qs = search.toString()
  return request(`/api/maintenance/networks${qs ? `?${qs}` : ''}`)
}

export function createMaintenanceNetwork(payload: MaintenanceCreateNetworkRequest): Promise<MaintenanceCreateNetworkResponse> {
  return request('/api/maintenance/networks', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function deleteMaintenanceNetwork(name: string): Promise<MaintenanceDeleteNetworkResponse> {
  return request(`/api/maintenance/networks/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
}

export function getMaintenanceVolumes(params?: MaintenanceVolumesQueryParams): Promise<MaintenanceVolumesResponse> {
  const search = new URLSearchParams()
  if (params?.q) search.set('q', params.q)
  if (params?.usage) search.set('usage', params.usage)
  if (params?.origin) search.set('origin', params.origin)
  const qs = search.toString()
  return request(`/api/maintenance/volumes${qs ? `?${qs}` : ''}`)
}

export function createMaintenanceVolume(payload: MaintenanceCreateVolumeRequest): Promise<MaintenanceCreateVolumeResponse> {
  return request('/api/maintenance/volumes', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function deleteMaintenanceVolume(name: string): Promise<MaintenanceDeleteVolumeResponse> {
  return request(`/api/maintenance/volumes/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
}

export function getMaintenancePrunePreview(params?: MaintenancePrunePreviewQueryParams): Promise<MaintenancePrunePreviewResponse> {
  const search = new URLSearchParams()
  if (params?.images !== undefined) search.set('images', String(params.images))
  if (params?.build_cache !== undefined) search.set('build_cache', String(params.build_cache))
  if (params?.stopped_containers !== undefined) search.set('stopped_containers', String(params.stopped_containers))
  if (params?.volumes !== undefined) search.set('volumes', String(params.volumes))
  const qs = search.toString()
  return request(`/api/maintenance/prune-preview${qs ? `?${qs}` : ''}`)
}

export function runMaintenancePrune(payload: MaintenancePruneRequest): Promise<JobEnvelopeResponse> {
  return request('/api/maintenance/prune', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function changePassword(currentPassword: string, newPassword: string): Promise<UpdatePasswordResponse> {
  const requestBody: UpdatePasswordRequest = { current_password: currentPassword, new_password: newPassword }
  return request('/api/settings/password', {
    method: 'POST',
    body: JSON.stringify(requestBody),
  })
}

export function cancelJob(jobId: string): Promise<JobEnvelopeResponse> {
  return request(`/api/jobs/${encodeURIComponent(jobId)}/cancel`, { method: 'POST' })
}

export { ApiClientError }
