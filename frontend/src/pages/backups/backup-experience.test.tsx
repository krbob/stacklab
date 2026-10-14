import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '@/lib/backup-api'
import { useJobStream } from '@/hooks/use-job-stream'
import { ProtectionDashboard } from './protection-dashboard'
import { RecoveryWorkspace } from './recovery-workspace'
import { RunHistory } from './run-history'
import { DestinationEditor } from './destination-editor'
import { RunPlanDialog } from './run-plan-dialog'
import { ScheduleFields } from './schedule-fields'
import { ApplyRecoveryDialog } from './apply-recovery-dialog'
import { sftpRepositoryAddress } from './repository-address'

vi.mock('@/lib/backup-api')
vi.mock('@/hooks/use-job-stream', () => ({ useJobStream: vi.fn(() => ({ state: null, events: [], clear: vi.fn() })) }))

const destination: api.BackupDestination = {
  metadata_check: { status: 'unknown', scope: 'unknown' },
  data_check: { status: 'unknown', scope: 'unknown' },
  id: 'nas',
  name: 'NAS',
  kind: 'local',
  repository: '/mnt/nas',
  expected_filesystem: '2',
  credentials_configured: true,
  status: 'ready',
  check_schedule: { frequency: 'manual' },
  prune_schedule: { frequency: 'manual' },
  check_read_data: 'metadata',
}
const source: api.BackupSource = {
  id: 'database',
  kind: 'service_mount',
  stack_id: 'demo',
  service: 'db',
  mount: '/data',
  path: 'demo_data',
}
const manifestSource: api.BackupManifest['sources'][number] = {
  source,
  original_path: '/data',
  saved_path: 'sources/database.tar',
  size_bytes: 1048576,
  archive: true,
}
const manifest: api.BackupManifest = {
  version: 1,
  run_id: 'run',
  plan_id: 'plan',
  captured_at: '2026-10-01T00:00:00Z',
  consistency: 'stop',
  stacklab_version: 'dev',
  sources: [manifestSource],
}
const plan: api.BackupPlan = {
  id: 'plan',
  name: 'Database',
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
  staging_max_bytes: 10485760,
  staging_ttl_hours: 24,
  stop_timeout_seconds: 60,
}
const overview: api.BackupOverview = {
  helper_ready: true,
  helper_configured: true,
  destinations: [destination],
  plans: [plan],
  runs: [],
  protection: [],
  restic_available: true,
  rclone_available: true,
  staging_directory: '/staging',
}
const snapshot: api.BackupSnapshot = {
  recovery_status: 'unknown',
  id: 'snap',
  short_id: 'snap',
  time: '2026-10-01T00:00:00Z',
  hostname: 'debian',
  paths: ['/'],
  tags: [],
}
const file: api.BackupFile = {
  path: 'record.txt',
  type: 'file',
  size_bytes: 12,
  modified_at: '2026-10-01T00:00:00Z',
  mode: 384,
  uid: 1000,
  gid: 1000,
  sha256: 'abc',
}
const catalog: api.BackupCatalog = {
  items: [file],
  sources: [manifestSource],
  path: '',
  total: 1,
  offset: 0,
  limit: 50,
  index_available: true,
  transfer_bytes: 1048576,
}
const result: api.BackupRestoreResult = {
  recovery_status: 'complete',
  id: 'restore',
  job_id: 'job',
  destination_id: 'nas',
  snapshot_id: 'snap',
  target: '/private/restore',
  created_at: '2026-10-01T12:00:00Z',
  available: true,
  test: true,
  manifest,
}
const job = { job: { id: 'job' } } as api.BackupJobResponse

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(useJobStream).mockReturnValue({ state: null, events: [], clear: vi.fn() })
  vi.mocked(api.getBackupCoverage).mockResolvedValue({
    stacks: [{ stack_id: 'demo', stack_name: 'Demo', sources: [source], plans: [plan] }],
  })
  vi.mocked(api.getBackupSnapshots).mockResolvedValue({ items: [snapshot, { ...snapshot, id: 'older' }] })
  vi.mocked(api.getBackupRestores).mockResolvedValue({ items: [] })
  vi.mocked(api.getBackupCatalog).mockResolvedValue(catalog)
  vi.mocked(api.getBackupRestoreCatalog).mockResolvedValue(catalog)
  vi.mocked(api.restoreBackupSnapshot).mockResolvedValue(job)
  vi.mocked(api.backupDownloadURL).mockReturnValue(
    '/api/backups/restores/restore/download?source_id=database&path=record.txt',
  )
  vi.mocked(api.backupArchiveURL).mockReturnValue('/api/backups/restores/restore/archive?source_id=database')
})

function workspace() {
  return render(<RecoveryWorkspace destinations={[destination]} onStarted={vi.fn()} />)
}
async function chooseSnapshotSource() {
  fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'snap' } })
  fireEvent.change(await screen.findByLabelText('Snapshot source'), { target: { value: 'database' } })
  await screen.findByText('record.txt')
}

