import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BackupsPage } from './backups-page'
import { DestinationEditor } from './backups/destination-editor'
import { PlanEditor } from './backups/plan-editor'
import { RecoveryBrowser } from './backups/recovery-browser'
import { ApplyRecoveryDialog } from './backups/apply-recovery-dialog'
import * as api from '@/lib/backup-api'
import { getStacks } from '@/lib/api-client'

vi.mock('@/lib/backup-api')
vi.mock('@/lib/api-client', () => ({ getStacks: vi.fn() }))
vi.mock('@/hooks/use-job-stream', () => ({ useJobStream: () => ({ state: null, events: [] }) }))

const destination: api.BackupDestination = { id: 'nas', name: 'NAS', kind: 'local', repository: '/mnt/nas/backups', expected_filesystem: '2049', credentials_configured: true, status: 'ready' }
const source: api.BackupSource = { id: 'data', kind: 'service_mount', stack_id: 'demo', service: 'db', mount: '/data', path: 'demo_data' }
const plan: api.BackupPlan = { id: 'daily', name: 'Application data', enabled: true, consistency: 'stop', sources: [source], destinations: [{ destination_id: 'nas', enabled: true, schedule: { frequency: 'manual' }, retention: { daily: 7, weekly: 4, monthly: 12 } }], staging_max_bytes: 1024 ** 3, staging_ttl_hours: 24, stop_timeout_seconds: 60 }
const overview: api.BackupOverview = { destinations: [destination, { ...destination, id: 'cloud', name: 'Cloud', kind: 's3' }], plans: [plan], runs: [], restic_available: true, rclone_available: true, staging_directory: '/state/staging' }
const jobResponse = { job: { id: 'job-1' } } as api.BackupJobResponse

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.getBackups).mockResolvedValue(overview)
  vi.mocked(api.getBackupSources).mockResolvedValue({ items: [source] })
  vi.mocked(getStacks).mockResolvedValue({ items: [], summary: { container_count: { running: 0, total: 0 }, defined_count: 0, error_count: 0, orphaned_count: 0, running_count: 0, stack_count: 0, stopped_count: 0 } })
})

function renderPage(view: string) {
  return render(<MemoryRouter initialEntries={[`/backups?view=${view}`]}><BackupsPage /></MemoryRouter>)
}

