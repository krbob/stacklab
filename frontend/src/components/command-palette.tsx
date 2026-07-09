import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { getStacks } from '@/lib/api-client'
import { cn } from '@/lib/cn'
import { hasActiveModal } from '@/lib/modal-state'

interface PaletteEntry {
  kind: 'section' | 'stack'
  label: string
  hint: string
  to: string
}

const sectionEntries: PaletteEntry[] = [
  { kind: 'section', label: 'Stacks', hint: 'section', to: '/stacks' },
  { kind: 'section', label: 'Host', hint: 'section', to: '/host' },
  { kind: 'section', label: 'Config', hint: 'section', to: '/config' },
  { kind: 'section', label: 'Maintenance', hint: 'section', to: '/maintenance' },
  { kind: 'section', label: 'Docker', hint: 'section', to: '/docker' },
  { kind: 'section', label: 'Audit', hint: 'section', to: '/audit' },
  { kind: 'section', label: 'Settings', hint: 'section', to: '/settings' },
  { kind: 'section', label: 'New stack', hint: 'action', to: '/stacks/new' },
]

// ⌘K / Ctrl+K jump palette (Z5): sections and stacks, filtered as you type.
export function CommandPalette() {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)
  const [stackEntries, setStackEntries] = useState<PaletteEntry[]>([])
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        if (!open && hasActiveModal()) return
        e.preventDefault()
        setOpen((current) => !current)
      }
      if (e.key === 'Escape') setOpen(false)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [open])

  useEffect(() => {
    if (!open) return
    setQuery('')
    setSelected(0)
    inputRef.current?.focus()
    getStacks()
      .then((response) =>
        setStackEntries(
          response.items.map((stack) => ({
            kind: 'stack' as const,
            label: stack.name,
            hint: stack.display_state,
            to: `/stacks/${stack.id}`,
          })),
        ),
      )
      .catch(() => {})
  }, [open])

  const entries = useMemo(() => {
    const all = [...stackEntries, ...sectionEntries]
    const needle = query.trim().toLowerCase()
    if (!needle) return all
    return all.filter((entry) => entry.label.toLowerCase().includes(needle))
  }, [stackEntries, query])

  if (!open) return null

  function activate(entry: PaletteEntry | undefined) {
    if (!entry) return
    setOpen(false)
    navigate(entry.to)
  }

  return (
    <div className="fixed inset-0 z-[60]">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={() => setOpen(false)} aria-hidden />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        className="absolute inset-x-4 top-[15vh] mx-auto max-w-lg overflow-hidden rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] shadow-[0_24px_60px_rgba(0,0,0,0.6)]"
      >
        <input
          ref={inputRef}
          data-testid="palette-input"
          type="text"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setSelected(0)
          }}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setSelected((current) => Math.min(current + 1, entries.length - 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setSelected((current) => Math.max(current - 1, 0))
            } else if (e.key === 'Enter') {
              e.preventDefault()
              activate(entries[selected])
            }
          }}
          placeholder="Jump to stack or section…"
          className="w-full border-b border-[var(--panel-border)] bg-transparent px-4 py-3 font-mono text-sm text-[var(--text)] outline-none placeholder:text-[var(--muted)]"
        />
        <div className="max-h-[45vh] overflow-y-auto p-1.5">
          {entries.length === 0 && (
            <div className="px-3 py-4 text-center text-xs text-[var(--muted)]">No matches</div>
          )}
          {entries.map((entry, index) => (
            <button
              key={entry.to + entry.label}
              onClick={() => activate(entry)}
              onMouseEnter={() => setSelected(index)}
              className={cn(
                'flex w-full items-center gap-3 rounded-md px-3 py-2 text-left text-sm transition',
                index === selected ? 'bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'text-[var(--muted)]',
              )}
            >
              <span className={cn('font-mono', entry.kind === 'stack' && 'font-semibold')}>{entry.label}</span>
              <span className="ml-auto font-mono text-[10px] uppercase tracking-wide opacity-60">{entry.hint}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
