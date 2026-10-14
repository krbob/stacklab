import { useCallback, useEffect, useRef, useState } from 'react'
import type { BackupOverview } from '@/lib/backup-api'

export type BackupJobEffects = {
  destinationIds?: string[]
  snapshots?: boolean
  restores?: boolean
  policy?: boolean
}
type TrackedEffect = { effect: BackupJobEffects; observed: Set<string>; finished: boolean }

// Reconcile completion and overview evidence together. Each copy becomes visible
// as soon as its retention settles, without waiting for slower sibling copies.
// Listing during that repository's maintenance lease would be rejected as busy.
export function useBackupRefresh(data: BackupOverview | null, refetch: () => void) {
  const [jobIds, setJobIds] = useState<string[]>([])
  const effects = useRef(new Map<string, TrackedEffect>())
  const evidence = useRef<Map<string, string> | null>(null)
  const [snapshotRevisions, setSnapshotRevisions] = useState<Record<string, number>>({})
  const [restoreRevisions, setRestoreRevisions] = useState<Record<string, number>>({})
  const [policyRevision, setPolicyRevision] = useState(0)
  const invalidate = useCallback((effect: BackupJobEffects) => {
    const increment = (previous: Record<string, number>) => {
      const next = { ...previous }
      for (const id of new Set(effect.destinationIds ?? [])) next[id] = (next[id] ?? 0) + 1
      return next
    }
    if (effect.snapshots) setSnapshotRevisions(increment)
    if (effect.restores) setRestoreRevisions(increment)
    if (effect.policy) setPolicyRevision((previous) => previous + 1)
  }, [])
  const track = useCallback((id: string, effect: BackupJobEffects = {}) => {
    effects.current.set(id, { effect, observed: new Set(), finished: false })
    setJobIds((previous) => previous.includes(id) ? previous : [...previous, id])
  }, [])
  const finished = useCallback((id: string) => {
    const tracked = effects.current.get(id)
    if (!tracked || tracked.finished) return
    tracked.finished = true
    setJobIds((previous) => previous.filter((value) => value !== id))
    invalidate({ ...tracked.effect, snapshots: false })
    // Snapshot effects are reconciled with the fresh overview, including prune
    // timestamps. Otherwise job completion and that overview list twice.
    refetch()
  }, [invalidate, refetch])
  useEffect(() => {
    if (!data) return
    const next = new Map<string, string>()
    const changed = new Set<string>()
    const observe = (key: string, value: string, id: string) => {
      next.set(key, value)
      if (evidence.current && value && evidence.current.get(key) !== value) changed.add(id)
    }
    const retaining = new Set<string>()
    for (const run of data.runs) {
      for (const copy of run.copies) {
        if (['queued', 'running', 'pending'].includes(copy.state)) continue
        const key = `copy:${run.id}:${copy.destination_id}`
        if (copy.state === 'succeeded' && ['pending', 'running'].includes(copy.retention)) {
          retaining.add(copy.destination_id)
          const previous = evidence.current?.get(key)
          if (previous) next.set(key, previous)
          continue
        }
        observe(key, [copy.state, copy.snapshot_id, copy.verification, copy.retention].join(':'), copy.destination_id)
      }
    }
    for (const protection of data.protection) {
      for (const copy of protection.copies) {
        const key = `inventory:${protection.plan_id}:${copy.destination_id}`
        if (retaining.has(copy.destination_id)) {
          const previous = evidence.current?.get(key)
          if (previous) next.set(key, previous)
          continue
        }
        observe(key, `${copy.snapshot_id ?? ''}:${copy.snapshot_status ?? 'unknown'}`, copy.destination_id)
      }
    }
    for (const destination of data.destinations) {
      observe(`prune:${destination.id}`, destination.last_prune_at ?? '', destination.id)
      observe(`test:${destination.id}`, destination.last_restore_test_at ?? '', destination.id)
    }
    for (const [id, tracked] of effects.current) {
      for (const destinationId of tracked.effect.destinationIds ?? []) {
        if (changed.has(destinationId)) tracked.observed.add(destinationId)
        if (tracked.finished && tracked.effect.snapshots && !tracked.observed.has(destinationId)) changed.add(destinationId)
      }
      if (tracked.finished) effects.current.delete(id)
    }
    evidence.current = next
    if (changed.size) invalidate({ destinationIds: [...changed], snapshots: true })
  }, [data, invalidate])
  const refreshPolicy = useCallback(() => setPolicyRevision((previous) => previous + 1), [])
  return { jobIds, track, finished, snapshotRevisions, restoreRevisions, policyRevision, refreshPolicy }
}
