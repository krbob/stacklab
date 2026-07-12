import {
  type CSSProperties,
  type ReactNode,
  type RefObject,
} from 'react'
import { cn } from '@/lib/cn'
import { useModalBehavior } from '@/hooks/use-modal-behavior'

type DrawerAccessibleName =
  | { label: string; labelledBy?: never }
  | { label?: never; labelledBy: string }

interface DrawerBaseProps {
  id?: string
  children: ReactNode
  onClose: () => void
  preventClose?: boolean
  busy?: boolean
  initialFocusRef?: RefObject<HTMLElement | null>
  side?: 'left' | 'right'
  panelClassName?: string
  backdropClassName?: string
  style?: CSSProperties
}

export type DrawerProps = DrawerBaseProps & DrawerAccessibleName

export function Drawer({
  id,
  children,
  onClose,
  label,
  labelledBy,
  preventClose = false,
  busy = false,
  initialFocusRef,
  side = 'right',
  panelClassName,
  backdropClassName,
  style,
}: DrawerProps) {
  const { panelRef: drawerRef, requestClose } = useModalBehavior<HTMLDivElement>({
    onClose,
    preventClose,
    initialFocusRef,
  })

  return (
    <>
      <div
        className={cn('fixed inset-0 z-40 bg-black/40', backdropClassName)}
        onClick={requestClose}
        aria-hidden="true"
      />
      <div
        id={id}
        ref={drawerRef}
        role="dialog"
        aria-modal="true"
        aria-label={label}
        aria-labelledby={labelledBy}
        aria-busy={busy}
        tabIndex={-1}
        className={cn(
          'fixed inset-y-0 z-50 flex w-full max-w-lg flex-col bg-[var(--panel)] shadow-lg',
          side === 'left' ? 'left-0 border-r border-[var(--panel-border)]' : 'right-0 border-l border-[var(--panel-border)]',
          panelClassName,
        )}
        style={style}
      >
        {children}
      </div>
    </>
  )
}
