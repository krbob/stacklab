import { describe, expect, it } from 'vitest'
import type { HostMetricSample, HostMetricsResponse } from '@/lib/api-types'
import { latestMetricSampleTimestamp, mergeHostMetrics } from '@/lib/host-metrics'

function sample(sampledAt: string): HostMetricSample {
  return { sampled_at: sampledAt } as HostMetricSample
}

function response(current: string | null, history: string[], historyWindowSeconds = 60): HostMetricsResponse {
  return {
    sample_interval_seconds: 1,
    background_sample_interval_seconds: 5,
    active_sample_interval_seconds: 1,
    history_window_seconds: historyWindowSeconds,
    current: current ? sample(current) : null,
    history: history.map(sample),
  }
}

describe('host metrics model', () => {
  it('uses the newest current or history timestamp for incremental requests', () => {
    expect(latestMetricSampleTimestamp(null)).toBeNull()
    expect(latestMetricSampleTimestamp(response(null, []))).toBeNull()
    expect(latestMetricSampleTimestamp(response('2026-04-04T12:00:05Z', ['2026-04-04T12:00:04Z']))).toBe('2026-04-04T12:00:05Z')
    expect(latestMetricSampleTimestamp(response('2026-04-04T12:00:03Z', ['2026-04-04T12:00:04Z']))).toBe('2026-04-04T12:00:04Z')
  })

  it('deduplicates, sorts, and trims merged history while retaining the previous current sample', () => {
    const previous = response('2026-04-04T12:00:10Z', [
      '2026-04-04T11:59:00Z',
      '2026-04-04T12:00:05Z',
      '2026-04-04T12:00:10Z',
    ])
    const next = response(null, [
      '2026-04-04T12:00:10Z',
      '2026-04-04T12:00:08Z',
    ], 10)

    const merged = mergeHostMetrics(previous, next)

    expect(merged.current?.sampled_at).toBe('2026-04-04T12:00:10Z')
    expect(merged.history.map((entry) => entry.sampled_at)).toEqual([
      '2026-04-04T12:00:05Z',
      '2026-04-04T12:00:08Z',
      '2026-04-04T12:00:10Z',
    ])
  })
})
