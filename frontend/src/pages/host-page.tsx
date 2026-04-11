import { useCallback, useEffect, useRef, useState } from 'react'
import { getHostOverview, getStacklabLogs } from '@/lib/api-client'
import type { HostOverviewResponse, StacklabLogEntry } from '@/lib/api-types'
import { cn } from '@/lib/cn'
import { formatBytes, formatUptime } from '@/pages/host-page-utils'

const OVERVIEW_POLL_INTERVAL_MS = 5_000

function PercentBar({ value, color }: { value: number; color: string }) {
  return (
    <div className="h-2 w-full rounded-full bg-[rgba(255,255,255,0.06)]">
      <div className={`h-2 rounded-full ${color}`} style={{ width: `${Math.min(value, 100)}%` }} />
    </div>
  )
}

export function HostPage() {
  const [overview, setOverview] = useState<HostOverviewResponse | null>(null)
  const [overviewError, setOverviewError] = useState<Error | null>(null)
  const [overviewLoading, setOverviewLoading] = useState(true)
  const [overviewUpdatedAt, setOverviewUpdatedAt] = useState<number | null>(null)
  const [nowMs, setNowMs] = useState(() => Date.now())
  const initialLoadRef = useRef(true)

  const loadOverview = useCallback(async () => {
    if (initialLoadRef.current) {
      setOverviewLoading(true)
    }

    try {
      const nextOverview = await getHostOverview()
      setOverview(nextOverview)
      setOverviewError(null)
      setOverviewUpdatedAt(Date.now())
    } catch (error) {
      setOverviewError(error instanceof Error ? error : new Error('Failed to load host overview'))
    } finally {
      initialLoadRef.current = false
      setOverviewLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadOverview()
  }, [loadOverview])

  // Auto-refresh overview every 15s
  useEffect(() => {
    const interval = setInterval(() => {
      void loadOverview()
    }, OVERVIEW_POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [loadOverview])

  useEffect(() => {
    function handleWindowFocus() {
      void loadOverview()
    }

    function handleVisibilityChange() {
      if (!document.hidden) {
        void loadOverview()
      }
    }

    window.addEventListener('focus', handleWindowFocus)
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => {
      window.removeEventListener('focus', handleWindowFocus)
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [loadOverview])

  useEffect(() => {
    if (!overview) return
    const interval = setInterval(() => setNowMs(Date.now()), 1_000)
    return () => clearInterval(interval)
  }, [overview])

  return (
    <div className="flex flex-col gap-4">
      {/* Overview cards */}
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <h2 className="text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">Host</h2>

        {overviewLoading && !overview && (
          <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            {[1, 2, 3, 4].map((i) => (
              <div key={i} className="h-32 animate-pulse rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />
            ))}
          </div>
        )}

        {overviewError && (
          <div className="mt-4 rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
            Failed to load host overview: {overviewError.message}
          </div>
        )}

        {overview && <OverviewCards overview={overview} nowMs={nowMs} fetchedAtMs={overviewUpdatedAt} />}
      </section>

      {/* Stacklab logs */}
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <StacklabLogs />
      </section>
    </div>
  )
}

function OverviewCards({
  overview,
  nowMs,
  fetchedAtMs,
}: {
  overview: HostOverviewResponse
  nowMs: number
  fetchedAtMs: number | null
}) {
  const { host, stacklab, docker, resources } = overview
  const liveUptimeSeconds = fetchedAtMs == null
    ? host.uptime_seconds
    : host.uptime_seconds + Math.max(0, Math.floor((nowMs - fetchedAtMs) / 1000))

  return (
    <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
      {/* Stacklab */}
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Stacklab</div>
        <div className="mt-2 text-lg font-medium text-[var(--text)]">{stacklab.version}</div>
        <div className="mt-1 space-y-0.5 text-xs text-[var(--muted)]">
          {stacklab.commit && <div>Commit: {stacklab.commit.slice(0, 8)}</div>}
          <div>Started: {new Date(stacklab.started_at).toLocaleString()}</div>
        </div>
      </div>

      {/* Host */}
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">System</div>
        <div className="mt-2 text-lg font-medium text-[var(--text)]">{host.hostname}</div>
        <div className="mt-1 space-y-0.5 text-xs text-[var(--muted)]">
          <div>{host.os_name}</div>
          <div>Kernel: {host.kernel_version}</div>
          <div>Uptime: {formatUptime(liveUptimeSeconds)}</div>
          <div>{host.architecture}</div>
        </div>
      </div>

      {/* Docker */}
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Docker</div>
        <div className="mt-2 text-lg font-medium text-[var(--text)]">Engine {docker.engine_version}</div>
        <div className="mt-1 text-xs text-[var(--muted)]">Compose {docker.compose_version}</div>
      </div>

      {/* Resources */}
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Resources</div>
        <div className="mt-3 space-y-2">
          <div>
            <div className="flex justify-between text-xs">
              <span className="text-[var(--muted)]">CPU ({resources.cpu.core_count} cores)</span>
              <span className="text-[var(--text)]">{resources.cpu.usage_percent.toFixed(1)}%</span>
            </div>
            <PercentBar value={resources.cpu.usage_percent} color="bg-cyan-400" />
          </div>
          <div>
            <div className="flex justify-between text-xs">
              <span className="text-[var(--muted)]">Memory</span>
              <span className="text-[var(--text)]">{formatBytes(resources.memory.used_bytes)} / {formatBytes(resources.memory.total_bytes)}</span>
            </div>
            <PercentBar value={resources.memory.usage_percent} color="bg-violet-400" />
          </div>
          <div>
            <div className="flex justify-between text-xs">
              <span className="text-[var(--muted)]">Disk</span>
              <span className="text-[var(--text)]">{formatBytes(resources.disk.used_bytes)} / {formatBytes(resources.disk.total_bytes)}</span>
            </div>
            <PercentBar value={resources.disk.usage_percent} color="bg-amber-400" />
          </div>
        </div>
      </div>
    </div>
  )
}

const LOG_LEVELS = ['debug', 'info', 'warn', 'error'] as const

function StacklabLogs() {
  const [entries, setEntries] = useState<StacklabLogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [level, setLevel] = useState<string>('')
  const [filter, setFilter] = useState('')
  const [following, setFollowing] = useState(true)
  const cursorRef = useRef<string | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)

  const fetchLogs = useCallback(async (append: boolean) => {
    try {
      const result = await getStacklabLogs({
        limit: append ? 50 : 200,
        cursor: append ? (cursorRef.current ?? undefined) : undefined,
        level: level || undefined,
      })

      if (append) {
        if (result.items.length > 0) {
          setEntries((prev) => [...prev, ...result.items])
        }
      } else {
        setEntries(result.items)
      }

      if (result.next_cursor) {
        cursorRef.current = result.next_cursor
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load logs')
    } finally {
      setLoading(false)
    }
  }, [level])

  // Initial load
  useEffect(() => {
    setLoading(true)
    setEntries([])
    cursorRef.current = null
    fetchLogs(false)
  }, [fetchLogs])

  // Polling follow mode
  useEffect(() => {
    if (following) {
      intervalRef.current = setInterval(() => fetchLogs(true), 3_000)
    }
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [following, fetchLogs])

  // Auto-scroll when following
  useEffect(() => {
    if (following && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [entries, following])

  const filteredEntries = filter
    ? entries.filter((e) => e.message.toLowerCase().includes(filter.toLowerCase()))
    : entries

  const levelColor: Record<string, string> = {
    debug: 'text-zinc-500',
    info: 'text-[var(--muted)]',
    warn: 'text-amber-400',
    error: 'text-red-400',
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-lg font-medium text-[var(--text)]">Stacklab logs</h3>

        <div className="flex flex-wrap items-center gap-2">
          {/* Level filter */}
          <div className="flex gap-1">
            <button
              onClick={() => setLevel('')}
              className={cn(
                'rounded-full border px-2.5 py-1 text-xs transition',
                !level
                  ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]'
                  : 'border-[var(--panel-border)] text-[var(--muted)]',
              )}
            >
              All
            </button>
            {LOG_LEVELS.map((l) => (
              <button
                key={l}
                onClick={() => setLevel(l)}
                className={cn(
                  'rounded-full border px-2.5 py-1 text-xs transition',
                  level === l
                    ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]'
                    : 'border-[var(--panel-border)] text-[var(--muted)]',
                )}
              >
                {l}
              </button>
            ))}
          </div>

          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter..."
            className="rounded-full border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]"
          />

          <button
            onClick={() => setFollowing(!following)}
            className={cn(
              'rounded-full border px-2.5 py-1 text-xs transition',
              following
                ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            {following ? 'Following' : 'Paused'}
          </button>

          <button
            onClick={() => { setEntries([]); cursorRef.current = null; setLoading(true); fetchLogs(false) }}
            className="rounded-full border border-[var(--panel-border)] px-2.5 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
          >
            Refresh
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
          {error}
        </div>
      )}

      <div
        ref={scrollRef}
        className="h-[400px] overflow-y-auto rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs leading-5"
      >
        {loading && entries.length === 0 && (
          <div className="py-8 text-center text-[var(--muted)]">Loading logs...</div>
        )}

        {!loading && entries.length === 0 && (
          <div className="py-8 text-center text-[var(--muted)]">
            No logs available. Stacklab service logs require journald access.
          </div>
        )}

        {filteredEntries.map((entry) => (
          <div key={entry.cursor || `${entry.timestamp}-${entry.message}`} className="flex gap-2 hover:bg-[rgba(255,255,255,0.02)]">
            <span className="shrink-0 text-zinc-600">
              {new Date(entry.timestamp).toLocaleTimeString()}
            </span>
            <span className={cn('shrink-0 w-12', levelColor[entry.level] ?? 'text-[var(--muted)]')}>
              {entry.level}
            </span>
            <span className="text-[var(--text)] break-all">{entry.message}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
