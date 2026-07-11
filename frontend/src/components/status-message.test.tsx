import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { StatusMessage } from './status-message'

describe('StatusMessage', () => {
  it('announces action feedback once as a polite atomic status', () => {
    render(<StatusMessage>Saved</StatusMessage>)

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent('Saved')
    expect(status).toHaveAttribute('aria-live', 'polite')
    expect(status).toHaveAttribute('aria-atomic', 'true')
  })
})
