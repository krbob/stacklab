import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createMemoryRouter, Link, RouterProvider, useNavigate } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { UnsavedChangesGuard } from './unsaved-changes-guard'

function GuardHarness({ when = true }: { when?: boolean }) {
  const navigate = useNavigate()

  return (
    <>
      <UnsavedChangesGuard when={when} />
      <Link to="/linked">Link navigation</Link>
      <button onClick={() => navigate('/programmatic')}>Programmatic navigation</button>
      <button onClick={() => navigate(-1)}>Back navigation</button>
    </>
  )
}

function renderGuard(initialEntries = ['/current'], initialIndex = initialEntries.length - 1, when = true) {
  const router = createMemoryRouter(
    [{ path: '*', element: <GuardHarness when={when} /> }],
    { initialEntries, initialIndex },
  )
  return { router, ...render(<RouterProvider router={router} />) }
}

describe('UnsavedChangesGuard', () => {
  it('blocks link navigation and preserves the original destination through cancel and confirm', () => {
    const { router } = renderGuard()

    fireEvent.click(screen.getByRole('link', { name: 'Link navigation' }))

    expect(router.state.location.pathname).toBe('/current')
    expect(screen.getByRole('dialog', { name: 'Discard unsaved changes?' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/current')

    fireEvent.click(screen.getByRole('link', { name: 'Link navigation' }))
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))

    expect(router.state.location.pathname).toBe('/linked')
  })

  it('blocks programmatic navigation used by hotkeys and the command palette', () => {
    const { router } = renderGuard()

    fireEvent.click(screen.getByRole('button', { name: 'Programmatic navigation' }))

    expect(router.state.location.pathname).toBe('/current')
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))
    expect(router.state.location.pathname).toBe('/programmatic')
  })

  it('blocks browser history back navigation', async () => {
    const { router } = renderGuard(['/previous', '/current'])

    fireEvent.click(screen.getByRole('button', { name: 'Back navigation' }))

    expect(router.state.location.pathname).toBe('/current')
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/previous'))
  })

  it('only prevents beforeunload while changes are unsaved', () => {
    const clean = renderGuard(['/clean'], 0, false)
    const cleanEvent = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(cleanEvent)
    expect(cleanEvent.defaultPrevented).toBe(false)
    clean.unmount()

    renderGuard(['/dirty'])
    const dirtyEvent = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(dirtyEvent)
    expect(dirtyEvent.defaultPrevented).toBe(true)
  })
})
