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

function StackOutlet() {
  return <Outlet context={{ stack }} />
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/stacks/demo/logs']}>
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
    mockUseLogStream.mockReset()
    mockUseLogStream.mockReturnValue({
      entries,
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
})
