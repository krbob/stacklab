import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { CommandPalette } from './command-palette'

const mockGetStacks = vi.fn()

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })

  return { promise, resolve, reject }
}

vi.mock('@/lib/api-client', () => ({
  getStacks: (...args: unknown[]) => mockGetStacks(...args),
}))

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{location.pathname}</output>
}

describe('CommandPalette', () => {
  beforeEach(() => {
    mockGetStacks.mockReset()
    mockGetStacks.mockResolvedValue({
      items: [{ id: 'demo', name: 'Demo', display_state: 'running' }],
      summary: { stack_count: 1 },
    })
  })

  it('exposes a keyboard-operated combobox and listbox', async () => {
    document.body.style.overflow = 'auto'
    render(
      <MemoryRouter>
        <button>Before palette</button>
        <CommandPalette />
      </MemoryRouter>,
    )

    const trigger = screen.getByRole('button', { name: 'Before palette' })
    trigger.focus()
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })

    const dialog = screen.getByRole('dialog', { name: 'Command palette' })
    const combobox = screen.getByRole('combobox', { name: 'Search commands' })
    expect(dialog).toBeInTheDocument()
    expect(combobox).toHaveFocus()
    expect(document.body).toHaveStyle({ overflow: 'hidden' })
    expect(combobox).toHaveAttribute('aria-controls', screen.getByRole('listbox').id)

    await waitFor(() => expect(screen.getAllByRole('option')).toHaveLength(14))
    const options = screen.getAllByRole('option')
    expect(options[0]).toHaveAttribute('aria-selected', 'true')
    expect(combobox).toHaveAttribute('aria-activedescendant', options[0].id)

    fireEvent.keyDown(combobox, { key: 'ArrowDown' })
    expect(options[0]).toHaveAttribute('aria-selected', 'false')
    expect(options[1]).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('status')).toHaveTextContent('14 results available.')

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: 'Command palette' })).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
    expect(document.body).toHaveStyle({ overflow: 'auto' })
    document.body.style.overflow = ''
  })

  it('announces an empty filtered result without creating an alert', async () => {
    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    )
    fireEvent.keyDown(window, { key: 'k', metaKey: true })
    await waitFor(() => expect(screen.getAllByRole('option')).toHaveLength(14))
    const combobox = screen.getByRole('combobox', { name: 'Search commands' })
    fireEvent.change(combobox, { target: { value: 'not-a-command' } })

    expect(screen.getByText('No matches')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('0 results available.'))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('keeps the palette usable and retries a failed stack load in place', async () => {
    const retry = deferred<{
      items: Array<{ id: string; name: string; display_state: string }>
      summary: { stack_count: number }
    }>()
    mockGetStacks
      .mockRejectedValueOnce(new Error('stack endpoint unavailable'))
      .mockReturnValueOnce(retry.promise)

    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    )
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Stack shortcuts are unavailable. Section shortcuts still work.')
    expect(screen.getAllByRole('option')).toHaveLength(13)

    const combobox = screen.getByRole('combobox', { name: 'Search commands' })
    const retryButton = screen.getByRole('button', { name: 'Retry' })
    combobox.focus()
    fireEvent.keyDown(combobox, { key: 'Tab' })
    expect(retryButton).toHaveFocus()
    fireEvent.keyDown(retryButton, { key: 'Tab' })
    expect(combobox).toHaveFocus()

    fireEvent.change(combobox, { target: { value: 'Demo' } })
    expect(screen.queryByText('No matches')).not.toBeInTheDocument()

    retryButton.focus()
    fireEvent.click(retryButton)

    expect(mockGetStacks).toHaveBeenCalledTimes(2)
    expect(combobox).toHaveValue('Demo')
    expect(combobox).toHaveFocus()
    expect(screen.queryByText('No matches')).not.toBeInTheDocument()

    await act(async () => {
      retry.resolve({
        items: [{ id: 'demo', name: 'Demo', display_state: 'running' }],
        summary: { stack_count: 1 },
      })
      await retry.promise
    })

    expect(await screen.findByRole('option', { name: /Demo/ })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(combobox).toHaveValue('Demo')
    expect(combobox).toHaveFocus()
  })

  it('finds every settings subsection by purpose and navigates to its direct route', async () => {
    const shortcuts = [
      { query: 'password', label: 'Settings: Security', to: '/settings/security' },
      { query: 'telegram', label: 'Settings: Notifications', to: '/settings/notifications' },
      { query: 'scheduled cleanup', label: 'Settings: Automation', to: '/settings/automation' },
      { query: 'upgrade', label: 'Settings: Updates', to: '/settings/updates' },
      { query: 'build information', label: 'Settings: About', to: '/settings/about' },
    ]

    render(
      <MemoryRouter>
        <CommandPalette />
        <LocationProbe />
      </MemoryRouter>,
    )

    for (const shortcut of shortcuts) {
      fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
      const combobox = screen.getByRole('combobox', { name: 'Search commands' })
      fireEvent.change(combobox, { target: { value: shortcut.query } })

      const option = screen.getByRole('option', { name: new RegExp(shortcut.label) })
      expect(screen.getAllByRole('option')).toEqual([option])
      fireEvent.click(option)

      expect(screen.getByTestId('location')).toHaveTextContent(shortcut.to)
      expect(screen.queryByRole('dialog', { name: 'Command palette' })).not.toBeInTheDocument()
    }
  })
})
