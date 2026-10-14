import { useState } from 'react'
import { ConfirmDialog } from '@/components/confirm-dialog'

export function useEditorCloseGuard(dirty: boolean, onClose: () => void) {
  const [confirming, setConfirming] = useState(false)
  return {
    requestClose: () => (dirty ? setConfirming(true) : onClose()),
    confirmation: confirming ? (
      <ConfirmDialog
        title="Discard unsaved changes?"
        message="Your changes have not been saved. Keep editing to preserve this configuration and any entered credentials."
        confirmLabel="Discard changes"
        cancelLabel="Keep editing"
        onConfirm={onClose}
        onCancel={() => setConfirming(false)}
      />
    ) : null,
  }
}
