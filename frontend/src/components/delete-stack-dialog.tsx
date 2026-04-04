import { useCallback, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { deleteStack } from '@/lib/api-client'
import { ProgressPanel } from '@/components/progress-panel'

interface DeleteStackDialogProps {
  stackId: string
  stackName: string
  onClose: () => void
}

export function DeleteStackDialog({ stackId, stackName, onClose }: DeleteStackDialogProps) {
  const navigate = useNavigate()
  const [removeRuntime, setRemoveRuntime] = useState(true)
  const [removeDefinition, setRemoveDefinition] = useState(false)
  const [removeConfig, setRemoveConfig] = useState(false)
  const [removeData, setRemoveData] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [jobId, setJobId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const handleDelete = useCallback(async () => {
    setDeleting(true)
    setError(null)
    try {
      const result = await deleteStack(stackId, {
        remove_runtime: removeRuntime,
        remove_definition: removeDefinition,
        remove_config: removeConfig,
        remove_data: removeData,
      })
      setJobId(result.job.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Remove failed')
      setDeleting(false)
    }
  }, [stackId, removeRuntime, removeDefinition, removeConfig, removeData])

  const handleJobDone = useCallback((state: string) => {
    setDeleting(false)
    if (state === 'succeeded') {
      if (removeDefinition) {
        navigate('/stacks')
      } else {
        onClose()
      }
    }
  }, [navigate, removeDefinition, onClose])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="w-full max-w-lg rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-6 shadow-[var(--shadow)]"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="text-lg font-semibold text-[var(--text)]">Remove stack "{stackName}"?</h3>

        <div className="mt-4 space-y-3">
          <label className="flex items-center gap-2 text-sm text-[var(--text)]">
            <input type="checkbox" checked={removeRuntime} onChange={(e) => setRemoveRuntime(e.target.checked)} disabled={deleting} className="rounded" />
            Stop and remove containers (runtime)
          </label>
          <label className="flex items-center gap-2 text-sm text-[var(--text)]">
            <input type="checkbox" checked={removeDefinition} onChange={(e) => setRemoveDefinition(e.target.checked)} disabled={deleting} className="rounded" />
            Delete stack definition (compose.yaml, .env)
          </label>
          <label className="flex items-center gap-2 text-sm text-[var(--text)]">
            <input type="checkbox" checked={removeConfig} onChange={(e) => setRemoveConfig(e.target.checked)} disabled={deleting} className="rounded" />
            Delete config directory (/opt/stacklab/config/{stackName}/)
          </label>
          <label className="flex items-center gap-2 text-sm text-amber-400">
            <input type="checkbox" checked={removeData} onChange={(e) => setRemoveData(e.target.checked)} disabled={deleting} className="rounded" />
            Delete data directory (/opt/stacklab/data/{stackName}/)
          </label>
          {removeData && (
            <p className="ml-6 text-xs text-red-400">Deleting data is irreversible.</p>
          )}
        </div>

        {error && (
          <div className="mt-4 rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {jobId && (
          <div className="mt-4">
            <ProgressPanel jobId={jobId} onDone={handleJobDone} />
          </div>
        )}

        <div className="mt-6 flex justify-end gap-2">
          <button
            onClick={onClose}
            disabled={deleting}
            className="rounded-full border border-[var(--panel-border)] px-4 py-2 text-sm text-[var(--muted)] hover:text-[var(--text)] disabled:opacity-40"
          >
            Cancel
          </button>
          <button
            data-testid="delete-confirm"
            onClick={handleDelete}
            disabled={deleting || (!removeRuntime && !removeDefinition && !removeConfig && !removeData)}
            className="rounded-full border border-red-400/30 bg-red-400/10 px-4 py-2 text-sm text-red-400 transition hover:bg-red-400/20 disabled:opacity-40"
          >
            {deleting ? 'Removing...' : 'Remove selected'}
          </button>
        </div>
      </div>
    </div>
  )
}
