import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { Link, MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BackupsPage } from './backups-page'
import { DestinationEditor } from './backups/destination-editor'
import { PlanEditor } from './backups/plan-editor'
import { ApplyRecoveryDialog } from './backups/apply-recovery-dialog'
import * as api from '@/lib/backup-api'
import { useJobStream } from '@/hooks/use-job-stream'
import { getStacks } from '@/lib/api-client'

vi.mock('@/lib/backup-api')
vi.mock('@/lib/api-client', () => ({ getStacks: vi.fn() }))
vi.mock('@/hooks/use-job-stream', () => ({ useJobStream: vi.fn(() => ({ state: null, events: [], clear: vi.fn() })) }))

const destination: api.BackupDestination = {
  metadata_check: { status: 'unknown', scope: 'unknown' },
  data_check: { status: 'unknown', scope: 'unknown' },
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
  helper_ready: true,
  helper_configured: true,
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
  vi.mocked(useJobStream).mockReturnValue({ state: null, events: [], clear: vi.fn() })
  vi.mocked(api.getBackups).mockImplementation(async () => ({ ...overview }))
  vi.mocked(api.getBackupPolicy).mockResolvedValue({
    paused: false,
    effective_paused: false,
    pause_reasons: [],
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
      items: [{ recovery_status: 'unknown', id: 'snap', short_id: 'snap', time: '2026-10-01T00:00:00Z', hostname: 'host', tags: [], paths: [] }],
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
    await waitFor(() => expect(screen.queryByText('Refreshing repository evidence. Cleanup is disabled until this finishes.')).not.toBeInTheDocument())
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
    await waitFor(() => expect(screen.getByRole('button', { name: 'Start check' })).toBeEnabled())
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
        recovery_status: 'complete',
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
          recovery_status: 'complete',
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


describe('Backup workflow round two regressions', () => {
  const oldSnapshot: api.BackupSnapshot = { recovery_status: 'unknown', id: 'old', short_id: 'old', time: '2026-10-01T00:00:00Z', hostname: 'host', tags: [], paths: [] }
  function recoveryFixtures() {
    vi.mocked(api.getBackupSnapshots).mockResolvedValue({ items: [oldSnapshot] })
    vi.mocked(api.getBackupRestores).mockResolvedValue({ items: [] })
    vi.mocked(api.getBackupCatalog).mockResolvedValue({ items: [], sources: [], path: '', total: 0, offset: 0, limit: 50, index_available: false, transfer_bytes: 0 })
  }

  it('refreshes a mounted Recovery list after backup completion and preserves the chosen snapshot', async () => {
    recoveryFixtures()
    vi.mocked(api.runBackupPlan).mockResolvedValue(jobResponse)
    renderPage('recovery')
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'old' } })
    fireEvent.click(screen.getByRole('tab', { name: 'Plans' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Back up now' }))
    const dialog = screen.getByRole('dialog')
    await within(dialog).findByText('demo / db')
    fireEvent.click(within(dialog).getByRole('checkbox', { name: /I understand/ }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Start backup' }))
    await waitFor(() => expect(api.runBackupPlan).toHaveBeenCalled())
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    vi.mocked(api.getBackupSnapshots).mockResolvedValue({ items: [oldSnapshot, { ...oldSnapshot, id: 'new', short_id: 'new' }] })
    vi.mocked(useJobStream).mockReturnValue({ state: 'succeeded', events: [], clear: vi.fn() })
    fireEvent.click(screen.getByRole('tab', { name: 'Recovery' }))
    await screen.findByRole('option', { name: /new/ })
    expect(screen.getByLabelText('Recovery set')).toHaveValue('old')
    expect(api.getBackupRestores).toHaveBeenCalledTimes(1)
  })

  it('invalidates snapshots and restored data on manual Refresh without discarding selection', async () => {
    recoveryFixtures()
    renderPage('recovery')
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'old' } })
    vi.mocked(api.getBackupSnapshots).mockResolvedValue({ items: [oldSnapshot, { ...oldSnapshot, id: 'new', short_id: 'new' }] })
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await screen.findByRole('option', { name: /new/ })
    expect(screen.getByLabelText('Recovery set')).toHaveValue('old')
    expect(api.getBackupRestores).toHaveBeenCalledTimes(2)
  })

  it('does not rediscover sources or policy on every running overview poll', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, runs: [{ id: 'active', job_id: 'active-job', plan_id: 'daily', plan_name: 'Daily', state: 'running', capture_state: 'running', phase: 'capture', created_at: '2026-10-01T00:00:00Z', expires_at: '2026-10-02T00:00:00Z', copies: [] }] })
    vi.useFakeTimers()
    try {
      await act(async () => { renderPage('protection') })
      expect(screen.getByText('Copies needing attention')).toBeVisible()
      expect(api.getBackupCoverage).toHaveBeenCalledTimes(1)
      expect(api.getBackupPolicy).toHaveBeenCalledTimes(1)
      await act(async () => { await vi.advanceTimersByTimeAsync(15000) })
      expect(api.getBackups).toHaveBeenCalledTimes(4)
      expect(api.getBackupCoverage).toHaveBeenCalledTimes(1)
      expect(api.getBackupPolicy).toHaveBeenCalledTimes(1)
    } finally { vi.useRealTimers() }
  })

  it('keeps failed refresh feedback visible while the next refresh is pending', async () => {
    renderPage('plans')
    await screen.findByRole('button', { name: 'Back up now' })
    vi.mocked(api.getBackups).mockRejectedValueOnce(new Error('Network is unavailable'))
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Network is unavailable')
    vi.mocked(api.getBackups).mockImplementation(() => new Promise(() => {}))
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(screen.getByRole('alert')).toBe(alert)
    expect(screen.getByRole('button', { name: 'Back up now' })).toBeVisible()
  })

  it('uses the configured check policy and disables initialization for an available repository', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, destinations: [{ ...destination, status: 'available', check_read_data: 'all', inventory_error: 'Inventory could not be confirmed', last_check_attempt_at: '2026-10-14T09:00:00Z' }] })
    renderPage('destinations')
    expect(await screen.findByRole('button', { name: 'Initialize' })).toBeDisabled()
    expect(screen.getByText(/Inventory could not be confirmed/)).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: 'Check repository' }))
    expect(screen.getByLabelText('Data-read policy')).toHaveValue('all')
  })
})


