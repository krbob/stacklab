import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { ExternalLink } from 'lucide-react'

import { checkImageUpdates, getStacks } from '@/lib/api-client'
import type { StackListItem, StackListResponse } from '@/lib/api-types'
import { useJobStream } from '@/hooks/use-job-stream'
import { PageHeader } from '@/components/page-header'
import { cn } from '@/lib/cn'

type StatusFilter = 'all' | 'problems' | 'updates'

const edgeColors: Record<string, string> = {
  running: 'border-l-[var(--ok)]',
  partial: 'border-l-[var(--warning)]',
  error: 'border-l-[var(--danger)]',
  orphaned: 'border-l-[var(--danger)]',
  stopped: 'border-l-stone-500',
  defined: 'border-l-stone-600',
}

const stateLabels: Record<string, { label: string; className: string }> = {
  running: { label: 'Running', className: 'text-[var(--ok)]' },
  partial: { label: 'Partial', className: 'text-[var(--warning)]' },
  error: { label: 'Error', className: 'text-[var(--danger)]' },
  orphaned: { label: 'Orphaned', className: 'text-[var(--danger)]' },
  stopped: { label: 'Stopped', className: 'text-stone-400' },
  defined: { label: 'Defined', className: 'text-stone-500' },
}

function hasProblem(stack: StackListItem): boolean {
  return (
    stack.display_state === 'error' ||
    stack.display_state === 'partial' ||
    stack.display_state === 'orphaned' ||
    stack.config_state === 'invalid' ||
    stack.health_summary.unhealthy_container_count > 0
  )
}

