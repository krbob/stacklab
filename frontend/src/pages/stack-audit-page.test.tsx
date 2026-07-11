import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import { StackAuditPage } from './stack-audit-page'
import { JobDrawerProvider } from '@/contexts/job-drawer-context'

const mockGetStackAudit = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getStackAudit: (...args: unknown[]) => mockGetStackAudit(...args),
}))

const stack = {
  id: 'demo',
  name: 'Demo',
  description: '',
  runtime_state: 'defined',
  activity_state: 'idle',
  service_count: { defined: 1, running: 0, stopped: 0, unhealthy: 0 },
  source: { compose_path: '/stacks/demo/compose.yaml', env_path: null },
  services: [],
  available_actions: [],
}

function StackOutlet() {
  return <Outlet context={{ stack }} />
}

describe('StackAuditPage', () => {
  beforeEach(() => {
    mockGetStackAudit.mockReset()
    mockGetStackAudit.mockResolvedValue({ items: [], next_cursor: null })
  })

  it('applies shared URL filters to the per-stack endpoint', async () => {
    render(
      <MemoryRouter initialEntries={['/stacks/demo/audit?q=pull&result=timed_out']}>
        <JobDrawerProvider>
          <Routes>
            <Route path="/stacks/:stackId" element={<StackOutlet />}>
              <Route path="audit" element={<StackAuditPage />} />
            </Route>
          </Routes>
        </JobDrawerProvider>
      </MemoryRouter>,
    )

    await waitFor(() => expect(mockGetStackAudit).toHaveBeenCalledWith(
      'demo',
      { q: 'pull', result: 'timed_out', limit: 50 },
      expect.any(AbortSignal),
    ))
    expect(screen.getByText('No stack operations match these filters')).toBeInTheDocument()
  })
})
