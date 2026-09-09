import type { StackStatsResponse } from '@/lib/api-types'

export const DASHBOARD_HISTORY_WINDOW_MS = 60_000
const MAX_HISTORY_POINTS = 60

export interface DashboardHistoryPoint {
  at: number
  cpu: number
  memory: number
}

export type DashboardHistory = Record<string, {
  sampledAt: number
  points: DashboardHistoryPoint[]
}>

// Plot by observation time so a host/browser clock difference cannot move the
// chart off-screen. The server timestamp identifies genuinely new samples.
export function updateDashboardHistory(
  previous: DashboardHistory,
  snapshot: StackStatsResponse | null,
  nowMs: number,
): DashboardHistory {
  const next: DashboardHistory = {}
  const ids = Object.keys(snapshot?.items ?? previous)
  for (const id of ids) {
    const sample = snapshot?.items[id]
    if (snapshot && !sample) continue
    const series = previous[id]
    const points = (series?.points ?? []).filter((point) => point.at > nowMs - DASHBOARD_HISTORY_WINDOW_MS)
    let sampledAt = series?.sampledAt ?? -Infinity
    const incomingTime = sample ? Date.parse(sample.sampled_at) : NaN
    if (sample && Number.isFinite(incomingTime) && incomingTime > sampledAt) {
      points.push({ at: nowMs, cpu: sample.cpu_percent, memory: sample.memory_bytes })
      sampledAt = incomingTime
    }
    next[id] = { sampledAt, points: points.slice(-MAX_HISTORY_POINTS) }
  }
  return next
}
