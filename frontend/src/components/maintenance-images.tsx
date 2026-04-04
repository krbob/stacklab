import { useState } from 'react'
import { getMaintenanceImages } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import type { MaintenanceImageItem, MaintenanceImageUsage, MaintenanceImageOrigin } from '@/lib/api-types'
import { cn } from '@/lib/cn'

function formatBytes(bytes: number): string {
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

export function MaintenanceImages() {
  const [usage, setUsage] = useState<MaintenanceImageUsage>('all')
  const [origin, setOrigin] = useState<MaintenanceImageOrigin>('all')
  const [search, setSearch] = useState('')

  const { data, error, loading, refetch } = useApi(
    () => getMaintenanceImages({ usage: usage !== 'all' ? usage : undefined, origin: origin !== 'all' ? origin : undefined, q: search || undefined }),
    [usage, origin, search],
  )

  const images = data?.items ?? []
  const totalSize = images.reduce((sum, img) => sum + img.size_bytes, 0)
  const unusedCount = images.filter((img) => img.is_unused).length

  return (
    <section className="rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-medium text-[var(--text)]">Images</h3>
          <p className="mt-1 text-xs text-[var(--muted)]">
            {images.length} images · {formatBytes(totalSize)} total · {unusedCount} unused
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* Usage filter */}
          {(['all', 'used', 'unused'] as const).map((v) => (
            <button
              key={v}
              onClick={() => setUsage(v)}
              className={cn('rounded-full border px-2.5 py-1 text-xs transition', usage === v ? 'border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}
            >
              {v}
            </button>
          ))}

          <span className="text-zinc-700">|</span>

          {/* Origin filter */}
          {(['all', 'stack_managed', 'external'] as const).map((v) => (
            <button
              key={v}
              onClick={() => setOrigin(v)}
              className={cn('rounded-full border px-2.5 py-1 text-xs transition', origin === v ? 'border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}
            >
              {v.replace('_', ' ')}
            </button>
          ))}

          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search..."
            className="rounded-full border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(79,209,197,0.35)]"
          />

          <button onClick={refetch} className="rounded-full border border-[var(--panel-border)] px-2.5 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">
            Refresh
          </button>
        </div>
      </div>

      {error && (
        <div className="mt-3 rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
          {error.message}
        </div>
      )}

      <div className="mt-4 space-y-1">
        {loading && images.length === 0 && (
          <>{[1, 2, 3].map((i) => <div key={i} className="h-14 animate-pulse rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />)}</>
        )}

        {!loading && images.length === 0 && (
          <p className="py-6 text-center text-sm text-[var(--muted)]">No images found matching filters.</p>
        )}

        {images.map((img) => (
          <ImageRow key={img.id} image={img} />
        ))}
      </div>
    </section>
  )
}

function ImageRow({ image }: { image: MaintenanceImageItem }) {
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-3 text-xs">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate font-mono text-[var(--text)]">{image.reference || `${image.repository}:${image.tag}`}</span>
          {image.is_dangling && <span className="shrink-0 text-amber-400">dangling</span>}
          {image.is_unused && <span className="shrink-0 text-zinc-500">unused</span>}
        </div>
        <div className="mt-1 flex flex-wrap gap-3 text-[var(--muted)]">
          <span>{formatBytes(image.size_bytes)}</span>
          <span>{image.source.replace('_', ' ')}</span>
          <span>{image.containers_using} container{image.containers_using !== 1 ? 's' : ''}</span>
          {image.stacks_using.length > 0 && (
            <span className="text-[var(--accent)]">{image.stacks_using.map((s) => s.stack_id).join(', ')}</span>
          )}
          <span className="font-mono text-zinc-600">{image.id.slice(0, 12)}</span>
        </div>
      </div>
    </div>
  )
}
