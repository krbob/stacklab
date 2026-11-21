import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { applyGitReconcile, discardGitReconcile, getGitReconcile, getGitReconcileFile, prepareGitReconcile, resolveGitReconcile } from '@/lib/api-client'
import type { GitIncomingResponse, GitPullReceipt, GitReconcileFileResponse, GitReconcileResponse, GitReconcileVersion, GitResolveRequest } from '@/lib/api-types'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DiffView } from '@/components/diff-view'
import { UnsavedChangesGuard } from '@/components/unsaved-changes-guard'
import { usePendingAction } from '@/hooks/use-pending-action'

const button = 'rounded-md border border-[var(--panel-border)] px-3 py-2 text-sm text-[var(--text)] hover:bg-[rgba(245,165,36,0.08)] disabled:opacity-40'
const requestFor = (draft: GitReconcileResponse) => ({ id: draft.id, revision: draft.revision })

export function GitReconcilePanel({ incoming, disabled, onApplied }: { incoming: GitIncomingResponse | null; disabled: boolean; onApplied: (receipt: GitPullReceipt) => Promise<void> }) {
  const [draft, setDraft] = useState<GitReconcileResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const loadingRef = useRef(false)
  const [operation, setOperation] = useState<string | null>(null)
  const operationRef = useRef(false)
  const [file, setFile] = useState<GitReconcileFileResponse | null>(null)
  const [selectedPath, setSelectedPath] = useState<string | null>(null)
  const [fileLoading, setFileLoading] = useState(false)
  const requestID = useRef(0)
  const [editor, setEditor] = useState('')
  const [dialog, setDialog] = useState<'apply' | 'discard' | null>(null)
  const initialEditor = file?.choice === 'edit' ? file.content : (file?.local.text ?? file?.remote.text ?? '')
  const dirty = file !== null && file.editable && editor !== initialEditor
  const pending = usePendingAction(dirty)
  const busy = disabled || loading || operation !== null
  const stale = !!draft && !!incoming && draft.expected_state !== incoming.expected_state

  const load = useCallback(async () => {
    if (loadingRef.current) return
    loadingRef.current = true
    setLoading(true)
    setError(null)
    requestID.current++
    setFile(null)
    setSelectedPath(null)
    setFileLoading(false)
    try { setDraft((await getGitReconcile()).draft) }
    catch (err) { setError(err instanceof Error ? err.message : 'Cannot load the reconciliation draft.') }
    finally { loadingRef.current = false; setLoading(false) }
  }, [])
  useEffect(() => { void load() }, [load])

  const openFile = async (path: string) => {
    if (!draft) return
    const id = ++requestID.current
    setSelectedPath(path)
    setFile(null)
    setFileLoading(true)
    setError(null)
    try {
      const result = await getGitReconcileFile(requestFor(draft), path)
      if (id !== requestID.current) return
      setFile(result)
      setEditor(result.choice === 'edit' ? result.content : (result.local.text ?? result.remote.text ?? ''))
    } catch (err) { if (id === requestID.current) setError(err instanceof Error ? err.message : 'Cannot load this file.') }
    finally { if (id === requestID.current) setFileLoading(false) }
  }

  const run = async (kind: string, action: () => Promise<void>) => {
    if (operationRef.current) return
    operationRef.current = true
    setOperation(kind)
    setError(null)
    try { await action() }
    catch (err) { setError(err instanceof Error ? err.message : 'Reconciliation failed. Reload the draft before retrying.') }
    finally { operationRef.current = false; setOperation(null) }
  }

  const resolve = (choice: GitResolveRequest['choice']) => {
    if (!draft || !file) return
    void run('resolve', async () => {
      const result = await resolveGitReconcile({ ...requestFor(draft), path: file.path, choice, content: choice === 'edit' ? editor : undefined })
      requestID.current++
      setDraft(result)
      setFile(null)
      setSelectedPath(null)
    })
  }

  const paths = [...new Set([...(draft?.conflicts.map((conflict) => conflict.path) ?? []), ...(draft?.files.map((item) => item.path) ?? [])])]
  const conflict = draft?.conflicts.find((item) => item.path === file?.path)
  const unresolved = draft?.conflicts.filter((item) => !item.resolved).length ?? 0

  return <section aria-label="Reconcile local and remote changes" className="min-w-0 space-y-4 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4 sm:p-5" aria-busy={loading || !!operation}>
    <UnsavedChangesGuard when={dirty} />
    <div className="flex flex-wrap items-center justify-between gap-3">
      <h2 className="text-lg font-medium">Reconcile local and remote changes</h2>
      <button className={button} disabled={busy} onClick={() => pending.requestAction(() => { void load() })}>Reload draft</button>
    </div>
    {loading && <p className="text-sm text-[var(--muted)]">Loading saved draft…</p>}
    {error && <p role="alert" className="wrap-anywhere text-sm text-[var(--danger)]">{error}</p>}
    {!loading && !draft && <>
      <p className="text-sm text-[var(--muted)]">Preserve uncommitted edits with <Link to="/config?mode=changes" className="text-[var(--accent)] hover:underline">Commit in Changes</Link> first. Then prepare a merge of local and remote commits in a private draft.</p>
      {incoming?.reconcile_blocked_reason && <p className="text-sm text-[var(--warning)]">Preparation is blocked: {incoming.reconcile_blocked_reason === 'local_changes' ? 'commit or resolve all local edits, including files outside the managed folders, then check remote again.' : incoming.reconcile_blocked_reason.replaceAll('_', ' ')}</p>}
      <button className={button} disabled={busy || !incoming?.can_reconcile || !!error} onClick={() => { if (incoming) void run('prepare', async () => { setDraft(await prepareGitReconcile(incoming.expected_state)) }) }}>{operation === 'prepare' ? 'Preparing draft…' : 'Prepare reconciliation'}</button>
      <p className="text-xs text-[var(--muted)]">Preparation keeps the current configuration and containers unchanged.</p>
    </>}
    {draft && <>
      <p className="wrap-anywhere text-sm text-[var(--muted)]">{draft.branch} · Local {draft.local_commit.slice(0, 8)} + remote {draft.remote_commit.slice(0, 8)} · Saved draft revision {draft.revision}</p>
      {draft.applied ? <p className="text-sm text-[var(--ok)]">This merge was applied. Review the affected stacks below and push the local merge commit from Changes when ready.</p> : <>
        {stale && <p className="text-sm text-[var(--warning)]">The checked repository state changed. Preserve any new local edits, then discard this draft and prepare a new one.</p>}
        <p className="text-sm">{unresolved > 0 ? `${unresolved} unresolved ${unresolved === 1 ? 'conflict' : 'conflicts'}. Choose a result for each file.` : 'All conflicts resolved. Review the resulting file changes before applying.'}</p>
        <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(12rem,1fr)_minmax(0,3fr)]">
          <div className="min-w-0 space-y-2" aria-label="Merge files">
            {paths.map((path) => {
              const item = draft.conflicts.find((entry) => entry.path === path)
              return <button key={path} className={`${button} w-full space-y-1 text-left ${path === selectedPath ? 'bg-[rgba(245,165,36,0.12)]' : ''}`} disabled={busy || dialog !== null} onClick={() => pending.requestAction(() => { void openFile(path) })}>
                <span className="block wrap-anywhere font-mono text-xs">{path}</span>
                <span className={`block text-xs ${item && !item.resolved ? 'text-[var(--warning)]' : 'text-[var(--muted)]'}`}>{item ? (item.resolved ? 'Resolved — review result' : 'Conflict') : 'Merged automatically'}</span>
              </button>
            })}
            {paths.length === 0 && <p className="text-sm text-[var(--muted)]">The resulting files match the local version. Applying still records both histories in a merge commit.</p>}
          </div>
          <div className="min-w-0 space-y-4" aria-busy={fileLoading}>
            {fileLoading && <p className="text-sm">Loading merge file…</p>}
            {!selectedPath && <p className="text-sm text-[var(--muted)]">Select a file to inspect the versions or the resulting diff.</p>}
            {file && <>
              <h3 className="wrap-anywhere font-mono text-sm">{file.path}</h3>
              {conflict && <>
                <div className="grid min-w-0 gap-3 xl:grid-cols-3">
                  <Version title="Base" value={file.base} />
                  <Version title="Local" value={file.local} />
                  <Version title="Remote" value={file.remote} />
                </div>
                <div className="flex flex-wrap gap-2">
                  <button className={button} disabled={busy || stale} onClick={() => pending.requestAction(() => resolve('local'))}>{file.local.exists ? 'Use local version' : 'Use local deletion'}</button>
                  <button className={button} disabled={busy || stale} onClick={() => pending.requestAction(() => resolve('remote'))}>{file.remote.exists ? 'Use remote version' : 'Use remote deletion'}</button>
                  <button className={button} disabled={busy || stale} onClick={() => pending.requestAction(() => resolve('delete'))}>Delete in merged result</button>
                </div>
                {file.editable && <div className="space-y-2">
                  <label className="block space-y-2 text-sm"><span>Edited result</span><textarea aria-label="Edited merge result" value={editor} onChange={(e) => setEditor(e.target.value)} disabled={busy || stale} spellCheck={false} className="min-h-48 w-full rounded-md border border-[var(--panel-border)] bg-[var(--bg)] p-3 font-mono text-xs" /></label>
                  <button className={button} disabled={busy || stale} onClick={() => resolve('edit')}>Save edited result</button>
                  <p className="text-xs text-[var(--muted)]">Saved only in this draft. The result keeps the local file mode when present, otherwise the remote mode.</p>
                </div>}
                {!file.editable && <p className="text-xs text-[var(--muted)]">Binary, large, non-regular and unmanaged files support version selection. Text editing is available for ordinary managed files up to 256 KiB.</p>}
              </>}
              {(!conflict || conflict.resolved) && <div className="space-y-2"><h4 className="text-sm font-medium">Result compared with local configuration</h4>{file.diff ? <DiffView diff={file.diff} truncated={file.truncated} /> : <p className="text-sm text-[var(--muted)]">No file content change from the local commit.</p>}</div>}
            </>}
          </div>
        </div>
        <p className="text-sm text-[var(--muted)]">Apply creates a local merge commit and updates the checkout. Applications watching mounted files may react immediately. Push and container reload remain separate actions.</p>
      </>}
      <div className="flex flex-wrap gap-2 border-t border-[var(--panel-border)] pt-4">
        {!draft.applied && <button className={button} disabled={busy || stale || !draft.ready || dirty || fileLoading} onClick={() => { setError(null); setDialog('apply') }}>Review and apply merge</button>}
        <button className={button} disabled={busy} onClick={() => { setError(null); setDialog('discard') }}>{draft.applied ? 'Clear completed draft' : 'Discard draft'}</button>
      </div>
    </>}
    {dialog && draft && <ConfirmDialog title={dialog === 'apply' ? 'Apply merged configuration?' : 'Discard reconciliation draft?'} message={dialog === 'apply' ? `Merge local ${draft.local_commit.slice(0,8)} with remote ${draft.remote_commit.slice(0,8)} and replace the listed checkout files with the reviewed result. Mounted configuration may react immediately. Existing commits remain in history; push and container reload are separate.` : 'Delete this private draft and its saved resolutions. The managed checkout, commits and running containers remain unchanged.'} items={dialog === 'apply' ? draft.files.map((item) => item.path) : []} confirmLabel={dialog === 'apply' ? 'Apply merged configuration' : 'Discard draft'} confirming={!!operation} error={error} onCancel={() => setDialog(null)} onConfirm={() => { void run(dialog, async () => {
      if (dialog === 'apply') {
        const result = await applyGitReconcile(requestFor(draft))
        setFile(null); setSelectedPath(null); setDraft({ ...draft, applied: true }); setDialog(null)
        await onApplied(result.receipt)
      } else {
        await discardGitReconcile(requestFor(draft))
        requestID.current++; setDraft(null); setFile(null); setSelectedPath(null); setFileLoading(false); setDialog(null)
      }
    }) }} />}
    {pending.hasPendingAction && <ConfirmDialog title="Discard unsaved resolution?" message="The edited result has not been saved to the draft. Continue without saving it?" confirmLabel="Discard edited text" onCancel={pending.cancelPendingAction} onConfirm={pending.confirmPendingAction} />}
  </section>
}

function Version({ title, value }: { title: string; value: GitReconcileVersion }) {
  return <details open className="min-w-0 rounded-md border border-[var(--panel-border)] p-3">
    <summary className="cursor-pointer text-sm font-medium">{title}{value.exists ? ` · ${value.size} B · ${value.mode}` : ' · absent'}</summary>
    <pre className="mt-3 max-h-64 overflow-auto whitespace-pre-wrap wrap-anywhere font-mono text-xs text-[var(--muted)]">{value.exists ? (value.text ?? 'Preview unavailable for this binary, large or non-regular file.') : 'This version has no file at this path.'}</pre>
  </details>
}
