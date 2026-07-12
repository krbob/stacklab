import { render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RouteErrorPage } from './route-error-page'

describe('RouteErrorPage', () => {
  afterEach(() => vi.restoreAllMocks())

  it('replaces a crashed route with safe full-page recovery actions', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const router = createMemoryRouter([
      {
        path: '*',
        element: <BrokenRoute />,
        errorElement: <RouteErrorPage />,
      },
    ], { initialEntries: ['/broken?tab=logs#tail'] })

    render(<RouterProvider router={router} />)

    const heading = await screen.findByRole('heading', { level: 1, name: 'This view could not be displayed' })
    expect(screen.getByRole('alert')).toContainElement(heading)
    expect(heading).toHaveFocus()
    expect(screen.queryByText('route chunk unavailable')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Retry' })).toHaveAttribute('href', '/broken?tab=logs#tail')
    expect(screen.getByRole('link', { name: 'Back to stacks' })).toHaveAttribute('href', '/stacks')
    expect(document.title).toBe('Application error | Stacklab')
  })
})

function BrokenRoute(): never {
  throw new Error('route chunk unavailable')
}
