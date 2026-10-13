import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

vi.mock('@/lib/backup-api')
vi.mock('@/hooks/use-job-stream', () => ({ useJobStream: vi.fn(() => ({ state: null, events: [], clear: vi.fn() })) }))

const destination: api.BackupDestination = {
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
  destinations: [destination],
  plans: [plan],
  runs: [],
  protection: [],
  restic_available: true,
  rclone_available: true,
  staging_directory: '/staging',
}
const snapshot: api.BackupSnapshot = {
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
                current_copies: 1,
                expected_copies: 1,
                copies: [{ destination_id: 'nas', state: 'manual', snapshot_id: 'snap' }],
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
            verification: 'verified',
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
    expect(await screen.findByRole('link', { name: 'Download' })).toHaveAttribute(
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
      preview: { mappings: [], writers: [], warnings: [], overwrites: true },
    })
    render(<ApplyRecoveryDialog destination={destination} restored={result} onStarted={vi.fn()} onClose={vi.fn()} />)
    fireEvent.click(screen.getByRole('checkbox', { name: /db → \/data/ }))
    fireEvent.change(screen.getByLabelText('Target directory for db'), { target: { value: '/recovered/db' } })
    fireEvent.click(screen.getByRole('button', { name: 'Preview recovery' }))
    await waitFor(() =>
      expect(api.previewBackupRestore).toHaveBeenCalledWith('nas', {
        restore_id: 'restore',
        mappings: [{ source_id: 'database', target: '/recovered/db' }],
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
    for (const checkbox of checkboxes) fireEvent.click(checkbox)
    expect(screen.getByText(/Select up to 100 files per restore/)).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Select file-100' })).not.toBeChecked()
    expect(screen.getByRole('button', { name: 'Restore 100 selected files for download' })).toBeEnabled()
  })
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
  })
})
