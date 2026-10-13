import { request } from '@/lib/api-client'
import type { components } from '@/lib/api-contract.generated'

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
export type BackupJobResponse = components['schemas']['BackupJobResponse'] & { run?: BackupRun }
const base = '/api/backups'
const encode = encodeURIComponent
const post = (body: unknown = {}) => ({ method: 'POST', body: JSON.stringify(body) })

export const getBackupFilesystem = (path: string) => request<{ device: string }>(`${base}/filesystem?path=${encode(path)}`)
export const getBackupRestoreManifest = (path: string) => request<{ manifest: BackupManifest }>(`${base}/restore-manifest?path=${encode(path)}`)
export const previewBackupRestore = (id: string, input: BackupRestoreRequest) => request<BackupRestoreReview>(`${base}/destinations/${encode(id)}/restore-preview`, post(input))
export const getBackups = (signal?: AbortSignal) => request<BackupOverview>(base, { signal })
export const getBackupSources = (stackId?: string) => request<{ items: BackupSource[] }>(`${base}/sources${stackId ? `?stack_id=${encode(stackId)}` : ''}`)
export const getStackBackups = (stackId: string) => request<{ sources: BackupSource[]; plans: BackupPlan[] }>(`/api/stacks/${encode(stackId)}/backups`)
export const saveBackupDestination = (input: BackupDestinationInput, id?: string) => request<{ destination: BackupDestination }>(`${base}/destinations${id ? `/${encode(id)}` : ''}`, { ...post(input), method: id ? 'PUT' : 'POST' })
export const deleteBackupDestination = (id: string) => request<{ deleted: boolean }>(`${base}/destinations/${encode(id)}`, { method: 'DELETE' })
export const testBackupDestination = (id: string) => request<BackupJobResponse>(`${base}/destinations/${encode(id)}/test`, post())
export const initializeBackupDestination = (id: string) => request<BackupJobResponse>(`${base}/destinations/${encode(id)}/initialize`, post())
export const checkBackupDestination = (id: string, readData: 'metadata' | 'subset' | 'all') => request<BackupJobResponse>(`${base}/destinations/${encode(id)}/check`, post({ read_data: readData }))
export const getBackupSnapshots = (id: string) => request<{ items: BackupSnapshot[] }>(`${base}/destinations/${encode(id)}/snapshots`)
export const restoreBackupSnapshot = (id: string, input: BackupRestoreRequest) => request<BackupJobResponse>(`${base}/destinations/${encode(id)}/restore`, post(input))
export const saveBackupPlan = (input: Omit<BackupPlan, 'id'>, id?: string) => request<{ plan: BackupPlan }>(`${base}/plans${id ? `/${encode(id)}` : ''}`, { ...post(input), method: id ? 'PUT' : 'POST' })
export const deleteBackupPlan = (id: string) => request<{ deleted: boolean }>(`${base}/plans/${encode(id)}`, { method: 'DELETE' })
export const runBackupPlan = (id: string, destinationIds?: string[]) => request<BackupJobResponse>(`${base}/plans/${encode(id)}/run`, post({ destination_ids: destinationIds }))
export const retryBackupRun = (id: string, destinationIds?: string[]) => request<BackupJobResponse>(`${base}/runs/${encode(id)}/retry`, post({ destination_ids: destinationIds }))
