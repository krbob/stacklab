import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { File, FileQuestion, FileWarning, Folder, FolderKanban, GitBranch, Plus } from 'lucide-react'
import { getConfigTree, getConfigFile, saveConfigFile, getGitWorkspaceStatus, getGitWorkspaceDiff } from '@/lib/api-client'
import type { ConfigTreeEntry, ConfigFileResponse, GitStatusItem, GitDiffResponse } from '@/lib/api-types'
import { YamlEditor } from '@/components/yaml-editor'
import { DiffView } from '@/components/diff-view'
import { GitCommitBar } from '@/components/git-commit-bar'
import { BlockedFileCard } from '@/components/blocked-file-card'
import { cn } from '@/lib/cn'

type Mode = 'files' | 'changes'

const entryIcons: Record<string, typeof File> = {
  directory: Folder,
  text_file: File,
  binary_file: FileWarning,
  unknown_file: FileQuestion,
}

const statusPrefixes: Record<string, { letter: string; color: string }> = {
  modified: { letter: 'M', color: 'text-emerald-400' },
  added: { letter: 'A', color: 'text-sky-400' },
  deleted: { letter: 'D', color: 'text-red-400' },
  renamed: { letter: 'R', color: 'text-violet-400' },
  untracked: { letter: 'U', color: 'text-amber-400' },
  conflicted: { letter: 'C', color: 'text-red-400' },
}

