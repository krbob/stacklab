import { useContext } from 'react'
import { ActivityContext } from '@/contexts/activity-context'
import type { ActiveJobsResponse } from '@/lib/api-types'

export function useActivity(): ActiveJobsResponse | null {
  return useContext(ActivityContext)
}
