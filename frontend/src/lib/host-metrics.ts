import type { HostMetricSample, HostMetricsResponse } from '@/lib/api-types'

export function latestMetricSampleTimestamp(metrics: HostMetricsResponse | null): string | null {
  if (!metrics) return null
  const lastHistorySample = metrics.history[metrics.history.length - 1]
  if (!lastHistorySample) return metrics.current?.sampled_at ?? null
  if (!metrics.current) return lastHistorySample.sampled_at
  return Date.parse(metrics.current.sampled_at) > Date.parse(lastHistorySample.sampled_at)
    ? metrics.current.sampled_at
    : lastHistorySample.sampled_at
}

export function mergeHostMetrics(previous: HostMetricsResponse, next: HostMetricsResponse): HostMetricsResponse {
  const byTimestamp = new Map<string, HostMetricSample>()
  for (const sample of previous.history) {
    byTimestamp.set(sample.sampled_at, sample)
  }
  for (const sample of next.history) {
    byTimestamp.set(sample.sampled_at, sample)
  }

  const currentTime = Date.parse(next.current?.sampled_at ?? previous.current?.sampled_at ?? '')
  const cutoff = Number.isFinite(currentTime)
    ? currentTime - next.history_window_seconds * 1000
    : Number.NEGATIVE_INFINITY
  const history = Array.from(byTimestamp.values())
    .sort((left, right) => Date.parse(left.sampled_at) - Date.parse(right.sampled_at))
    .filter((sample) => Date.parse(sample.sampled_at) >= cutoff)

  return {
    ...next,
    current: next.current ?? previous.current,
    history,
  }
}
