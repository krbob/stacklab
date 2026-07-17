import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import type { LogEntry } from '@/lib/ws-types'
import { StackLogsPage } from './stack-logs-page'

const mockUseLogStream = vi.fn()

vi.mock('@/hooks/use-log-stream', () => ({
  useLogStream: (...args: unknown[]) => mockUseLogStream(...args),
}))

vi.mock('@/hooks/use-ws', () => ({
  useWs: () => ({ connected: true }),
}))

const entries: LogEntry[] = [
  {
    timestamp: '2026-07-12T08:30:00Z',
    service_name: 'api',
    container_id: 'container-api',
    stream: 'stdout',
    line: 'server  started',
  },
  {
    timestamp: '2026-07-12T08:30:01Z',
    service_name: 'worker',
    container_id: 'container-worker',
    stream: 'stderr',
    line: 'retry scheduled',
  },
]

const stack = {
  id: 'demo',
  services: [{ name: 'api' }, { name: 'worker' }],
  containers: [
    { service_name: 'api', status: 'running' },
    { service_name: 'worker', status: 'running' },
  ],
}

let outletStack = stack

function StackOutlet() {
  return <Outlet context={{ stack: outletStack }} />
}

function renderPage(initialEntry = '/stacks/demo/logs') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/stacks/:stackId" element={<StackOutlet />}>
          <Route path="logs" element={<StackLogsPage />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

describe('StackLogsPage', () => {
  beforeEach(() => {
    outletStack = stack
    mockUseLogStream.mockReset()
    mockUseLogStream.mockReturnValue({
      entries,
      error: null,
      paused: false,
      pause: vi.fn(),
      resume: vi.fn(),
      clear: vi.fn(),
    })
  })

  it('copies and downloads only visible lines and toggles wrapping', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:logs') })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    renderPage()

    const wrap = screen.getByRole('button', { name: 'Wrap lines' })
    expect(wrap).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getAllByTestId('log-line')[0]).toHaveClass('whitespace-pre-wrap')
    fireEvent.click(wrap)
    expect(wrap).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getAllByTestId('log-line')[0]).toHaveClass('whitespace-pre')

    fireEvent.change(screen.getByPlaceholderText('Filter...'), { target: { value: 'server' } })
    expect(screen.getByText('Lines: 1 of 2')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Copy visible' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(
      '[2026-07-12T08:30:00Z] [api] [stdout] server  started\n',
    ))
    expect(screen.getByRole('status')).toHaveTextContent('Copied 1 log line.')

    fireEvent.click(screen.getByRole('button', { name: 'Download visible' }))
    expect(screen.getByRole('status')).toHaveTextContent('Downloaded 1 log line.')
  })

  it('shows a filtered empty state and disables transfer actions', () => {
    renderPage()

    fireEvent.change(screen.getByPlaceholderText('Filter...'), { target: { value: 'missing' } })

    expect(screen.getByText('No log lines match the current filter.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy visible' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Download visible' })).toBeDisabled()
  })

  it.each(['restarting', 'exited', 'dead'])('reads retained logs from a %s container', (status) => {
    outletStack = {
      ...stack,
      containers: [{ service_name: 'api', status }],
    }

    renderPage('/stacks/demo/logs?service=api')

    expect(mockUseLogStream).toHaveBeenCalledWith({
      stackId: 'demo',
      serviceNames: ['api'],
      enabled: true,
    })
    expect(screen.queryByText('No logs available')).not.toBeInTheDocument()
  })

  it('reads logs from an orphaned runtime container when All is selected', () => {
    outletStack = {
      ...stack,
      services: [],
      containers: [{ service_name: 'removed-service', status: 'restarting' }],
    }

    renderPage()

    expect(mockUseLogStream).toHaveBeenCalledWith({
      stackId: 'demo',
      serviceNames: [],
      enabled: true,
    })
    expect(screen.queryByText('No logs available')).not.toBeInTheDocument()
  })

  it('shows an empty state only when the selected service has no container', () => {
    outletStack = {
      ...stack,
      containers: [{ service_name: 'worker', status: 'running' }],
    }

    renderPage('/stacks/demo/logs?service=api')

    expect(mockUseLogStream).toHaveBeenCalledWith({
      stackId: 'demo',
      serviceNames: ['api'],
      enabled: false,
    })
    expect(screen.getByText('No logs available')).toBeInTheDocument()
    expect(screen.getByText('The selected service has no container yet.')).toBeInTheDocument()
  })

  it('explains a log stream failure instead of waiting forever', () => {
    mockUseLogStream.mockReturnValue({
      entries: [],
      error: 'Container log stream terminated unexpectedly.',
      paused: false,
      pause: vi.fn(),
      resume: vi.fn(),
      clear: vi.fn(),
    })

    renderPage()

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Log stream failed: Container log stream terminated unexpectedly.',
    )
    expect(screen.getByText('Log stream unavailable.')).toBeInTheDocument()
    expect(screen.queryByText('Waiting for logs...')).not.toBeInTheDocument()
  })
})
