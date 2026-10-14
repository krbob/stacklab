import { request } from '@/lib/api-client'
import type { components, operations } from '@/lib/api-contract.generated'

export type BackupDestination = components['schemas']['BackupDestination']
export type BackupSecrets = components['schemas']['BackupSecrets']
export type BackupDestinationInput = components['schemas']['BackupDestinationInput']
export type BackupSource = components['schemas']['BackupSource']
export type BackupAssignment = components['schemas']['BackupAssignment']
export type BackupPlan = components['schemas']['BackupPlan']
export type BackupCopy = components['schemas']['BackupCopyResult']
export type BackupRun = components['schemas']['BackupRun']
export type BackupSnapshot = components['schemas']['BackupSnapshot']
export type BackupOverview = components['schemas']['BackupOverview']
export type BackupManifest = components['schemas']['BackupManifest']
export type BackupRestoreRequest = components['schemas']['BackupRestoreRequest']
export type BackupRestoreReview = components['schemas']['BackupRestoreReview']
export type BackupRestoreMapping = components['schemas']['BackupRestoreMapping']
export type BackupJobResponse = components['schemas']['BackupJobResponse']
export type BackupPolicy = components['schemas']['BackupPolicy']
export type BackupPolicyInput = components['schemas']['BackupPolicyInput']
export type BackupRecoveryJournal = components['schemas']['BackupRecoveryJournal']
export type BackupRecoveryResolution = components['schemas']['BackupRecoveryResolution']
const base = '/api/backups'
const encode = encodeURIComponent
const post = (body: unknown = {}) => ({ method: 'POST', body: JSON.stringify(body) })

export const getBackupFilesystem = (path: string) =>
  request<{ device: string }>(`${base}/filesystem?path=${encode(path)}`)
export const getBackupRestoreManifest = (path: string) =>
  request<{ manifest: BackupManifest }>(`${base}/restore-manifest?path=${encode(path)}`)
export const previewBackupRestore = (id: string, input: BackupRestoreRequest) =>
  request<BackupRestoreReview>(`${base}/destinations/${encode(id)}/restore-preview`, post(input))
export const getBackups = (signal?: AbortSignal) => request<BackupOverview>(base, { signal })
export const getBackupPolicy = (signal?: AbortSignal) => request<BackupPolicy>(`${base}/policy`, { signal })
export const saveBackupPolicy = (input: BackupPolicyInput) =>
  request<BackupPolicy>(`${base}/policy`, { ...post(input), method: 'PUT' })
export const getBackupRecoveryJournals = (signal?: AbortSignal) =>
  request<components['schemas']['BackupRecoveryJournals']>(`${base}/recovery`, { signal })
export const resolveBackupRecovery = (id: string, input: BackupRecoveryResolution) =>
  request<BackupJobResponse>(`${base}/recovery/${encode(id)}`, post(input))
export const getBackupSources = (stackId?: string, signal?: AbortSignal) =>
  request<components['schemas']['BackupSourcesResponse']>(`${base}/sources${stackId ? `?stack_id=${encode(stackId)}` : ''}`, { signal })
export const getStackBackups = (stackId: string, signal?: AbortSignal) =>
  request<components['schemas']['BackupCoverageResponse']>(`/api/stacks/${encode(stackId)}/backups`, { signal })
export const saveBackupDestination = (input: BackupDestinationInput, id?: string) =>
  request<components['schemas']['BackupDestinationResponse']>(`${base}/destinations${id ? `/${encode(id)}` : ''}`, {
    ...post(input),
    method: id ? 'PUT' : 'POST',
  })
export const deleteBackupDestination = (id: string) =>
  request<{ deleted: boolean }>(`${base}/destinations/${encode(id)}`, { method: 'DELETE' })
export const testBackupDestination = (id: string) =>
  request<BackupJobResponse>(`${base}/destinations/${encode(id)}/test`, post())
export const initializeBackupDestination = (id: string) =>
  request<BackupJobResponse>(`${base}/destinations/${encode(id)}/initialize`, post())
export const checkBackupDestination = (id: string, readData: 'metadata' | 'subset' | 'all') =>
  request<BackupJobResponse>(`${base}/destinations/${encode(id)}/check`, post({ read_data: readData }))
export const getBackupSnapshots = (id: string, signal?: AbortSignal) =>
  request<components['schemas']['BackupSnapshotsResponse']>(`${base}/destinations/${encode(id)}/snapshots`, { signal })
