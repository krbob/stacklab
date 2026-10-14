import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '@/lib/backup-api'
import { BackupOperations, BackupPolicyDialog, ServiceRecoveryDialog } from './backup-operations'

vi.mock('@/lib/backup-api')
vi.mock('@/hooks/use-job-stream', () => ({ useJobStream: () => ({ state: null, events: [] }) }))

const policy: api.BackupPolicy = {
  paused: false,
  host_review_required: false,
  overdue_grace_minutes: 60,
  heartbeat_configured: true,
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.getBackupPolicy).mockResolvedValue(policy)
  vi.mocked(api.saveBackupPolicy).mockResolvedValue(policy)
  vi.mocked(api.resolveBackupRecovery).mockResolvedValue({ job: { id: 'recovery-job' } } as api.BackupJobResponse)
})

describe('Backup operational controls', () => {
  it('requires reviewing a recovered host before resuming and preserves the write-only heartbeat', async () => {
    const onSaved = vi.fn()
    render(
      <BackupPolicyDialog
        policy={{ ...policy, paused: true, host_review_required: true }}
        onClose={vi.fn()}
        onSaved={onSaved}
      />,
    )
    expect(screen.getByLabelText('External success heartbeat URL')).toHaveValue('')
    fireEvent.click(screen.getByRole('checkbox', { name: 'Pause automatic backups and repository maintenance' }))
    expect(screen.getByRole('button', { name: 'Save backup settings' })).toBeDisabled()
    fireEvent.click(screen.getByRole('checkbox', { name: /I reviewed the recovered sources/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Save backup settings' }))
    await waitFor(() =>
      expect(api.saveBackupPolicy).toHaveBeenCalledWith({
        paused: false,
        overdue_grace_minutes: 60,
        acknowledge_host: true,
      }),
    )
    expect(onSaved).toHaveBeenCalledOnce()
  })

  it('removes a configured heartbeat only through the explicit removal control', async () => {
    render(<BackupPolicyDialog policy={policy} onClose={vi.fn()} onSaved={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('External success heartbeat URL'), {
      target: { value: 'https://monitor.example/secret' },
    })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Remove the saved heartbeat URL' }))
    expect(screen.getByLabelText('External success heartbeat URL')).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Save backup settings' }))
    await waitFor(() =>
      expect(api.saveBackupPolicy).toHaveBeenCalledWith({
        paused: false,
        overdue_grace_minutes: 60,
        clear_heartbeat: true,
      }),
    )
  })

  it('keeps settings open after a failed save so secrets and choices can be corrected', async () => {
    vi.mocked(api.saveBackupPolicy).mockRejectedValue(new Error('Monitor URL is invalid'))
    render(<BackupPolicyDialog policy={policy} onClose={vi.fn()} onSaved={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('External success heartbeat URL'), { target: { value: 'bad address' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save backup settings' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Monitor URL is invalid')
    expect(screen.getByLabelText('External success heartbeat URL')).toHaveValue('bad address')
  })

  it('keeps the panel usable and explains degraded recovery and host review', async () => {
    vi.mocked(api.getBackupPolicy).mockResolvedValue({ ...policy, paused: true, host_review_required: true })
    render(
      <BackupOperations
        refreshKey={1}
        recoveryError="Recorded container is missing"
        onChanged={vi.fn()}
        onStarted={vi.fn()}
      />,
    )
    expect(await screen.findByText(/Backup state needs review on this host after recovery or upgrade/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Service recovery' })).toBeEnabled()
    expect(screen.getByText(/Recorded container is missing/)).toBeInTheDocument()
  })

  it('requires explicit inspection acknowledgement for held data and never offers automatic restart', async () => {
    vi.mocked(api.getBackupRecoveryJournals).mockResolvedValue({
      items: [
        {
          run_id: 'held-run',
          hold_stopped: true,
          writers: [{ container_id: 'db-container', stack_id: 'demo', service: 'db', running: true }],
          phases: { 'db-container': 'stopped' },
          prepared_tokens: { db: 'private-token' },
        },
      ],
    })
    render(<ServiceRecoveryDialog onClose={vi.fn()} onChanged={vi.fn()} onStarted={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Review held services' }))
    const review = screen.getByRole('dialog', { name: 'Acknowledge inspected recovery' })
    expect(within(review).getByRole('button', { name: 'Acknowledge inspection' })).toBeDisabled()
    expect(review).toHaveTextContent('Services remain stopped and rollback data is preserved')
    expect(screen.queryByText('private-token')).not.toBeInTheDocument()
    fireEvent.change(within(review).getByRole('textbox'), { target: { value: 'INSPECTED' } })
    fireEvent.click(within(review).getByRole('button', { name: 'Acknowledge inspection' }))
    await waitFor(() =>
      expect(api.resolveBackupRecovery).toHaveBeenCalledWith('held-run', { action: 'acknowledge_hold' }),
    )
  })

  it('shows corrupt journal records without offering an unsafe automatic resolution', async () => {
    vi.mocked(api.getBackupRecoveryJournals).mockResolvedValue({
      items: [
        {
          run_id: 'broken.json',
          hold_stopped: false,
          writers: [],
          error: 'Invalid journal requires manual inspection',
        },
      ],
    })
    render(<ServiceRecoveryDialog onClose={vi.fn()} onChanged={vi.fn()} onStarted={vi.fn()} />)
    expect(await screen.findByText('Invalid journal requires manual inspection')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry recorded service recovery' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Review held services' })).not.toBeInTheDocument()
  })
})
