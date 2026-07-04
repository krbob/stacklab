import { useEffect, type ReactNode } from 'react'

interface BottomSheetProps {
  open: boolean
  onClose: () => void
  label: string
  children: ReactNode
}

// Mobile-only bottom sheet: file trees and pickers slide over full-screen
// content instead of competing with it for vertical space.
export function BottomSheet({ open, onClose, label, children }: BottomSheetProps) {
  useEffect(() => {
    if (!open) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = prev
      window.removeEventListener('keydown', onKey)
    }
  }, [open, onClose])

  if (!open) return null

  return (
    <div className="lg:hidden">
      <div className="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm" onClick={onClose} aria-hidden />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={label}
        className="fixed inset-x-0 bottom-0 z-50 flex max-h-[70vh] flex-col rounded-t-xl border-t border-x border-[var(--panel-border)] bg-[var(--panel)] shadow-[0_-18px_50px_rgba(0,0,0,0.5)]"
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="flex w-full shrink-0 items-center justify-center py-3"
        >
          <span className="h-1 w-9 rounded-full bg-[rgba(255,255,255,0.18)]" aria-hidden />
        </button>
        <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4">{children}</div>
      </div>
    </div>
  )
}
