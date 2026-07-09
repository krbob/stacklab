import { useEffect, useState } from 'react'

interface ConfirmDialogProps {
  title: string
  message: string
  items?: string[]
  confirmLabel: string
  cancelLabel?: string
  requireText?: string
  requireTextLabel?: string
  confirming?: boolean
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
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const [typedText, setTypedText] = useState('')
  const confirmed = !requireText || typedText === requireText

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !confirming) onCancel()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [confirming, onCancel])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4" onClick={() => { if (!confirming) onCancel() }}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-dialog-title"
        className="w-full max-w-md rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]"
        onClick={(event) => event.stopPropagation()}
      >
        <h3 id="confirm-dialog-title" className="text-base font-semibold text-[var(--text)]">{title}</h3>
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
            {confirming ? 'Removing...' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
