import { useEffect, useId, useRef, type ReactNode, type RefObject } from 'react'
import { cn } from '@/lib/cn'

interface DialogProps {
  title: ReactNode
  children: ReactNode
  onClose: () => void
  preventClose?: boolean
  busy?: boolean
  initialFocusRef?: RefObject<HTMLElement | null>
  panelClassName?: string
}

export function Dialog({
  title,
  children,
  onClose,
  preventClose = false,
  busy = false,
  initialFocusRef,
  panelClassName,
}: DialogProps) {
  const titleId = useId()
  const dialogRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const previouslyFocused = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'

    const focusable = getFocusableElements(dialogRef.current)
    const preferred = initialFocusRef?.current
    const initial = preferred && focusable.includes(preferred)
      ? preferred
      : (focusable[0] ?? dialogRef.current)
    initial?.focus()

    return () => {
      document.body.style.overflow = previousOverflow
      previouslyFocused?.focus()
    }
  }, [initialFocusRef])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopPropagation()
        if (!preventClose) onClose()
        return
      }
      if (event.key !== 'Tab') return

      const focusable = getFocusableElements(dialogRef.current)
      if (focusable.length === 0) {
        event.preventDefault()
        dialogRef.current?.focus()
        return
      }

      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      const active = document.activeElement
      if (event.shiftKey && (active === first || !dialogRef.current?.contains(active))) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && (active === last || !dialogRef.current?.contains(active))) {
        event.preventDefault()
        first.focus()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose, preventClose])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4"
      onClick={(event) => {
        if (event.target === event.currentTarget && !preventClose) onClose()
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-busy={busy}
        tabIndex={-1}
        className={cn(
          'max-h-[calc(100dvh-2rem)] w-full max-w-md overflow-y-auto rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]',
          panelClassName,
        )}
      >
        <h2 id={titleId} className="text-lg font-semibold text-[var(--text)]">{title}</h2>
        {children}
      </div>
    </div>
  )
}

function getFocusableElements(root: HTMLElement | null): HTMLElement[] {
  if (!root) return []
  return Array.from(root.querySelectorAll<HTMLElement>(
    'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [contenteditable="true"], [tabindex]:not([tabindex="-1"])',
  )).filter((element) => (
    element.tabIndex !== -1
    && !element.hasAttribute('disabled')
    && !element.closest('[hidden], [aria-hidden="true"], [inert]')
  ))
}