describe('Backup workflow round three regressions', () => {
  const snapshot: api.BackupSnapshot = { id: 'existing-snapshot', short_id: 'existing', time: '2026-10-01T00:00:00Z', hostname: 'host', tags: [], paths: [], recovery_status: 'complete' }
  function recoveryFixtures() {
    vi.mocked(api.getBackupSnapshots).mockResolvedValue({ items: [snapshot] })
    vi.mocked(api.getBackupRestores).mockResolvedValue({ items: [] })
    vi.mocked(api.getBackupCatalog).mockResolvedValue({ items: [], sources: [], path: '', total: 0, offset: 0, limit: 50, index_available: false, transfer_bytes: 0 })
  }

  it('discovers scheduled work while idle and refreshes a finished copy before its sibling completes', async () => {
    recoveryFixtures()
    vi.useFakeTimers()
    try {
      await act(async () => { renderPage('recovery') })
      fireEvent.change(screen.getByLabelText('Recovery set'), { target: { value: snapshot.id } })
      const run: api.BackupRun = { id: 'scheduled', job_id: 'scheduler-job', plan_id: 'daily', plan_name: 'Daily', state: 'running', capture_state: 'succeeded', phase: 'upload', created_at: '2026-10-14T00:00:00Z', expires_at: '2026-10-15T00:00:00Z', copies: [
        { destination_id: 'nas', state: 'running', verification: 'pending', retention: 'pending' },
        { destination_id: 'cloud', state: 'running', verification: 'pending', retention: 'pending' },
      ] }
      vi.mocked(api.getBackups).mockResolvedValue({ ...overview, runs: [run] })
      await act(async () => { await vi.advanceTimersByTimeAsync(60000) })
      expect(api.getBackups).toHaveBeenCalledTimes(2)
      vi.mocked(api.getBackups).mockResolvedValue({ ...overview, runs: [{ ...run, copies: [{ ...run.copies[0], state: 'succeeded', snapshot_id: 'new', verification: 'recovery_objects_verified', retention: 'succeeded' }, run.copies[1]] }] })
      vi.mocked(api.getBackupSnapshots).mockResolvedValue({ items: [snapshot, { ...snapshot, id: 'new', short_id: 'new' }] })
      await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
      expect(screen.getByRole('option', { name: /new/ })).toBeVisible()
      expect(screen.getByLabelText('Recovery set')).toHaveValue(snapshot.id)
      expect(api.getBackupSnapshots).toHaveBeenCalledTimes(2)
      expect(api.getBackupSnapshots).not.toHaveBeenCalledWith('cloud', expect.anything())
    } finally { vi.useRealTimers() }
  })

  it('refreshes overview on focus and leaves fresh snapshot lists alone after access checks', async () => {
    recoveryFixtures()
    vi.mocked(api.testBackupDestination).mockResolvedValue(jobResponse)
    renderPage('recovery')
    await screen.findByLabelText('Recovery set')
    const overviewCalls = vi.mocked(api.getBackups).mock.calls.length
    fireEvent(window, new Event('focus'))
    await waitFor(() => expect(api.getBackups).toHaveBeenCalledTimes(overviewCalls + 1))
    fireEvent.click(screen.getByRole('tab', { name: 'Destinations' }))
    fireEvent.click(screen.getAllByRole('button', { name: 'Test access' })[0])
    await waitFor(() => expect(api.testBackupDestination).toHaveBeenCalled())
    vi.mocked(useJobStream).mockReturnValue({ state: 'succeeded', events: [], clear: vi.fn() })
    fireEvent.click(screen.getByRole('tab', { name: 'Recovery' }))
    await waitFor(() => expect(screen.getByLabelText('Recovery set')).toBeVisible())
    expect(api.getBackupSnapshots).toHaveBeenCalledTimes(1)
  })

  it('preserves an explicit missing snapshot selection with a persistent explanation', async () => {
    recoveryFixtures()
    renderPage('recovery')
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: snapshot.id } })
    vi.mocked(api.getBackupSnapshots).mockResolvedValue({ items: [{ ...snapshot, id: 'another' }] })
    fireEvent.click(screen.getByRole('button', { name: 'Refresh snapshots' }))
    expect(await screen.findByText(/The selected snapshot existing is no longer/)).toHaveAttribute('role', 'status')
    expect(screen.getByLabelText('Recovery set')).toHaveValue(snapshot.id)
    fireEvent.click(screen.getByRole('tab', { name: 'Plans' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Recovery' }))
    expect(screen.getByText(/The selected snapshot existing is no longer/)).toBeVisible()
    fireEvent.change(screen.getByLabelText('Recovery set'), { target: { value: 'another' } })
    expect(screen.queryByText(/The selected snapshot existing is no longer/)).not.toBeInTheDocument()
  })

  it.each(['host_review_required', 'policy_error', 'initialization_error', 'service_recovery_required'] as const)('blocks captures for %s with the reason by the action', async (code) => {
    vi.mocked(api.getBackupPolicy).mockResolvedValue({ paused: false, host_review_required: code === 'host_review_required', effective_paused: true, pause_reasons: [{ code, message: 'Inspect the backup state.' }], overdue_grace_minutes: 60, heartbeat_configured: false })
    renderPage('plans')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Back up now' })).toBeDisabled())
    expect(screen.getByRole('button', { name: 'Back up now' })).toHaveAccessibleDescription(/Inspect the backup state/)
    expect(api.runBackupPlan).not.toHaveBeenCalled()
  })

  it('allows manual captures during an explicit user pause', async () => {
    vi.mocked(api.getBackupPolicy).mockResolvedValue({ paused: true, host_review_required: false, effective_paused: true, pause_reasons: [{ code: 'user_paused', message: 'Automatic backups paused.' }], overdue_grace_minutes: 60, heartbeat_configured: false })
    renderPage('plans')
    expect(await screen.findByRole('button', { name: 'Back up now' })).toBeEnabled()
  })

  it('describes an unconfigured helper as optional setup rather than an operational failure', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, helper_ready: false, helper_configured: false })
    renderPage('plans')
    expect(await screen.findByText(/In-place recovery is not configured/)).toHaveAttribute('role', 'status')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('keeps stored-data failure visible after a metadata success', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, destinations: [{ ...destination, metadata_check: { status: 'verified', scope: 'metadata' }, data_check: { status: 'failed', scope: 'all', error: 'Pack damaged' }, last_check_attempt_policy: 'metadata', last_check_attempt_status: 'succeeded' }] })
    renderPage('destinations')
    expect(await screen.findByText('Pack damaged')).toBeVisible()
    expect(screen.getByText(/A metadata or sampled check cannot resolve it/)).toBeVisible()
    expect(screen.getByText('Stored data evidence')).toBeVisible()
  })
  it('reports a removed linked plan at page scope after a destination command failed', async () => {
    vi.mocked(api.testBackupDestination).mockRejectedValue(new Error('Destination access failed'))
    render(<MemoryRouter initialEntries={['/backups?view=destinations']}><Link to="/backups?view=plans&plan=removed">Open removed plan</Link><BackupsPage /></MemoryRouter>)
    fireEvent.click((await screen.findAllByRole('button', { name: 'Test access' }))[0])
    await screen.findByText('Destination access failed')
    fireEvent.click(screen.getByRole('link', { name: 'Open removed plan' }))
    expect(await screen.findByText(/The linked backup plan no longer exists/)).toBeVisible()
  })

})


