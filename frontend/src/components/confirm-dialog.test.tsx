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

  it('focuses required confirmation text and enables confirm only on an exact match', () => {
    const onConfirm = vi.fn()

    render(
      <ConfirmDialog
        title="Delete data?"
        message="This action is permanent."
        confirmLabel="Delete"
        requireText="demo"
        onCancel={vi.fn()}
        onConfirm={onConfirm}
      />,
    )

    const input = screen.getByRole('textbox')
    const confirm = screen.getByRole('button', { name: 'Delete' })
    expect(input).toHaveFocus()
    expect(confirm).toBeDisabled()

    fireEvent.change(input, { target: { value: 'Demo' } })
    expect(confirm).toBeDisabled()
    fireEvent.change(input, { target: { value: 'demo' } })
    expect(confirm).toBeEnabled()
    fireEvent.click(confirm)
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('blocks Escape and backdrop dismissal while confirming', () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog
        title="Apply settings?"
        message="Docker may restart."
        confirmLabel="Apply"
        confirming
        onCancel={onCancel}
        onConfirm={vi.fn()}
      />,
    )

    const dialog = screen.getByRole('dialog', { name: 'Apply settings?' })
    expect(dialog).toHaveAttribute('aria-busy', 'true')
    fireEvent.keyDown(window, { key: 'Escape' })
    fireEvent.click(dialog.parentElement!)
    expect(onCancel).not.toHaveBeenCalled()
  })
})