describe('Backup workflow', () => {
  it('keeps successful copies visible and retries only missing copies', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, runs: [{ id: 'run-1', job_id: 'old-job', plan_id: plan.id, plan_name: plan.name, state: 'failed', capture_state: 'succeeded', created_at: '2026-10-13T12:00:00Z', captured_at: '2026-10-13T12:01:00Z', expires_at: '2099-10-14T12:00:00Z', copies: [{ destination_id: 'nas', state: 'succeeded', snapshot_id: 'abcd', verification: 'snapshot_metadata_verified', retention: 'succeeded' }, { destination_id: 'cloud', state: 'failed', verification: 'pending', retention: 'pending', error: 'Remote unavailable' }] }] })
    vi.mocked(api.retryBackupRun).mockResolvedValue(jobResponse)
    renderPage('runs')
    expect(await screen.findByText('Remote unavailable')).toBeInTheDocument()
    expect(screen.getByText('NAS')).toBeInTheDocument()
    expect(screen.getByText('abcd')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry failed copies' }))
    await waitFor(() => expect(api.retryBackupRun).toHaveBeenCalledWith('run-1', ['cloud']))
  })

  it('requires review of service disruption before starting a capture', async () => {
    vi.mocked(api.runBackupPlan).mockResolvedValue(jobResponse)
    renderPage('plans')
    fireEvent.click(await screen.findByRole('button', { name: 'Back up now' }))
    const dialog = screen.getByRole('dialog')
    const start = within(dialog).getByRole('button', { name: 'Start backup' })
    expect(start).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('checkbox', { name: /I understand that running services/ }))
    fireEvent.click(start)
    await waitFor(() => expect(api.runBackupPlan).toHaveBeenCalledWith(plan.id, ['nas']))
  })

  it('disables retries when the prepared recovery set expired', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, runs: [{ id: 'old', job_id: 'job', plan_id: 'daily', plan_name: 'Old run', state: 'failed', capture_state: 'succeeded', created_at: '2000-01-01T00:00:00Z', expires_at: '2000-01-02T00:00:00Z', copies: [{ destination_id: 'nas', state: 'failed', verification: 'pending', retention: 'pending' }] }] })
    renderPage('runs')
    expect(await screen.findByRole('button', { name: 'Retry failed copies' })).toBeDisabled()
    expect(screen.getByText(/A new capture is required/)).toBeInTheDocument()
  })

  it('keeps repository checks and restore tests visibly separate', async () => {
    vi.mocked(api.checkBackupDestination).mockResolvedValue(jobResponse)
    renderPage('destinations')
    fireEvent.click((await screen.findAllByRole('button', { name: 'Check repository' }))[0])
    fireEvent.change(screen.getByLabelText('Data-read policy'), { target: { value: 'all' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start check' }))
    await waitFor(() => expect(api.checkBackupDestination).toHaveBeenCalledWith('nas', 'all'))
  })
})

describe('Backup configuration and recovery', () => {
  it('does not read or resend saved secrets while editing a destination', async () => {
    vi.mocked(api.saveBackupDestination).mockResolvedValue({ destination })
    const saved = vi.fn()
    render(<DestinationEditor destination={destination} onSaved={saved} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Repository password')).toHaveValue('')
    fireEvent.change(screen.getByLabelText('Destination name'), { target: { value: 'NAS renamed' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save destination' }))
    await waitFor(() => expect(api.saveBackupDestination).toHaveBeenCalledWith(expect.objectContaining({ name: 'NAS renamed', secrets: undefined }), 'nas'))
    expect(saved).toHaveBeenCalled()
  })

  it('requires explicit source selection and preserves exclusions in a plan', async () => {
    vi.mocked(api.saveBackupPlan).mockResolvedValue({ plan })
    render(<PlanEditor destinations={[destination]} initialStackId="demo" onSaved={vi.fn()} onClose={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Plan name'), { target: { value: 'Database' } })
    const data = await screen.findByRole('checkbox', { name: /db → \/data/ })
    expect(data).not.toBeChecked()
    fireEvent.click(data)
    fireEvent.click(screen.getByRole('checkbox', { name: 'NAS' }))
    fireEvent.change(screen.getByLabelText('Exclusions for db /data'), { target: { value: 'cache\ntmp' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save plan' }))
    await waitFor(() => expect(api.saveBackupPlan).toHaveBeenCalledWith(expect.objectContaining({ consistency: 'stop', sources: [{ ...source, excludes: ['cache', 'tmp'] }], destinations: [expect.objectContaining({ destination_id: 'nas', schedule: expect.objectContaining({ frequency: 'manual' }) })] }), undefined))
  })

  it('defaults restoration to a complete isolated test with no live destination', async () => {
    vi.mocked(api.getBackupSnapshots).mockResolvedValue({ items: [{ id: 'abcdef12', short_id: 'abcdef12', time: '2026-10-13T12:00:00Z', paths: ['/'], tags: ['stacklab'], hostname: 'host' }] })
    vi.mocked(api.restoreBackupSnapshot).mockResolvedValue(jobResponse)
    render(<RecoveryBrowser destination={destination} onClose={vi.fn()} onStarted={vi.fn()} />)
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'abcdef12' } })
    expect(screen.getByLabelText('Separate restore directory (optional)')).toHaveValue('')
    expect(screen.getByRole('checkbox', { name: 'Record a complete isolated restore test' })).toBeChecked()
    fireEvent.click(screen.getByRole('button', { name: 'Start isolated restore test' }))
    await waitFor(() => expect(api.restoreBackupSnapshot).toHaveBeenCalledWith('nas', { snapshot_id: 'abcdef12', target: undefined, includes: [], test: true }))
  })

  it('does not label a selective restore as a complete restore test', async () => {
    vi.mocked(api.getBackupSnapshots).mockResolvedValue({ items: [{ id: 'abcdef12', short_id: 'abcdef12', time: '2026-10-13T12:00:00Z', paths: ['/'], tags: [], hostname: '' }] })
    render(<RecoveryBrowser destination={destination} onClose={vi.fn()} onStarted={vi.fn()} />)
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'abcdef12' } })
    fireEvent.change(screen.getByLabelText('Snapshot paths to include (optional)'), { target: { value: '/sources/data.tar' } })
    expect(screen.getByRole('checkbox', { name: 'Record a complete isolated restore test' })).not.toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'Record a complete isolated restore test' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Restore to separate directory' })).toBeEnabled()
  })

  it('requires explicit target mapping, server preview and typed confirmation before applying data', async () => {
    vi.mocked(api.getBackupRestoreManifest).mockResolvedValue({ manifest: { version: 1, run_id: 'run-1', plan_id: 'daily', captured_at: '2026-10-13T12:00:00Z', consistency: 'stop', stacklab_version: 'dev', sources: [{ source, saved_path: 'sources/data.tar', original_path: '/old/data', archive: true, size_bytes: 42 }] } })
    vi.mocked(api.previewBackupRestore).mockResolvedValue({ review_token: 'bound-review', expires_at: '2099-10-13T12:00:00Z', preview: { mappings: [{ source_id: 'data', target: '/recovered/data' }], writers: [{ container_id: 'container', stack_id: 'demo', service: 'db', running: true }], warnings: ['Unrelated files remain.'], overwrites: true } })
    vi.mocked(api.restoreBackupSnapshot).mockResolvedValue(jobResponse)
    render(<ApplyRecoveryDialog destination={destination} onClose={vi.fn()} onStarted={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Completed isolated restore directory'), { target: { value: '/restore/run-1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Load recovery manifest' }))
    fireEvent.click(await screen.findByRole('checkbox', { name: /db → \/data/ }))
    expect(screen.getByLabelText('Target directory for db')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Preview recovery' })).toBeDisabled()
    expect(screen.getByRole('checkbox', { name: 'Restart services that were running before recovery' })).not.toBeChecked()
    fireEvent.change(screen.getByLabelText('Target directory for db'), { target: { value: '/recovered/data' } })
    fireEvent.click(screen.getByRole('button', { name: 'Preview recovery' }))
    expect(await screen.findByText('Stop writer: demo/db')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Apply recovered data' })).toBeDisabled()
    expect(api.restoreBackupSnapshot).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText('Type RESTORE to apply recovered data'), { target: { value: 'RESTORE' } })
    fireEvent.click(screen.getByRole('button', { name: 'Apply recovered data' }))
    await waitFor(() => expect(api.restoreBackupSnapshot).toHaveBeenCalledWith('nas', { restored_directory: '/restore/run-1', mappings: [{ source_id: 'data', target: '/recovered/data' }], resume_services: false, review_token: 'bound-review' }))
  })
})
