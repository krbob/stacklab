import { useState } from 'react'
import { Link } from 'react-router-dom'
import { getMaintenanceImages } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useDebouncedValue } from '@/hooks/use-debounced-value'
import type { MaintenanceImageItem, MaintenanceImageUsage, MaintenanceImageOrigin } from '@/lib/api-types'
import { cn } from '@/lib/cn'
import { AsyncState } from '@/components/async-state'

function formatBytes(bytes: number): string {
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

export function MaintenanceImages() {
  const [usage, setUsage] = useState<MaintenanceImageUsage>('all')
  const [origin, setOrigin] = useState<MaintenanceImageOrigin>('all')
  const [search, setSearch] = useState('')
  const debouncedSearch = useDebouncedValue(search)

  const { data, error, loading, refetch } = useApi(
    () => getMaintenanceImages({ usage: usage !== 'all' ? usage : undefined, origin: origin !== 'all' ? origin : undefined, q: debouncedSearch || undefined }),
    [usage, origin, debouncedSearch],
  )

  const images = data?.items ?? []
  const totalSize = images.reduce((sum, img) => sum + img.size_bytes, 0)
  const unusedCount = images.filter((img) => img.is_unused).length

  return (
    <section aria-busy={loading} className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-medium text-[var(--text)]">Images</h2>
          {data && (
            <p className="mt-1 text-xs text-[var(--muted)]">
              {images.length} images · {formatBytes(totalSize)} total · {unusedCount} unused
            </p>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* Usage filter */}
          {(['all', 'used', 'unused'] as const).map((v) => (
            <button
              key={v}
              onClick={() => setUsage(v)}
              aria-pressed={usage === v}
              className={cn('rounded-md border px-2.5 py-1 text-xs transition', usage === v ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}
            >
              {v}
            </button>
          ))}

          <span className="text-[var(--muted)]">|</span>

          {/* Origin filter */}
          {(['all', 'stack_managed', 'external'] as const).map((v) => (
            <button
              key={v}
              onClick={() => setOrigin(v)}
              aria-pressed={origin === v}
              className={cn('rounded-md border px-2.5 py-1 text-xs transition', origin === v ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}
            >
              {v.replace('_', ' ')}
            </button>
          ))}

          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search..."
            aria-label="Search images"
            className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]"
          />

          <button onClick={refetch} className="rounded-md border border-[var(--panel-border)] px-2.5 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">
            Refresh
          </button>
        </div>
      </div>

      <div className="mt-4 space-y-2">
        <AsyncState
          loading={loading}
          error={error}
          hasData={data !== null}
          isEmpty={images.length === 0}
          loadingLabel="Loading images..."
          emptyMessage="No images found matching filters."
          onRetry={refetch}
          loadingFallback={
            <>
              {[1, 2, 3].map((i) => <div key={i} className="h-14 animate-pulse rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />)}
            </>
          }
        >
          {images.map((img) => (
            <ImageRow key={img.id} image={img} />
          ))}
        </AsyncState>
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
          {image.is_dangling && <span className="shrink-0 text-[var(--warning)]">dangling</span>}
          {image.is_unused && <span className="shrink-0 text-[var(--muted)]">unused</span>}
        </div>
        <div className="mt-1 flex flex-wrap gap-3 text-[var(--muted)]">
          <span>{formatBytes(image.size_bytes)}</span>
          <span>{image.source.replace('_', ' ')}</span>
          <span>{image.containers_using} container{image.containers_using !== 1 ? 's' : ''}</span>
          {image.stacks_using.length > 0 && (
            <span className="flex flex-wrap gap-2">
              {image.stacks_using.map((s) => (
                <Link key={s.stack_id} to={`/stacks/${s.stack_id}`} className="text-[var(--accent)] hover:underline">
                  {s.stack_id}
                </Link>
              ))}
            </span>
          )}
          <span className="font-mono text-[var(--muted)]">{image.id.slice(0, 12)}</span>
        </div>
      </div>
    </div>
  )
}
