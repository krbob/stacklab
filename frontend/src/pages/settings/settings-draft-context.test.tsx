import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { createMemoryRouter, Link, RouterProvider } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { SettingsDraftProvider, useSettingsDraft } from './settings-draft-context'

function DraftToggle({ sectionId }: { sectionId: string }) {
  const [isDirty, setIsDirty] = useState(false)
  useSettingsDraft(sectionId, isDirty)

  return (
    <button type="button" onClick={() => setIsDirty((current) => !current)}>
      {isDirty ? `Clear ${sectionId}` : `Edit ${sectionId}`}
    </button>
  )
}

function SettingsDraftHarness() {
  return (
    <SettingsDraftProvider>
      <DraftToggle sectionId="password" />
      <DraftToggle sectionId="notifications" />
      <Link to="/outside">Leave settings</Link>
    </SettingsDraftProvider>
  )
}

function renderHarness() {
  const router = createMemoryRouter([
    { path: '/settings', element: <SettingsDraftHarness /> },
    { path: '/outside', element: <h1>Outside settings</h1> },
  ], { initialEntries: ['/settings'] })

  return { router, ...render(<RouterProvider router={router} />) }
}

describe('SettingsDraftProvider', () => {
  it('keeps one navigation blocker active until every section is clean', async () => {
    const { router } = renderHarness()

    fireEvent.click(screen.getByRole('button', { name: 'Edit password' }))
    fireEvent.click(screen.getByRole('button', { name: 'Edit notifications' }))
    fireEvent.click(screen.getByRole('link', { name: 'Leave settings' }))

    expect(router.state.location.pathname).toBe('/settings')
    expect(screen.getAllByRole('dialog', { name: 'Discard unsaved settings?' })).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getByRole('button', { name: 'Clear password' }))
    fireEvent.click(screen.getByRole('link', { name: 'Leave settings' }))

    expect(router.state.location.pathname).toBe('/settings')
    expect(screen.getAllByRole('dialog', { name: 'Discard unsaved settings?' })).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getByRole('button', { name: 'Clear notifications' }))
    fireEvent.click(screen.getByRole('link', { name: 'Leave settings' }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/outside'))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