describe('Protection and operations', () => {
  it('does not present configuration or a manual copy as scheduled protection', async () => {
    render(
      <MemoryRouter>
        <ProtectionDashboard
          data={{
            ...overview,
            protection: [
              {
                plan_id: 'plan',
                plan_name: 'Database',
                state: 'manual',
                activity: 'idle',
                current_copies: 1,
                expected_copies: 1,
                copies: [{ destination_id: 'nas', activity: 'idle', state: 'manual', snapshot_id: 'snap' }],
              },
            ],
          }}
          onCreatePlan={vi.fn()}
          onAddDestination={vi.fn()}
          onRun={vi.fn()}
          onRecovery={vi.fn()}
        />
      </MemoryRouter>,
    )
    expect(await screen.findByText(/All discovered sources are selected/)).toBeInTheDocument()
    expect(screen.getByText('Copy available. Manual schedule has no freshness guarantee.')).toBeInTheDocument()
    expect(screen.getByText('Copies meeting schedule').parentElement).toHaveTextContent('0')
  })

  it('blocks capture until concrete writers have been reviewed and preflight succeeds', async () => {
    vi.mocked(api.getBackupPreflight).mockRejectedValue(new Error('Mount identity changed'))
    render(
      <RunPlanDialog
        plan={plan}
        destinations={[destination]}
        pending={false}
        error={null}
        onClose={vi.fn()}
        onRun={vi.fn()}
      />,
    )
    expect(await screen.findByText('Mount identity changed')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: /I understand/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Start backup' })).toBeDisabled()
    vi.mocked(api.getBackupPreflight).mockResolvedValue({
      sources: [manifestSource],
      writers: [
        { container_id: 'one', stack_id: 'shared', service: 'writer', running: true },
        { container_id: 'two', service: 'reader', running: true, read_only: true },
      ],
      warnings: [],
    })
    fireEvent.click(screen.getByRole('button', { name: 'Retry preflight' }))
    expect(await screen.findByText('shared / writer')).toBeInTheDocument()
    expect(screen.queryByText(/reader/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: /I understand/ }))
    expect(screen.getByRole('button', { name: 'Start backup' })).toBeEnabled()
  })

  it('filters retained history and paginates without mixing result states', () => {
    const runs = Array.from(
      { length: 12 },
      (_, index): api.BackupRun => ({
        id: `run-${index}`,
        job_id: `job-${index}`,
        plan_id: 'plan',
        plan_name: `Run ${index}`,
        state: index === 11 ? 'failed' : 'succeeded',
        phase: 'finished',
        capture_state: 'succeeded',
        created_at: '2026-10-01T00:00:00Z',
        expires_at: '2099-01-01T00:00:00Z',
        copies: [
          {
            destination_id: 'nas',
            state: index === 11 ? 'failed' : 'succeeded',
            verification: 'recovery_objects_verified',
            retention: 'succeeded',
          },
        ],
      }),
    )
    render(
      <RunHistory
        runs={runs}
        plans={[plan]}
        destinations={[destination]}
        clock={0}
        pending={false}
        onJob={vi.fn()}
        onRetry={vi.fn()}
      />,
    )
    expect(screen.queryByText('Run 11')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(screen.getByText('Run 11')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Filter by result'), { target: { value: 'failed' } })
    expect(screen.getByText('Run 11')).toBeInTheDocument()
    expect(screen.queryByText('Run 10')).not.toBeInTheDocument()
    expect(screen.getByText(/1 matching runs/)).toBeInTheDocument()
  })
})

describe('Incomplete protection evidence', () => {
  it('does not declare complete selection when any stack discovery failed', async () => {
    vi.mocked(api.getBackupCoverage).mockResolvedValue({
      stacks: [{ stack_id: 'demo', stack_name: 'Demo', sources: [], plans: [], error: 'Docker unavailable' }],
    })
    render(
      <MemoryRouter>
        <ProtectionDashboard
          data={overview}
          onCreatePlan={vi.fn()}
          onAddDestination={vi.fn()}
          onRun={vi.fn()}
          onRecovery={vi.fn()}
        />
      </MemoryRouter>,
    )
    expect(await screen.findByText(/Source inventory is incomplete/)).toBeInTheDocument()
    expect(screen.queryByText(/All discovered sources are selected/)).not.toBeInTheDocument()
  })
})

describe('Continuous recovery workspace', () => {
  it('restores a selected file with visible full archive cost and never claims a full recovery test', async () => {
    workspace()
    await chooseSnapshotSource()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select record.txt' }))
    expect(screen.getByText(/1 files selected/)).toHaveTextContent('1 MiB')
    fireEvent.click(screen.getByRole('button', { name: 'Restore 1 selected file for download' }))
    await waitFor(() =>
      expect(api.restoreBackupSnapshot).toHaveBeenCalledWith('nas', {
        snapshot_id: 'snap',
        files: [{ source_id: 'database', path: 'record.txt' }],
        test: false,
      }),
    )
  })

  it('automatically selects a completed restore and downloads from its registered identity', async () => {
    let completed = false
    vi.mocked(api.restoreBackupSnapshot).mockImplementation(async () => {
      completed = true
      return job
    })
    vi.mocked(api.getBackupRestores).mockImplementation(async () => ({ items: completed ? [result] : [] }))
    vi.mocked(useJobStream).mockImplementation(({ jobId }) => ({
      state: jobId ? 'succeeded' : null,
      events: [],
      clear: vi.fn(),
    }))
    workspace()
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'snap' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Restore complete set and test retrieval' }))
    expect(await screen.findByLabelText('Completed isolated restore')).toHaveValue('restore')
    fireEvent.change(await screen.findByLabelText('Restored source'), { target: { value: 'database' } })
    expect(await screen.findByRole('link', { name: 'Download record.txt' })).toHaveAttribute(
      'href',
      '/api/backups/restores/restore/download?source_id=database&path=record.txt',
    )
    fireEvent.click(screen.getByRole('button', { name: 'Review applying recovered data' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).queryByLabelText('Completed isolated restore directory')).not.toBeInTheDocument()
    expect(within(dialog).getByRole('checkbox', { name: /db → \/data/ })).not.toBeChecked()
  })

  it('offers local browsing for legacy snapshots without fabricating a file index', async () => {
    vi.mocked(api.getBackupCatalog).mockResolvedValue({ ...catalog, index_available: false, items: [] })
    workspace()
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'snap' } })
    fireEvent.change(await screen.findByLabelText('Snapshot source'), { target: { value: 'database' } })
    expect(await screen.findByText(/This snapshot has no file index/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Search snapshot files')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Restore this source' }))
    await waitFor(() =>
      expect(api.restoreBackupSnapshot).toHaveBeenCalledWith('nas', {
        snapshot_id: 'snap',
        source_ids: ['database'],
        test: false,
      }),
    )
  })

  it('compares the chosen source against a baseline snapshot', async () => {
    vi.mocked(api.compareBackupSnapshots).mockResolvedValue({
      items: [
        {
          path: 'record.txt',
          change: 'modified',
          before: file,
          after: { ...file, sha256: 'def' },
          content_changed: true,
          metadata_changed: false,
        },
      ],
      total: 1,
      offset: 0,
      limit: 50,
      index_available: true,
      added: 0,
      removed: 0,
      modified: 1,
    })
    workspace()
    await chooseSnapshotSource()
    fireEvent.change(screen.getByLabelText('Compare this source with another snapshot'), { target: { value: 'older' } })
    expect(await screen.findByText('modified')).toBeInTheDocument()
    expect(api.compareBackupSnapshots).toHaveBeenCalledWith(
      'nas',
      'snap',
      'older',
      'database',
      0,
      expect.any(AbortSignal),
    )
  })

  it('keeps unavailable restore registrations visible and prevents applying them', async () => {
    vi.mocked(api.getBackupRestores).mockResolvedValue({
      items: [{ ...result, available: false, unavailable_reason: 'Restore directory was removed' }],
    })
    workspace()
    fireEvent.change(await screen.findByLabelText('Completed isolated restore'), { target: { value: 'restore' } })
    expect(screen.getByText('Restore directory was removed')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Review applying recovered data' })).not.toBeInTheDocument()
  })

  it('binds reviewed apply to a registered restore ID without exposing path input', async () => {
    vi.mocked(api.previewBackupRestore).mockResolvedValue({
      review_token: 'review',
      expires_at: '2099-01-01T00:00:00Z',
      preview: { recovery_status: 'complete', mappings: [], writers: [], warnings: [], overwrites: true },
    })
    render(<ApplyRecoveryDialog destination={destination} restored={result} onStarted={vi.fn()} onClose={vi.fn()} />)
    fireEvent.click(screen.getByRole('checkbox', { name: /db → \/data/ }))
    fireEvent.change(screen.getByLabelText('Target directory for db'), { target: { value: '/recovered/db' } })
    fireEvent.click(screen.getByRole('button', { name: 'Preview recovery' }))
    await waitFor(() =>
      expect(api.previewBackupRestore).toHaveBeenCalledWith('nas', {
        restore_id: 'restore',
        mappings: [{ source_id: 'database', target: '/recovered/db', mode: 'replace' }],
        resume_services: false,
      }),
    )
    expect(api.restoreBackupSnapshot).not.toHaveBeenCalled()
  })
})

