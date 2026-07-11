import { useEffect } from 'react'
import { useBlocker } from 'react-router-dom'
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
  const blocker = useBlocker(when)

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
    if (!when && blocker.state === 'blocked') blocker.reset()
  }, [when, blocker])

  if (blocker.state !== 'blocked') return null

  return (
    <ConfirmDialog
      title={title}
      message={message}
      confirmLabel="Discard changes"
      onCancel={() => blocker.reset()}
      onConfirm={() => blocker.proceed()}
    />
  )
}
