import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ConfirmDialog } from './confirm-dialog'

describe('ConfirmDialog', () => {
  it('keeps keyboard focus inside the dialog and closes on Escape', () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()

    render(
      <>
        <button type="button">Outside</button>
        <ConfirmDialog
          title="Remove volume?"
          message="This action is permanent."
          confirmLabel="Remove"
          onCancel={onCancel}
          onConfirm={onConfirm}
        />
      </>,
    )

    const cancel = screen.getByRole('button', { name: 'Cancel' })
    const confirm = screen.getByRole('button', { name: 'Remove' })
    expect(cancel).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
    expect(confirm).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Tab' })
    expect(cancel).toHaveFocus()

    screen.getByRole('button', { name: 'Outside' }).focus()
    fireEvent.keyDown(window, { key: 'Tab' })
    expect(cancel).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onCancel).toHaveBeenCalledTimes(1)
  })
})
