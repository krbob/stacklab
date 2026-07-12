import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import { isTopmostModalLayer, registerModalLayer } from '@/lib/modal-state'

interface UseModalBehaviorOptions {
  active?: boolean
  onClose: () => void
  preventClose?: boolean
  initialFocusRef?: RefObject<HTMLElement | null>
}

export function useModalBehavior<T extends HTMLElement>({
  active = true,
  onClose,
  preventClose = false,
  initialFocusRef,
}: UseModalBehaviorOptions) {
  const panelRef = useRef<T>(null)
  const [layer] = useState(() => Symbol('modal-layer'))

  useEffect(() => {
    if (!active) return
    const previouslyFocused = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null
    const unregister = registerModalLayer(layer)

    const focusable = getFocusableElements(panelRef.current)
    const preferred = initialFocusRef?.current
    const initial = preferred && focusable.includes(preferred)
      ? preferred
      : (focusable[0] ?? panelRef.current)
    initial?.focus()

    return () => {
      unregister()
      if (previouslyFocused?.isConnected) previouslyFocused.focus()
    }
  }, [active, initialFocusRef, layer])

  const requestClose = useCallback(() => {
    if (!isTopmostModalLayer(layer) || preventClose) return
    onClose()
  }, [layer, onClose, preventClose])

  useEffect(() => {
    if (!active) return
    const handleKeyDown = (event: KeyboardEvent) => {
      if (!isTopmostModalLayer(layer)) return
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopImmediatePropagation()
        if (!preventClose) onClose()
        return
      }
      if (event.key !== 'Tab') return

      const focusable = getFocusableElements(panelRef.current)
      if (focusable.length === 0) {
        event.preventDefault()
        panelRef.current?.focus()
        return
      }

      const activeIndex = focusable.findIndex((element) => element === document.activeElement)
      const nextIndex = event.shiftKey
        ? activeIndex <= 0 ? focusable.length - 1 : activeIndex - 1
        : activeIndex < 0 || activeIndex === focusable.length - 1 ? 0 : activeIndex + 1
      event.preventDefault()
      focusable[nextIndex]?.focus()
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [active, layer, onClose, preventClose])

  return { panelRef, requestClose }
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
