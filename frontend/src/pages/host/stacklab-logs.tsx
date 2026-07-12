import { useCallback, useEffect, useRef, useState } from 'react'
import { getStacklabLogs } from '@/lib/api-client'
import type { StacklabLogEntry } from '@/lib/api-types'
import { cn } from '@/lib/cn'

const MAX_STACKLAB_LOG_ENTRIES = 1_000
const LOG_LEVELS = ['debug', 'info', 'warn', 'error'] as const

export function StacklabLogs() {
  const [entries, setEntries] = useState<StacklabLogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [level, setLevel] = useState<'' | (typeof LOG_LEVELS)[number]>('')
  const [filter, setFilter] = useState('')
  const [includeHttpAccess, setIncludeHttpAccess] = useState(false)
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
        include_http: includeHttpAccess,
      })

      if (append) {
        if (result.items.length > 0) {
          setEntries((prev) => mergeLogEntries(prev, result.items))
        }
      } else {
        setEntries(mergeLogEntries([], result.items))
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
  }, [includeHttpAccess, level])

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
    debug: 'text-[var(--muted)]',
    info: 'text-[var(--muted)]',
    warn: 'text-[var(--warning)]',
    error: 'text-[var(--danger)]',
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-lg font-medium text-[var(--text)]">Stacklab logs</h2>

        <div className="flex flex-wrap items-center gap-2">
          {/* Level filter */}
          <div className="flex gap-1">
            <button
              onClick={() => setLevel('')}
              aria-pressed={!level}
              className={cn(
                'rounded-md border px-2.5 py-1 text-xs transition',
                !level
                  ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                  : 'border-[var(--panel-border)] text-[var(--muted)]',
              )}
            >
              All
            </button>
            {LOG_LEVELS.map((l) => (
              <button
                key={l}
                onClick={() => setLevel(l)}
                aria-pressed={level === l}
                className={cn(
                  'rounded-md border px-2.5 py-1 text-xs transition',
                  level === l
                    ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
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
            className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]"
          />

          <button
            type="button"
            title="Show HTTP access logs"
            onClick={() => setIncludeHttpAccess((value) => !value)}
            aria-pressed={includeHttpAccess}
            className={cn(
              'rounded-md border px-2.5 py-1 text-xs transition',
              includeHttpAccess
                ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            HTTP
          </button>

          <button
            onClick={() => setFollowing(!following)}
            aria-pressed={following}
            className={cn(
              'rounded-md border px-2.5 py-1 text-xs transition',
              following
                ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            {following ? 'Following' : 'Paused'}
          </button>

          <button
            onClick={() => { setEntries([]); cursorRef.current = null; setLoading(true); fetchLogs(false) }}
            className="rounded-md border border-[var(--panel-border)] px-2.5 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
          >
            Refresh
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
          {error}
        </div>
      )}

      <div
        ref={scrollRef}
        aria-busy={loading}
        className="h-[calc(100vh-430px)] min-h-[320px] overflow-y-auto rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs leading-5"
      >
        {loading && entries.length === 0 && (
          <div className="py-8 text-center text-[var(--muted)]" role="status" aria-live="polite">Loading logs...</div>
        )}

        {!loading && entries.length === 0 && !error && (
          <div className="py-8 text-center text-[var(--muted)]">
            No Stacklab log entries match the current view.
          </div>
        )}

        {filteredEntries.map((entry) => (
          <div key={entry.cursor || `${entry.timestamp}-${entry.message}`} className="flex gap-2 hover:bg-[rgba(255,255,255,0.02)]">
            <span className="shrink-0 text-[var(--muted)]">
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

function trimLogEntries(entries: StacklabLogEntry[]): StacklabLogEntry[] {
  if (entries.length <= MAX_STACKLAB_LOG_ENTRIES) return entries
  return entries.slice(entries.length - MAX_STACKLAB_LOG_ENTRIES)
}

function mergeLogEntries(current: StacklabLogEntry[], incoming: StacklabLogEntry[]): StacklabLogEntry[] {
  const entriesByCursor = new Map(current.map((entry) => [entry.cursor, entry]))
  for (const entry of incoming) entriesByCursor.set(entry.cursor, entry)
  return trimLogEntries(Array.from(entriesByCursor.values()))
}