describe('Backup workflow round four regressions', () => {
  it('defaults a known data failure to a full read and explains lighter checks', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, destinations: [{ ...destination, data_check: { status: 'failed', scope: 'all', error: 'Damaged pack' } }] })
    renderPage('destinations')
    fireEvent.click(await screen.findByRole('button', { name: 'Check repository' }))
    expect(screen.getByLabelText('Data-read policy')).toHaveValue('all')
    expect(screen.getByRole('dialog')).toHaveTextContent('Only a successful full data read can clear it')
    expect(screen.getByRole('dialog')).toHaveTextContent('downloads and verifies all stored data')
    fireEvent.change(screen.getByLabelText('Data-read policy'), { target: { value: 'metadata' } })
    expect(screen.getByRole('dialog')).toHaveTextContent('leaves this data-failure evidence unchanged')
  })

  it('requires explicit integrity acknowledgment before cleanup', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, destinations: [{ ...destination, maintenance_review_token: 'failed-proof', data_check: { status: 'failed', scope: 'all', error: 'Damaged pack' } }] })
    vi.mocked(api.pruneBackupDestination).mockResolvedValue(jobResponse)
    renderPage('destinations')
    fireEvent.click(await screen.findByRole('button', { name: 'Run retention cleanup' }))
    await waitFor(() => expect(screen.queryByText('Refreshing repository evidence. Cleanup is disabled until this finishes.')).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Start cleanup' })).toBeDisabled()
    expect(screen.getByRole('dialog')).toHaveTextContent('remove recovery material needed for repair')
    fireEvent.change(within(screen.getByRole('dialog')).getByRole('textbox'), { target: { value: 'NAS' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start cleanup' }))
    await waitFor(() => expect(api.pruneBackupDestination).toHaveBeenCalledWith('nas', 'failed-proof'))
  })

  it('offers an explicit stale-lock operation without force-unlock semantics', async () => {
    vi.mocked(api.unlockBackupDestination).mockResolvedValue(jobResponse)
    renderPage('destinations')
    fireEvent.click((await screen.findAllByRole('button', { name: 'Remove stale locks' }))[0])
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent('refuses live, uncertain or foreign-host owners')
    expect(dialog).toHaveTextContent('does not check or repair repository data')
    expect(dialog).toHaveTextContent('Keep all external repository clients stopped throughout this operation')
    expect(dialog).toHaveTextContent('at least 30 minutes old')
    expect(api.unlockBackupDestination).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove stale locks' }))
    await waitFor(() => expect(api.unlockBackupDestination).toHaveBeenCalledWith('nas'))
  })

  it('coalesces focus and visibility and keeps Refresh enabled during background polling', async () => {
    vi.useFakeTimers()
    try {
      await act(async () => { renderPage('plans') })
      vi.mocked(api.getBackups).mockImplementation(() => new Promise(() => {}))
      fireEvent(window, new Event('focus'))
      fireEvent(document, new Event('visibilitychange'))
      await act(async () => { await vi.advanceTimersByTimeAsync(100) })
      expect(api.getBackups).toHaveBeenCalledTimes(2)
      expect(api.getBackupPolicy).toHaveBeenCalledTimes(2)
      expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
    } finally { vi.useRealTimers() }
  })

  it('does not repeat stored-data errors or invent a check-attempt scope', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, destinations: [{ ...destination, data_check: { status: 'failed', scope: 'all', error: 'Damaged pack' }, check_error: 'Damaged pack', last_check_attempt_error: 'Damaged pack', last_operation_error: 'Damaged pack' }] })
    renderPage('destinations')
    expect(await screen.findAllByText('Damaged pack')).toHaveLength(1)
    expect(screen.queryByText(/unknown scope/)).not.toBeInTheDocument()
  })

  it('shows initialization failures once without a misleading service-recovery prefix', async () => {
    const message = 'Backup module is unavailable: configuration cannot be read.'
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, recovery_error: message })
    vi.mocked(api.getBackupPolicy).mockResolvedValue({ paused: false, host_review_required: false, effective_paused: true, pause_reasons: [{ code: 'initialization_error', message }], overdue_grace_minutes: 60, heartbeat_configured: false })
    renderPage('plans')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Back up now' })).toBeDisabled())
    expect(screen.getByRole('alert')).toHaveTextContent(message)
    expect(screen.queryByText(/Service recovery needs attention/)).not.toBeInTheDocument()
  })
})

