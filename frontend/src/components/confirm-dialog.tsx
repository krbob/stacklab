import { useRef, useState } from 'react'
import { Dialog } from '@/components/dialog'

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
  const cancelRef = useRef<HTMLButtonElement>(null)
  const textRef = useRef<HTMLInputElement>(null)
  const confirmed = !requireText || typedText === requireText

  return (
    <Dialog
      title={title}
      onClose={onCancel}
      preventClose={confirming}
      busy={confirming}
      initialFocusRef={requireText ? textRef : cancelRef}
    >
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
            ref={textRef}
            type="text"
            value={typedText}
            onChange={(event) => setTypedText(event.target.value)}
            disabled={confirming}
            className="mt-2 w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-sm text-[var(--text)] outline-none focus:border-[var(--danger)]/50 disabled:opacity-50"
          />
        </label>
      )}
      <div className="mt-5 flex justify-end gap-2">
        <button
          ref={cancelRef}
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
    </Dialog>
  )
}
