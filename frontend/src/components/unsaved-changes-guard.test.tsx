import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { UnsavedChangesGuard } from './unsaved-changes-guard'

describe('UnsavedChangesGuard', () => {
  it('blocks internal link clicks until confirmed', () => {
    window.history.pushState(null, '', '/current')

    render(
      <>
        <UnsavedChangesGuard when />
        <a href="/next">Next</a>
      </>,
    )

    fireEvent.click(screen.getByRole('link', { name: 'Next' }))

    expect(window.location.pathname).toBe('/current')
    expect(screen.getByRole('dialog', { name: 'Discard unsaved changes?' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(window.location.pathname).toBe('/current')

    fireEvent.click(screen.getByRole('link', { name: 'Next' }))
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))

    expect(window.location.pathname).toBe('/next')
  })

})