export function ConfigPage() {
  const [mode, setMode] = useState<Mode>('files')

  // --- Files mode state ---
  const [treePath, setTreePath] = useState('')
  const [treeEntries, setTreeEntries] = useState<ConfigTreeEntry[]>([])
  const [parentPath, setParentPath] = useState<string | null>(null)
  const [treeLoading, setTreeLoading] = useState(true)
  const [treeError, setTreeError] = useState<string | null>(null)

  const [selectedFile, setSelectedFile] = useState<ConfigFileResponse | null>(null)
  const [fileLoading, setFileLoading] = useState(false)
  const [fileError, setFileError] = useState<string | null>(null)

  const [editContent, setEditContent] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveMessage, setSaveMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  const [creatingFile, setCreatingFile] = useState(false)
  const [newFileName, setNewFileName] = useState('')

  const isDirty = selectedFile?.type === 'text_file' && editContent !== (selectedFile.content ?? '')

  // --- Changes mode state ---
  const [gitItems, setGitItems] = useState<GitStatusItem[]>([])
  const [gitAvailable, setGitAvailable] = useState(true)
  const [gitBranch, setGitBranch] = useState<string | null>(null)
  const [gitAhead, setGitAhead] = useState(0)
  const [gitClean, setGitClean] = useState(true)
  const [gitLoading, setGitLoading] = useState(false)
  const [gitError, setGitError] = useState<string | null>(null)
  const [gitReason, setGitReason] = useState<string | null>(null)

  const [gitHasUpstream, setGitHasUpstream] = useState(false)

  const [selectedDiff, setSelectedDiff] = useState<GitDiffResponse | null>(null)
  const [diffLoading, setDiffLoading] = useState(false)
  const [diffError, setDiffError] = useState<string | null>(null)
  const [selectedChangePath, setSelectedChangePath] = useState<string | null>(null)
  const [selectedGitPaths, setSelectedGitPaths] = useState<Set<string>>(new Set())
  const selectedChangePathRef = useRef<string | null>(null)

  useEffect(() => {
    selectedChangePathRef.current = selectedChangePath
  }, [selectedChangePath])

  // --- Files mode logic ---

  const loadTree = useCallback(async (path: string) => {
    setTreeLoading(true)
    setTreeError(null)
    try {
      const result = await getConfigTree(path || undefined)
      setTreeEntries(result.items)
      setParentPath(result.parent_path)
      setTreePath(result.current_path)
    } catch (err) {
      setTreeError(err instanceof Error ? err.message : 'Failed to load tree')
    } finally {
      setTreeLoading(false)
    }
  }, [])

  useEffect(() => { loadTree('') }, [loadTree])

  const openFile = useCallback(async (path: string) => {
    setFileLoading(true)
    setFileError(null)
    setSaveMessage(null)
    setCreatingFile(false)
    try {
      const file = await getConfigFile(path)
      setSelectedFile(file)
      setEditContent(file.content ?? '')
    } catch (err) {
      setFileError(err instanceof Error ? err.message : 'Failed to load file')
      setSelectedFile(null)
    } finally {
      setFileLoading(false)
    }
  }, [])

  const navigateDir = useCallback((path: string) => {
    loadTree(path)
    setSelectedFile(null)
    setFileError(null)
    setSaveMessage(null)
    setCreatingFile(false)
  }, [loadTree])

  const handleSave = useCallback(async () => {
    if (!selectedFile) return
    setSaving(true)
    setSaveMessage(null)
    try {
      await saveConfigFile(selectedFile.path, editContent)
      setSaveMessage({ type: 'success', text: 'Saved' })
      const updated = await getConfigFile(selectedFile.path)
      setSelectedFile(updated)
      setEditContent(updated.content ?? '')
    } catch (err) {
      setSaveMessage({ type: 'error', text: err instanceof Error ? err.message : 'Save failed' })
    } finally {
      setSaving(false)
    }
  }, [selectedFile, editContent])

  const handleCreateFile = useCallback(async () => {
    if (!newFileName.trim()) return
    const path = treePath ? `${treePath}/${newFileName.trim()}` : newFileName.trim()
    setSaving(true)
    try {
      await saveConfigFile(path, '', false)
      setCreatingFile(false)
      setNewFileName('')
      await loadTree(treePath)
      await openFile(path)
    } catch (err) {
      setSaveMessage({ type: 'error', text: err instanceof Error ? err.message : 'Create failed' })
    } finally {
      setSaving(false)
    }
  }, [treePath, newFileName, loadTree, openFile])

  const handleDiscard = useCallback(() => {
    if (selectedFile) setEditContent(selectedFile.content ?? '')
  }, [selectedFile])

  // --- Changes mode logic ---

  const loadGitStatus = useCallback(async () => {
    setGitLoading(true)
    setGitError(null)
    try {
      const result = await getGitWorkspaceStatus()
      setGitAvailable(result.available)
      setGitItems(result.items ?? [])
      setGitBranch(result.branch ?? null)
      setGitAhead(result.ahead_count ?? 0)
      setGitHasUpstream(result.has_upstream ?? false)
      setGitClean(result.clean ?? true)
      setGitReason(result.reason ?? null)
      setSelectedGitPaths(new Set())
      if (selectedChangePathRef.current && !(result.items ?? []).some((item) => item.path === selectedChangePathRef.current)) {
        setSelectedDiff(null)
        setSelectedChangePath(null)
        setDiffError(null)
      }
    } catch (err) {
      setGitError(err instanceof Error ? err.message : 'Failed to load Git status')
    } finally {
      setGitLoading(false)
    }
  }, [])

  useEffect(() => {
    if (mode === 'changes') loadGitStatus()
  }, [mode, loadGitStatus])

  const openDiff = useCallback(async (path: string) => {
    setSelectedChangePath(path)
    setDiffLoading(true)
    setDiffError(null)
    setSelectedDiff(null)
    try {
      const result = await getGitWorkspaceDiff(path)
      setSelectedDiff(result)
    } catch (err) {
      setDiffError(err instanceof Error ? err.message : 'Failed to load diff')
    } finally {
      setDiffLoading(false)
    }
  }, [])

  const groupedGitItems = useMemo(() => {
    const groups = new Map<string, GitStatusItem[]>()
    for (const item of gitItems) {
      const key = item.stack_id ?? '__other__'
      if (!groups.has(key)) groups.set(key, [])
      groups.get(key)!.push(item)
    }
    return groups
  }, [gitItems])

  const toggleGitPath = useCallback((path: string) => {
    setSelectedGitPaths((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }, [])

  const toggleGroupPaths = useCallback((groupKey: string) => {
    const items = groupedGitItems.get(groupKey)
    if (!items) return
    // Only select committable files — skip blocked
    const committable = items.filter((i) => i.commit_allowed).map((i) => i.path)
    setSelectedGitPaths((prev) => {
      const allSelected = committable.every((p) => prev.has(p))
      const next = new Set(prev)
      if (allSelected) {
        committable.forEach((p) => next.delete(p))
      } else {
        committable.forEach((p) => next.add(p))
      }
      return next
    })
  }, [groupedGitItems])

  const handleModeSwitch = useCallback((newMode: Mode) => {
    setMode(newMode)
    if (newMode === 'files') {
      setSelectedDiff(null)
      setSelectedChangePath(null)
      setDiffError(null)
    } else {
      setSelectedFile(null)
      setFileError(null)
      setSaveMessage(null)
    }
  }, [])

  return (
    <div className="flex gap-4" style={{ minHeight: 'calc(100vh - 120px)' }}>
      {/* Left panel */}
      <div className="hidden w-64 shrink-0 flex-col rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4 shadow-[var(--shadow)] lg:flex">
        <div className="mb-3 text-xs uppercase tracking-wider text-[var(--accent)]">Config workspace</div>

        {/* Mode toggle */}
        <div className="mb-3 flex gap-1">
          <button
            onClick={() => handleModeSwitch('files')}
            className={cn(
              'flex-1 rounded-full border px-3 py-1.5 text-xs transition',
              mode === 'files'
                ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            Files
          </button>
          <button
            onClick={() => handleModeSwitch('changes')}
            disabled={!gitAvailable && !gitLoading}
            title={!gitAvailable ? (gitReason ?? 'Git not available') : undefined}
            className={cn(
              'flex-1 rounded-full border px-3 py-1.5 text-xs transition disabled:opacity-40',
              mode === 'changes'
                ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            Changes{gitItems.length > 0 && ` (${gitItems.length})`}
          </button>
        </div>

        {/* Files mode tree */}
        {mode === 'files' && (
          <>
            {treeLoading && (
              <div className="space-y-2">
                {[1, 2, 3].map((i) => (
                  <div key={i} className="h-6 animate-pulse rounded bg-[rgba(255,255,255,0.05)]" />
                ))}
              </div>
            )}
            {treeError && <p className="text-xs text-red-400">{treeError}</p>}
            {!treeLoading && !treeError && (
              <nav className="flex-1 space-y-0.5 overflow-y-auto">
                {parentPath !== null && (
                  <button onClick={() => navigateDir(parentPath)} className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-[var(--muted)] transition hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]">
                    <Folder className="size-3.5" /><span>.. (up)</span>
                  </button>
                )}
                {treeEntries.map((entry) => {
                  const Icon = entryIcons[entry.type] ?? File
                  const isDir = entry.type === 'directory'
                  const isSelected = selectedFile?.path === entry.path
                  return (
                    <button key={entry.path} onClick={() => isDir ? navigateDir(entry.path) : openFile(entry.path)} className={cn('flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs transition', isSelected ? 'bg-[rgba(34,197,94,0.14)] text-[var(--text)]' : 'text-[var(--muted)] hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]')}>
                      {entry.stack_id && isDir && treePath === '' ? <FolderKanban className="size-3.5 text-[var(--accent)]" /> : <Icon className="size-3.5" />}
                      <span className="truncate">{entry.name}</span>
                    </button>
                  )
                })}
                {treeEntries.length === 0 && <p className="px-2 py-4 text-xs text-[var(--muted)]">Empty directory</p>}
                {!creatingFile && (
                  <button onClick={() => { setCreatingFile(true); setNewFileName('') }} className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-[var(--muted)] transition hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]">
                    <Plus className="size-3.5" /><span>New file</span>
                  </button>
                )}
                {creatingFile && (
                  <div className="flex items-center gap-1 px-2 py-1">
                    <input type="text" value={newFileName} onChange={(e) => setNewFileName(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') handleCreateFile(); if (e.key === 'Escape') setCreatingFile(false) }} placeholder="filename" autoFocus className="w-full rounded border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]" />
                  </div>
                )}
              </nav>
            )}
          </>
        )}

        {/* Changes mode list */}
        {mode === 'changes' && (
          <>
            {gitLoading && (
              <div className="space-y-2">
                {[1, 2, 3].map((i) => <div key={i} className="h-6 animate-pulse rounded bg-[rgba(255,255,255,0.05)]" />)}
              </div>
            )}
            {gitError && <p className="text-xs text-red-400">{gitError}</p>}
            {!gitLoading && !gitError && gitAvailable && (
              <div className="flex-1 space-y-2 overflow-y-auto">
                {gitBranch && (
                  <div className="flex items-center gap-2 px-2 py-1 text-xs text-[var(--muted)]">
                    <GitBranch className="size-3" />
                    <span>{gitBranch}</span>
                    {gitAhead > 0 && <span className="text-amber-400">+{gitAhead}</span>}
                  </div>
                )}
                {gitClean && <p className="px-2 py-4 text-xs text-[var(--muted)]">Working tree clean</p>}
                {Array.from(groupedGitItems.entries()).map(([groupKey, items]) => {
                  const committablePaths = items.filter((i) => i.commit_allowed).map((i) => i.path)
                  const allGroupSelected = committablePaths.length > 0 && committablePaths.every((p) => selectedGitPaths.has(p))
                  return (
                    <div key={groupKey}>
                      <button
                        onClick={() => toggleGroupPaths(groupKey)}
                        className="flex w-full items-center gap-2 px-2 py-1 text-xs font-medium text-[var(--text)] hover:bg-[rgba(255,255,255,0.03)]"
                      >
                        <input type="checkbox" checked={allGroupSelected} readOnly className="rounded pointer-events-none" />
                        {groupKey === '__other__' ? 'Other' : groupKey}
                        <span className="text-[var(--muted)]">({items.length})</span>
                      </button>
                      {items.map((item) => {
                        const prefix = statusPrefixes[item.status]
                        const fileName = item.path.split('/').pop() ?? item.path
                        const isDiffSelected = selectedChangePath === item.path
                        const isChecked = selectedGitPaths.has(item.path)
                        const isBlocked = !item.commit_allowed
                        return (
                          <div key={item.path} className={cn('flex items-center gap-1 rounded-lg px-2 py-1 text-xs transition', isDiffSelected ? 'bg-[rgba(34,197,94,0.14)] text-[var(--text)]' : 'text-[var(--muted)] hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]')}>
                            <input type="checkbox" checked={isChecked} onChange={() => toggleGitPath(item.path)} disabled={isBlocked} title={isBlocked ? 'File cannot be committed — permissions blocked' : undefined} className="rounded shrink-0 disabled:opacity-30" />
                            <button onClick={() => openDiff(item.path)} className="flex min-w-0 flex-1 items-center gap-1">
                              {prefix && <span className={cn('w-3 shrink-0 font-mono font-bold', prefix.color)}>{prefix.letter}</span>}
                              <span className={cn('truncate', isBlocked && 'opacity-50')}>{fileName}</span>
                              {isBlocked && <span className="shrink-0 text-amber-400" title="Access blocked">⚠</span>}
                            </button>
                          </div>
                        )
                      })}
                    </div>
                  )
                })}
                <button onClick={loadGitStatus} className="mt-2 flex w-full items-center justify-center rounded-lg px-2 py-1.5 text-xs text-[var(--muted)] transition hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]">
                  Refresh
                </button>

                {/* Commit bar */}
                {!gitClean && (
                  <GitCommitBar
                    selectedPaths={selectedGitPaths}
                    hasUpstream={gitHasUpstream}
                    aheadCount={gitAhead}
                    onCommitted={loadGitStatus}
                    onPushed={loadGitStatus}
                  />
                )}
              </div>
            )}
            {!gitLoading && !gitAvailable && (
              <p className="px-2 py-4 text-xs text-[var(--muted)]">
                {gitReason === 'not_a_git_repository' ? 'Not a Git repository. Initialize Git to track changes.' : gitReason ?? 'Git not available'}
              </p>
            )}
          </>
        )}
      </div>

      {/* Right panel */}
      <div className="flex min-w-0 flex-1 flex-col rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        {/* Files mode - editor */}
        {mode === 'files' && (
          <>
            {!selectedFile && !fileLoading && !fileError && (
              <div className="flex flex-1 items-center justify-center">
                <div className="text-center">
                  <p className="text-lg text-[var(--text)]">Select a file to view or edit</p>
                  <p className="mt-1 text-sm text-[var(--muted)]">Browse the config workspace in the tree on the left.</p>
                </div>
              </div>
            )}
            {fileLoading && (
              <div className="flex flex-1 items-center justify-center"><div className="text-sm text-[var(--muted)]">Loading file...</div></div>
            )}
            {fileError && (
              <div className="rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">{fileError}</div>
            )}
            {selectedFile && (
              <>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div>
                    <div className="flex items-center gap-2">
                      <h3 className="text-lg font-medium text-[var(--text)]">{selectedFile.name}</h3>
                      <span className="rounded-full border border-[var(--panel-border)] px-2 py-0.5 text-xs text-[var(--muted)]">{selectedFile.type.replace('_', ' ')}</span>
                    </div>
                    <div className="mt-1 flex items-center gap-3 text-xs text-[var(--muted)]">
                      <span>{selectedFile.path}</span>
                      {selectedFile.stack_id && <Link to={`/stacks/${selectedFile.stack_id}`} className="text-[var(--accent)] hover:underline">{selectedFile.stack_id}</Link>}
                      <span>{new Date(selectedFile.modified_at).toLocaleString()}</span>
                    </div>
                  </div>
                  {selectedFile.type === 'text_file' && selectedFile.writable && (
                    <div className="flex items-center gap-2">
                      {isDirty && <span className="text-xs text-amber-400">Unsaved changes</span>}
                      {isDirty && <button onClick={handleDiscard} className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">Discard</button>}
                      <button data-testid="config-save" onClick={handleSave} disabled={saving || !isDirty} className="rounded-full border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40">
                        {saving ? 'Saving...' : 'Save'}
                      </button>
                    </div>
                  )}
                </div>
                {saveMessage && <div className={cn('mt-2 text-xs', saveMessage.type === 'success' ? 'text-emerald-400' : 'text-red-400')}>{saveMessage.text}</div>}
                <div className="mt-3 flex-1" style={{ minHeight: '400px' }}>
                  {selectedFile.blocked_reason ? (
                    <BlockedFileCard blockedReason={selectedFile.blocked_reason} permissions={selectedFile.permissions} />
                  ) : selectedFile.type === 'text_file' ? (
                    <YamlEditor value={editContent} onChange={setEditContent} readOnly={!selectedFile.writable} />
                  ) : (
                    <div className="flex h-full items-center justify-center rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.2)]">
                      <div className="text-center">
                        <FileWarning className="mx-auto size-8 text-[var(--muted)]" />
                        <p className="mt-2 text-sm text-[var(--text)]">{selectedFile.type === 'binary_file' ? 'Binary file' : 'Unknown file type'}</p>
                        <p className="mt-1 text-xs text-[var(--muted)]">This file cannot be edited in the browser. Size: {(selectedFile.size_bytes / 1024).toFixed(1)} KB</p>
                      </div>
                    </div>
                  )}
                </div>
              </>
            )}
          </>
        )}

        {/* Changes mode - diff */}
        {mode === 'changes' && (
          <>
            {!selectedDiff && !diffLoading && !diffError && (
              <div className="flex flex-1 items-center justify-center">
                <div className="text-center">
                  <p className="text-lg text-[var(--text)]">Select a changed file to view diff</p>
                  <p className="mt-1 text-sm text-[var(--muted)]">{gitClean ? 'No local changes detected.' : 'Click a file in the Changes list.'}</p>
                </div>
              </div>
            )}
            {diffLoading && <div className="flex flex-1 items-center justify-center"><div className="text-sm text-[var(--muted)]">Loading diff...</div></div>}
            {diffError && <div className="rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">{diffError}</div>}
            {selectedDiff && (
              <>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div>
                    <div className="flex items-center gap-2">
                      <h3 className="text-lg font-medium text-[var(--text)]">{selectedDiff.path.split('/').pop()}</h3>
                      {statusPrefixes[selectedDiff.status] && (
                        <span className={cn('rounded-full border border-[var(--panel-border)] px-2 py-0.5 text-xs', statusPrefixes[selectedDiff.status].color)}>
                          {selectedDiff.status}
                        </span>
                      )}
                    </div>
                    <div className="mt-1 flex items-center gap-3 text-xs text-[var(--muted)]">
                      <span>{selectedDiff.path}</span>
                      {selectedDiff.stack_id && <Link to={`/stacks/${selectedDiff.stack_id}`} className="text-[var(--accent)] hover:underline">{selectedDiff.stack_id}</Link>}
                      <span className="text-zinc-600">{selectedDiff.scope}</span>
                    </div>
                  </div>
                  {selectedDiff.status !== 'deleted' && selectedDiff.scope === 'config' && !selectedDiff.blocked_reason && (
                    <button
                      onClick={() => {
                        const configPath = selectedDiff.path.replace(/^config\//, '')
                        handleModeSwitch('files')
                        openFile(configPath)
                      }}
                      className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
                    >
                      Open in editor
                    </button>
                  )}
                  {selectedDiff.status !== 'deleted' && selectedDiff.scope === 'stacks' && selectedDiff.stack_id && (
                    <Link
                      to={`/stacks/${selectedDiff.stack_id}/editor`}
                      className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
                    >
                      Open stack editor
                    </Link>
                  )}
                </div>
                <div className="mt-3 flex-1" style={{ minHeight: '400px' }}>
                  {selectedDiff.blocked_reason ? (
                    <BlockedFileCard blockedReason={selectedDiff.blocked_reason} permissions={selectedDiff.permissions} />
                  ) : selectedDiff.is_binary ? (
                    <div className="flex h-full items-center justify-center rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.2)]">
                      <div className="text-center">
                        <FileWarning className="mx-auto size-8 text-[var(--muted)]" />
                        <p className="mt-2 text-sm text-[var(--text)]">Binary file changed</p>
                        <p className="mt-1 text-xs text-[var(--muted)]">Diff not available for binary files.</p>
                      </div>
                    </div>
                  ) : selectedDiff.diff ? (
                    <DiffView diff={selectedDiff.diff} truncated={selectedDiff.truncated} />
                  ) : (
                    <div className="flex h-full items-center justify-center rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.2)]">
                      <p className="text-sm text-[var(--muted)]">No diff content available.</p>
                    </div>
                  )}
                </div>
              </>
            )}
          </>
        )}
      </div>
    </div>
  )
}
