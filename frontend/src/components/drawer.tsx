import {
  useEffect,
  useRef,
  type CSSProperties,
  type ReactNode,
  type RefObject,
} from 'react'
import { cn } from '@/lib/cn'

type DrawerAccessibleName =
  | { label: string; labelledBy?: never }
  | { label?: never; labelledBy: string }

interface DrawerBaseProps {
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
  const drawerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const previouslyFocused = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'

    const focusable = getFocusableElements(drawerRef.current)
    const preferred = initialFocusRef?.current
    const initial = preferred && focusable.includes(preferred)
      ? preferred
      : (focusable[0] ?? drawerRef.current)
    initial?.focus()

    return () => {
      document.body.style.overflow = previousOverflow
      if (previouslyFocused?.isConnected) previouslyFocused.focus()
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

      const focusable = getFocusableElements(drawerRef.current)
      if (focusable.length === 0) {
        event.preventDefault()
        drawerRef.current?.focus()
        return
      }

      const activeIndex = focusable.findIndex((element) => element === document.activeElement)
      if (activeIndex === -1) {
        event.preventDefault()
        const target = event.shiftKey ? focusable[focusable.length - 1] : focusable[0]
        target?.focus()
      } else if (event.shiftKey && activeIndex === 0) {
        event.preventDefault()
        focusable[focusable.length - 1]?.focus()
      } else if (!event.shiftKey && activeIndex === focusable.length - 1) {
        event.preventDefault()
        focusable[0]?.focus()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose, preventClose])

  return (
    <>
      <div
        className={cn('fixed inset-0 z-40 bg-black/40', backdropClassName)}
        onClick={() => {
          if (!preventClose) onClose()
        }}
        aria-hidden="true"
      />
      <div
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
