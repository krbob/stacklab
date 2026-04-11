import { createContext, useCallback, useState, type ReactNode } from 'react'

interface JobDrawerContextValue {
  jobId: string | null
  openJob: (jobId: string) => void
  closeJob: () => void
}

// eslint-disable-next-line react-refresh/only-export-components
export const JobDrawerContext = createContext<JobDrawerContextValue>({
  jobId: null,
  openJob: () => {},
  closeJob: () => {},
})

export function JobDrawerProvider({ children }: { children: ReactNode }) {
  const [jobId, setJobId] = useState<string | null>(null)

  const openJob = useCallback((id: string) => setJobId(id), [])
  const closeJob = useCallback(() => setJobId(null), [])

  return (
    <JobDrawerContext.Provider value={{ jobId, openJob, closeJob }}>
      {children}
    </JobDrawerContext.Provider>
  )
}
