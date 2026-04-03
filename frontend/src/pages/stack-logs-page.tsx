import { useEffect, useMemo, useRef, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { StackDetailResponse } from '@/lib/api-types'
import { useLogStream } from '@/hooks/use-log-stream'
import { useWs } from '@/hooks/use-ws'
import { cn } from '@/lib/cn'

const SERVICE_COLORS = [
  'text-cyan-400',
  'text-violet-400',
  'text-amber-400',
  'text-emerald-400',
  'text-rose-400',
  'text-sky-400',
  'text-orange-400',
  'text-lime-400',
]

export function StackLogsPage() {
  const { stack } = useOutletContext<{ stack: StackDetailResponse['stack'] }>()
  const { connected } = useWs()

  const serviceNames = stack.services.map((s) => s.name)
  const [selectedServices, setSelectedServices] = useState<string[]>([])
  const [filter, setFilter] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)

  const { entries, paused, pause, resume, clear } = useLogStream({
    stackId: stack.id,
    serviceNames: selectedServices,
    enabled: stack.containers.some((c) => c.status === 'running'),
  })

  const scrollRef = useRef<HTMLDivElement>(null)

  const serviceKey = serviceNames.join(',')
  const colorMap = useMemo(() => {
    const map = new Map<string, string>()
    serviceNames.forEach((name, i) => {
      map.set(name, SERVICE_COLORS[i % SERVICE_COLORS.length])
    })
    return map
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serviceKey])

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

  const filteredEntries = filter
    ? entries.filter((e) => e.line.toLowerCase().includes(filter.toLowerCase()))
    : entries

  const activeServices = selectedServices.length > 0 ? selectedServices : serviceNames
  const noRunning = !stack.containers.some(
    (c) => c.status === 'running' && activeServices.includes(c.service_name),
  )

  if (noRunning) {
    return (
      <div className="rounded-[20px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-10 text-center">
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
              'rounded-full border px-3 py-1 text-xs transition',
              selectedServices.length === 0
                ? 'border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] text-[var(--text)]'
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
                'rounded-full border px-3 py-1 text-xs transition',
                selectedServices.includes(name)
                  ? 'border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] text-[var(--text)]'
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
          className="ml-auto rounded-full border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(79,209,197,0.35)]"
        />

        <button
          onClick={paused ? resume : pause}
          className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
        >
          {paused ? 'Resume' : 'Pause'}
        </button>

        <button
          onClick={clear}
          className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
        >
          Clear
        </button>
      </div>

      {/* Connection status */}
      {!connected && (
        <div className="text-xs text-amber-400">Stream disconnected. Reconnecting...</div>
      )}
      {paused && (
        <div className="text-xs text-amber-400">Paused — new logs are buffered.</div>
      )}

      {/* Log output */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="h-[500px] overflow-y-auto rounded-[16px] border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs leading-5"
      >
        {filteredEntries.length === 0 && (
          <div className="py-8 text-center text-[var(--muted)]">Waiting for logs...</div>
        )}
        {filteredEntries.map((entry, i) => (
          <div key={i} className="flex gap-2 hover:bg-[rgba(255,255,255,0.02)]">
            <span className="shrink-0 text-zinc-600">
              {new Date(entry.timestamp).toLocaleTimeString()}
            </span>
            <span className={cn('shrink-0 w-24 truncate', colorMap.get(entry.service_name))}>
              {entry.service_name}
            </span>
            <span className="text-[var(--text)] break-all">{entry.line}</span>
          </div>
        ))}
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between text-xs text-[var(--muted)]">
        <span>Lines: {filteredEntries.length}</span>
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
