import { useCallback, useEffect, useRef, useState } from 'react'
import type { BackupOverview } from '@/lib/backup-api'

export type BackupJobEffects = {
  destinationIds?: string[]
  snapshots?: boolean
  restores?: boolean
  policy?: boolean
}

// Local jobs carry their resource scope. Overview evidence also observes scheduled
// work and other sessions, including a copy finishing before its sibling copies.
export function useBackupRefresh(data: BackupOverview | null, refetch: () => void) {
  const [jobIds, setJobIds] = useState<string[]>([])
  const effects = useRef(new Map<string, BackupJobEffects>())
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
    effects.current.set(id, effect)
    setJobIds((previous) => previous.includes(id) ? previous : [...previous, id])
  }, [])
  const finished = useCallback((id: string) => {
    const effect = effects.current.get(id)
    if (!effect) return
    effects.current.delete(id)
    setJobIds((previous) => previous.filter((value) => value !== id))
    invalidate(effect)
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
    for (const run of data.runs) {
      for (const copy of run.copies) {
        if (['queued', 'running', 'pending'].includes(copy.state)) continue
        observe(`copy:${run.id}:${copy.destination_id}`, [copy.state, copy.snapshot_id, copy.verification, copy.retention].join(':'), copy.destination_id)
      }
    }
    for (const protection of data.protection) {
      for (const copy of protection.copies) {
        observe(`inventory:${protection.plan_id}:${copy.destination_id}`, `${copy.snapshot_id ?? ''}:${copy.snapshot_status ?? 'unknown'}`, copy.destination_id)
      }
    }
    for (const destination of data.destinations) {
      observe(`prune:${destination.id}`, destination.last_prune_at ?? '', destination.id)
      observe(`test:${destination.id}`, destination.last_restore_test_at ?? '', destination.id)
    }
    evidence.current = next
    if (changed.size) invalidate({ destinationIds: [...changed], snapshots: true })
  }, [data, invalidate])
  const refreshPolicy = useCallback(() => setPolicyRevision((previous) => previous + 1), [])
  return { jobIds, track, finished, snapshotRevisions, restoreRevisions, policyRevision, refreshPolicy }
}