describe('Restore selection boundaries', () => {
  it('only offers complete sources present in a partial source restore', () => {
    const other = { ...manifestSource, source: { ...source, id: 'other', service: 'cache', mount: '/cache' } }
    render(
      <ApplyRecoveryDialog
        destination={destination}
        restored={{ ...result, source_ids: ['database'], manifest: { ...manifest, sources: [manifestSource, other] } }}
        onStarted={vi.fn()}
        onClose={vi.fn()}
      />,
    )
    expect(screen.getByRole('checkbox', { name: /db → \/data/ })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: /cache → \/cache/ })).not.toBeInTheDocument()
  })

  it('limits individual file selections to one hundred', async () => {
    const entries = Array.from({ length: 101 }, (_, index) => ({ ...file, path: `file-${index}` }))
    vi.mocked(api.getBackupCatalog).mockResolvedValue({ ...catalog, items: entries, total: 101, limit: 500 })
    workspace()
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'snap' } })
    fireEvent.change(await screen.findByLabelText('Snapshot source'), { target: { value: 'database' } })
    await screen.findByRole('checkbox', { name: 'Select file-0' })
    const checkboxes = screen.getAllByRole('checkbox', { name: /^Select file-/ })
    act(() => { for (const checkbox of checkboxes.slice(0, 100)) fireEvent.click(checkbox) })
    fireEvent.click(checkboxes[100])
    expect(screen.getByText(/Select up to 100 files per restore/)).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Select file-100' })).not.toBeChecked()
    expect(screen.getByRole('button', { name: 'Restore 100 selected files for download' })).toBeEnabled()
  }, 15_000) // 101 deliberate interactions need headroom on loaded test runners.
})

