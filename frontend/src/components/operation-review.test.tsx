import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { OperationReview } from './operation-review'

describe('OperationReview', () => {
  it('presents target, scope, impact, snapshot, and recovery consistently', () => {
    render(
      <OperationReview review={{
        target: 'stack demo',
        scope: ['runtime containers', 'compose definition'],
        impact: ['services will stop', 'definition will be deleted'],
        snapshot: 'Current definition is committed in Git.',
        recovery: 'Restore the definition and deploy the stack again.',
      }} />,
    )

    const review = screen.getByRole('region', { name: 'Review operation' })
    for (const label of ['Target', 'Scope', 'Impact', 'Snapshot', 'Recovery']) {
      expect(within(review).getByText(label)).toBeInTheDocument()
    }
    expect(review).toHaveTextContent('stack demo')
    expect(review).toHaveTextContent('runtime containers')
    expect(review).toHaveTextContent('services will stop')
    expect(review).toHaveTextContent('Current definition is committed in Git.')
    expect(review).toHaveTextContent('Restore the definition and deploy the stack again.')
  })
})
