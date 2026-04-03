import { useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { invokeAction } from '@/lib/api-client'
import type { StackDetailResponse } from '@/lib/api-types'
import { StackBadge } from '@/components/stack-badge'
import { DeleteStackDialog } from '@/components/delete-stack-dialog'
import { cn } from '@/lib/cn'

const containerStatusColor: Record<string, string> = {
  running: 'bg-emerald-400',
  created: 'bg-zinc-500',
  restarting: 'bg-amber-400',
  paused: 'bg-amber-400',
  exited: 'bg-zinc-500',
  dead: 'bg-red-400',
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
        <div className="rounded-2xl border border-amber-400/30 bg-amber-400/5 px-4 py-3 text-sm text-amber-400">
          Stack definition missing — runtime containers exist without compose.yaml.
        </div>
      )}

      <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
        <StackBadge
          displayState={stack.display_state}
          configState={stack.config_state}
          activityState={stack.activity_state}
        />
        <ActionBar stack={stack} onAction={refetch} onRemove={() => setShowDelete(true)} />
      </div>

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
              className="rounded-[20px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4"
            >
              <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <span className={cn(
                      'inline-block size-2 rounded-full',
                      containerStatusColor[container?.status ?? 'created'] ?? 'bg-zinc-600',
                    )} />
                    <span className="text-base font-medium text-[var(--text)]">{svc.name}</span>
                    {container?.health_status && (
                      <span className={cn(
                        'text-xs',
                        container.health_status === 'healthy' ? 'text-emerald-400' : 'text-red-400',
                      )}>
                        {healthIcon[container.health_status]}
                      </span>
                    )}
                  </div>

                  <div className="mt-2 grid gap-1 text-sm text-[var(--muted)]">
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
                      <div className="text-zinc-600">Not created</div>
                    )}

                    {svc.volumes.length > 0 && (
                      <div className="text-xs">
                        Mounts: {svc.volumes.map((v) => `${v.source} → ${v.target}`).join(', ')}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </div>
          )
        })}

        {stack.services.length === 0 && stack.runtime_state !== 'orphaned' && (
          <div className="rounded-[20px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-8 text-center">
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
  const locked = stack.activity_state === 'locked'
  const actions = stack.available_actions

  async function handleAction(action: string) {
    try {
      await invokeAction(stack.id, action)
      // brief delay to let backend start the job, then refresh
      setTimeout(onAction, 500)
    } catch (err) {
      console.error('Action failed:', err)
    }
  }

  const buttons: { label: string; action: string; variant?: 'danger' }[] = [
    { label: 'Deploy', action: 'up' },
    { label: 'Restart', action: 'restart' },
    { label: 'Stop', action: 'stop' },
    { label: 'Down', action: 'down', variant: 'danger' },
    { label: 'Pull', action: 'pull' },
    { label: 'Build', action: 'build' },
  ]

  return (
    <div className="flex flex-wrap gap-2">
      {buttons.map((btn) => {
        if (!actions.includes(btn.action as typeof actions[number])) return null
        return (
          <button
            key={btn.action}
            disabled={locked}
            onClick={() => handleAction(btn.action)}
            className={cn(
              'rounded-full border px-3 py-1.5 text-xs font-medium transition disabled:opacity-40',
              btn.variant === 'danger'
                ? 'border-red-400/30 text-red-400 hover:bg-red-400/10'
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
          className="rounded-full border border-red-400/30 px-3 py-1.5 text-xs font-medium text-red-400 transition hover:bg-red-400/10 disabled:opacity-40"
        >
          Remove
        </button>
      )}
    </div>
  )
}
