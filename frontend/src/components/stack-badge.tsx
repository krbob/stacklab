import type { DisplayState, ConfigState, ActivityState } from '@/lib/api-types'
import { cn } from '@/lib/cn'

const runtimeStyles: Record<DisplayState, { dot: string; label: string; text: string }> = {
  running: { dot: 'bg-emerald-400', label: 'Running', text: 'text-emerald-400' },
  stopped: { dot: 'bg-zinc-500', label: 'Stopped', text: 'text-zinc-400' },
  partial: { dot: 'bg-amber-400', label: 'Partial', text: 'text-amber-400' },
  error: { dot: 'bg-red-400', label: 'Error', text: 'text-red-400' },
  defined: { dot: 'bg-zinc-600 border border-zinc-500', label: 'Defined', text: 'text-zinc-500' },
  orphaned: { dot: 'bg-red-500', label: 'Orphaned', text: 'text-red-400' },
}

const configLabels: Partial<Record<ConfigState, { label: string; className: string }>> = {
  drifted: { label: 'Drifted', className: 'text-amber-400' },
  invalid: { label: 'Invalid', className: 'text-red-400' },
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
  const runtime = runtimeStyles[displayState]
  const config = configLabels[configState]

  return (
    <div className="flex items-center gap-2">
      <div className="relative flex items-center gap-2">
        <span className={cn('inline-block size-2.5 rounded-full', runtime.dot)} />
        {activityState === 'locked' && (
          <span className="absolute -left-0.5 -top-0.5 size-3.5 animate-ping rounded-full bg-sky-400/40" />
        )}
        <span className={cn('text-sm font-medium', runtime.text)}>{runtime.label}</span>
      </div>

      {config && (
        <span className={cn('text-xs', config.className)}>
          · {config.label}
        </span>
      )}

      {activityState === 'locked' && (
        <span className="text-xs text-sky-400">· Locked</span>
      )}
    </div>
  )
}
