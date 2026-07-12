import { useRef } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Dialog } from './dialog'

function ExampleDialog({ onClose, preventClose = false }: {
  onClose: () => void
  preventClose?: boolean
}) {
  const cancelRef = useRef<HTMLButtonElement>(null)
  return (
    <Dialog
      title="Remove stack?"
      onClose={onClose}
      preventClose={preventClose}
      busy={preventClose}
      initialFocusRef={cancelRef}
    >
      <button type="button" ref={cancelRef}>Cancel</button>
      <button type="button">Remove</button>
    </Dialog>
  )
}

describe('Dialog', () => {
  it('labels the modal, locks scrolling, closes on Escape, and restores focus', () => {
    const outside = document.createElement('button')
    document.body.append(outside)
    outside.focus()
    document.body.style.overflow = 'auto'
    const onClose = vi.fn()

    const { unmount } = render(<ExampleDialog onClose={onClose} />)

    expect(screen.getByRole('dialog', { name: 'Remove stack?' })).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()
    expect(document.body).toHaveStyle({ overflow: 'hidden' })

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)

    unmount()
    expect(document.body).toHaveStyle({ overflow: 'auto' })
    expect(outside).toHaveFocus()
    outside.remove()
    document.body.style.overflow = ''
  })

  it('keeps keyboard focus inside the dialog', () => {
    const outside = document.createElement('button')
    document.body.append(outside)
    const onClose = vi.fn()
    render(<ExampleDialog onClose={onClose} />)

    const cancel = screen.getByRole('button', { name: 'Cancel' })
    const remove = screen.getByRole('button', { name: 'Remove' })

    cancel.focus()
    fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
    expect(remove).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Tab' })
    expect(cancel).toHaveFocus()

    outside.focus()
    fireEvent.keyDown(window, { key: 'Tab' })
    expect(cancel).toHaveFocus()

    fireEvent.click(remove)
    expect(onClose).not.toHaveBeenCalled()
    outside.remove()
  })

  it('blocks Escape and backdrop dismissal while close is prevented', () => {
    const onClose = vi.fn()
    const { rerender } = render(<ExampleDialog onClose={onClose} preventClose />)
    const dialog = screen.getByRole('dialog', { name: 'Remove stack?' })

    expect(dialog).toHaveAttribute('aria-busy', 'true')
    fireEvent.keyDown(window, { key: 'Escape' })
    fireEvent.click(dialog.parentElement!)
    expect(onClose).not.toHaveBeenCalled()

    rerender(<ExampleDialog onClose={onClose} />)
    fireEvent.click(screen.getByRole('dialog', { name: 'Remove stack?' }).parentElement!)
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
