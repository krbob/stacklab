import { useCallback, useEffect, useState } from 'react'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { applyStacklabUpdate, getStacklabUpdateOverview } from '@/lib/api-client'
import type { StacklabUpdateOverviewResponse } from '@/lib/api-types'

export function useStacklabUpdate() {
  const { openJob } = useJobDrawer()
  const [loading, setLoading] = useState(true)
  const [overview, setOverview] = useState<StacklabUpdateOverviewResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [applying, setApplying] = useState(false)
  const [applyError, setApplyError] = useState<string | null>(null)
  const [confirmUpdate, setConfirmUpdate] = useState(false)

  const loadOverview = useCallback(async () => {
    try {
      const data = await getStacklabUpdateOverview()
      setOverview(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load update status')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadOverview() }, [loadOverview])

  const handleApply = useCallback(async () => {
    if (!overview) return
    setApplying(true)
    setApplyError(null)
    try {
      const result = await applyStacklabUpdate({
        expected_candidate_version: overview.package.candidate_version,
        refresh_package_index: true,
      })
      if (result.job?.id) {
        openJob(result.job.id)
      }
      // Refresh overview after triggering
      setTimeout(loadOverview, 2000)
    } catch (err) {
      setApplyError(err instanceof Error ? err.message : 'Update failed')
    } finally {
      setApplying(false)
    }
  }, [overview, openJob, loadOverview])

  return {
    loading,
    overview,
    error,
    applying,
    applyError,
    confirmUpdate,
    setConfirmUpdate,
    handleApply,
    openJob,
  }
}
