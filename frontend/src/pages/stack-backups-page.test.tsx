import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { ApiClientError } from '@/lib/api-client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import { StackBackupsPage } from './stack-backups-page'
import { getStackBackups, getUpdateProtection, saveUpdateProtection, type BackupPlan } from '@/lib/backup-api'

vi.mock('@/lib/backup-api', () => ({
  getStackBackups: vi.fn(), getUpdateProtection: vi.fn(async () => ({ auto_rollback: false, revision: 'new' })), saveUpdateProtection: vi.fn(),
}))

it('shows shared data coverage, changed identities and newly unselected mounts separately', async () => {
  const selected = { id: 'writer-data', kind: 'service_mount', stack_id: 'demo', service: 'writer', mount: '/data', path: 'demo_data' }
  const plan: BackupPlan = { id: 'plan', name: 'Daily data', enabled: true, consistency: 'stop', sources: [selected, { id: 'changed', kind: 'service_mount', stack_id: 'demo', service: 'db', mount: '/db', path: 'old_data' }], destinations: [{ destination_id: 'nas', enabled: true, schedule: { frequency: 'manual' }, retention: { daily: 7, weekly: 4, monthly: 12 } }], staging_max_bytes: 1024 ** 3, staging_ttl_hours: 24, stop_timeout_seconds: 300 }
  vi.mocked(getStackBackups).mockResolvedValue({ plans: [plan], sources: [selected, { ...selected, id: 'shared-data', service: 'shared' }, { id: 'changed', kind: 'service_mount', stack_id: 'demo', service: 'db', mount: '/db', path: 'new_data' }, { id: 'new-mount', kind: 'service_mount', stack_id: 'demo', service: 'cache', mount: '/cache', path: 'cache_data' }] })
  render(<MemoryRouter initialEntries={['/stacks/demo/backups']}><Routes><Route path="/stacks/:stackId/backups" element={<StackBackupsPage />} /></Routes></MemoryRouter>)
  const shared = await screen.findByText('shared → /data')
  expect(within(shared.closest('div')!).getByText('configured')).toBeInTheDocument()
  expect(within(screen.getByText('db → /db').closest('div')!).getByText('source changed')).toBeInTheDocument()
  expect(within(screen.getByText('cache → /cache').closest('div')!).getByText('unconfigured')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Manage backup plans' })).toHaveAttribute('href', '/backups?view=plans&stack=demo')
})

it('pins exactly the reviewed plan and requires saving again when its scope changes', async () => {
  const source = { id: 'db', kind: 'service_mount', stack_id: 'demo', service: 'db', mount: '/data', path: 'demo_db' }
  const plan: BackupPlan = {
    id: 'plan', name: 'Demo restore point', enabled: true, consistency: 'stop', sources: [source],
    destinations: [{ destination_id: 'nas', enabled: true, schedule: { frequency: 'manual' }, retention: { daily: 7, weekly: 4, monthly: 12 } }],
    staging_max_bytes: 1024 ** 3, staging_ttl_hours: 24, stop_timeout_seconds: 300, update_backup_fingerprint: 'new',
  }
  vi.mocked(getStackBackups).mockResolvedValue({ sources: [source], plans: [plan], destinations: [{
    id: 'nas', name: 'Home NAS', kind: 'local', repository: '/mnt/backup/demo', expected_filesystem: '7001',
    credentials_configured: true, status: 'available', check_read_data: 'metadata',
    metadata_check: { status: 'unknown', scope: 'unknown' }, data_check: { status: 'unknown', scope: 'unknown' },
    check_schedule: { frequency: 'manual' }, prune_schedule: { frequency: 'manual' },
  }] })
  vi.mocked(getUpdateProtection).mockResolvedValue({ revision: 'first', auto_rollback: true, backup_plan_id: 'plan', backup_source_ids: ['db'], backup_plan_fingerprint: 'old' })
  vi.mocked(saveUpdateProtection).mockImplementation(async (_id, policy) => ({ ...policy, revision: 'next' }))
  render(<MemoryRouter initialEntries={['/stacks/demo/backups']}><Routes><Route path="/stacks/:stackId/backups" element={<StackBackupsPage />} /></Routes></MemoryRouter>)
  const form = (await screen.findByText('Maintenance update protection')).closest('section')!
  const controls = within(form)
  expect(await controls.findByRole('checkbox')).toBeChecked()
  expect(await controls.findByText(/The plan has changed/)).toBeVisible()
  expect(controls.getByText(/Home NAS/)).toHaveTextContent('/mnt/backup/demo · Filesystem 7001')
  fireEvent.click(controls.getByRole('button', { name: 'Save update protection' }))
  await waitFor(() => expect(saveUpdateProtection).toHaveBeenCalledWith('demo', {
    auto_rollback: true, revision: 'first', backup_plan_id: 'plan', backup_source_ids: ['db'], backup_plan_fingerprint: 'new',
  }))
  expect(await controls.findByText('Saved')).toBeVisible()
  fireEvent.change(controls.getByRole('combobox'), { target: { value: '' } })
  fireEvent.click(controls.getByRole('button', { name: 'Save update protection' }))
  await waitFor(() => expect(saveUpdateProtection).toHaveBeenLastCalledWith('demo', { auto_rollback: true, revision: 'next' }))
})

it('requires reloading the current policy after a stale save is rejected', async () => {
  vi.mocked(getStackBackups).mockResolvedValue({ sources: [], plans: [] })
  vi.mocked(getUpdateProtection).mockResolvedValueOnce({ auto_rollback: false, revision: 'new' })
    .mockResolvedValueOnce({ auto_rollback: true, revision: 'updated' })
  vi.mocked(saveUpdateProtection).mockRejectedValueOnce(new ApiClientError(409, 'conflict', 'Update protection changed. Reload and review.'))
  render(<MemoryRouter initialEntries={['/stacks/demo/backups']}><Routes><Route path="/stacks/:stackId/backups" element={<StackBackupsPage />} /></Routes></MemoryRouter>)
  const checkbox = await screen.findByRole('checkbox')
  fireEvent.click(checkbox)
  fireEvent.click(screen.getByRole('button', { name: 'Save update protection' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Reload and review')
  expect(screen.getByRole('button', { name: 'Save update protection' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: 'Reload current settings' }))
  await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  expect(screen.getByRole('checkbox')).toBeChecked()
  expect(screen.getByRole('button', { name: 'Save update protection' })).toBeDisabled()
  fireEvent.click(screen.getByRole('checkbox'))
  fireEvent.click(screen.getByRole('button', { name: 'Save update protection' }))
  await waitFor(() => expect(saveUpdateProtection).toHaveBeenLastCalledWith('demo', { auto_rollback: false, revision: 'updated' }))
})
