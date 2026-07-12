import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { BottomSheet } from './bottom-sheet'

function BottomSheetHarness({ onClose = vi.fn() }: { onClose?: () => void }) {
  const [open, setOpen] = useState(false)

  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>Open actions</button>
      <BottomSheet
        open={open}
        label="Stack actions"
        onClose={() => {
          onClose()
          setOpen(false)
        }}
      >
        <button type="button">First action</button>
        <a href="/last-action">Last action</a>
      </BottomSheet>
    </>
  )
}

function openSheet() {
  const trigger = screen.getByRole('button', { name: 'Open actions' })
  trigger.focus()
  fireEvent.click(trigger)
  return trigger
}

describe('BottomSheet', () => {
  afterEach(() => {
    document.body.style.overflow = ''
  })

  it('labels the modal and initially focuses its close button', () => {
    render(<BottomSheetHarness />)

    openSheet()

    expect(screen.getByRole('dialog', { name: 'Stack actions' })).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus()
  })

  it('wraps focus in both directions', () => {
    render(<BottomSheetHarness />)
    openSheet()

    const close = screen.getByRole('button', { name: 'Close' })
    const last = screen.getByRole('link', { name: 'Last action' })

    close.focus()
    fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
    expect(last).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Tab' })
    expect(close).toHaveFocus()
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    render(<BottomSheetHarness onClose={onClose} />)
    openSheet()

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('dialog', { name: 'Stack actions' })).not.toBeInTheDocument()
  })

  it('closes only when the backdrop is clicked', () => {
    const onClose = vi.fn()
    render(<BottomSheetHarness onClose={onClose} />)
    openSheet()

    const dialog = screen.getByRole('dialog', { name: 'Stack actions' })
    fireEvent.click(screen.getByRole('button', { name: 'First action' }))
    expect(onClose).not.toHaveBeenCalled()

    fireEvent.click(dialog.previousElementSibling as Element)
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('dialog', { name: 'Stack actions' })).not.toBeInTheDocument()
  })

  it('locks body scrolling while open, then restores overflow and trigger focus', () => {
    document.body.style.overflow = 'auto'
    render(<BottomSheetHarness />)

    const trigger = openSheet()
    expect(document.body).toHaveStyle({ overflow: 'hidden' })

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))

    expect(document.body).toHaveStyle({ overflow: 'auto' })
    expect(trigger).toHaveFocus()
  })
})
