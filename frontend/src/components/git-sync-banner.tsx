import { Link } from 'react-router-dom'
import { useGitSync } from '@/hooks/use-git-sync'

export function GitSyncBanner() {
  const { incoming, checking, error, refresh } = useGitSync()
  const behind = incoming?.behind_count ?? 0
  const ahead = incoming?.ahead_count ?? 0
  return <section aria-label="Git synchronization" aria-busy={checking} className="flex min-w-0 flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4 text-sm">
    <div className="min-w-0 space-y-1" aria-live="polite">
      <p className={error || behind > 0 ? 'text-[var(--warning)]' : 'text-[var(--text)]'}>
        {error ? 'Remote not verified. Local editing is available.' : !incoming ? (checking ? 'Checking remote…' : 'Remote not checked.') : !incoming.available || incoming.reason ? 'Remote synchronization is unavailable for this checkout.' : behind > 0 ? `${behind} incoming ${behind === 1 ? 'commit' : 'commits'}${ahead > 0 ? ` · ${ahead} local commits — reconciliation needed` : ' — review before editing'}` : ahead > 0 ? `${ahead} local ${ahead === 1 ? 'commit' : 'commits'} waiting to be pushed` : 'No incoming commits at the last check.'}
      </p>
      {incoming && <p className="wrap-anywhere text-xs text-[var(--muted)]">{incoming.branch} · {incoming.local_changes ?? 0} uncommitted files · Last checked {new Date(incoming.checked_at).toLocaleString()}{checking ? ' · Checking again…' : ''}</p>}
      {error && <p className="wrap-anywhere text-xs text-[var(--muted)]">{error}</p>}
      {behind > 0 && (incoming?.local_changes ?? 0) > 0 && <p className="text-xs text-[var(--muted)]">Preserve local edits with Commit in Changes, then reconcile them with the remote version.</p>}
    </div>
    <div className="flex flex-wrap gap-3">
      <button disabled={checking} onClick={() => { void refresh() }} className="text-[var(--accent)] hover:underline disabled:opacity-40">Check remote</button>
      <Link to="/config/remote" className="text-[var(--accent)] hover:underline">Remote updates →</Link>
    </div>
  </section>
}
