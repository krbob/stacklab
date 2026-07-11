import { useEffect, useState } from 'react'
import { getMeta } from '@/lib/api-client'
import { useActivity } from '@/hooks/use-activity'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import type { ActiveJobItem, MetaResponse } from '@/lib/api-types'
import { cn } from '@/lib/cn'

function jobChipLabel(job: ActiveJobItem): string {
  const step = job.current_step
  if (step) {
    const target = step.target_stack_id ? ` · ${step.target_stack_id}` : ''
    return `${step.action}${target} · ${step.index}/${step.total}`
  }
  return job.stack_id ? `${job.action} · ${job.stack_id}` : job.action
}

// k9s-style status strip: static host facts plus a live background-job chip
// fed by the activity stream (Z5 — the system state is always one glance away).
export function HostStrip() {
  const [meta, setMeta] = useState<MetaResponse | null>(null)
  const activity = useActivity()
  const { openJob } = useJobDrawer()

  useEffect(() => {
    getMeta().then(setMeta).catch(() => {})
  }, [])

  const activeJobs = activity?.items ?? []
  const primary = activeJobs[0] ?? null

  return (
    <div className="hidden items-center gap-4 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] px-4 py-2 font-mono text-xs text-[var(--muted)] shadow-[var(--shadow)] lg:flex">
      {meta && (
        <>
          <span className="text-[var(--accent)]">stacklab {meta.app.version}</span>
          <span>docker {meta.docker.engine_version}</span>
          <span>compose {meta.docker.compose_version}</span>
        </>
      )}
      <span className="ml-auto flex items-center gap-2">
        <span
          className={cn(
            'inline-block size-1.5 rounded-full',
            primary ? 'animate-pulse bg-[var(--run)]' : 'bg-[var(--ok)]',
          )}
        />
        {primary ? (
          <button
            onClick={() => openJob(primary.id)}
            className="text-[var(--accent)] hover:underline"
          >
            {activeJobs.length > 1 && `${activeJobs.length} jobs · `}
            {jobChipLabel(primary)}
          </button>
        ) : (
          <span>idle</span>
        )}
      </span>
    </div>
  )
}
