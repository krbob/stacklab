import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AsyncState } from './async-state'

function renderState(overrides: Partial<Parameters<typeof AsyncState>[0]> = {}) {
  const props: Parameters<typeof AsyncState>[0] = {
    loading: false,
    error: null,
    hasData: true,
    isEmpty: false,
    loadingLabel: 'Loading inventory...',
    emptyMessage: 'No inventory found.',
    onRetry: vi.fn(),
    loadingFallback: <div>Skeleton</div>,
    children: <div>Inventory row</div>,
    ...overrides,
  }
  return { ...render(<AsyncState {...props} />), props }
}

describe('AsyncState', () => {
  it('shows only the loading fallback before the first response', () => {
    renderState({ loading: true, hasData: false, isEmpty: true })

    expect(screen.getByRole('status')).toHaveTextContent('Loading inventory...')
    expect(screen.getByText('Skeleton')).toBeInTheDocument()
    expect(screen.queryByText('Inventory row')).not.toBeInTheDocument()
    expect(screen.queryByText('No inventory found.')).not.toBeInTheDocument()
  })

  it('shows an initial error with Retry and no false empty state', () => {
    const onRetry = vi.fn()
    renderState({
      error: new Error('Docker unavailable'),
      hasData: false,
      isEmpty: true,
      onRetry,
    })

    expect(screen.getByRole('alert')).toHaveTextContent('Docker unavailable')
    expect(screen.queryByText('No inventory found.')).not.toBeInTheDocument()
    expect(screen.queryByText('Inventory row')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('keeps loaded content visible while refreshing', () => {
    renderState({ loading: true })

    expect(screen.getByRole('status')).toHaveTextContent('Refreshing…')
    expect(screen.getByText('Inventory row')).toBeInTheDocument()
  })

  it('keeps the last successful data visible after a refetch error', () => {
    renderState({ error: new Error('Refresh failed') })

    expect(screen.getByRole('alert')).toHaveTextContent('Showing the last successfully loaded data.')
    expect(screen.getByText('Inventory row')).toBeInTheDocument()
  })

  it('shows the empty state only after a successful empty response', () => {
    renderState({ isEmpty: true })

    expect(screen.getByText('No inventory found.')).toBeInTheDocument()
    expect(screen.queryByText('Inventory row')).not.toBeInTheDocument()
  })
})
