import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BackupsPage } from './backups-page'
import { DestinationEditor } from './backups/destination-editor'
import { PlanEditor } from './backups/plan-editor'
import { ApplyRecoveryDialog } from './backups/apply-recovery-dialog'
import * as api from '@/lib/backup-api'
import { getStacks } from '@/lib/api-client'

vi.mock('@/lib/backup-api')
vi.mock('@/lib/api-client', () => ({ getStacks: vi.fn() }))
vi.mock('@/hooks/use-job-stream', () => ({ useJobStream: () => ({ state: null, events: [] }) }))

const destination: api.BackupDestination = {
  id: 'nas',
  name: 'NAS',
  kind: 'local',
  repository: '/mnt/nas/backups',
  expected_filesystem: '2049',
  credentials_configured: true,
  status: 'ready',
  check_schedule: { frequency: 'manual' },
  prune_schedule: { frequency: 'manual' },
  check_read_data: 'metadata',
}
const source: api.BackupSource = {
  id: 'data',
  kind: 'service_mount',
  stack_id: 'demo',
  service: 'db',
  mount: '/data',
  path: 'demo_data',
}
const plan: api.BackupPlan = {
  id: 'daily',
  name: 'Application data',
  enabled: true,
  consistency: 'stop',
  sources: [source],
  destinations: [
    {
      destination_id: 'nas',
      enabled: true,
      schedule: { frequency: 'manual' },
      retention: { daily: 7, weekly: 4, monthly: 12 },
    },
  ],
  staging_max_bytes: 1024 ** 3,
  staging_ttl_hours: 24,
  stop_timeout_seconds: 60,
}
const overview: api.BackupOverview = {
  destinations: [destination, { ...destination, id: 'cloud', name: 'Cloud', kind: 's3' }],
  plans: [plan],
  runs: [],
  protection: [],
  restic_available: true,
  rclone_available: true,
  staging_directory: '/state/staging',
}
const jobResponse = { job: { id: 'job-1' } } as api.BackupJobResponse

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.getBackups).mockResolvedValue(overview)
  vi.mocked(api.getBackupPolicy).mockResolvedValue({
    paused: false,
    host_review_required: false,
    overdue_grace_minutes: 60,
    heartbeat_configured: false,
  })
  vi.mocked(api.getBackupCoverage).mockResolvedValue({ stacks: [] })
  vi.mocked(api.getBackupPreflight).mockResolvedValue({
    sources: [],
    writers: [{ container_id: 'db', stack_id: 'demo', service: 'db', running: true }],
    warnings: [],
  })
  vi.mocked(api.getBackupSources).mockResolvedValue({ items: [source] })
  vi.mocked(getStacks).mockResolvedValue({
    items: [],
    summary: {
      container_count: { running: 0, total: 0 },
      defined_count: 0,
      error_count: 0,
      orphaned_count: 0,
      running_count: 0,
      stack_count: 0,
      stopped_count: 0,
    },
  })
})

function renderPage(view: string) {
  return render(
    <MemoryRouter initialEntries={[`/backups?view=${view}`]}>
      <BackupsPage />
    </MemoryRouter>,
  )
}