describe('Backup repository decision regressions', () => {
  it('shows the imported failed check status and scope even when its timestamp is unavailable', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, destinations: [{ ...destination, last_check_attempt_status: 'failed', last_check_attempt_policy: 'unknown', last_check_attempt_error: 'Imported check failure' }] })
    renderPage('destinations')
    const details = (await screen.findByText('Latest check attempt')).parentElement!
    expect(details).toHaveTextContent('Time not recorded')
    expect(details).toHaveTextContent('Scope not recorded')
    expect(details).toHaveTextContent('failed')
  })
  it.each(['metadata', 'all'] as const)('keeps historical proof visible beside an inconclusive %s check and selects the resolving scope', async (scope) => {
    const held: api.BackupDestination = { ...destination, metadata_check: { status: 'verified', scope: 'metadata', checked_at: '2026-10-01T00:00:00Z' }, data_check: { status: 'verified', scope: 'all', checked_at: '2026-10-01T00:00:00Z' }, check_status: 'unverified', check_hold: { scope, error: 'Check did not establish repository integrity', checked_at: '2026-10-02T00:00:00Z' } }
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, destinations: [held] })
    renderPage('destinations')
    expect(await screen.findByText(/Saved verification evidence is historical/)).toBeVisible()
    expect(screen.getAllByText('verified')).toHaveLength(2)
    fireEvent.click(screen.getByRole('button', { name: 'Check repository' }))
    expect(screen.getByLabelText('Data-read policy')).toHaveValue(scope)
    expect(within(screen.getByRole('dialog')).getByText(held.check_hold!.error)).toBeVisible()
  })

  it('resets typed cleanup acknowledgment when current evidence changes while the dialog is open', async () => {
    let current: api.BackupDestination = { ...destination, maintenance_review_token: 'proof-a', data_check: { status: 'failed', scope: 'all', error: 'First damaged pack', checked_at: '2026-10-01T00:00:00Z' } }
    vi.mocked(api.getBackups).mockImplementation(async () => ({ ...overview, destinations: [current] }))
    vi.mocked(api.pruneBackupDestination).mockResolvedValue(jobResponse)
    renderPage('destinations')
    fireEvent.click(await screen.findByRole('button', { name: 'Run retention cleanup' }))
    await waitFor(() => expect(screen.queryByText('Refreshing repository evidence. Cleanup is disabled until this finishes.')).not.toBeInTheDocument())
    fireEvent.change(within(screen.getByRole('dialog')).getByRole('textbox'), { target: { value: 'NAS' } })
    expect(screen.getByRole('button', { name: 'Start cleanup' })).toBeEnabled()
    current = { ...current, maintenance_review_token: 'proof-b', data_check: { status: 'failed', scope: 'all', error: 'Second damaged pack', checked_at: '2026-10-02T00:00:00Z' } }
    fireEvent(window, new Event('focus'))
    await waitFor(() => expect(within(screen.getByRole('dialog')).getByText('Second damaged pack')).toBeVisible())
    expect(within(screen.getByRole('dialog')).getByRole('textbox')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Start cleanup' })).toBeDisabled()
    expect(api.pruneBackupDestination).not.toHaveBeenCalled()
    fireEvent.change(within(screen.getByRole('dialog')).getByRole('textbox'), { target: { value: 'NAS' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start cleanup' }))
    await waitFor(() => expect(api.pruneBackupDestination).toHaveBeenCalledWith('nas', 'proof-b'))
  })

  it('refreshes a stale cleanup review after the dedicated conflict and requires new acknowledgment', async () => {
    let current = destination
    vi.mocked(api.getBackups).mockImplementation(async () => ({ ...overview, destinations: [current] }))
    vi.mocked(api.pruneBackupDestination).mockImplementationOnce(async () => {
      current = { ...destination, maintenance_review_token: 'new-hold', check_hold: { scope: 'metadata', error: 'Index discovery was inconclusive' } }
      throw Object.assign(new Error('raw API acknowledgement details'), { code: 'maintenance_acknowledgement_required' })
    }).mockResolvedValue(jobResponse)
    renderPage('destinations')
    fireEvent.click(await screen.findByRole('button', { name: 'Run retention cleanup' }))
    await waitFor(() => expect(screen.queryByText('Refreshing repository evidence. Cleanup is disabled until this finishes.')).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: 'Start cleanup' }))
    await waitFor(() => expect(within(screen.getByRole('dialog')).getByText('Index discovery was inconclusive')).toBeVisible())
    expect(screen.getByRole('dialog')).toHaveTextContent('Review the latest evidence and confirm')
    expect(screen.queryByText('raw API acknowledgement details')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Start cleanup' })).toBeDisabled()
    fireEvent.change(within(screen.getByRole('dialog')).getByRole('textbox'), { target: { value: 'NAS' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start cleanup' }))
    await waitFor(() => expect(api.pruneBackupDestination).toHaveBeenLastCalledWith('nas', 'new-hold'))
  })

  it('offers lock recovery beside persisted errors without inferring locks from arbitrary prose', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, destinations: [{ ...destination, inventory_error: 'Repository locked (repository_locked)' }, { ...destination, id: 'cloud', name: 'Cloud', inventory_error: 'The old repository_locked label is unavailable' }] })
    renderPage('destinations')
    expect(await screen.findAllByRole('button', { name: 'Review stale locks' })).toHaveLength(1)
  })

  it('keeps unlock refusal in review and refreshes a recently loaded Recovery list after successful unlock', async () => {
    let completed = false
    vi.mocked(useJobStream).mockImplementation(({ jobId }) => ({ state: jobId ? completed ? 'succeeded' : 'running' : null, events: [], clear: vi.fn() }))
    vi.mocked(api.getBackups).mockImplementation(async () => ({ ...overview, destinations: [{ ...destination, last_check_attempt_error: 'Previous check was locked (repository_locked)', ...(completed ? { last_operation: 'backup_unlock', last_operation_status: 'succeeded', last_operation_at: '2026-10-15T09:00:00Z' } : {}) }] }))
    vi.mocked(api.getBackupRestores).mockResolvedValue({ items: [] })
    vi.mocked(api.getBackupSnapshots).mockResolvedValueOnce({ items: [] }).mockRejectedValueOnce(Object.assign(new Error('Repository is locked'), { code: 'repository_locked' })).mockResolvedValue({ items: [] })
    vi.mocked(api.unlockBackupDestination).mockRejectedValueOnce(new Error('Local lock owner is still running')).mockResolvedValue(jobResponse)
    renderPage('recovery')
    await screen.findByText('No snapshots found in this repository.')
    fireEvent.click(screen.getByRole('button', { name: 'Refresh snapshots' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Review stale locks' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove stale locks' }))
    expect(await within(screen.getByRole('dialog')).findByText('Local lock owner is still running')).toBeVisible()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove stale locks' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    completed = true
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() => expect(api.getBackupSnapshots).toHaveBeenCalledTimes(3))
    expect(screen.queryByText('Repository is locked')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: 'Destinations' }))
    expect(await screen.findByText(/Stale-lock removal completed/)).toHaveTextContent('repository data was not checked')
    expect(screen.queryByText(/A repository operation reported a lock/)).not.toBeInTheDocument()
    expect(screen.getByText(/Previous check was locked/)).toBeVisible()
    expect(screen.queryByText(/A repository client holds a lock/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Unlock job details' })).toBeVisible()
  })
})