describe('Destination onboarding and schedules', () => {
  it('disables immutable repository fields when editing', () => {
    render(<DestinationEditor destination={destination} onSaved={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Storage type')).toBeDisabled()
    expect(screen.getByLabelText('Repository')).toBeDisabled()
    expect(screen.getByLabelText('Repository password')).toHaveValue('')
  })

  it('builds a typed S3 repository, generates a password, and verifies after saving', async () => {
    vi.mocked(api.saveBackupDestination).mockResolvedValue({
      destination: { ...destination, kind: 's3', repository: 's3:https://s3.example.com/bucket/stacklab' },
    })
    vi.mocked(api.testBackupDestination).mockResolvedValue(job)
    render(<DestinationEditor onSaved={vi.fn()} onClose={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Destination name'), { target: { value: 'Offsite' } })
    fireEvent.change(screen.getByLabelText('Storage type'), { target: { value: 's3' } })
    fireEvent.change(screen.getByLabelText('S3 HTTPS endpoint'), { target: { value: 'https://s3.example.com' } })
    fireEvent.change(screen.getByLabelText('Bucket'), { target: { value: 'bucket' } })
    fireEvent.click(screen.getByRole('button', { name: 'Continue to credentials' }))
    fireEvent.click(screen.getByRole('button', { name: 'Generate password' }))
    expect((screen.getByLabelText('Repository password') as HTMLInputElement).value).toMatch(/^[a-f0-9]{48}$/)
    fireEvent.change(screen.getByLabelText('Access key ID'), { target: { value: 'key' } })
    fireEvent.change(screen.getByLabelText('Secret access key'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save destination' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Test existing repository' }))
    await waitFor(() => expect(api.testBackupDestination).toHaveBeenCalledWith('nas'))
    expect(api.saveBackupDestination).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 's3',
        repository: 's3:https://s3.example.com/bucket/stacklab',
        secrets: expect.objectContaining({ password: expect.stringMatching(/^[a-f0-9]{48}$/) }),
      }),
      undefined,
    )
  })

  it('previews authoritative interval occurrences from the server', async () => {
    vi.mocked(api.previewBackupSchedule).mockResolvedValue({
      next_occurrences: ['2026-10-01T06:00:00Z'],
      timezone: 'UTC',
    })
    render(
      <ScheduleFields label="Copy schedule" value={{ frequency: 'interval', interval_hours: 6 }} onChange={vi.fn()} />,
    )
    expect(await screen.findByText(/Next occurrences \(UTC\)/)).toBeInTheDocument()
    expect(api.previewBackupSchedule).toHaveBeenCalledWith(
      { frequency: 'interval', interval_hours: 6 },
      expect.any(AbortSignal),
    )
    expect(
      screen.getByText(
        new Date('2026-10-01T06:00:00Z').toLocaleString(undefined, { timeZone: 'UTC', timeZoneName: 'short' }),
      ),
    ).toBeInTheDocument()
  })
})

