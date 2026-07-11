import { useCallback, useRef, useState } from 'react'

export function usePendingAction(when: boolean) {
  const actionRef = useRef<(() => void) | null>(null)
  const [hasPendingAction, setHasPendingAction] = useState(false)

  const requestAction = useCallback((action: () => void) => {
    if (!when) {
      action()
      return
    }
    actionRef.current = action
    setHasPendingAction(true)
  }, [when])

  const cancelPendingAction = useCallback(() => {
    actionRef.current = null
    setHasPendingAction(false)
  }, [])

  const confirmPendingAction = useCallback(() => {
    const action = actionRef.current
    actionRef.current = null
    setHasPendingAction(false)
    action?.()
  }, [])

  return {
    hasPendingAction,
    requestAction,
    cancelPendingAction,
    confirmPendingAction,
  }
}
