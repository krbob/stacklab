import { useId, type ReactNode, type RefObject } from 'react'
import { cn } from '@/lib/cn'
import { useModalBehavior } from '@/hooks/use-modal-behavior'

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
  const { panelRef: dialogRef, requestClose } = useModalBehavior<HTMLDivElement>({
    onClose,
    preventClose,
    initialFocusRef,
  })

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4"
      onClick={(event) => {
        if (event.target === event.currentTarget) requestClose()
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