describe('Backup round six review regressions', () => {
  it('refreshes on opening cleanup and refuses submission during a pending or failed refresh', async () => {
    renderPage('destinations')
    const open = (await screen.findAllByRole('button', { name: 'Run retention cleanup' }))[0]
    let reject!: (reason: Error) => void
    vi.mocked(api.getBackups).mockImplementationOnce(() => new Promise((_, failure) => { reject = failure }))
    fireEvent.click(open)
    expect(screen.getByRole('button', { name: 'Start cleanup' })).toBeDisabled()
    expect(screen.getByRole('dialog')).toHaveTextContent('Refreshing repository evidence')
    await act(async () => reject(new Error('Evidence refresh failed')))
    expect(screen.getByRole('button', { name: 'Start cleanup' })).toBeDisabled()
    expect(within(screen.getByRole('dialog')).getByRole('alert')).toHaveTextContent('Evidence refresh failed')
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Refresh repository evidence' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Start cleanup' })).toBeEnabled())
    expect(api.pruneBackupDestination).not.toHaveBeenCalled()
  })

  it('explains a missing review token instead of leaving an unexplained disabled cleanup action', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, destinations: [{ ...destination, check_hold: { scope: 'all', error: 'Unknown tree diagnostic' } }] })
    renderPage('destinations')
    fireEvent.click(await screen.findByRole('button', { name: 'Run retention cleanup' }))
    expect(await within(screen.getByRole('dialog')).findByText(/current evidence review token is unavailable/)).toBeVisible()
    expect(screen.getByRole('button', { name: 'Start cleanup' })).toBeDisabled()
  })

  it('polls an open idle cleanup review every five seconds and invalidates confirmation even after evidence returns', async () => {
    let current: api.BackupDestination = { ...destination, maintenance_review_token: 'a', check_hold: { scope: 'all', error: 'Unknown repository diagnostic' } }
    vi.mocked(api.getBackups).mockImplementation(async () => ({ ...overview, destinations: [current] }))
    vi.useFakeTimers()
    try {
      await act(async () => renderPage('destinations'))
      await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Run retention cleanup' })))
      fireEvent.change(within(screen.getByRole('dialog')).getByRole('textbox'), { target: { value: 'NAS' } })
      expect(screen.getByRole('button', { name: 'Start cleanup' })).toBeEnabled()
      current = { ...destination }
      await act(async () => vi.advanceTimersByTimeAsync(5000))
      expect(screen.getByRole('dialog')).toHaveTextContent('Typed confirmation was reset')
      current = { ...destination, maintenance_review_token: 'a', check_hold: { scope: 'all', error: 'Unknown repository diagnostic' } }
      await act(async () => vi.advanceTimersByTimeAsync(5000))
      expect(within(screen.getByRole('dialog')).getByRole('textbox')).toHaveValue('')
      expect(screen.getByRole('button', { name: 'Start cleanup' })).toBeDisabled()
      expect(api.getBackups).toHaveBeenCalledTimes(4)
    } finally { vi.useRealTimers() }
  })

  it('keeps the configured full check for a metadata failure and displays new evidence in the open dialog', async () => {
    let current: api.BackupDestination = { ...destination, check_read_data: 'all', metadata_check: { status: 'failed', scope: 'metadata', error: 'Index failed' } }
    vi.mocked(api.getBackups).mockImplementation(async () => ({ ...overview, destinations: [current] }))
    renderPage('destinations')
    fireEvent.click(await screen.findByRole('button', { name: 'Check repository' }))
    expect(screen.getByLabelText('Data-read policy')).toHaveValue('all')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Start check' })).toBeEnabled())
    current = { ...current, check_hold: { scope: 'all', error: 'New unresolved data diagnostic' } }
    fireEvent(window, new Event('focus'))
    expect(await within(screen.getByRole('dialog')).findByText('New unresolved data diagnostic')).toBeVisible()
    expect(screen.getByLabelText('Data-read policy')).toHaveValue('all')
  })

  it.each(['succeeded', 'failed'] as const)('does not suggest repeating unlock after its newer %s outcome', async (status) => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, destinations: [{ ...destination,
      last_check_attempt_at: '2026-10-21T08:00:00Z', last_check_attempt_error: 'Old check was locked (repository_locked)',
      last_operation: 'backup_unlock', last_operation_status: status, last_operation_at: '2026-10-21T08:01:00Z',
      ...(status === 'failed' ? { last_operation_error: 'Lock is younger than 30 minutes (unlock_refused)' } : {}),
    }] })
    renderPage('destinations')
    expect(await screen.findByText(/Old check was locked/)).toBeVisible()
    expect(screen.queryByRole('button', { name: 'Review stale locks' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Check repository' })).toBeEnabled()
    if (status === 'failed') expect(screen.getByText(/younger than 30 minutes/)).toBeVisible()
  })

  it('presents deleted-plan cleanup as historical while retaining the original failure', async () => {
    vi.mocked(api.getBackups).mockResolvedValue({ ...overview, plans: [], runs: [{
      id: 'orphan', job_id: 'old-job', plan_id: 'deleted', plan_name: 'Old plan', state: 'succeeded', capture_state: 'succeeded', phase: 'finished',
      created_at: '2020-01-01T00:00:00Z', expires_at: '2020-01-02T00:00:00Z',
      copies: [{ destination_id: 'nas', state: 'succeeded', verification: 'recovery_objects_verified', snapshot_id: 'snapshot', verified_at: '2020-01-01T00:00:00Z', retention: 'failed', retention_error: 'Original cleanup error' }],
    }] })
    renderPage('history')
    expect(await screen.findByText('historical')).toBeVisible()
    expect(screen.getByText(/Its retention policy no longer applies/)).toBeVisible()
    expect(screen.getByText(/Original cleanup error/)).toBeVisible()
    expect(screen.queryByRole('button', { name: 'Retry retention cleanup' })).not.toBeInTheDocument()
    expect(screen.queryByText('attention')).not.toBeInTheDocument()
  })
})
