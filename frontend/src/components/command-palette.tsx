import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { getStacks } from '@/lib/api-client'
import { cn } from '@/lib/cn'
import { hasActiveModal } from '@/lib/modal-state'
import { useModalBehavior } from '@/hooks/use-modal-behavior'

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
  const [loadingStacks, setLoadingStacks] = useState(false)
  const [loadError, setLoadError] = useState(false)
  const [loadAttempt, setLoadAttempt] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listboxId = useId()
  const instructionsId = useId()
  const closePalette = useCallback(() => setOpen(false), [])
  const { panelRef, requestClose } = useModalBehavior<HTMLDivElement>({
    active: open,
    onClose: closePalette,
    initialFocusRef: inputRef,
  })

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        if (!open && hasActiveModal()) return
        e.preventDefault()
        if (open) requestClose()
        else setOpen(true)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [open, requestClose])

  useEffect(() => {
    if (!open) return
    setQuery('')
    setSelected(0)
    setStackEntries([])
  }, [open])

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoadingStacks(true)
    setLoadError(false)

    getStacks()
      .then((response) => {
        if (cancelled) return
        setStackEntries(
          response.items.map((stack) => ({
            kind: 'stack' as const,
            label: stack.name,
            hint: stack.display_state,
            to: `/stacks/${stack.id}`,
          })),
        )
      })
      .catch(() => {
        if (!cancelled) setLoadError(true)
      })
      .finally(() => {
        if (!cancelled) setLoadingStacks(false)
      })

    return () => {
      cancelled = true
    }
  }, [open, loadAttempt])

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

  function retryStackLoad() {
    if (loadingStacks) return
    inputRef.current?.focus()
    setLoadingStacks(true)
    setLoadError(false)
    setLoadAttempt((current) => current + 1)
  }

  const selectedOptionId = entries[selected] ? `${listboxId}-option-${selected}` : undefined
  const resultAnnouncement = loadingStacks
    ? 'Loading stacks.'
    : loadError
      ? `${entries.length} navigation options available. Stack list unavailable.`
      : `${entries.length} ${entries.length === 1 ? 'result' : 'results'} available.`

  return (
    <div className="fixed inset-0 z-[60]">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={requestClose} aria-hidden />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        tabIndex={-1}
        className="absolute inset-x-4 top-[15vh] mx-auto max-w-lg overflow-hidden rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] shadow-[0_24px_60px_rgba(0,0,0,0.6)]"
      >
        <input
          ref={inputRef}
          data-testid="palette-input"
          type="text"
          role="combobox"
          aria-label="Search commands"
          aria-autocomplete="list"
          aria-expanded="true"
          aria-controls={listboxId}
          aria-activedescendant={selectedOptionId}
          aria-describedby={instructionsId}
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
            } else if (e.key === 'Home') {
              e.preventDefault()
              setSelected(0)
            } else if (e.key === 'End') {
              e.preventDefault()
              setSelected(Math.max(entries.length - 1, 0))
            } else if (e.key === 'Enter') {
              e.preventDefault()
              activate(entries[selected])
            }
          }}
          placeholder="Jump to stack or section…"
          className="w-full border-b border-[var(--panel-border)] bg-transparent px-4 py-3 font-mono text-sm text-[var(--text)] outline-none placeholder:text-[var(--muted)]"
        />
        <p id={instructionsId} className="sr-only">Use arrow keys to choose an option, then press Enter.</p>
        <div
          id={listboxId}
          role="listbox"
          aria-label="Commands"
          aria-busy={loadingStacks}
          className="max-h-[45vh] overflow-y-auto p-1.5"
        >
          {entries.length === 0 && !loadingStacks && !loadError && (
            <div className="px-3 py-4 text-center text-xs text-[var(--muted)]">No matches</div>
          )}
          {entries.map((entry, index) => (
            <button
              key={entry.to + entry.label}
              id={`${listboxId}-option-${index}`}
              role="option"
              aria-selected={index === selected}
              tabIndex={-1}
              onClick={() => activate(entry)}
              onMouseDown={(event) => event.preventDefault()}
              onMouseEnter={() => setSelected(index)}
              className={cn(
                'flex w-full items-center gap-3 rounded-md px-3 py-2 text-left text-sm transition',
                index === selected ? 'bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'text-[var(--muted)]',
              )}
            >
              <span className={cn('font-mono', entry.kind === 'stack' && 'font-semibold')}>{entry.label}</span>
              <span className="ml-auto font-mono text-xs uppercase tracking-wide text-[var(--muted)]">{entry.hint}</span>
            </button>
          ))}
        </div>
        {loadError && (
          <div
            role="alert"
            className="flex items-center gap-3 border-t border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-xs text-[var(--danger)]"
          >
            <span>Stack shortcuts are unavailable. Section shortcuts still work.</span>
            <button
              type="button"
              onClick={retryStackLoad}
              className="ml-auto shrink-0 rounded-md border border-[var(--danger)]/30 px-3 py-1.5 font-medium hover:bg-[var(--danger)]/10"
            >
              Retry
            </button>
          </div>
        )}
        <div className="sr-only" role="status" aria-live="polite" aria-atomic="true">
          {resultAnnouncement}
        </div>
      </div>
    </div>
  )
}
