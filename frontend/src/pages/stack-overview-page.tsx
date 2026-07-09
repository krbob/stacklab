import { useCallback, useState } from 'react'
import { Link, useOutletContext } from 'react-router-dom'
import { FileText, Terminal } from 'lucide-react'
import { invokeAction, updateStacksMaintenance } from '@/lib/api-client'
import { useJobStream } from '@/hooks/use-job-stream'
import type { StackDetailResponse } from '@/lib/api-types'
import { DeleteStackDialog } from '@/components/delete-stack-dialog'
import { ProgressPanel } from '@/components/progress-panel'
import { cn } from '@/lib/cn'

const containerStatusColor: Record<string, string> = {
  running: 'bg-[var(--ok)]',
  created: 'bg-stone-500',
  restarting: 'bg-[var(--warning)]',
  paused: 'bg-[var(--warning)]',
  exited: 'bg-stone-500',
  dead: 'bg-[var(--danger)]',
}

const healthIcon: Record<string, string> = {
  healthy: '♥',
  unhealthy: '!',
  starting: '~',
}

export function StackOverviewPage() {
  const { stack, refetch } = useOutletContext<{
    stack: StackDetailResponse['stack']
    refetch: () => void
  }>()
  const [showDelete, setShowDelete] = useState(false)

  return (
    <div className="flex flex-col gap-5">
      {stack.runtime_state === 'orphaned' && (
        <div className="rounded-lg border border-[var(--warning)]/30 bg-[var(--warning)]/5 px-4 py-3 text-sm text-[var(--warning)]">
          Stack definition missing — runtime containers exist without compose.yaml.
        </div>
      )}

      {/* Status lives once in the stack header (stack-layout); the overview
          only carries the action bar. The bar spans full width so the output
          panel below it does too; the buttons themselves stay right-aligned. */}
      <ActionBar stack={stack} onAction={refetch} onRemove={() => setShowDelete(true)} />

      {showDelete && (
        <DeleteStackDialog
          stackId={stack.id}
          stackName={stack.name}
          onClose={() => { setShowDelete(false); refetch() }}
        />
      )}

      <div className="grid gap-3">
        {stack.services.map((svc) => {
          const container = stack.containers.find((c) => c.service_name === svc.name)

          return (
            <div
              key={svc.name}
              className="min-w-0 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4"
            >
              <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className={cn(
                      'inline-block size-2 shrink-0 rounded-full',
                      containerStatusColor[container?.status ?? 'created'] ?? 'bg-stone-600',
                    )} />
                    <span className="min-w-0 break-words text-base font-medium text-[var(--text)]">{svc.name}</span>
                    {container?.health_status && (
                      <span className={cn(
                        'text-xs',
                        container.health_status === 'healthy' ? 'text-[var(--ok)]' : 'text-[var(--danger)]',
                      )}>
                        {healthIcon[container.health_status]}
                      </span>
                    )}
                  </div>

                  <div className="mt-2 grid gap-1 [overflow-wrap:anywhere] text-sm text-[var(--muted)]">
                    <div>
                      {svc.image_ref && <span>Image: {svc.image_ref}</span>}
                      {svc.build_context && <span>Build: {svc.build_context}</span>}
                      <span className="ml-2 text-xs">({svc.mode})</span>
                    </div>

                    {svc.ports.length > 0 && (
                      <div>
                        Ports: {svc.ports.map((p) => `${p.published}:${p.target}`).join(', ')}
                      </div>
                    )}

                    {container && (
                      <div>
                        Status: {container.status}
                        {container.started_at && (
                          <span className="ml-1 text-xs">
                            (since {new Date(container.started_at).toLocaleString()})
                          </span>
                        )}
                      </div>
                    )}

                    {!container && (
                      <div className="text-[var(--muted)]">Not created</div>
                    )}

                    {svc.volumes.length > 0 && (
                      <div className="text-xs">
                        Mounts: {svc.volumes.map((v) => `${v.source} → ${v.target}`).join(', ')}
                      </div>
                    )}
                  </div>
                </div>
                <div className="flex shrink-0 flex-wrap gap-2">
                  {stack.capabilities.can_view_logs && (
                    <Link
                      to={`/stacks/${stack.id}/logs?service=${encodeURIComponent(svc.name)}`}
                      className="inline-flex items-center gap-1 rounded-md border border-[var(--panel-border)] px-2 py-1 text-xs text-[var(--muted)] transition hover:text-[var(--text)]"
                    >
                      <FileText className="size-3.5" />
                      Logs
                    </Link>
                  )}
                  {stack.capabilities.can_open_terminal && container?.status === 'running' && (
                    <Link
                      to={`/stacks/${stack.id}/terminal?service=${encodeURIComponent(svc.name)}`}
                      className="inline-flex items-center gap-1 rounded-md border border-[var(--panel-border)] px-2 py-1 text-xs text-[var(--muted)] transition hover:text-[var(--text)]"
                    >
                      <Terminal className="size-3.5" />
                      Shell
                    </Link>
                  )}
                </div>
              </div>
            </div>
          )
        })}

        {stack.services.length === 0 && stack.runtime_state !== 'orphaned' && (
          <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-8 text-center">
            <p className="text-[var(--text)]">Stack is defined but not running</p>
            <p className="mt-1 text-sm text-[var(--muted)]">compose.yaml found. No containers exist.</p>
          </div>
        )}
      </div>
    </div>
  )
}

