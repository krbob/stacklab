import { cn } from '@/lib/cn'

interface DiffViewProps {
  diff: string
  truncated?: boolean
}

export function DiffView({ diff, truncated = false }: DiffViewProps) {
  const lines = diff.split('\n')

  return (
    <div className="overflow-auto rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs leading-5">
      {lines.map((line, i) => {
        let color = 'text-[var(--muted)]'
        let bg = ''
        if (line.startsWith('+') && !line.startsWith('+++')) {
          color = 'text-[var(--ok)]'
          bg = 'bg-[var(--ok)]/5'
        } else if (line.startsWith('-') && !line.startsWith('---')) {
          color = 'text-[var(--danger)]'
          bg = 'bg-[var(--danger)]/5'
        } else if (line.startsWith('@@')) {
          color = 'text-[var(--accent)]'
        }

        return (
          <div key={i} className={cn('px-1', color, bg)}>
            {line || '\u00A0'}
          </div>
        )
      })}

      {truncated && (
        <div className="mt-2 rounded border border-[var(--warning)]/20 bg-[var(--warning)]/5 px-2 py-1 text-xs text-[var(--warning)]">
          Diff truncated — file is too large to display completely.
        </div>
      )}
    </div>
  )
}
