import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from 'react'
import { UnsavedChangesGuard } from '@/components/unsaved-changes-guard'

interface SettingsDraftRegistry {
  setDraftDirty: (sectionId: string, isDirty: boolean) => void
}

const SettingsDraftContext = createContext<SettingsDraftRegistry | null>(null)

export function SettingsDraftProvider({ children }: { children: ReactNode }) {
  const [dirtySections, setDirtySections] = useState<Set<string>>(() => new Set())

  const setDraftDirty = useCallback((sectionId: string, isDirty: boolean) => {
    setDirtySections((current) => {
      if (current.has(sectionId) === isDirty) return current

      const next = new Set(current)
      if (isDirty) next.add(sectionId)
      else next.delete(sectionId)
      return next
    })
  }, [])

  const registry = useMemo(() => ({ setDraftDirty }), [setDraftDirty])

  return (
    <SettingsDraftContext.Provider value={registry}>
      {children}
      <UnsavedChangesGuard
        when={dirtySections.size > 0}
        title="Discard unsaved settings?"
        message="One or more settings sections have unsaved changes. Leaving now will discard them."
      />
    </SettingsDraftContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export function useSettingsDraft(sectionId: string, isDirty: boolean) {
  const registry = useContext(SettingsDraftContext)

  useLayoutEffect(() => {
    registry?.setDraftDirty(sectionId, isDirty)
  }, [isDirty, registry, sectionId])

  useEffect(() => () => {
    registry?.setDraftDirty(sectionId, false)
  }, [registry, sectionId])

  return useCallback(() => {
    registry?.setDraftDirty(sectionId, false)
  }, [registry, sectionId])
}