function ActionBar({
  stack,
  onAction,
  onRemove,
}: {
  stack: StackDetailResponse['stack']
  onAction: () => void
  onRemove: () => void
}) {
  const [activeJobId, setActiveJobId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const activeJobStream = useJobStream({ jobId: activeJobId })
  const activeJobState = activeJobStream.state
  const terminalActionState = activeJobState === 'succeeded' || activeJobState === 'failed' || activeJobState === 'cancelled' || activeJobState === 'timed_out'
  const runningAction = activeJobId !== null && !terminalActionState
  const locked = stack.activity_state === 'locked' || runningAction
  const actions = stack.available_actions

  const handleAction = useCallback(async (action: string) => {
    try {
      setActionError(null)
      const result = await invokeAction(stack.id, action)
      setActiveJobId(result.job.id)
    } catch (err) {
      console.error('Action failed:', err)
      setActionError(err instanceof Error ? err.message : 'Action failed')
    }
  }, [stack.id])

  const handleUpdateStack = useCallback(async () => {
    try {
      setActionError(null)
      const result = await updateStacksMaintenance({
        target: {
          mode: 'selected',
          stack_ids: [stack.id],
        },
        options: {
          pull_images: true,
          build_images: true,
          remove_orphans: true,
          prune_after: {
            enabled: false,
            include_volumes: false,
          },
        },
      })
      setActiveJobId(result.job.id)
    } catch (err) {
      console.error('Update failed:', err)
      setActionError(err instanceof Error ? err.message : 'Update failed')
    }
  }, [stack.id])

  // Refresh stack state but keep the output visible — closing here used to
  // unmount the panel the instant the replay reached the terminal event, so
  // build/pull logs flashed for a frame and vanished.
  const handleJobDone = useCallback(() => {
    onAction()
  }, [onAction])

  const buttons: { label: string; action: string; variant?: 'danger' }[] = [
    { label: 'Deploy', action: 'up' },
    { label: 'Restart', action: 'restart' },
    { label: 'Stop', action: 'stop' },
    { label: 'Down', action: 'down', variant: 'danger' },
    { label: 'Pull', action: 'pull' },
    { label: 'Build', action: 'build' },
  ]

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap justify-end gap-2">
        {stack.updates?.state === 'available' && (
          <button
            disabled={locked}
            onClick={handleUpdateStack}
            className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-3 py-1.5 text-xs font-medium text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)] disabled:opacity-40"
          >
            Update
          </button>
        )}

        {buttons.map((btn) => {
          if (!actions.includes(btn.action as typeof actions[number])) return null
          return (
            <button
              key={btn.action}
              disabled={locked}
              onClick={() => handleAction(btn.action)}
              className={cn(
                'rounded-md border px-3 py-1.5 text-xs font-medium transition disabled:opacity-40',
                btn.variant === 'danger'
                  ? 'border-[var(--danger)]/30 text-[var(--danger)] hover:bg-[var(--danger)]/10'
                  : 'border-[var(--panel-border)] text-[var(--text)] hover:bg-[rgba(255,255,255,0.05)]',
              )}
            >
              {btn.label}
            </button>
          )
        })}

        {actions.includes('remove_stack_definition') && (
          <button
            disabled={locked}
            onClick={onRemove}
            className="rounded-md border border-[var(--danger)]/30 px-3 py-1.5 text-xs font-medium text-[var(--danger)] transition hover:bg-[var(--danger)]/10 disabled:opacity-40"
          >
            Remove
          </button>
        )}
      </div>

      {actionError && (
        <div className="rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
          {actionError}
        </div>
      )}

      {activeJobId && <ProgressPanel jobId={activeJobId} stream={activeJobStream} onDone={handleJobDone} onClose={() => setActiveJobId(null)} />}
    </div>
  )
}
