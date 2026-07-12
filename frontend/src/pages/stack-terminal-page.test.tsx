import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { StackTerminalPage } from './stack-terminal-page'

const terminal = {
  state: 'disconnected' as const,
  exitInfo: null,
  errorMessage: null,
  sessionId: 'term_demo',
  open: vi.fn(),
  attach: vi.fn(),
  write: vi.fn(),
  resize: vi.fn(),
  close: vi.fn(),
  onData: vi.fn(),
}

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useOutletContext: () => ({
      stack: {
        id: 'demo',
        containers: [{
          id: 'container_1',
          service_name: 'web',
          name: 'demo-web-1',
          status: 'running',
        }],
      },
    }),
  }
})

vi.mock('@/hooks/use-ws', () => ({
  useWs: () => ({ connected: false }),
}))

vi.mock('@/hooks/use-terminal', () => ({
  useTerminal: () => terminal,
}))

vi.mock('@/components/terminal-view', () => ({
  TerminalView: ({ readOnly }: { readOnly?: boolean }) => (
    <div data-testid="terminal-view" data-read-only={String(readOnly)} />
  ),
}))

describe('StackTerminalPage', () => {
  it('keeps a disconnected session visible but read-only while reattaching', () => {
    render(
      <MemoryRouter>
        <StackTerminalPage />
      </MemoryRouter>,
    )

    expect(screen.getByText('Connection lost. Attempting to reattach...')).toBeInTheDocument()
    expect(screen.getByText('Reconnecting...')).toBeInTheDocument()
    expect(screen.queryByText('Connected')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Container:')).toBeDisabled()
    expect(screen.getByLabelText('Shell:')).toBeDisabled()
    expect(screen.getByTestId('terminal-view')).toHaveAttribute('data-read-only', 'true')
    expect(screen.getByRole('button', { name: 'End session' })).toBeInTheDocument()
  })
})
