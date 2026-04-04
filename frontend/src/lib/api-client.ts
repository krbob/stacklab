import type {
  AuditResponse,
  ConfigFileResponse,
  ConfigFileSaveResponse,
  ConfigTreeResponse,
  DefinitionResponse,
  GitDiffResponse,
  GitWorkspaceStatusResponse,
  HealthResponse,
  HostOverviewResponse,
  JobDetail,
  JobRef,
  MaintenanceUpdateStacksRequest,
  MetaResponse,
  ResolvedConfigResponse,
  SessionResponse,
  StackDetailResponse,
  StackListResponse,
  StacklabLogsResponse,
} from '@/lib/api-types'

class ApiClientError extends Error {
  status: number
  code: string
  details?: Record<string, unknown>

  constructor(
    status: number,
    code: string,
    message: string,
    details?: Record<string, unknown>,
  ) {
    super(message)
    this.name = 'ApiClientError'
    this.status = status
    this.code = code
    this.details = details
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      ...init?.headers,
    },
  })

  if (!res.ok) {
    let code = 'unknown'
    let message = res.statusText
    let details: Record<string, unknown> | undefined

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

    throw new ApiClientError(res.status, code, message, details)
  }

  return res.json() as Promise<T>
}

// --- Read endpoints ---

export function getHealth(): Promise<HealthResponse> {
  return request('/api/health')
}

export function getSession(): Promise<SessionResponse> {
  return request('/api/session')
}

export function getMeta(): Promise<MetaResponse> {
  return request('/api/meta')
}

export function getHostOverview(): Promise<HostOverviewResponse> {
  return request('/api/host/overview')
}

export function getStacklabLogs(params?: { limit?: number; cursor?: string; level?: string; q?: string }): Promise<StacklabLogsResponse> {
  const search = new URLSearchParams()
  if (params?.limit) search.set('limit', String(params.limit))
  if (params?.cursor) search.set('cursor', params.cursor)
  if (params?.level) search.set('level', params.level)
  if (params?.q) search.set('q', params.q)
  const qs = search.toString()
  return request(`/api/host/stacklab-logs${qs ? `?${qs}` : ''}`)
}

// --- Git workspace ---

export function getGitWorkspaceStatus(): Promise<GitWorkspaceStatusResponse> {
  return request('/api/git/workspace/status')
}

export function getGitWorkspaceDiff(path: string): Promise<GitDiffResponse> {
  return request(`/api/git/workspace/diff?path=${encodeURIComponent(path)}`)
}

// --- Config workspace ---

export function getConfigTree(path?: string): Promise<ConfigTreeResponse> {
  const search = new URLSearchParams()
  if (path) search.set('path', path)
  const qs = search.toString()
  return request(`/api/config/workspace/tree${qs ? `?${qs}` : ''}`)
}

export function getConfigFile(path: string): Promise<ConfigFileResponse> {
  return request(`/api/config/workspace/file?path=${encodeURIComponent(path)}`)
}

export function saveConfigFile(path: string, content: string, createParentDirectories = false): Promise<ConfigFileSaveResponse> {
  return request('/api/config/workspace/file', {
    method: 'PUT',
    body: JSON.stringify({ path, content, create_parent_directories: createParentDirectories }),
  })
}

// --- Stack management ---

export function getStacks(params?: { q?: string; sort?: string }): Promise<StackListResponse> {
  const search = new URLSearchParams()
  if (params?.q) search.set('q', params.q)
  if (params?.sort) search.set('sort', params.sort)
  const qs = search.toString()
  return request(`/api/stacks${qs ? `?${qs}` : ''}`)
}

export function getStack(stackId: string): Promise<StackDetailResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}`)
}

export function getDefinition(stackId: string): Promise<DefinitionResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/definition`)
}

export function getResolvedConfig(stackId: string): Promise<ResolvedConfigResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/resolved-config`)
}

export function getStackAudit(stackId: string, params?: { cursor?: string; limit?: number }): Promise<AuditResponse> {
  const search = new URLSearchParams()
  if (params?.cursor) search.set('cursor', params.cursor)
  if (params?.limit) search.set('limit', String(params.limit))
  const qs = search.toString()
  return request(`/api/stacks/${encodeURIComponent(stackId)}/audit${qs ? `?${qs}` : ''}`)
}

export function getGlobalAudit(params?: { stack_id?: string; cursor?: string; limit?: number }): Promise<AuditResponse> {
  const search = new URLSearchParams()
  if (params?.stack_id) search.set('stack_id', params.stack_id)
  if (params?.cursor) search.set('cursor', params.cursor)
  if (params?.limit) search.set('limit', String(params.limit))
  const qs = search.toString()
  return request(`/api/audit${qs ? `?${qs}` : ''}`)
}

export function getJob(jobId: string): Promise<{ job: JobDetail }> {
  return request(`/api/jobs/${encodeURIComponent(jobId)}`)
}

// --- Mutating endpoints ---

export function login(password: string): Promise<{ authenticated: boolean }> {
  return request('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}

export function logout(): Promise<{ authenticated: boolean }> {
  return request('/api/auth/logout', { method: 'POST' })
}

export function createStack(payload: {
  stack_id: string
  compose_yaml: string
  env: string
  create_config_dir: boolean
  create_data_dir: boolean
  deploy_after_create: boolean
}): Promise<{ job: JobRef }> {
  return request('/api/stacks', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function saveDefinition(stackId: string, payload: {
  compose_yaml: string
  env: string
  validate_after_save: boolean
}): Promise<{ job: JobRef }> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/definition`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function resolveConfigDraft(stackId: string, payload: {
  compose_yaml: string
  env: string
}): Promise<ResolvedConfigResponse> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/resolved-config`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function invokeAction(stackId: string, action: string): Promise<{ job: JobRef }> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}/actions/${encodeURIComponent(action)}`, {
    method: 'POST',
    body: JSON.stringify({}),
  })
}

export function deleteStack(stackId: string, flags: {
  remove_runtime: boolean
  remove_definition: boolean
  remove_config: boolean
  remove_data: boolean
}): Promise<{ job: JobRef }> {
  return request(`/api/stacks/${encodeURIComponent(stackId)}`, {
    method: 'DELETE',
    body: JSON.stringify(flags),
  })
}

export function updateStacksMaintenance(payload: MaintenanceUpdateStacksRequest): Promise<{ job: JobRef }> {
  return request('/api/maintenance/update-stacks', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function changePassword(currentPassword: string, newPassword: string): Promise<{ updated: boolean }> {
  return request('/api/settings/password', {
    method: 'POST',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  })
}

export function cancelJob(jobId: string): Promise<{ job: { id: string; state: string } }> {
  return request(`/api/jobs/${encodeURIComponent(jobId)}/cancel`, { method: 'POST' })
}

export { ApiClientError }
