import { useEffect, useMemo, useRef, useState } from 'react'
import { useOutletContext, useSearchParams } from 'react-router-dom'
import type { StackDetailResponse } from '@/lib/api-types'
import { useLogStream } from '@/hooks/use-log-stream'
import { useWs } from '@/hooks/use-ws'
import { cn } from '@/lib/cn'
import { copyLogText, downloadLogFile, logExportFilename, serializeLogEntries } from '@/lib/log-export'

// Warm hues only: service labels must never collide with status colors (Z2).
const SERVICE_COLORS = [
  'text-[var(--accent)]',
  'text-[#E8C07A]',
  'text-[#C9855B]',
  'text-[#B5A276]',
  'text-[#F0D090]',
  'text-[#D9B08C]',
  'text-[#A68A64]',
  'text-[#8C7B5A]',
]

export function StackLogsPage() {
  const { stack } = useOutletContext<{ stack: StackDetailResponse['stack'] }>()
  const { connected } = useWs()
  const [searchParams] = useSearchParams()

  const serviceNames = stack.services.map((s) => s.name)
  const serviceKey = serviceNames.join(',')
  const requestedService = searchParams.get('service')?.trim() ?? ''
  const [selectedServices, setSelectedServices] = useState<string[]>([])
  const [filter, setFilter] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)
  const [wrapLines, setWrapLines] = useState(true)
  const [transferStatus, setTransferStatus] = useState<{ kind: 'success' | 'error'; message: string } | null>(null)

  const { entries, paused, pause, resume, clear } = useLogStream({
    stackId: stack.id,
    serviceNames: selectedServices,
    enabled: stack.containers.some((c) => c.status === 'running'),
  })

  const scrollRef = useRef<HTMLDivElement>(null)

  const colorMap = useMemo(() => {
    const map = new Map<string, string>()
    serviceNames.forEach((name, i) => {
      map.set(name, SERVICE_COLORS[i % SERVICE_COLORS.length])
    })
    return map
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serviceKey])

  useEffect(() => {
    if (requestedService && serviceNames.includes(requestedService)) {
      setSelectedServices([requestedService])
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [requestedService, serviceKey])

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [entries, autoScroll])

  function handleScroll() {
    if (!scrollRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current
    const atBottom = scrollHeight - scrollTop - clientHeight < 40
    setAutoScroll(atBottom)
  }

  const filteredEntries = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    return needle ? entries.filter((entry) => entry.line.toLowerCase().includes(needle)) : entries
  }, [entries, filter])
  const visibleLogText = useMemo(() => serializeLogEntries(filteredEntries), [filteredEntries])

  const activeServices = selectedServices.length > 0 ? selectedServices : serviceNames
  const noRunning = !stack.containers.some(
    (c) => c.status === 'running' && activeServices.includes(c.service_name),
  )

  async function handleCopy() {
    if (!visibleLogText) return
    try {
      await copyLogText(visibleLogText)
      setTransferStatus({ kind: 'success', message: copiedLineMessage(filteredEntries.length) })
    } catch {
      setTransferStatus({ kind: 'error', message: 'Could not copy log lines.' })
    }
  }

  function handleDownload() {
    if (!visibleLogText) return
    try {
      downloadLogFile(logExportFilename(stack.id), visibleLogText)
      setTransferStatus({ kind: 'success', message: downloadedLineMessage(filteredEntries.length) })
    } catch {
      setTransferStatus({ kind: 'error', message: 'Could not download log lines.' })
    }
  }

  if (noRunning) {
    return (
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-10 text-center">
        <p className="text-[var(--text)]">No logs available</p>
        <p className="mt-1 text-sm text-[var(--muted)]">
          {selectedServices.length > 0
            ? 'The selected service has no running container or no log output.'
            : 'This stack has no running containers with log output.'}
        </p>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {/* Controls */}
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex flex-wrap gap-1">
          <button
            onClick={() => setSelectedServices([])}
            className={cn(
              'rounded-md border px-3 py-1 text-xs transition',
              selectedServices.length === 0
                ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)] hover:text-[var(--text)]',
            )}
          >
            All
          </button>
          {serviceNames.map((name) => (
            <button
              key={name}
              onClick={() =>
                setSelectedServices((prev) =>
                  prev.includes(name) ? prev.filter((s) => s !== name) : [...prev, name],
                )
              }
              className={cn(
                'rounded-md border px-3 py-1 text-xs transition',
                selectedServices.includes(name)
                  ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                  : 'border-[var(--panel-border)] text-[var(--muted)] hover:text-[var(--text)]',
              )}
            >
              {name}
            </button>
          ))}
        </div>

        <input
          type="text"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter..."
          className="ml-auto rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]"
        />

        <button
          onClick={paused ? resume : pause}
          className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
        >
          {paused ? 'Resume' : 'Pause'}
        </button>

        <button
          onClick={() => {
            clear()
            setTransferStatus(null)
          }}
          className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
        >
          Clear
        </button>

        <button
          type="button"
          aria-pressed={wrapLines}
          onClick={() => setWrapLines((current) => !current)}
          className={cn(
            'rounded-md border px-3 py-1 text-xs transition',
            wrapLines
              ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
              : 'border-[var(--panel-border)] text-[var(--muted)] hover:text-[var(--text)]',
          )}
        >
          Wrap lines
        </button>

        <button
          type="button"
          onClick={() => void handleCopy()}
          disabled={filteredEntries.length === 0}
          className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)] disabled:cursor-not-allowed disabled:opacity-40"
        >
          Copy visible
        </button>

        <button
          type="button"
          onClick={handleDownload}
          disabled={filteredEntries.length === 0}
          className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)] disabled:cursor-not-allowed disabled:opacity-40"
        >
          Download visible
        </button>
      </div>

      {/* Connection status */}
      {!connected && (
        <div className="text-xs text-[var(--warning)]">Stream disconnected. Reconnecting...</div>
      )}
      {paused && (
        <div className="text-xs text-[var(--warning)]">Paused — new logs are buffered.</div>
      )}
      {transferStatus && (
        <div
          role={transferStatus.kind === 'error' ? 'alert' : 'status'}
          aria-live="polite"
          className={cn('text-xs', transferStatus.kind === 'error' ? 'text-[var(--danger)]' : 'text-[var(--ok)]')}
        >
          {transferStatus.message}
        </div>
      )}

      {/* Log output */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="h-[min(70vh,720px)] min-h-[320px] overflow-auto rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs leading-5"
      >
        {filteredEntries.length === 0 && (
          <div className="py-8 text-center text-[var(--muted)]">
            {entries.length > 0 && filter.trim() ? 'No log lines match the current filter.' : 'Waiting for logs...'}
          </div>
        )}
        {filteredEntries.map((entry, i) => (
          <div
            key={logEntryKey(entry, i)}
            className={cn(
              'flex py-0.5 hover:bg-[rgba(255,255,255,0.02)]',
              wrapLines ? 'flex-col gap-0.5 sm:flex-row sm:gap-2 sm:py-0' : 'min-w-max flex-row gap-2 py-0',
            )}
          >
            {/* Meta (time · service) stays on its own line on phones so the
                message below can use the full width instead of a cramped
                right-hand column. Inline on sm+ (tablet/desktop). */}
            <div className="flex shrink-0 gap-2">
              <span className="shrink-0 text-[var(--muted)]">
                {new Date(entry.timestamp).toLocaleTimeString()}
              </span>
              <span className={cn('shrink-0 w-24 truncate', colorMap.get(entry.service_name))}>
                {entry.service_name}
              </span>
            </div>
            <span
              data-testid="log-line"
              className={cn(
                'min-w-0 text-[var(--text)] sm:flex-1',
                wrapLines ? 'whitespace-pre-wrap break-all' : 'whitespace-pre',
              )}
            >
              {(entry.spans ?? [{ text: entry.line }]).map((s, j) => (
                <span
                  key={j}
                  style={{
                    color: s.color,
                    fontWeight: s.bold ? 600 : undefined,
                    opacity: s.dim ? 0.6 : undefined,
                  }}
                >
                  {s.text}
                </span>
              ))}
            </span>
          </div>
        ))}
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between text-xs text-[var(--muted)]">
        <span>
          Lines: {filteredEntries.length}
          {filter.trim() && ` of ${entries.length}`}
        </span>
        {!autoScroll && (
          <button
            onClick={() => {
              setAutoScroll(true)
              scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
            }}
            className="text-[var(--accent)] hover:underline"
          >
            Scroll to bottom
          </button>
        )}
      </div>
    </div>
  )
}

function logEntryKey(entry: { timestamp: string; service_name: string; container_id: string; stream: string; line: string }, index: number): string {
  return `${entry.timestamp}:${entry.container_id}:${entry.stream}:${entry.service_name}:${entry.line}:${index}`
}

function copiedLineMessage(count: number): string {
  return `Copied ${count} log ${count === 1 ? 'line' : 'lines'}.`
}

function downloadedLineMessage(count: number): string {
  return `Downloaded ${count} log ${count === 1 ? 'line' : 'lines'}.`
}
