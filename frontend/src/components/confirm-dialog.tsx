import { useEffect, useRef, useState } from 'react'

interface ConfirmDialogProps {
  title: string
  message: string
  items?: string[]
  confirmLabel: string
  cancelLabel?: string
  requireText?: string
  requireTextLabel?: string
  confirming?: boolean
  confirmingLabel?: string
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog({
  title,
  message,
  items = [],
  confirmLabel,
  cancelLabel = 'Cancel',
  requireText,
  requireTextLabel,
  confirming = false,
  confirmingLabel = 'Working...',
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const [typedText, setTypedText] = useState('')
  const dialogRef = useRef<HTMLDivElement>(null)
  const confirmed = !requireText || typedText === requireText

  useEffect(() => {
    const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const focusable = getFocusableElements(dialogRef.current)
    ;(focusable[0] ?? dialogRef.current)?.focus()

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !confirming) {
        onCancel()
        return
      }
      if (event.key !== 'Tab') return

      const currentFocusable = getFocusableElements(dialogRef.current)
      if (currentFocusable.length === 0) {
        event.preventDefault()
        dialogRef.current?.focus()
        return
      }

      const first = currentFocusable[0]
      const last = currentFocusable[currentFocusable.length - 1]
      const active = document.activeElement
      if (event.shiftKey) {
        if (active === first || !dialogRef.current?.contains(active)) {
          event.preventDefault()
          last.focus()
        }
        return
      }
      if (active === last || !dialogRef.current?.contains(active)) {
        event.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      previouslyFocused?.focus()
    }
  }, [confirming, onCancel])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4" onClick={() => { if (!confirming) onCancel() }}>
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-dialog-title"
        tabIndex={-1}
        className="w-full max-w-md rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]"
        onClick={(event) => event.stopPropagation()}
      >
        <h2 id="confirm-dialog-title" className="text-base font-semibold text-[var(--text)]">{title}</h2>
        <p className="mt-2 text-sm leading-6 text-[var(--muted)]">{message}</p>
        {items.length > 0 && (
          <ul className="mt-3 space-y-1 rounded-md border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-3 py-2 text-xs text-[var(--text)]">
            {items.map((item) => (
              <li key={item} className="break-all font-mono">{item}</li>
            ))}
          </ul>
        )}
        {requireText && (
          <label className="mt-4 block text-xs text-[var(--muted)]">
            {requireTextLabel ?? `Type ${requireText} to confirm`}
            <input
              type="text"
              value={typedText}
              onChange={(event) => setTypedText(event.target.value)}
              disabled={confirming}
              autoFocus
              className="mt-2 w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-sm text-[var(--text)] outline-none focus:border-[var(--danger)]/50 disabled:opacity-50"
            />
          </label>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button
            onClick={onCancel}
            disabled={confirming}
            className="rounded-md border border-[var(--panel-border)] px-3 py-1.5 text-xs text-[var(--muted)] hover:text-[var(--text)] disabled:opacity-40"
          >
            {cancelLabel}
          </button>
          <button
            onClick={onConfirm}
            disabled={confirming || !confirmed}
            className="rounded-md border border-[var(--danger)]/30 bg-[var(--danger)]/10 px-3 py-1.5 text-xs text-[var(--danger)] transition hover:bg-[var(--danger)]/20 disabled:opacity-40"
          >
            {confirming ? confirmingLabel : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

function getFocusableElements(root: HTMLElement | null): HTMLElement[] {
  if (!root) return []
  return Array.from(root.querySelectorAll<HTMLElement>(
    'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
  )).filter((element) => !element.hasAttribute('disabled') && element.tabIndex !== -1)
}
