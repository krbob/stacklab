import { useEffect, useState } from 'react'
import { ConfirmDialog } from '@/components/confirm-dialog'

interface UnsavedChangesGuardProps {
  when: boolean
  title?: string
  message?: string
}

export function UnsavedChangesGuard({
  when,
  title = 'Discard unsaved changes?',
  message = 'This page has unsaved changes. Leaving now will discard them.',
}: UnsavedChangesGuardProps) {
  const [pendingHref, setPendingHref] = useState<string | null>(null)

  useEffect(() => {
    if (!when) return

    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', handleBeforeUnload)
    return () => window.removeEventListener('beforeunload', handleBeforeUnload)
  }, [when])

  useEffect(() => {
    if (!when) return

    const handleClick = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
        return
      }
      const target = event.target
      if (!(target instanceof Element)) return
      const anchor = target.closest('a[href]')
      if (!(anchor instanceof HTMLAnchorElement)) return
      if (anchor.target && anchor.target !== '_self') return
      if (anchor.hasAttribute('download')) return

      const nextURL = new URL(anchor.href, window.location.href)
      if (nextURL.origin !== window.location.origin || nextURL.href === window.location.href) return

      event.preventDefault()
      event.stopPropagation()
      setPendingHref(nextURL.href)
    }

    document.addEventListener('click', handleClick, true)
    return () => document.removeEventListener('click', handleClick, true)
  }, [when])

  if (!pendingHref) return null

  const proceed = () => {
    const nextURL = pendingHref
    setPendingHref(null)
    window.history.pushState(null, '', nextURL)
    window.dispatchEvent(new PopStateEvent('popstate', { state: null }))
  }

  return (
    <ConfirmDialog
      title={title}
      message={message}
      confirmLabel="Discard changes"
      onCancel={() => setPendingHref(null)}
      onConfirm={proceed}
    />
  )
}
