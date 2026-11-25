import { useState } from 'react'
import { retryRemoval } from '@/lib/api-client'
import type { JobDetailResponse } from '@/lib/api-types'

interface Props {
  jobId: string
  recovery: NonNullable<JobDetailResponse['removal_recovery']>
  onStarted: (id: string) => void
}

export function RemovalRecovery({ jobId, recovery, onStarted }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function retry() {
    setBusy(true)
    setError(null)
    try {
      const { job } = await retryRemoval(jobId)
      onStarted(job.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to retry removal.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="my-3 space-y-2 text-sm">
      <p>Retrying will repair directory access if needed and permanently remove the remaining items.</p>
      <ul className="list-inside list-disc font-mono text-xs">
        {recovery.targets.map((target) => <li key={target}>{target}</li>)}
      </ul>
      {recovery.capability.supported ? (
        <button type="button" disabled={busy} onClick={retry} className="rounded-md border border-[var(--danger)]/40 px-3 py-2 text-[var(--danger)] disabled:opacity-50">
          {busy ? 'Starting retry…' : 'Repair access and retry removal'}
        </button>
      ) : <p>{recovery.capability.reason}</p>}
      {error && <p role="alert" className="text-[var(--danger)]">{error}</p>}
    </section>
  )
}
