import { useRef } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Drawer } from './drawer'

function ExampleDrawer({ onClose, preventClose = false }: {
  onClose: () => void
  preventClose?: boolean
}) {
  const closeRef = useRef<HTMLButtonElement>(null)

  return (
    <Drawer
      id="activity-drawer"
      label="Activity"
      onClose={onClose}
      preventClose={preventClose}
      busy={preventClose}
      initialFocusRef={closeRef}
    >
      <button type="button" ref={closeRef}>Close</button>
      <button type="button">Inspect</button>
    </Drawer>
  )
}

describe('Drawer', () => {
  it('labels the modal, locks scrolling, closes on Escape, and restores focus', () => {
    const outside = document.createElement('button')
    document.body.append(outside)
    outside.focus()
    document.body.style.overflow = 'auto'
    const onClose = vi.fn()

    const { unmount } = render(<ExampleDrawer onClose={onClose} />)

    expect(screen.getByRole('dialog', { name: 'Activity' })).toHaveAttribute('id', 'activity-drawer')
    expect(screen.getByRole('dialog', { name: 'Activity' })).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus()
    expect(document.body).toHaveStyle({ overflow: 'hidden' })

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)

    unmount()
    expect(document.body).toHaveStyle({ overflow: 'auto' })
    expect(outside).toHaveFocus()
    outside.remove()
    document.body.style.overflow = ''
  })

  it('keeps keyboard focus inside the drawer in both directions', () => {
    const onClose = vi.fn()
    render(
      <>
        <button type="button">Outside</button>
        <ExampleDrawer onClose={onClose} />
      </>,
    )

    const outside = screen.getByRole('button', { name: 'Outside' })
    const close = screen.getByRole('button', { name: 'Close' })
    const inspect = screen.getByRole('button', { name: 'Inspect' })

    close.focus()
    fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
    expect(inspect).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Tab' })
    expect(close).toHaveFocus()

    outside.focus()
    fireEvent.keyDown(window, { key: 'Tab' })
    expect(close).toHaveFocus()

    outside.focus()
    fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
    expect(inspect).toHaveFocus()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('blocks Escape and backdrop dismissal while close is prevented', () => {
    const onClose = vi.fn()
    const { rerender } = render(<ExampleDrawer onClose={onClose} preventClose />)
    const dialog = screen.getByRole('dialog', { name: 'Activity' })
    const backdrop = dialog.previousElementSibling as HTMLElement

    expect(dialog).toHaveAttribute('aria-busy', 'true')
    fireEvent.keyDown(window, { key: 'Escape' })
    fireEvent.click(backdrop)
    expect(onClose).not.toHaveBeenCalled()

    rerender(<ExampleDrawer onClose={onClose} />)
    fireEvent.click(screen.getByRole('dialog', { name: 'Activity' }).previousElementSibling as HTMLElement)
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
