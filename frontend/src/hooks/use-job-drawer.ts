import { useContext } from 'react'
import { JobDrawerContext } from '@/contexts/job-drawer-context'

export function useJobDrawer() {
  return useContext(JobDrawerContext)
}
