import { useCallback, useEffect, useRef, useState } from 'react'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { applyStacklabUpdate, getStacklabUpdateOverview } from '@/lib/api-client'
import type { StacklabUpdateOverviewResponse } from '@/lib/api-types'

export function useStacklabUpdate() {
  const { openJob } = useJobDrawer()
  const [loading, setLoading] = useState(true)
  const [overview, setOverview] = useState<StacklabUpdateOverviewResponse | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [applying, setApplying] = useState(false)
  const [applyError, setApplyError] = useState<string | null>(null)
  const [confirmUpdate, setConfirmUpdate] = useState(false)
  const refreshTimerRef = useRef<number | null>(null)

  const loadOverview = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await getStacklabUpdateOverview()
      setOverview(data)
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to load update status'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadOverview() }, [loadOverview])

  useEffect(() => () => {
    if (refreshTimerRef.current !== null) {
      window.clearTimeout(refreshTimerRef.current)
    }
  }, [])

  const handleApply = useCallback(async () => {
    if (!overview) return
    setApplying(true)
    setApplyError(null)
    try {
      const result = await applyStacklabUpdate({
        expected_candidate_version: overview.package.candidate_version,
        refresh_package_index: true,
      })
      setOverview((current) => current ? {
        ...current,
        package: result.package,
        runtime: result.runtime ?? {
          job_id: result.job.id,
          pending_finalize: false,
          requested_version: result.package.candidate_version,
        },
      } : current)
      if (result.job?.id) {
        openJob(result.job.id)
      }
      setLoading(true)
      setError(null)
      if (refreshTimerRef.current !== null) {
        window.clearTimeout(refreshTimerRef.current)
      }
      refreshTimerRef.current = window.setTimeout(() => {
        refreshTimerRef.current = null
        void loadOverview()
      }, 2000)
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
    refetchOverview: loadOverview,
    openJob,
  }
}