describe('Backup workflow', () => {
  it('allows configuration but disables repository commands when restic is unavailable', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, restic_available: false })
    renderPage('destinations')
    for (const action of ['Test access', 'Initialize', 'Check repository', 'Run retention cleanup']) {
      for (const button of await screen.findAllByRole('button', { name: action }))
        expect(button).toBeDisabled()
    }
    expect(screen.getAllByRole('button', { name: 'Edit' })[0]).toBeEnabled()
  })

  it('retains the selected recovery set and history filters when changing tabs', async () => {
    vi.mocked(api.getBackupSnapshots).mockResolvedValue({
      items: [{ id: 'snap', short_id: 'snap', time: '2026-10-01T00:00:00Z', hostname: 'host', tags: [], paths: [] }],
    })
    vi.mocked(api.getBackupRestores).mockResolvedValue({ items: [] })
    vi.mocked(api.getBackupCatalog).mockResolvedValue({
      items: [],
      sources: [],
      path: '',
      total: 0,
      offset: 0,
      limit: 50,
      index_available: false,
      transfer_bytes: 0,
    })
    renderPage('recovery')
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'snap' } })
    fireEvent.click(screen.getByRole('tab', { name: 'History' }))
    fireEvent.change(await screen.findByLabelText('Filter by result'), { target: { value: 'failed' } })
    fireEvent.click(screen.getByRole('tab', { name: 'Recovery' }))
    expect(screen.getByLabelText('Recovery set')).toHaveValue('snap')
    expect(api.getBackupSnapshots).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('tab', { name: 'History' }))
    expect(screen.getByLabelText('Filter by result')).toHaveValue('failed')
  })

  it('keeps an operation error in its dialog and clears it before another action', async () => {
    vi.mocked(api.initializeBackupDestination).mockRejectedValue(new Error('Already initialized'))
    renderPage('destinations')
    fireEvent.click((await screen.findAllByRole('button', { name: 'Initialize' }))[0])
    fireEvent.click(screen.getByRole('button', { name: 'Initialize repository' }))
    expect(await screen.findAllByText('Already initialized')).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getAllByRole('button', { name: 'Remove' })[1])
    expect(screen.queryByText('Already initialized')).not.toBeInTheDocument()
  })

  it('reviews retention cleanup costs before submitting a repository operation', async () => {
    vi.mocked(api.pruneBackupDestination).mockResolvedValue(jobResponse)
    renderPage('destinations')
    fireEvent.click((await screen.findAllByRole('button', { name: 'Run retention cleanup' }))[0])
    expect(api.pruneBackupDestination).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toHaveTextContent('provider transfer charges')
    fireEvent.click(screen.getByRole('button', { name: 'Start cleanup' }))
    await waitFor(() => expect(api.pruneBackupDestination).toHaveBeenCalledWith('nas'))
  })

  it('keeps the current page stable while an overview refresh is pending', async () => {
    renderPage('plans')
    await screen.findByRole('button', { name: 'Back up now' })
    vi.mocked(api.getBackups).mockImplementation(() => new Promise(() => {}))
    const start = screen.getByRole('button', { name: 'Back up now' })
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(screen.queryByText('Refreshing…')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Back up now' })).toBe(start)
  })

  it('keeps successful copies visible and retries only missing copies', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({
      ...overview,
      runs: [
        {
          id: 'run-1',
          job_id: 'old-job',
          plan_id: plan.id,
          plan_name: plan.name,
          state: 'failed',
          phase: 'finished',
          capture_state: 'succeeded',
          created_at: '2026-10-13T12:00:00Z',
          captured_at: '2026-10-13T12:01:00Z',
          expires_at: '2099-10-14T12:00:00Z',
          copies: [
            {
              destination_id: 'nas',
              state: 'succeeded',
              snapshot_id: 'abcd',
              verification: 'snapshot_metadata_verified',
              retention: 'succeeded',
            },
            {
              destination_id: 'cloud',
              state: 'failed',
              verification: 'pending',
              retention: 'pending',
              error: 'Remote unavailable',
            },
          ],
        },
      ],
    })
    vi.mocked(api.retryBackupRun).mockResolvedValue(jobResponse)
    renderPage('runs')
    expect(await screen.findByText('Remote unavailable')).toBeInTheDocument()
    expect(screen.getAllByText('NAS').length).toBeGreaterThan(0)
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
    await within(dialog).findByText('demo / db')
    fireEvent.click(within(dialog).getByRole('checkbox', { name: /I understand that the listed running services/ }))
    fireEvent.click(start)
    await waitFor(() => expect(api.runBackupPlan).toHaveBeenCalledWith(plan.id, ['nas']))
  })

  it('disables retries when the prepared recovery set expired', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({
      ...overview,
      runs: [
        {
          id: 'old',
          job_id: 'job',
          plan_id: 'daily',
          plan_name: 'Old run',
          state: 'failed',
          phase: 'finished',
          capture_state: 'succeeded',
          created_at: '2000-01-01T00:00:00Z',
          expires_at: '2000-01-02T00:00:00Z',
          copies: [{ destination_id: 'nas', state: 'failed', verification: 'pending', retention: 'pending' }],
        },
      ],
    })
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
    await waitFor(() =>
      expect(api.saveBackupDestination).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'NAS renamed', secrets: undefined }),
        'nas',
      ),
    )
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
    await waitFor(() =>
      expect(api.saveBackupPlan).toHaveBeenCalledWith(
        expect.objectContaining({
          consistency: 'stop',
          sources: [{ ...source, excludes: ['cache', 'tmp'] }],
          destinations: [
            expect.objectContaining({
              destination_id: 'nas',
              schedule: expect.objectContaining({ frequency: 'manual' }),
            }),
          ],
        }),
        undefined,
      ),
    )
  })

  it('requires explicit target mapping, server preview and typed confirmation before applying data', async () => {
    vi.mocked(api.getBackupRestoreManifest).mockResolvedValue({
      manifest: {
        version: 1,
        run_id: 'run-1',
        plan_id: 'daily',
        captured_at: '2026-10-13T12:00:00Z',
        consistency: 'stop',
        stacklab_version: 'dev',
        sources: [
          { source, saved_path: 'sources/data.tar', original_path: '/old/data', archive: true, size_bytes: 42 },
        ],
      },
    })
    vi.mocked(api.previewBackupRestore).mockResolvedValue({
      review_token: 'bound-review',
      expires_at: '2099-10-13T12:00:00Z',
      preview: {
        mappings: [{ source_id: 'data', target: '/recovered/data', mode: 'replace' }],
        writers: [{ container_id: 'container', stack_id: 'demo', service: 'db', running: true }],
        warnings: ['Unrelated files remain.'],
        overwrites: true,
      },
    })
    vi.mocked(api.restoreBackupSnapshot).mockResolvedValue(jobResponse)
    render(
      <ApplyRecoveryDialog
        destination={destination}
        restored={{
          id: 'restore',
          destination_id: 'nas',
          snapshot_id: 'snap',
          target: '/restore/run-1',
          available: true,
          created_at: '2026-10-13T12:00:00Z',
          test: true,
        }}
        onClose={vi.fn()}
        onStarted={vi.fn()}
      />,
    )
    fireEvent.click(await screen.findByRole('checkbox', { name: /db → \/data/ }))
    expect(screen.getByLabelText('Target directory for db')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Preview recovery' })).toBeDisabled()
    expect(
      screen.getByRole('checkbox', { name: 'Restart services that were running before recovery' }),
    ).not.toBeChecked()
    fireEvent.change(screen.getByLabelText('Target directory for db'), { target: { value: '/recovered/data' } })
    fireEvent.click(screen.getByRole('button', { name: 'Preview recovery' }))
    expect(await screen.findByText('Stop writer: demo/db')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Apply recovered data' })).toBeDisabled()
    expect(api.restoreBackupSnapshot).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText('Type RESTORE to apply recovered data'), { target: { value: 'RESTORE' } })
    fireEvent.click(screen.getByRole('button', { name: 'Apply recovered data' }))
    await waitFor(() =>
      expect(api.restoreBackupSnapshot).toHaveBeenCalledWith('nas', {
        restore_id: 'restore',
        mappings: [{ source_id: 'data', target: '/recovered/data', mode: 'replace' }],
        resume_services: false,
        review_token: 'bound-review',
      }),
    )
  })
})
