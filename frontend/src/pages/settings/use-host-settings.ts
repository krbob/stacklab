import { useCallback, useEffect, useState } from 'react'
import { getHostSettings, updateHostSettings } from '@/lib/api-client'

export function useHostSettings() {
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [publicIPLookupEnabled, setPublicIPLookupEnabled] = useState(false)
  const [savedState, setSavedState] = useState('')
  const [savingHost, setSavingHost] = useState(false)
  const [saveResult, setSaveResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  const currentState = JSON.stringify({ publicIPLookupEnabled })
  const isDirty = currentState !== savedState

  const loadSettings = useCallback(() => {
    setLoading(true)
    setLoadError(null)
    setSaveResult(null)
    getHostSettings()
      .then((settings) => {
        setPublicIPLookupEnabled(settings.public_ip_lookup_enabled)
        setSavedState(JSON.stringify({ publicIPLookupEnabled: settings.public_ip_lookup_enabled }))
      })
      .catch((err) => {
        setSavedState('')
        setLoadError(err instanceof Error ? err.message : 'Failed to load host observability settings')
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { loadSettings() }, [loadSettings])

  const handleSave = useCallback(async () => {
    if (loadError) return
    setSavingHost(true)
    setSaveResult(null)
    try {
      const saved = await updateHostSettings({ public_ip_lookup_enabled: publicIPLookupEnabled })
      setPublicIPLookupEnabled(saved.public_ip_lookup_enabled)
      setSavedState(JSON.stringify({ publicIPLookupEnabled: saved.public_ip_lookup_enabled }))
      setSaveResult({ type: 'success', text: 'Saved' })
    } catch (err) {
      setSaveResult({ type: 'error', text: err instanceof Error ? err.message : 'Save failed' })
    } finally {
      setSavingHost(false)
    }
  }, [loadError, publicIPLookupEnabled])

  return {
    loading,
    loadError,
    publicIPLookupEnabled,
    setPublicIPLookupEnabled,
    savingHost,
    saveResult,
    isDirty,
    loadSettings,
    handleSave,
  }
}
