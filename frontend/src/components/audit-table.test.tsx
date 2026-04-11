import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AuditTable } from './audit-table'
import { JobDrawerProvider } from '@/contexts/job-drawer-context'
import { JobDetailDrawer } from './job-detail-drawer'
import type { AuditEntry } from '@/lib/api-types'

const mockGetJob = vi.fn()
const mockGetJobEvents = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getJob: (...args: unknown[]) => mockGetJob(...args),
  getJobEvents: (...args: unknown[]) => mockGetJobEvents(...args),
}))

const entries: AuditEntry[] = [
  {
    id: 'audit_1',
    stack_id: 'demo',
    job_id: 'job_1',
    action: 'pull',
    requested_by: 'local',
    result: 'succeeded',
    requested_at: '2026-04-09T08:00:00Z',
    finished_at: '2026-04-09T08:00:05Z',
    duration_ms: 5000,
  },
]

function renderAudit() {
  return render(
    <MemoryRouter>
      <JobDrawerProvider>
        <AuditTable entries={entries} />
        <JobDetailDrawer />
      </JobDrawerProvider>
    </MemoryRouter>,
  )
}

describe('AuditTable', () => {
  beforeEach(() => {
    mockGetJob.mockReset()
    mockGetJobEvents.mockReset()
  })

  it('opens shared job detail drawer', async () => {
    mockGetJob.mockResolvedValue({
      job: {
        id: 'job_1',
        stack_id: 'demo',
        action: 'pull',
        state: 'succeeded',
        requested_at: '2026-04-09T08:00:00Z',
        started_at: '2026-04-09T08:00:01Z',
        finished_at: '2026-04-09T08:00:05Z',
        workflow: null,
      },
    })
    mockGetJobEvents.mockResolvedValue({
      job_id: 'job_1',
      retained: true,
      items: [],
    })

    renderAudit()
    fireEvent.click(screen.getByRole('button', { name: 'View detail' }))

    expect(await screen.findByText('Job detail')).toBeInTheDocument()
    expect(mockGetJob).toHaveBeenCalledWith('job_1')
    expect(mockGetJobEvents).toHaveBeenCalledWith('job_1')
  })
})
