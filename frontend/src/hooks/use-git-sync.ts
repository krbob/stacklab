import { useEffect, useSyncExternalStore } from 'react'
import { checkGitIncoming } from '@/lib/api-client'
import type { GitIncomingResponse } from '@/lib/api-types'

type Snapshot = { incoming: GitIncomingResponse | null; checking: boolean; error: string | null }
let snapshot: Snapshot = { incoming: null, checking: false, error: null }
let pending: Promise<GitIncomingResponse | null> | null = null
let lastAttempt: number | null = null
let generation = 0
const listeners = new Set<() => void>()
const publish = (next: Snapshot) => { snapshot = next; listeners.forEach((listener) => listener()) }
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener) } }
const getSnapshot = () => snapshot

export function clearGitSync() {
  generation++
  pending = null
  lastAttempt = null
  publish({ incoming: null, checking: false, error: null })
}

export function checkGitSync(force = true): Promise<GitIncomingResponse | null> {
  if (pending) return pending
  if (!force && lastAttempt !== null && Date.now() - lastAttempt < 60_000) return Promise.resolve(snapshot.error ? null : snapshot.incoming)
  lastAttempt = Date.now()
  const current = generation
  publish({ ...snapshot, checking: true })
  pending = Promise.resolve().then(checkGitIncoming).then((incoming) => {
    if (current === generation) publish({ incoming, checking: false, error: null })
    return current === generation ? incoming : null
  }, (err: unknown) => {
    if (current === generation) publish({ ...snapshot, checking: false, error: err instanceof Error ? err.message : 'Cannot check the remote.' })
    return null
  }).finally(() => { if (current === generation) pending = null })
  return pending
}

export function useGitSync(automatic = true) {
  const state = useSyncExternalStore(subscribe, getSnapshot)
  useEffect(() => {
    if (!automatic) return
    const check = () => { if (document.visibilityState !== 'hidden') void checkGitSync(false) }
    check()
    window.addEventListener('focus', check)
    document.addEventListener('visibilitychange', check)
    return () => { window.removeEventListener('focus', check); document.removeEventListener('visibilitychange', check) }
  }, [automatic])
  return { ...state, refresh: checkGitSync }
}