function formatMemory(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)}G`
  if (bytes >= 1 << 20) return `${Math.round(bytes / (1 << 20))}M`
  return `${Math.max(1, Math.round(bytes / 1024))}K`
}

function StackTile({ stack }: { stack: StackListItem }) {
  const state = stateLabels[stack.display_state] ?? stateLabels.defined
  const unhealthy = stack.health_summary.unhealthy_container_count
  const links = stack.metadata?.links ?? []

  // Stretched-link card: the stack link is an overlay, so external metadata
  // links are siblings, not anchors nested inside an anchor.
  return (
    <div className="break-inside-avoid pb-3">
    <article
      data-testid={`stack-card-${stack.id}`}
      className={cn(
        'relative rounded-lg border border-l-[3px] border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-3 transition focus-within:border-[rgba(245,165,36,0.35)] hover:border-[rgba(245,165,36,0.35)] hover:bg-[rgba(255,255,255,0.05)]',
        edgeColors[stack.display_state] ?? edgeColors.defined,
      )}
    >
      <Link
        to={`/stacks/${stack.id}`}
        aria-label={stack.name}
        className="absolute inset-0 rounded-lg"
      />
      <div className="flex items-center gap-2">
        <StackGlyph name={stack.name} icon={stack.metadata?.icon} />
        <span className="min-w-0 truncate font-mono text-sm font-semibold text-[var(--text)]">{stack.name}</span>
        {stack.updates?.state === 'available' && (
          <span className="shrink-0 rounded border border-[rgba(245,165,36,0.4)] px-1 font-mono text-[9px] font-bold uppercase tracking-wide text-[var(--accent)]">
            update
          </span>
        )}
        <span className={cn('ml-auto shrink-0 text-xs', state.className)}>
          {stack.activity_state === 'locked' ? 'Working…' : state.label}
        </span>
      </div>

      <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[11px] text-[var(--muted)]">
        <span>
          {stack.service_count.running}/{stack.service_count.defined} services
        </span>
        {stack.config_state === 'drifted' && (
          <span className="rounded border border-[rgba(245,165,36,0.35)] px-1 text-[10px] uppercase tracking-wide text-[var(--accent)]">drift</span>
        )}
        {stack.config_state === 'invalid' && (
          <span className="rounded border border-[var(--danger)]/40 px-1 text-[10px] uppercase tracking-wide text-[var(--danger)]">invalid</span>
        )}
        {unhealthy > 0 && (
          <span className="text-[var(--warning)]">{unhealthy} unhealthy</span>
        )}
      </div>

      {stack.stats && (
        <div className="mt-2 flex items-center gap-2 font-mono text-[11px] tabular-nums text-[var(--muted)]">
          <span>cpu {stack.stats.cpu_percent.toFixed(1)}%</span>
          <span
            role="progressbar"
            aria-label={`${stack.name} CPU usage`}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.min(100, Math.max(0, Number(stack.stats.cpu_percent.toFixed(1))))}
            className="h-1 flex-1 overflow-hidden rounded-full bg-[rgba(255,255,255,0.07)]"
          >
            <span
              className="block h-full bg-[var(--accent)]/75"
              style={{ width: `${Math.min(100, Math.max(0, stack.stats.cpu_percent))}%` }}
              aria-hidden="true"
            />
          </span>
          <span>mem {formatMemory(stack.stats.memory_bytes)}</span>
        </div>
      )}

      {(stack.last_action || links.length > 0) && (
        <div className="mt-2 flex items-center gap-2 font-mono text-[11px] text-[var(--muted)]">
          {stack.last_action && (
            <span className={cn('min-w-0 truncate', stack.last_action.result === 'failed' && 'text-[var(--danger)]')}>
              last: {stack.last_action.action} ({stack.last_action.result})
            </span>
          )}
          <span className="relative z-10 ml-auto flex min-w-0 gap-2">
            {links.map((link) => (
              <a
                key={link.url}
                href={link.url}
                target="_blank"
                rel="noreferrer"
                className="flex min-w-0 items-center gap-1 text-[var(--accent)] hover:underline"
              >
                <ExternalLink className="size-3 shrink-0" />
                <span className="truncate">{link.label}</span>
              </a>
            ))}
          </span>
        </div>
      )}
    </article>
    </div>
  )
}

// Monogram glyph; when metadata declares an icon slug we still monogram until
// a bundled icon set lands (design decision: no network-fetched icons).
function StackGlyph({ name, icon }: { name: string; icon?: string }) {
  const letter = (icon ?? name).charAt(0).toUpperCase()
  return (
    <span
      aria-hidden
      className="flex size-5 shrink-0 items-center justify-center rounded border border-[rgba(245,165,36,0.25)] bg-[rgba(245,165,36,0.08)] font-mono text-[10px] font-bold text-[var(--accent)]"
    >
      {letter}
    </span>
  )
}

export function StacksPage() {
  const [reloadKey, setReloadKey] = useState(0)
  const [data, setData] = useState<StackListResponse | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const loading = data === null && error === null

  // The stats collector samples every 10s server-side; refresh the list on
  // the same cadence — silently, keeping the previous data on screen (and on
  // poll errors), and only while the tab is visible.
  useEffect(() => {
    let cancelled = false
    let hasData = false
    const load = () => {
      getStacks()
        .then((response) => {
          if (cancelled) return
          hasData = true
          setData(response)
          setError(null)
        })
        .catch((err) => {
          if (!cancelled && !hasData) setError(err instanceof Error ? err : new Error('Failed to load stacks'))
        })
    }
    load()
    const interval = setInterval(() => {
      if (document.visibilityState === 'visible') load()
    }, 10_000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [reloadKey])

  const [filter, setFilter] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [checkJobId, setCheckJobId] = useState<string | null>(null)
  const filterRef = useRef<HTMLInputElement>(null)

  // The check runs as a detached job — follow its stream and reload the list
  // only once it reaches a terminal state (the response returns immediately).
  const checkStream = useJobStream({ jobId: checkJobId })
  const checkTerminal = ['succeeded', 'failed', 'cancelled', 'timed_out'].includes(checkStream.state ?? '')
  const checking = checkJobId !== null && !checkTerminal
  const checkProgress = checking
    ? [...checkStream.events].reverse().find((event) => event.progress)?.progress ?? null
    : null

  useEffect(() => {
    if (!checkJobId || !checkTerminal) return
    setCheckJobId(null)
    setReloadKey((key) => key + 1)
  }, [checkJobId, checkTerminal])

  async function handleCheckUpdates() {
    try {
      const result = await checkImageUpdates()
      setCheckJobId(result.job.id)
    } catch {
      // start failures surface in audit; the button simply stays idle
    }
  }

  // "/" focuses the filter from anywhere on the page (Z5).
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return
      e.preventDefault()
      filterRef.current?.focus()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  const items = useMemo(() => data?.items ?? [], [data])
  const problemCount = useMemo(() => items.filter(hasProblem).length, [items])
  const updateCount = useMemo(() => items.filter((s) => s.updates?.state === 'available').length, [items])

  const visible = items.filter((stack) => {
    if (filter && !stack.name.toLowerCase().includes(filter.toLowerCase())) return false
    if (status === 'problems' && !hasProblem(stack)) return false
    if (status === 'updates' && stack.updates?.state !== 'available') return false
    return true
  })

  return (
    <section aria-busy={loading || checking} className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <PageHeader
        kicker="Dashboard"
        title="Stacks"
        meta={data?.summary && (
          <>
            <span>{data.summary.stack_count} stacks</span>
            <span className="text-[var(--ok)]">{data.summary.running_count} running</span>
            {data.summary.stopped_count > 0 && <span>{data.summary.stopped_count} stopped</span>}
            {data.summary.error_count > 0 && (
              <span className="text-[var(--danger)]">{data.summary.error_count} error</span>
            )}
            <span>
              {data.summary.container_count.running}/{data.summary.container_count.total} containers
            </span>
          </>
        )}
        actions={
          <Link
            to="/stacks/new"
            className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)]"
          >
            New stack
          </Link>
        }
      />

      {/* Toolbar */}
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <input
          ref={filterRef}
          data-testid="stacks-filter"
          type="text"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="/ filter stacks…"
          className="w-full max-w-64 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1.5 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]"
        />
        <button
          onClick={() => setStatus('all')}
          aria-pressed={status === 'all'}
          className={cn(
            'rounded-md border px-3 py-1.5 text-xs transition',
            status === 'all'
              ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
              : 'border-[var(--panel-border)] text-[var(--muted)] hover:text-[var(--text)]',
          )}
        >
          All {items.length > 0 && items.length}
        </button>
        <button
          onClick={() => setStatus('problems')}
          aria-pressed={status === 'problems'}
          className={cn(
            'rounded-md border px-3 py-1.5 text-xs transition',
            status === 'problems'
              ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
              : 'border-[var(--panel-border)] text-[var(--muted)] hover:text-[var(--text)]',
            problemCount > 0 && status !== 'problems' && 'text-[var(--warning)]',
          )}
        >
          Problems {problemCount}
        </button>
        <button
          onClick={() => setStatus('updates')}
          aria-pressed={status === 'updates'}
          className={cn(
            'rounded-md border px-3 py-1.5 text-xs transition',
            status === 'updates'
              ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
              : 'border-[var(--panel-border)] text-[var(--muted)] hover:text-[var(--text)]',
            updateCount > 0 && status !== 'updates' && 'text-[var(--accent)]',
          )}
        >
          Updates {updateCount}
        </button>
        <button
          onClick={handleCheckUpdates}
          disabled={checking}
          className="ml-auto rounded-md border border-[var(--panel-border)] px-3 py-1.5 text-xs text-[var(--muted)] transition hover:text-[var(--text)] disabled:opacity-50"
        >
          {checking
            ? checkProgress
              ? `Checking… ${checkProgress.completed}/${checkProgress.total}`
              : 'Checking…'
            : 'Check updates'}
        </button>
      </div>

      {/* Tile grid */}
      <div className="mt-5">
        {loading && (
          <div className="columns-[15rem] gap-3">
            <span className="sr-only" role="status" aria-live="polite">Loading stacks...</span>
            {[1, 2, 3, 4, 5, 6].map((i) => (
              <div key={i} className="break-inside-avoid pb-3">
                <div className="h-20 animate-pulse rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)]" />
              </div>
            ))}
          </div>
        )}

        {error && (
          <div className="rounded-md border border-[var(--danger)]/20 bg-[var(--danger)]/5 p-5">
            <p className="text-sm text-[var(--danger)]">Failed to load stacks: {error.message}</p>
          </div>
        )}

        {!loading && !error && (
          <div className="columns-[15rem] gap-3">
            {visible.map((stack) => (
              <StackTile key={stack.id} stack={stack} />
            ))}
          </div>
        )}

        {!loading && !error && visible.length === 0 && items.length > 0 && (
          <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-10 text-center">
            <p className="text-[var(--text)]">No stacks match</p>
            <p className="mt-1 text-sm text-[var(--muted)]">Adjust the filter or status chips above.</p>
          </div>
        )}

        {data && items.length === 0 && (
          <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-10 text-center">
            <p className="text-lg text-[var(--text)]">No stacks found</p>
            <p className="mt-2 text-sm text-[var(--muted)]">
              No compose.yaml files detected in the managed stacks root.
            </p>
            <Link
              to="/stacks/new"
              className="mt-4 inline-block rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)]"
            >
              Create your first stack
            </Link>
          </div>
        )}
      </div>
    </section>
  )
}
