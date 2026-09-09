import { describe, expect, it } from 'vitest'
import { updateDashboardHistory, type DashboardHistory } from './dashboard-history'

const base = Date.parse('2026-09-09T10:00:00Z')
const snapshot = (second: number) => ({
  items: {
    demo: { cpu_percent: second, memory_bytes: second * 1024, sampled_at: new Date(base + second * 1000).toISOString() },
  },
})

describe('dashboard history', () => {
  it('retains at most the last minute of actual observations without mutating earlier snapshots', () => {
    let history: DashboardHistory = {}
    for (let second = 0; second < 70; second++) {
      history = updateDashboardHistory(history, snapshot(second), base + second * 1000)
    }
    expect(history.demo.points).toHaveLength(60)
    expect(history.demo.points[0]).toEqual({ at: base + 10_000, cpu: 10, memory: 10_240 })
    const next = updateDashboardHistory(history, snapshot(70), base + 70_000)
    expect(history.demo.points.at(-1)?.cpu).toBe(69)
    expect(next.demo.points.at(-1)?.cpu).toBe(70)
  })

  it('does not invent new points for duplicate or out-of-order server samples', () => {
    let history = updateDashboardHistory({}, snapshot(10), base)
    history = updateDashboardHistory(history, snapshot(10), base + 1_000)
    history = updateDashboardHistory(history, snapshot(9), base + 2_000)
    expect(history.demo.points).toHaveLength(1)
    expect(history.demo.points[0].cpu).toBe(10)
    history = updateDashboardHistory(history, snapshot(10), base + 61_000)
    history = updateDashboardHistory(history, snapshot(10), base + 62_000)
    expect(history.demo.points).toEqual([])
  })

  it('drops expired and stopped-stack history, then starts fresh when samples return', () => {
    const history = updateDashboardHistory({}, snapshot(0), base)
    expect(updateDashboardHistory(history, null, base + 61_000).demo.points).toEqual([])
    expect(updateDashboardHistory(history, { items: {} }, base + 1_000)).toEqual({})
    expect(updateDashboardHistory(history, { items: { demo: null } }, base + 1_000)).toEqual({})
    const resumed = updateDashboardHistory(history, snapshot(90), base + 90_000)
    expect(resumed.demo.points).toEqual([{ at: base + 90_000, cpu: 90, memory: 90 * 1024 }])
  })

  it('plots by browser observation time even when the server clock differs', () => {
    const history = updateDashboardHistory({}, snapshot(0), base + 3_600_000)
    expect(history.demo.points[0].at).toBe(base + 3_600_000)
    expect(history.demo.sampledAt).toBe(base)
  })
})
