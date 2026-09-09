import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { LayoutGrid, List } from 'lucide-react'

import { checkImageUpdates, getStacks, updateStacksMaintenance } from '@/lib/api-client'
import type { StackListItem } from '@/lib/api-types'
import { useApi } from '@/hooks/use-api'
import { useJobStream } from '@/hooks/use-job-stream'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { useDashboardStats } from '@/hooks/use-dashboard-stats'
import { AsyncState } from '@/components/async-state'
import { PageHeader } from '@/components/page-header'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { cn } from '@/lib/cn'
import { StackCards, StackTable, type StackSortKey, type StackView } from '@/pages/stacks/stack-presentations'

type StatusFilter = 'all' | 'problems' | 'updates'
const VIEW_STORAGE_KEY = 'stacklab.dashboard.view'

function hasProblem(stack: StackListItem): boolean {
  return (
    stack.display_state === 'error' ||
    stack.display_state === 'partial' ||
    stack.display_state === 'orphaned' ||
    stack.config_state === 'invalid' ||
    stack.health_summary.unhealthy_container_count > 0
  )
}

export function StacksPage() {
  const { data, error, loading, refetch } = useApi(() => getStacks(), [])
  const stats = useDashboardStats()
  const { openJob } = useJobDrawer()
  const loadError = error ? new Error(`Failed to load stacks: ${error.message}`) : null

  // Resource usage refreshes separately every second; the full inventory
  // scan keeps its slower cadence and runs only while the tab is visible.
  useEffect(() => {
    const interval = setInterval(() => {
      if (document.visibilityState === 'visible') refetch()
    }, 10_000)
    return () => clearInterval(interval)
  }, [refetch])

  const [filter, setFilter] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [sortKey, setSortKey] = useState<StackSortKey>('name')
  const [view, setView] = useState<StackView>(() => {
    try {
      return localStorage.getItem(VIEW_STORAGE_KEY) === 'list' ? 'list' : 'cards'
    } catch {
      return 'cards'
    }
  })

  function changeView(next: StackView) {
    setView(next)
    try {
      localStorage.setItem(VIEW_STORAGE_KEY, next)
    } catch {
      // The toggle still works when browser storage is unavailable.
    }
  }
  const [checkJobId, setCheckJobId] = useState<string | null>(null)
  const [startingCheck, setStartingCheck] = useState(false)
  const [checkError, setCheckError] = useState<Error | null>(null)
  const [pendingUpdates, setPendingUpdates] = useState<StackListItem[] | null>(null)
  const [updateJobId, setUpdateJobId] = useState<string | null>(null)
  const [startingUpdate, setStartingUpdate] = useState(false)
  const [updateError, setUpdateError] = useState<string | null>(null)
  const filterRef = useRef<HTMLInputElement>(null)

  // The check runs as a detached job — follow its stream and reload the list
  // only once it reaches a terminal state (the response returns immediately).
  const checkStream = useJobStream({ jobId: checkJobId })
  const checkTerminal = ['succeeded', 'failed', 'cancelled', 'timed_out'].includes(checkStream.state ?? '')
  const followingCheck = checkJobId !== null && !checkTerminal
  const checking = startingCheck || followingCheck
  const checkProgress = followingCheck
    ? [...checkStream.events].reverse().find((event) => event.progress)?.progress ?? null
    : null

  useEffect(() => {
    if (!checkJobId || !checkTerminal) return
    if (checkStream.state !== 'succeeded') {
      setCheckError(new Error(`Image update check ${checkStream.state?.replace('_', ' ') ?? 'failed'}.`))
    }
    setCheckJobId(null)
    refetch()
  }, [checkJobId, checkStream.state, checkTerminal, refetch])

  const updateStream = useJobStream({ jobId: updateJobId })
  const updateTerminal = ['succeeded', 'failed', 'cancelled', 'timed_out'].includes(updateStream.state ?? '')
  const updating = startingUpdate || (updateJobId !== null && !updateTerminal)
  const updateStep = [...updateStream.events].reverse().find((event) => event.step)?.step

  useEffect(() => {
    if (updateJobId && updateTerminal) refetch()
  }, [updateJobId, updateTerminal, refetch])

  async function handleUpdateAll() {
    if (updating || !pendingUpdates?.length) return
    const [first, ...rest] = pendingUpdates
    setStartingUpdate(true)
    setUpdateError(null)
    try {
      const result = await updateStacksMaintenance({
        target: { mode: 'selected', stack_ids: [first.id, ...rest.map((stack) => stack.id)] },
        options: {
          pull_images: true,
          build_images: false,
          remove_orphans: false,
          preserve_inactive: true,
          prune_after: { enabled: false, include_volumes: false },
        },
      })
      setUpdateJobId(result.job.id)
      setPendingUpdates(null)
      openJob(result.job.id)
    } catch (error) {
      setUpdateError(error instanceof Error ? error.message : 'Failed to start updates')
    } finally {
      setStartingUpdate(false)
    }
  }

  async function handleCheckUpdates() {
    if (checking || updating) return
    setCheckError(null)
    setStartingCheck(true)
    try {
      const result = await checkImageUpdates()
      setCheckJobId(result.job.id)
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Unknown error'
      setCheckError(new Error(`Failed to check image updates: ${message}`))
    } finally {
      setStartingCheck(false)
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

  const items = useMemo(() => (data?.items ?? []).map((stack) => ({
    ...stack,
    stats: stats.data ? stats.data.items[stack.id] ?? null : stack.stats,
  })), [data, stats.data])
  const problemCount = useMemo(() => items.filter(hasProblem).length, [items])
  const availableUpdates = useMemo(() => items.filter((s) => s.updates?.state === 'available')
    .sort((left, right) => left.name.localeCompare(right.name)), [items])
  const updateCount = availableUpdates.length

  const visible = items.filter((stack) => {
    if (filter && !stack.name.toLowerCase().includes(filter.toLowerCase())) return false
    if (status === 'problems' && !hasProblem(stack)) return false
    if (status === 'updates' && stack.updates?.state !== 'available') return false
    return true
  }).sort((left, right) => {
    if (sortKey !== 'name') {
      // Missing samples belong below measured zero usage as well.
      if (!left.stats && right.stats) return 1
      if (left.stats && !right.stats) return -1
      const field = sortKey === 'cpu' ? 'cpu_percent' : 'memory_bytes'
      const difference = (right.stats?.[field] ?? 0) - (left.stats?.[field] ?? 0)
      if (difference !== 0) return difference
    }
    return left.name.localeCompare(right.name) || left.id.localeCompare(right.id)
  })

  return (
    <section aria-busy={(loading && data === null) || checking || startingUpdate} className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
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
          All {data !== null && items.length}
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
          Problems {data !== null && problemCount}
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
          Updates {data !== null && updateCount}
        </button>
        <button
          onClick={handleCheckUpdates}
          disabled={checking || updating}
          className="ml-auto rounded-md border border-[var(--panel-border)] px-3 py-1.5 text-xs text-[var(--muted)] transition hover:text-[var(--text)] disabled:opacity-50"
        >
          {startingCheck
            ? 'Starting…'
            : followingCheck
            ? checkProgress
              ? `Checking… ${checkProgress.completed}/${checkProgress.total}`
              : 'Checking…'
            : 'Check updates'}
        </button>
        {(updateCount > 0 || updating) && (
          <button
            type="button"
            onClick={() => {
              setUpdateError(null)
              setPendingUpdates(availableUpdates)
            }}
            disabled={checking || updating || !!error}
            className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-3 py-1.5 text-xs text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)] disabled:opacity-50"
          >
            {updating ? 'Updating…' : 'Update all (' + updateCount + ')'}
          </button>
        )}
        <label className="flex items-center gap-2 text-xs text-[var(--muted)]">
          Sort by
          <select
            value={sortKey}
            onChange={(event) => setSortKey(event.target.value as StackSortKey)}
            className="rounded-md border border-[var(--panel-border)] bg-[var(--panel)] px-2 py-1.5 text-xs text-[var(--text)]"
          >
            <option value="name">Name A–Z</option>
            <option value="cpu">CPU ↓</option>
            <option value="memory">RAM ↓</option>
          </select>
        </label>
        <div role="group" aria-label="Stack view" className="inline-flex rounded-md border border-[var(--panel-border)] bg-black/15 p-1">
          {([
            ['cards', 'Cards', LayoutGrid],
            ['list', 'List', List],
          ] as const).map(([key, label, Icon]) => (
            <button key={key} type="button" aria-pressed={view === key} onClick={() => changeView(key)}
              className={cn('inline-flex items-center gap-1.5 rounded px-2.5 py-1 text-xs transition-colors',
                view === key ? 'bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'text-[var(--muted)] hover:text-[var(--text)]')}>
              <Icon aria-hidden="true" className="size-3.5" />
              {label}
            </button>
          ))}
        </div>
      </div>

      {pendingUpdates && (
        <ConfirmDialog
          title="Update all available stacks?"
          message="Pull the available images and redeploy these stacks. Services may restart; stopped stacks will stay stopped."
          error={updateError ? 'Could not start updates: ' + updateError + '. You can retry.' : null}
          items={pendingUpdates.map((stack) => stack.name)}
          confirmLabel="Start updates"
          confirmingLabel="Starting…"
          confirming={startingUpdate}
          onConfirm={() => { void handleUpdateAll() }}
          onCancel={() => setPendingUpdates(null)}
        />
      )}

      {updateJobId && (
        <div className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--panel-border)] px-3 py-2 text-xs">
          <p
            role="status"
            className={updateTerminal && updateStream.state !== 'succeeded' ? 'text-[var(--danger)]' : 'text-[var(--text)]'}
          >
            {updateTerminal
              ? updateStream.state === 'succeeded'
                ? 'Updates completed.'
                : 'Updates ' + updateStream.state?.replace('_', ' ') + '. Open details to review the result.'
              : updateStep
                ? 'Updating ' + (updateStep.target_stack_id ?? 'stacks') + ' · step ' + updateStep.index + '/' + updateStep.total
                : 'Updating stacks…'}
          </p>
          <button type="button" onClick={() => openJob(updateJobId)} className="text-[var(--accent)] hover:underline">
            View update details
          </button>
        </div>
      )}

      {stats.error && (
        <p role="alert" className="mt-3 text-xs text-[var(--warning)]">
          Resource usage could not be refreshed. Retrying automatically…
        </p>
      )}

      {checkError && (
        <div
          role="alert"
          className="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]"
        >
          <p>{checkError.message}</p>
          <button
            type="button"
            onClick={handleCheckUpdates}
            disabled={checking || updating}
            className="rounded-md border border-[var(--danger)]/30 px-3 py-1.5 text-xs text-[var(--danger)] hover:bg-[var(--danger)]/10 disabled:opacity-50"
          >
            Retry check
          </button>
        </div>
      )}

      {/* Tile grid */}
      <div className="mt-5">
        <AsyncState
          loading={loading && data === null}
          error={loadError}
          hasData={data !== null}
          isEmpty={data !== null && items.length === 0}
          loadingLabel="Loading stacks…"
          emptyMessage="No stacks found."
          emptyFallback={
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
          }
          onRetry={refetch}
          loadingFallback={
            <div className={view === 'cards' ? 'grid grid-cols-[repeat(auto-fill,minmax(min(100%,19rem),1fr))] gap-4' : 'space-y-2'}>
              {[1, 2, 3, 4, 5, 6].map((i) => (
                <div key={i} className={cn('animate-pulse rounded-xl border border-[var(--panel-border)] bg-white/3', view === 'cards' ? 'h-72' : 'h-16')} />
              ))}
            </div>
          }
        >
          {visible.length > 0 && (view === 'cards'
            ? <StackCards stacks={visible} history={stats.history} nowMs={stats.nowMs} />
            : <StackTable stacks={visible} history={stats.history} nowMs={stats.nowMs} sortKey={sortKey} onSortChange={setSortKey} />)}

          {visible.length === 0 && items.length > 0 && (
            <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-10 text-center">
              <p className="text-[var(--text)]">No stacks match</p>
              <p className="mt-1 text-sm text-[var(--muted)]">Adjust the filter or status chips above.</p>
            </div>
          )}
        </AsyncState>
      </div>
    </section>
  )
}