export const restoreBackupSnapshot = (id: string, input: BackupRestoreRequest) =>
  request<BackupJobResponse>(`${base}/destinations/${encode(id)}/restore`, post(input))
export const saveBackupPlan = (input: Omit<BackupPlan, 'id'>, id?: string) =>
  request<components['schemas']['BackupPlanResponse']>(`${base}/plans${id ? `/${encode(id)}` : ''}`, {
    ...post(input),
    method: id ? 'PUT' : 'POST',
  })
export const deleteBackupPlan = (id: string) =>
  request<{ deleted: boolean }>(`${base}/plans/${encode(id)}`, { method: 'DELETE' })
export const runBackupPlan = (id: string, destinationIds?: string[]) =>
  request<BackupJobResponse>(`${base}/plans/${encode(id)}/run`, post({ destination_ids: destinationIds }))
export const retryBackupRun = (id: string, destinationIds?: string[]) =>
  request<BackupJobResponse>(`${base}/runs/${encode(id)}/retry`, post({ destination_ids: destinationIds }))

export type BackupSchedule = components['schemas']['BackupSchedule']
export type BackupProtection = components['schemas']['BackupPlanProtection']
export type BackupCoverage = components['schemas']['BackupGlobalCoverageResponse']
export type BackupPreflight = components['schemas']['BackupPreflight']
export type BackupRestoreResult = components['schemas']['BackupRestoreResult']
export type BackupFile = components['schemas']['BackupFileEntry']
export type BackupCatalog = components['schemas']['BackupCatalogPage']
export type BackupFileChange = components['schemas']['BackupFileChange']
export type BackupComparison = components['schemas']['BackupFileComparison']
export type BackupCatalogQuery = NonNullable<operations['getBackupSnapshotFiles']['parameters']['query']>

const query = (value: Record<string, string | number | undefined>) =>
  new URLSearchParams(
    Object.entries(value)
      .filter(([, item]) => item !== undefined)
      .map(([key, item]) => [key, String(item)]),
  ).toString()
export const getBackupCoverage = (signal?: AbortSignal) => request<BackupCoverage>(`${base}/coverage`, { signal })
export const getBackupPreflight = (id: string, signal?: AbortSignal) =>
  request<BackupPreflight>(`${base}/plans/${encode(id)}/preflight`, { signal })
export const previewBackupSchedule = (schedule: BackupSchedule, signal?: AbortSignal) =>
  request<components['schemas']['BackupSchedulePreview']>(`${base}/schedule-preview`, {
    ...post({ schedule, count: 3 }),
    signal,
  })
export const pruneBackupDestination = (id: string, acknowledgeIntegrityFailure = false) =>
  request<BackupJobResponse>(`${base}/destinations/${encode(id)}/prune`, post(acknowledgeIntegrityFailure ? { acknowledge_integrity_failure: true } : {}))
export const unlockBackupDestination = (id: string) =>
  request<BackupJobResponse>(`${base}/destinations/${encode(id)}/unlock`, post())
export const getBackupRestores = (signal?: AbortSignal) =>
  request<components['schemas']['BackupRestoreLibraryResponse']>(`${base}/restores`, { signal })
export const deleteBackupRestore = (id: string) => request<void>(`${base}/restores/${encode(id)}`, { method: 'DELETE' })
export const backupArchiveURL = (id: string, sourceId: string) =>
  `${base}/restores/${encode(id)}/archive?${query({ source_id: sourceId })}`
export const getBackupCatalog = (id: string, snapshotId: string, params: BackupCatalogQuery, signal?: AbortSignal) =>
  request<BackupCatalog>(`${base}/destinations/${encode(id)}/snapshots/${encode(snapshotId)}/files?${query(params)}`, {
    signal,
  })
export const getBackupRestoreCatalog = (id: string, params: BackupCatalogQuery, signal?: AbortSignal) =>
  request<BackupCatalog>(`${base}/restores/${encode(id)}/files?${query(params)}`, { signal })
export const compareBackupSnapshots = (
  id: string,
  snapshotId: string,
  baselineId: string,
  sourceId: string,
  offset = 0,
  signal?: AbortSignal,
) =>
  request<BackupComparison>(
    `${base}/destinations/${encode(id)}/snapshots/${encode(snapshotId)}/compare?${query({ base_snapshot_id: baselineId, source_id: sourceId, offset, limit: 50 })}`,
    { signal },
  )
export const backupDownloadURL = (id: string, sourceId: string, path: string) =>
  `${base}/restores/${encode(id)}/download?${query({ source_id: sourceId, path })}`
