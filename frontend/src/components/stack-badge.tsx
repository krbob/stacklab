import type { DisplayState, ConfigState, ActivityState } from '@/lib/api-types'
import { cn } from '@/lib/cn'

const runtimeStyles: Record<DisplayState, { dot: string; label: string; text: string }> = {
  running: { dot: 'bg-[var(--ok)]', label: 'Running', text: 'text-[var(--ok)]' },
  stopped: { dot: 'bg-stone-500', label: 'Stopped', text: 'text-stone-400' },
  partial: { dot: 'bg-[var(--warning)]', label: 'Partial', text: 'text-[var(--warning)]' },
  error: { dot: 'bg-[var(--danger)]', label: 'Error', text: 'text-[var(--danger)]' },
  defined: { dot: 'bg-stone-600 border border-stone-500', label: 'Defined', text: 'text-stone-500' },
  orphaned: { dot: 'bg-red-500', label: 'Orphaned', text: 'text-[var(--danger)]' },
}

const configLabels: Partial<Record<ConfigState, { label: string; className: string }>> = {
  drifted: { label: 'Drifted', className: 'text-[var(--warning)]' },
  invalid: { label: 'Invalid', className: 'text-[var(--danger)]' },
}

export function StackBadge({
  displayState,
  configState,
  activityState,
}: {
  displayState: DisplayState
  configState: ConfigState
  activityState: ActivityState
}) {
  const runtime = runtimeStyles[displayState] ?? {
    dot: 'bg-stone-500',
    label: String(displayState || 'unknown'),
    text: 'text-[var(--muted)]',
  }
  const config = configLabels[configState]

  return (
    <div className="flex items-center gap-2">
      <div className="relative flex items-center gap-2">
        <span className={cn('inline-block size-2.5 rounded-full', runtime.dot)} />
        {activityState === 'locked' && (
          <span className="absolute -left-0.5 -top-0.5 size-3.5 animate-ping rounded-full bg-[var(--run)]/40" />
        )}
        <span className={cn('text-sm font-medium', runtime.text)}>{runtime.label}</span>
      </div>

      {config && (
        <span className={cn('text-xs', config.className)}>
          · {config.label}
        </span>
      )}

      {activityState === 'locked' && (
        <span className="text-xs text-[var(--run)]">· Locked</span>
      )}
    </div>
  )
}
