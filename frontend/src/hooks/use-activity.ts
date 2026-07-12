import { useContext } from 'react'
import { ActivityContext, type ActivityContextValue } from '@/contexts/activity-context'

export function useActivity(): ActivityContextValue {
  return useContext(ActivityContext)
}
