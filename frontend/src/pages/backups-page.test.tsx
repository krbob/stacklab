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
