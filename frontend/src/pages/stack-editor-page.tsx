import { useCallback, useEffect, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { StackDetailResponse } from '@/lib/api-types'
import { getDefinition, getResolvedConfig, invokeAction, resolveConfigDraft, saveDefinition } from '@/lib/api-client'
import { YamlEditor } from '@/components/yaml-editor'
import { ProgressPanel } from '@/components/progress-panel'
import { cn } from '@/lib/cn'

type ActiveTab = 'compose' | 'env'

export function StackEditorPage() {
  const { stack, refetch } = useOutletContext<{
    stack: StackDetailResponse['stack']
    refetch: () => void
  }>()

  const [activeTab, setActiveTab] = useState<ActiveTab>('compose')
  const [composeYaml, setComposeYaml] = useState('')
  const [envContent, setEnvContent] = useState('')
  const [envExists, setEnvExists] = useState(false)
  const [savedCompose, setSavedCompose] = useState('')
  const [savedEnv, setSavedEnv] = useState('')

  const [resolvedContent, setResolvedContent] = useState('')
  const [resolvedValid, setResolvedValid] = useState(true)
  const [resolvedError, setResolvedError] = useState('')

  const [saving, setSaving] = useState(false)
  const [activeJobId, setActiveJobId] = useState<string | null>(null)
  const [pendingDeploy, setPendingDeploy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loadingDef, setLoadingDef] = useState(true)

  const isDirty = composeYaml !== savedCompose || envContent !== savedEnv

  // Load definition
  useEffect(() => {
    let cancelled = false
    setLoadingDef(true)
    Promise.all([
      getDefinition(stack.id),
      getResolvedConfig(stack.id),
    ]).then(([def, resolved]) => {
      if (cancelled) return
      setComposeYaml(def.files.compose_yaml.content)
      setSavedCompose(def.files.compose_yaml.content)
      setEnvContent(def.files.env.content)
      setSavedEnv(def.files.env.content)
      setEnvExists(def.files.env.exists)
      if (resolved.valid && resolved.content) {
        setResolvedContent(resolved.content)
        setResolvedValid(true)
        setResolvedError('')
      } else if (resolved.error) {
        setResolvedContent('')
        setResolvedValid(false)
        setResolvedError(resolved.error.message)
      }
    }).catch((err) => {
      if (cancelled) return
      setError(err.message)
    }).finally(() => {
      if (!cancelled) setLoadingDef(false)
    })
    return () => { cancelled = true }
  }, [stack.id])

  // Preview draft
  const handlePreview = useCallback(async () => {
    try {
      const result = await resolveConfigDraft(stack.id, {
        compose_yaml: composeYaml,
        env: envContent,
      })
      if (result.valid && result.content) {
        setResolvedContent(result.content)
        setResolvedValid(true)
        setResolvedError('')
      } else if (result.error) {
        setResolvedContent('')
        setResolvedValid(false)
        setResolvedError(result.error.message)
      }
    } catch (err) {
      setResolvedError(err instanceof Error ? err.message : 'Preview failed')
      setResolvedValid(false)
    }
  }, [stack.id, composeYaml, envContent])

  // Save (and optionally deploy after save completes)
  const handleSave = useCallback(async (deploy: boolean) => {
    setSaving(true)
    setError(null)
    setActiveJobId(null)
    setPendingDeploy(deploy)
    try {
      const result = await saveDefinition(stack.id, {
        compose_yaml: composeYaml,
        env: envContent,
        validate_after_save: true,
      })
      setSavedCompose(composeYaml)
      setSavedEnv(envContent)
      setActiveJobId(result.job.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed')
      setPendingDeploy(false)
    } finally {
      setSaving(false)
    }
  }, [stack.id, composeYaml, envContent])

  const handleDiscard = useCallback(() => {
    setComposeYaml(savedCompose)
    setEnvContent(savedEnv)
  }, [savedCompose, savedEnv])

  const handleJobDone = useCallback(async (state: string) => {
    if (state === 'succeeded') {
      refetch()
      // Refresh resolved config
      getResolvedConfig(stack.id).then((resolved) => {
        if (resolved.valid && resolved.content) {
          setResolvedContent(resolved.content)
          setResolvedValid(true)
          setResolvedError('')
        }
      }).catch(() => {})

      // Chain deploy if Save & Deploy was requested
      if (pendingDeploy) {
        setPendingDeploy(false)
        try {
          const deployResult = await invokeAction(stack.id, 'up')
          setActiveJobId(deployResult.job.id)
        } catch (err) {
          setError(err instanceof Error ? err.message : 'Deploy failed after save')
        }
      }
    } else {
      // Save failed — don't chain deploy
      setPendingDeploy(false)
    }
  }, [refetch, stack.id, pendingDeploy])

  if (loadingDef) {
    return (
      <div className="animate-pulse space-y-3">
        <div className="h-8 w-40 rounded bg-[rgba(255,255,255,0.05)]" />
        <div className="h-96 rounded-[16px] bg-[rgba(255,255,255,0.03)]" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {error && (
        <div className="rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
          {error}
        </div>
      )}

      {/* Tab selector + actions */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex gap-1">
          <button
            onClick={() => setActiveTab('compose')}
            className={cn(
              'rounded-full border px-3 py-1 text-xs transition',
              activeTab === 'compose'
                ? 'border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            compose.yaml
          </button>
          <button
            onClick={() => setActiveTab('env')}
            className={cn(
              'rounded-full border px-3 py-1 text-xs transition',
              activeTab === 'env'
                ? 'border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            .env {!envExists && '(new)'}
          </button>
        </div>

        <div className="flex items-center gap-2">
          <button
            onClick={handlePreview}
            className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
          >
            Preview
          </button>
          {isDirty && (
            <button
              onClick={handleDiscard}
              className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
            >
              Discard
            </button>
          )}
          <button
            data-testid="editor-save"
            onClick={() => handleSave(false)}
            disabled={saving || stack.activity_state === 'locked'}
            className="rounded-full border border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
          <button
            data-testid="editor-save-deploy"
            onClick={() => handleSave(true)}
            disabled={saving || !resolvedValid || stack.activity_state === 'locked'}
            className="rounded-full border border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40"
          >
            Save & Deploy
          </button>
        </div>
      </div>

      {/* Validation status */}
      <div className="flex items-center gap-2 text-xs">
        {resolvedValid ? (
          <span className="text-emerald-400">✓ Config valid</span>
        ) : (
          <span className="text-red-400">✗ {resolvedError || 'Invalid config'}</span>
        )}
        {isDirty && <span className="text-amber-400">· Unsaved changes</span>}
      </div>

      {/* Editor split */}
      <div className="grid gap-3 xl:grid-cols-2" style={{ height: '500px' }}>
        <div className="min-h-0">
          {activeTab === 'compose' ? (
            <YamlEditor
              value={composeYaml}
              onChange={setComposeYaml}
            />
          ) : (
            <YamlEditor
              value={envContent}
              onChange={setEnvContent}
            />
          )}
        </div>
        <div className="min-h-0 overflow-auto rounded-[16px] border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs text-[var(--muted)]">
          <div className="mb-2 text-[var(--accent)] text-xs uppercase tracking-wider">
            Resolved config
          </div>
          {resolvedContent ? (
            <pre className="whitespace-pre-wrap text-[var(--text)]">{resolvedContent}</pre>
          ) : resolvedError ? (
            <pre className="text-red-400">{resolvedError}</pre>
          ) : (
            <span>Click "Preview" to resolve current editor contents.</span>
          )}
        </div>
      </div>

      {/* Progress panel */}
      {activeJobId && (
        <ProgressPanel jobId={activeJobId} onDone={handleJobDone} />
      )}
    </div>
  )
}
