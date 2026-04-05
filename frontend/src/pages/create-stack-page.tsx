import { useCallback, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { createStack } from '@/lib/api-client'
import { YamlEditor } from '@/components/yaml-editor'
import { ProgressPanel } from '@/components/progress-panel'

const STACK_ID_REGEX = /^[a-z0-9]+(?:-[a-z0-9]+)*$/
const DEFAULT_COMPOSE = `services:
  app:
    image:
`

export function CreateStackPage() {
  const navigate = useNavigate()
  const [stackId, setStackId] = useState('')
  const [composeYaml, setComposeYaml] = useState(DEFAULT_COMPOSE)
  const [deployAfter, setDeployAfter] = useState(false)
  const [creating, setCreating] = useState(false)
  const [jobId, setJobId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const idValid = stackId.length > 0 && STACK_ID_REGEX.test(stackId)

  const handleSubmit = useCallback(async (e: FormEvent) => {
    e.preventDefault()
    if (!idValid) return

    setCreating(true)
    setError(null)
    try {
      const result = await createStack({
        stack_id: stackId,
        compose_yaml: composeYaml,
        env: '',
        create_config_dir: true,
        create_data_dir: true,
        deploy_after_create: deployAfter,
      })
      setJobId(result.job.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Create failed')
      setCreating(false)
    }
  }, [stackId, composeYaml, deployAfter, idValid])

  const handleJobDone = useCallback((state: string) => {
    setCreating(false)
    if (state === 'succeeded') {
      navigate(`/stacks/${stackId}`)
    }
  }, [navigate, stackId])

  return (
    <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="font-brand text-xs uppercase tracking-[0.28em] text-[var(--accent)]">New stack</div>
      <h2 className="mt-2 text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">Create stack</h2>

      <form onSubmit={handleSubmit} className="mt-6 flex flex-col gap-4">
        <label className="block">
          <span className="mb-2 block text-sm text-[var(--muted)]">Stack name</span>
          <input
            data-testid="create-stack-name"
            type="text"
            value={stackId}
            onChange={(e) => setStackId(e.target.value.toLowerCase())}
            placeholder="my-new-app"
            disabled={creating}
            className="w-full max-w-md rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-3 font-mono text-[var(--text)] outline-none transition focus:border-[rgba(34,197,94,0.35)] disabled:opacity-50"
          />
          {stackId.length > 0 && !idValid && (
            <p className="mt-1 text-xs text-red-400">Lowercase letters, numbers, and dashes only.</p>
          )}
          {idValid && (
            <p className="mt-1 text-xs text-[var(--muted)]">
              Will create: /opt/stacklab/stacks/{stackId}/compose.yaml
            </p>
          )}
        </label>

        <div>
          <span className="mb-2 block text-sm text-[var(--muted)]">Initial compose.yaml</span>
          <div style={{ height: '300px' }}>
            <YamlEditor value={composeYaml} onChange={setComposeYaml} readOnly={creating} />
          </div>
        </div>

        <label className="flex items-center gap-2 text-sm text-[var(--muted)]">
          <input
            type="checkbox"
            checked={deployAfter}
            onChange={(e) => setDeployAfter(e.target.checked)}
            disabled={creating}
            className="rounded"
          />
          Deploy immediately after creation
        </label>

        {error && (
          <div className="rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {jobId && <ProgressPanel jobId={jobId} onDone={handleJobDone} />}

        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => navigate('/stacks')}
            className="rounded-full border border-[var(--panel-border)] px-4 py-2 text-sm text-[var(--muted)] hover:text-[var(--text)]"
          >
            Cancel
          </button>
          <button
            data-testid="create-stack-submit"
            type="submit"
            disabled={!idValid || creating}
            className="rounded-full border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(34,197,94,0.2)] disabled:opacity-40"
          >
            {creating ? 'Creating...' : deployAfter ? 'Create & Deploy' : 'Create'}
          </button>
        </div>
      </form>
    </section>
  )
}
