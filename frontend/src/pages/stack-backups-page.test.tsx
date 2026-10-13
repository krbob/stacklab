import { render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import { StackBackupsPage } from './stack-backups-page'
import { getStackBackups, type BackupPlan } from '@/lib/backup-api'

vi.mock('@/lib/backup-api', () => ({ getStackBackups: vi.fn() }))

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
