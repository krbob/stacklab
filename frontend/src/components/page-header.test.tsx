import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { PageHeader } from './page-header'

describe('PageHeader', () => {
  it('renders the screen title as the sole top-level heading', () => {
    render(<PageHeader kicker="System" title="Host" meta={<span>Online</span>} />)

    expect(screen.getByRole('heading', { level: 1, name: 'Host' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  })
})
