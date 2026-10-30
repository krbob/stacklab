import { fireEvent, render, screen, within } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import type { BackupDestination } from '@/lib/backup-api'
import { Field } from './backup-primitives'
import { PruneReviewDialog } from './prune-review-dialog'

const destination: BackupDestination = {
  id: 'nas', name: 'NAS', kind: 'local', repository: '/repo', status: 'available',
  credentials_configured: true, metadata_check: { status: 'unknown', scope: 'unknown' },
  data_check: { status: 'unknown', scope: 'unknown' }, check_schedule: { frequency: 'manual' },
  prune_schedule: { frequency: 'manual' }, check_read_data: 'metadata',
  maintenance_review_token: 'a', check_hold: { scope: 'all', error: 'Review evidence' },
}
const callbacks = { onCancel: vi.fn(), onConfirm: vi.fn(), onRefresh: vi.fn(), pending: false, refreshing: false, refreshFailed: false, error: null }

it.each([undefined, 'Additional help'])('preserves child descriptions alongside a Field hint: %s', hint => {
  render(<><p id="reason">Evidence changed</p><Field label="Confirmation" hint={hint}><input aria-describedby="reason" /></Field></>)
  expect(screen.getByLabelText('Confirmation')).toHaveAccessibleDescription(hint ? 'Evidence changed Additional help' : 'Evidence changed')
})

it('keeps the live region mounted and its explanation until confirmation is complete', () => {
  const { rerender } = render(<PruneReviewDialog {...callbacks} destination={destination} />)
  const region = within(screen.getByRole('dialog')).getByRole('status')
  const input = screen.getByRole('textbox')
  fireEvent.change(input, { target: { value: 'NAS' } })
  rerender(<PruneReviewDialog {...callbacks} destination={{ ...destination, maintenance_review_token: 'b' }} />)
  expect(screen.getByRole('status')).toBe(region)
  expect(input).toHaveAccessibleDescription(/Typed confirmation was reset/)
  fireEvent.change(input, { target: { value: 'N' } })
  expect(region).toHaveTextContent('Typed confirmation was reset')
  fireEvent.change(input, { target: { value: 'NAS' } })
  expect(region).toBeEmptyDOMElement()
})

it('retains focus on the panel while the focused submit and cancel controls are disabled', () => {
  const { rerender } = render(<PruneReviewDialog {...callbacks} destination={destination} />)
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'NAS' } })
  screen.getByRole('button', { name: 'Start cleanup' }).focus()
  rerender(<PruneReviewDialog {...callbacks} destination={destination} pending />)
  expect(screen.getByRole('dialog')).toHaveFocus()
  rerender(<PruneReviewDialog {...callbacks} destination={{ ...destination, check_hold: undefined, maintenance_review_token: '' }} pending />)
  expect(screen.getByRole('dialog')).toHaveFocus()
  expect(screen.getByRole('status')).toHaveTextContent('Starting cleanup')
})