describe('Backup review regressions', () => {
  it('uses only a recovery test of the exact protected snapshot as evidence', async () => {
    render(
      <MemoryRouter>
        <ProtectionDashboard
          data={{
            ...overview,
            destinations: [{ ...destination, last_restore_test_at: '2026-10-14T12:00:00Z' }],
            protection: [
              {
                plan_id: 'plan',
                plan_name: 'Database',
                state: 'attention',
                activity: 'idle',
                current_copies: 0,
                expected_copies: 1,
                copies: [
                  {
                    destination_id: 'nas',
                    activity: 'idle',
                    state: 'integrity_failed',
                    snapshot_id: 'snap',
                    snapshot_status: 'present',
                    integrity_status: 'failed',
                    availability_status: 'available',
                  },
                ],
              },
            ],
          }}
          onCreatePlan={vi.fn()}
          onAddDestination={vi.fn()}
          onRun={vi.fn()}
          onRecovery={vi.fn()}
        />
      </MemoryRouter>,
    )
    expect(await screen.findByText('Recovery test of this snapshot')).toBeInTheDocument()
    expect(screen.getByText('Recovery test of this snapshot').parentElement).toHaveTextContent('Never')
    expect(screen.getByText('Repository integrity').parentElement).toHaveTextContent('failed')
    expect(screen.getByText('Copies needing attention').parentElement).toHaveTextContent('1')
    expect(screen.getByText('Copies meeting schedule').parentElement).toHaveTextContent('0')
  })

  it('shows unavailable index reasons while retaining original archive download for offline recovery', async () => {
    const restricted = {
      ...manifestSource,
      recovery_restrictions: ['absolute symlink'],
      index_unavailable_reason: 'Index limit reached',
    }
    vi.mocked(api.getBackupRestores).mockResolvedValue({
      items: [{ ...result, manifest: { ...manifest, sources: [restricted] } }],
    })
    vi.mocked(api.getBackupRestoreCatalog).mockResolvedValue({
      ...catalog,
      sources: [restricted],
      items: [],
      index_available: false,
      index_unavailable_reason: 'Index limit reached',
    })
    workspace()
    fireEvent.change(await screen.findByLabelText('Completed isolated restore'), { target: { value: 'restore' } })
    expect(await screen.findByText(/Offline recovery required: absolute symlink/)).toBeInTheDocument()
    expect(screen.getByText(/Index limit reached/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Download original source archive' })).toHaveAttribute(
      'href',
      '/api/backups/restores/restore/archive?source_id=database',
    )
    fireEvent.click(screen.getByRole('button', { name: 'Review applying recovered data' }))
    expect(screen.getByRole('checkbox', { name: /db → \/data/ })).toBeDisabled()
  })

  it('retrieves a full set for offline recovery without claiming a successful recovery test', async () => {
    workspace()
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'snap' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Retrieve complete set for offline recovery' }))
    await waitFor(() =>
      expect(api.restoreBackupSnapshot).toHaveBeenCalledWith('nas', { snapshot_id: 'snap', test: false }),
    )
  })

  it('requires reviewing local restore deletion and keeps repository snapshots out of the request', async () => {
    vi.mocked(api.getBackupRestores).mockResolvedValue({ items: [{ ...result, managed: true }] })
    vi.mocked(api.deleteBackupRestore).mockResolvedValue(undefined)
    workspace()
    fireEvent.change(await screen.findByLabelText('Completed isolated restore'), { target: { value: 'restore' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Delete isolated restore' }))
    expect(api.deleteBackupRestore).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toHaveTextContent('Repository snapshots remain unchanged')
    fireEvent.click(screen.getByRole('button', { name: 'Delete restored data' }))
    await waitFor(() => expect(api.deleteBackupRestore).toHaveBeenCalledWith('restore'))
  })

  it('forgets custom recovery registrations with an explicit promise to preserve their files', async () => {
    vi.mocked(api.getBackupRestores).mockResolvedValue({
      items: [{ ...result, managed: false, target: '/operator/recovered-data' }],
    })
    vi.mocked(api.deleteBackupRestore).mockResolvedValue(undefined)
    workspace()
    fireEvent.change(await screen.findByLabelText('Completed isolated restore'), { target: { value: 'restore' } })
    expect(screen.queryByRole('button', { name: 'Delete isolated restore' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Forget result' }))
    const dialog = screen.getByRole('dialog', { name: 'Forget recovery result' })
    expect(dialog).toHaveTextContent('Its custom directory and every file remain on this host')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Forget result' }))
    await waitFor(() => expect(api.deleteBackupRestore).toHaveBeenCalledWith('restore'))
  })

  it('preserves neighboring files by choosing merge for a single-file source', () => {
    render(
      <ApplyRecoveryDialog
        destination={destination}
        restored={{
          ...result,
          manifest: { ...manifest, sources: [{ ...manifestSource, file_name: 'settings.yaml' }] },
        }}
        onStarted={vi.fn()}
        onClose={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('checkbox', { name: /db → \/data/ }))
    expect(screen.getByLabelText('Recovery mode for db')).toHaveValue('merge')
    expect(screen.getByLabelText('Recovery mode for db')).toBeDisabled()
  })

  it('keeps the focused search mounted throughout debounced requests and errors', async () => {
    workspace()
    await chooseSnapshotSource()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Restore this source' })).toBeEnabled())
    let rejectRequest!: (error: Error) => void
    vi.mocked(api.getBackupCatalog).mockImplementation(
      () =>
        new Promise((_resolve, reject) => {
          rejectRequest = reject
        }),
    )
    const input = screen.getByLabelText('Search snapshot files')
    input.focus()
    fireEvent.change(input, { target: { value: 'record' } })
    await waitFor(() =>
      expect(api.getBackupCatalog).toHaveBeenLastCalledWith(
        'nas',
        'snap',
        expect.objectContaining({ search: 'record' }),
        expect.any(AbortSignal),
      ),
    )
    expect(screen.getByLabelText('Search snapshot files')).toBe(input)
    expect(input).toHaveFocus()
    expect(screen.getByLabelText('File entries')).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByRole('checkbox', { name: 'Select record.txt' })).toBeDisabled()
    await act(async () => rejectRequest(new Error('Repository busy')))
    expect(input).toHaveFocus()
    fireEvent.change(input, { target: { value: 'record2' } })
    expect(input).toHaveValue('record2')
    expect(screen.getByRole('button', { name: 'Retry file listing' })).toBeInTheDocument()
  })

  it('formats wall-clock preview dates in the selected zone instead of the browser zone', async () => {
    const date = '2026-10-01T06:00:00Z'
    vi.mocked(api.previewBackupSchedule).mockResolvedValue({ next_occurrences: [date], timezone: 'America/New_York' })
    render(
      <ScheduleFields
        label="Copy schedule"
        value={{ frequency: 'daily', time: '02:00', timezone: 'America/New_York' }}
        onChange={vi.fn()}
      />,
    )
    expect(
      await screen.findByText(
        new Date(date).toLocaleString(undefined, { timeZone: 'America/New_York', timeZoneName: 'short' }),
      ),
    ).toBeInTheDocument()
  })

  it.each([
    ['nas', '22', '/srv/backups', 'sftp://backup@nas//srv/backups'],
    ['nas', '22', 'backups', 'sftp://backup@nas/backups'],
    ['nas', '2222', '/srv/backups', 'sftp://backup@nas:2222//srv/backups'],
    ['2001:db8::1', '2222', '/srv/backups', 'sftp://backup@[2001:db8::1]:2222//srv/backups'],
    ['[::1]', '22', '/space #1/żółć', 'sftp://backup@[::1]//space%20%231/%C5%BC%C3%B3%C5%82%C4%87'],
  ])('preserves SFTP server path semantics for %s:%s %s', (host, port, path, expected) => {
    expect(sftpRepositoryAddress('backup', host, port, path)).toBe(expected)
  })

  it('requires explicit credential replacement and offers verification without generating a mismatched key', async () => {
    vi.mocked(api.saveBackupDestination).mockResolvedValue({ destination })
    render(<DestinationEditor destination={destination} onSaved={vi.fn()} onClose={vi.fn()} />)
    expect(screen.queryByRole('button', { name: 'Generate password' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('Repository password')).toBeDisabled()
    fireEvent.click(screen.getByRole('checkbox', { name: /Replace stored password/ }))
    fireEvent.change(screen.getByLabelText('Repository password'), { target: { value: 'existing-key' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save destination' }))
    const verify = await screen.findByRole('button', { name: 'Test existing repository' })
    await waitFor(() => expect(verify).toHaveFocus())
    expect(screen.queryByRole('button', { name: 'Initialize new repository' })).not.toBeInTheDocument()
    expect(api.saveBackupDestination).toHaveBeenCalledWith(
      expect.objectContaining({ secrets: { password: 'existing-key' } }),
      'nas',
    )
  })

  it('retains unsaved credentials after Escape until discard is explicitly confirmed', async () => {
    const onClose = vi.fn()
    render(<DestinationEditor destination={destination} onSaved={vi.fn()} onClose={onClose} />)
    fireEvent.change(screen.getByLabelText('Destination name'), { target: { value: 'Edited name' } })
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.getByRole('dialog', { name: 'Discard unsaved changes?' })).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }))
    expect(screen.getByLabelText('Destination name')).toHaveValue('Edited name')
    fireEvent.keyDown(window, { key: 'Escape' })
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('loads a missing recovery manifest visibly and retries a transient failure', async () => {
    let rejectRequest!: (error: Error) => void
    vi.mocked(api.getBackupRestoreManifest).mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          rejectRequest = reject
        }),
    )
    render(
      <ApplyRecoveryDialog
        destination={destination}
        restored={{ ...result, manifest: undefined }}
        onStarted={vi.fn()}
        onClose={vi.fn()}
      />,
    )
    expect(screen.getByRole('status')).toHaveTextContent('Loading recovery manifest')
    expect(screen.getByRole('button', { name: 'Preview recovery' })).toBeDisabled()
    await act(async () => rejectRequest(new Error('Temporary storage error')))
    vi.mocked(api.getBackupRestoreManifest).mockResolvedValue({ manifest })
    fireEvent.click(screen.getByRole('button', { name: 'Retry recovery manifest' }))
    expect(await screen.findByRole('checkbox', { name: /db → \/data/ })).toBeInTheDocument()
    expect(screen.queryByText('Temporary storage error')).not.toBeInTheDocument()
  })

  it('refreshes source coverage when the overview refresh generation changes', async () => {
    const props = {
      data: overview,
      onCreatePlan: vi.fn(),
      onAddDestination: vi.fn(),
      onRun: vi.fn(),
      onRecovery: vi.fn(),
    }
    const view = render(
      <MemoryRouter>
        <ProtectionDashboard {...props} refreshKey={1} />
      </MemoryRouter>,
    )
    await waitFor(() => expect(api.getBackupCoverage).toHaveBeenCalledTimes(1))
    view.rerender(
      <MemoryRouter>
        <ProtectionDashboard {...props} refreshKey={2} />
      </MemoryRouter>,
    )
    await waitFor(() => expect(api.getBackupCoverage).toHaveBeenCalledTimes(2))
  })
})

describe('Round two evidence and recovery flows', () => {
  it('keeps an integrity warning visible during active work and excludes disabled copies from attention', async () => {
    render(<MemoryRouter><ProtectionDashboard data={{ ...overview, protection: [{ plan_id: 'plan', plan_name: 'Database', state: 'attention', activity: 'running', current_copies: 0, expected_copies: 1, copies: [
      { destination_id: 'nas', state: 'integrity_failed', activity: 'running', evidence_usable: false },
      { destination_id: 'disabled', state: 'disabled', activity: 'idle', last_error: 'Old error' },
    ] }] }} onCreatePlan={vi.fn()} onAddDestination={vi.fn()} onRun={vi.fn()} onRecovery={vi.fn()} /></MemoryRouter>)
    expect(await screen.findByText(/A repository integrity check failed/)).toBeVisible()
    expect(screen.getByText('Copies needing attention').parentElement).toHaveTextContent('1')
    expect(screen.getByText('Saved copy does not have usable recovery evidence.')).toBeVisible()
    expect(screen.getAllByText('running')).toHaveLength(2)
  })

  it('retains snapshot and restore job state for each visited destination', async () => {
    render(<RecoveryWorkspace destinations={[destination, { ...destination, id: 'cloud', name: 'Cloud', kind: 's3' }]} onStarted={vi.fn()} />)
    fireEvent.change(await screen.findByRole('combobox', { name: 'Recovery set' }), { target: { value: 'snap' } })
    fireEvent.click(screen.getByRole('button', { name: 'Restore complete set and test retrieval' }))
    await waitFor(() => expect(api.restoreBackupSnapshot).toHaveBeenCalled())
    fireEvent.change(screen.getByLabelText('Recovery destination'), { target: { value: 'cloud' } })
    fireEvent.change(await screen.findByRole('combobox', { name: 'Recovery set' }), { target: { value: 'older' } })
    fireEvent.change(screen.getByLabelText('Recovery destination'), { target: { value: 'nas' } })
    expect(screen.getByRole('combobox', { name: 'Recovery set' })).toHaveValue('snap')
    expect(screen.getByText('Isolated restore')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Restore complete set and test retrieval' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Recovery destination'), { target: { value: 'cloud' } })
    expect(screen.getByRole('combobox', { name: 'Recovery set' })).toHaveValue('older')
  })

  it('labels a complete offline retrieval and lets the user clear the auto-selected result', async () => {
    vi.mocked(api.getBackupRestores).mockResolvedValue({ items: [{ ...result, test: false, source_ids: [source.id] }] })
    vi.mocked(useJobStream).mockReturnValue({ state: 'succeeded', events: [], clear: vi.fn() })
    workspace()
    fireEvent.change(await screen.findByLabelText('Recovery set'), { target: { value: 'snap' } })
    fireEvent.click(screen.getByRole('button', { name: 'Retrieve complete set for offline recovery' }))
    await waitFor(() => expect(screen.getByLabelText('Completed isolated restore')).toHaveValue('restore'))
    expect(screen.getByRole('option', { name: /complete set \(offline retrieval\)/ })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Completed isolated restore'), { target: { value: '' } })
    expect(screen.getByLabelText('Completed isolated restore')).toHaveValue('')
    expect(screen.queryByRole('button', { name: 'Review applying recovered data' })).not.toBeInTheDocument()
  })

  it('registers destination verification jobs with the page before the wizard closes', async () => {
    vi.mocked(api.saveBackupDestination).mockResolvedValue({ destination })
    vi.mocked(api.testBackupDestination).mockResolvedValue(job)
    const onStarted = vi.fn()
    render(<DestinationEditor destination={destination} onSaved={vi.fn()} onClose={vi.fn()} onStarted={onStarted} />)
    fireEvent.click(screen.getByLabelText('Replace stored password (must match repository)'))
    fireEvent.change(screen.getByLabelText('Repository password'), { target: { value: 'matching-saved-password' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save destination' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Test existing repository' }))
    await waitFor(() => expect(onStarted).toHaveBeenCalledWith(job))
  })

  it('requires an explicit merge decision for an excluded source and invalidates the previous review', async () => {
    const excluded = { ...manifest, sources: [{ ...manifestSource, source: { ...source, excludes: ['uploads/private'] } }] }
    vi.mocked(api.previewBackupRestore).mockResolvedValue({ review_token: 'review', expires_at: '2099-01-01T00:00:00Z', preview: { recovery_status: 'complete', mappings: [{ source_id: source.id, target: '/data', mode: 'merge' }], writers: [], warnings: [], overwrites: true } })
    render(<ApplyRecoveryDialog destination={destination} restored={{ ...result, manifest: excluded }} onStarted={vi.fn()} onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: /db → \/data/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Use original location' }))
    expect(screen.getByLabelText('Recovery mode for db')).toHaveValue('replace')
    expect(screen.getByRole('button', { name: 'Preview recovery' })).toBeDisabled()
    expect(screen.getByText(/Replacement is blocked/)).toBeVisible()
    fireEvent.change(screen.getByLabelText('Recovery mode for db'), { target: { value: 'merge' } })
    fireEvent.click(screen.getByRole('button', { name: 'Preview recovery' }))
    expect(await screen.findByText(/Server inspection confirms/)).toBeVisible()
    fireEvent.change(screen.getByLabelText('Type RESTORE to apply recovered data'), { target: { value: 'RESTORE' } })
    fireEvent.click(screen.getByRole('button', { name: 'Change mappings' }))
    fireEvent.change(screen.getByLabelText('Target directory for db'), { target: { value: '/other' } })
    expect(screen.queryByRole('button', { name: 'Apply recovered data' })).not.toBeInTheDocument()
    expect(api.restoreBackupSnapshot).not.toHaveBeenCalled()
  })
})

describe('Round four recovery evidence and maintenance', () => {
  it('retains retrieval provenance after its snapshot is absent and repeats it in apply review', async () => {
    vi.mocked(api.getBackupRestores).mockResolvedValue({ items: [{ ...result, snapshot_id: 'no-longer-listed', recovery_status: 'incomplete', classified_at: '2026-10-13T12:00:00Z' }] })
    workspace()
    fireEvent.change(await screen.findByLabelText('Completed isolated restore'), { target: { value: result.id } })
    expect(await screen.findByText(/This restore came from a snapshot classified as incomplete/)).toBeVisible()
    const button = screen.getByRole('button', { name: 'Review applying recovered data' })
    expect(button).not.toHaveAttribute('aria-describedby')
    fireEvent.click(button)
    expect(screen.getByRole('dialog')).toHaveTextContent('This restore came from a snapshot classified as incomplete')
    expect(screen.getByRole('dialog')).toHaveTextContent('intact sources may still be recovered')
  })

  it('explains hardlink recovery and offers the regular member without permitting individual hardlink selection', async () => {
    vi.mocked(api.getBackupCatalog).mockResolvedValue({ ...catalog, items: [{ ...file, path: 'alias.txt', type: 'hardlink', link_target: 'record.txt' }, file] })
    workspace()
    await chooseSnapshotSource()
    expect(screen.queryByRole('checkbox', { name: 'Select alias.txt' })).not.toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Select record.txt' })).toBeEnabled()
    expect(screen.getByText(/This hardlink cannot be retrieved/)).toHaveTextContent('select the regular file at record.txt')
  })

  it('shows overdue timing even when repository evidence is unavailable', async () => {
    const copy: api.BackupProtection['copies'][number] = { destination_id: 'nas', state: 'unverified', activity: 'idle', due_at: '2026-10-14T14:00:00Z', first_outstanding_due: '2026-10-14T11:00:00Z', overdue_since: '2026-10-14T12:00:00Z' }
    render(<MemoryRouter><ProtectionDashboard data={{ ...overview, protection: [{ plan_id: 'plan', plan_name: 'Database', state: 'attention', activity: 'idle', expected_copies: 1, current_copies: 0, copies: [copy] }] }} onCreatePlan={vi.fn()} onAddDestination={vi.fn()} onRun={vi.fn()} onRecovery={vi.fn()} /></MemoryRouter>)
    const warning = await screen.findByText(/Scheduled copy missing since/)
    expect(warning.querySelectorAll('time')[0]).toHaveAttribute('dateTime', copy.first_outstanding_due)
    expect(warning.querySelectorAll('time')[1]).toHaveAttribute('dateTime', copy.overdue_since)
    expect(screen.getByText(/complete recovery objects have not been verified/)).toBeVisible()
  })

  it('keeps transfer retry wording for legacy copies without full verification evidence', () => {
    const run: api.BackupRun = { id: 'legacy', job_id: 'job', plan_id: 'plan', plan_name: 'Database', state: 'failed', capture_state: 'succeeded', phase: 'finished', created_at: '2026-10-01T00:00:00Z', expires_at: '2026-10-02T00:00:00Z', copies: [{ destination_id: 'nas', state: 'succeeded', verification: 'snapshot_metadata_verified', snapshot_id: 'snap', retention: 'failed' }] }
    render(<RunHistory runs={[run]} plans={[plan]} destinations={[destination]} clock={0} pending={false} onJob={vi.fn()} onRetry={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Retry failed copies' })).toBeEnabled()
    expect(screen.queryByText(/do not require retained staging/)).not.toBeInTheDocument()
    expect(screen.getByText(/Prepared data available for retries until/)).toBeVisible()
  })

  it.each([false, true])('retries verified retention after staging expires (mixed run: %s)', (mixed) => {
    const onRetry = vi.fn()
    const good: api.BackupCopy = { destination_id: 'nas', state: 'succeeded', verification: 'recovery_objects_verified', verified_at: '2026-10-01T00:00:00Z', snapshot_id: 'snap', retention: 'failed', retention_error: 'Repository unavailable' }
    const run: api.BackupRun = { id: 'expired', job_id: 'job', plan_id: 'plan', plan_name: 'Database', state: 'failed', capture_state: 'succeeded', phase: 'finished', created_at: '2026-10-01T00:00:00Z', expires_at: '2026-10-02T00:00:00Z', copies: [good, ...(mixed ? [{ ...good, destination_id: 'cloud', state: 'failed', verification: 'unavailable' as const, retention: 'pending' as const }] : [])] }
    render(<RunHistory runs={[run]} plans={[plan]} destinations={[destination]} clock={Date.parse('2026-10-14T00:00:00Z')} pending={false} onJob={vi.fn()} onRetry={onRetry} />)
    expect(screen.getByRole('button', { name: 'Retry retention cleanup' })).toBeEnabled()
    expect(screen.getByText(/do not require retained staging/)).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: 'Retry retention cleanup' }))
    expect(onRetry).toHaveBeenCalledWith(run, ['nas'])
    if (mixed) expect(screen.getByText(/Other failed copies need a new capture/)).toBeVisible()
    else expect(screen.getByText('attention')).toBeVisible()
  })
})
