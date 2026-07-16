import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { HostMetricSample } from '@/lib/api-types'
import { HostProcessesPanel } from './host-processes-panel'

type ProcessItem = NonNullable<HostMetricSample['processes']>['items'][number]

function makeProcess(partial: Partial<ProcessItem> & Pick<ProcessItem, 'pid'>): ProcessItem {
  return {
    user: 'app',
    state: 'S',
    cpu_percent: 1,
    memory_bytes: 1024,
    memory_percent: 0.1,
    command: `process-${partial.pid}`,
    ...partial,
  }
}

describe('HostProcessesPanel', () => {
  it('puts service and container identity before the shared stack name', () => {
    render(
      <HostProcessesPanel
        processes={{
          total: 2,
          items: [
            makeProcess({
              pid: 100,
              cpu_percent: 2,
              container: {
                id: 'aaaaaaaaaaaaaaaaaaaaaaaa',
                name: 'portfolio-portfolio-1',
                stack_id: 'portfolio',
                service_name: 'portfolio',
              },
            }),
            makeProcess({
              pid: 200,
              cpu_percent: 1,
              container: {
                id: 'bbbbbbbbbbbbbbbbbbbbbbbb',
                name: 'portfolio-backend-1',
                stack_id: 'portfolio',
                service_name: 'portfolio-backend',
              },
            }),
          ],
        }}
        sortKey="cpu"
        onSortChange={vi.fn()}
      />,
    )

    const frontendRow = screen.getByText('portfolio-portfolio-1').closest('tr')
    const backendRow = screen.getByText('portfolio-backend-1').closest('tr')
    expect(frontendRow).not.toBeNull()
    expect(backendRow).not.toBeNull()

    expect(within(frontendRow!).getByText('portfolio')).toBeInTheDocument()
    expect(within(frontendRow!).getByText('stack portfolio')).toBeInTheDocument()
    expect(within(backendRow!).getByText('portfolio-backend')).toBeInTheDocument()
    expect(within(backendRow!).getByText('stack portfolio')).toBeInTheDocument()
    expect(screen.queryByText('portfolio / portfolio-backend')).not.toBeInTheDocument()
    expect(within(backendRow!).getByText('portfolio-backend-1')).toHaveClass('[overflow-wrap:anywhere]')
    expect(within(backendRow!).getByText('portfolio-backend-1')).not.toHaveClass('truncate')

    expect(within(backendRow!).getByLabelText(
      'Stack: portfolio · Service: portfolio-backend · Container: portfolio-backend-1 · ID: bbbbbbbbbbbbbbbbbbbbbbbb',
    )).toBeInTheDocument()
  })

  it('keeps unmanaged Docker containers and host processes identifiable', () => {
    render(
      <HostProcessesPanel
        processes={{
          total: 2,
          items: [
            makeProcess({
              pid: 300,
              cpu_percent: 2,
              container: {
                id: 'cccccccccccccccccccccccc',
                name: 'standalone-proxy',
                stack_id: '',
                service_name: '',
              },
            }),
            makeProcess({ pid: 400, cpu_percent: 1 }),
          ],
        }}
        sortKey="cpu"
        onSortChange={vi.fn()}
      />,
    )

    expect(screen.getByText('standalone-proxy')).toBeInTheDocument()
    expect(screen.getByText('Docker')).toBeInTheDocument()
    expect(screen.getByLabelText('Host process')).toHaveTextContent('Host')
  })
})
