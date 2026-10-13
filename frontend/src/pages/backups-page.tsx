import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { AsyncState } from '@/components/async-state'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { PageHeader } from '@/components/page-header'
import { useApi } from '@/hooks/use-api'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { useJobStream } from '@/hooks/use-job-stream'
import { checkBackupDestination, deleteBackupDestination, deleteBackupPlan, getBackups, initializeBackupDestination, retryBackupRun, runBackupPlan, testBackupDestination, type BackupDestination, type BackupJobResponse, type BackupPlan } from '@/lib/backup-api'
import { cn } from '@/lib/cn'
import { BackupDate, BackupError, BackupPanel, BackupSize, BackupStatus, buttonClass, Field, inputClass, primaryClass } from './backups/backup-primitives'
import { DestinationEditor } from './backups/destination-editor'
import { PlanEditor } from './backups/plan-editor'
import { RecoveryBrowser } from './backups/recovery-browser'
import { ApplyRecoveryDialog } from './backups/apply-recovery-dialog'
import { SourceLabel } from './backups/source-label'

type Editor = { kind: 'destination'; value?: BackupDestination } | { kind: 'plan'; value?: BackupPlan } | null
type Review = { kind: 'initialize' | 'deleteDestination'; destination: BackupDestination } | { kind: 'deletePlan'; plan: BackupPlan } | null
const tabs = ['destinations', 'plans', 'runs'] as const
type Tab = typeof tabs[number]

export function BackupsPage() {
  const [search, setSearch] = useSearchParams()
  const rawTab = search.get('view')
  const tab: Tab = rawTab === 'plans' || rawTab === 'runs' ? rawTab : 'destinations'
  const state = useApi((signal) => getBackups(signal), [])
  const { refetch } = state
  const { openJob } = useJobDrawer()
  const [editor, setEditor] = useState<Editor>(null)
  const [review, setReview] = useState<Review>(null)
  const [runPlan, setRunPlan] = useState<BackupPlan | null>(null)
  const [recovery, setRecovery] = useState<BackupDestination | null>(null)
  const [applyRecovery, setApplyRecovery] = useState<BackupDestination | null>(null)
  const [check, setCheck] = useState<BackupDestination | null>(null)
  const [readData, setReadData] = useState<'metadata' | 'subset' | 'all'>('metadata')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [jobId, setJobId] = useState<string | null>(null)
  const [clock, setClock] = useState(() => Date.now())
  const { state: jobState } = useJobStream({ jobId })
  const data = state.data
  const destinations = data?.destinations ?? []
  const plans = data?.plans ?? []
  const runs = data?.runs ?? []
  const hasRunning = runs.some((run) => ['queued', 'running'].includes(run.state))

  useEffect(() => {
    if (jobId && ['succeeded', 'failed', 'cancelled', 'timed_out'].includes(jobState ?? '')) refetch()
  }, [jobId, jobState, refetch])
  useEffect(() => {
    const timer = window.setInterval(() => {
      setClock(Date.now())
      if (hasRunning) refetch()
    }, hasRunning ? 5000 : 60000)
    return () => window.clearInterval(timer)
  }, [hasRunning, refetch])

  function started(result: BackupJobResponse) {
    setJobId(result.job.id)
    openJob(result.job.id)
    state.refetch()
  }

  async function command(action: () => Promise<unknown>): Promise<boolean> {
    if (pending) return false
    setPending(true)
    setError(null)
    try {
      const result = await action()
      if (result && typeof result === 'object' && 'job' in result) started(result as BackupJobResponse)
      else state.refetch()
      return true
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Backup operation failed')
      return false
    } finally {
      setPending(false)
    }
  }

  const destinationName = (id: string) => destinations.find((item) => item.id === id)?.name ?? id
  const finishEditor = () => { setEditor(null); state.refetch() }

  return <div className="flex flex-col gap-4">
    <PageHeader kicker="Recovery" title="Backups" meta="Encrypted recovery sets with independent copies per destination" actions={<button className={buttonClass} disabled={state.loading} onClick={state.refetch}>Refresh</button>} />
    <BackupError error={error} />
    <div className="flex gap-2 overflow-x-auto" role="tablist" aria-label="Backup views">
      {tabs.map((item) => <button key={item} id={`backup-tab-${item}`} aria-controls={`backup-panel-${item}`} role="tab" aria-selected={tab === item} tabIndex={tab === item ? 0 : -1} onKeyDown={(event) => {
        if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
        event.preventDefault()
        const index = tabs.indexOf(item)
        const next = event.key === 'Home' ? tabs[0] : event.key === 'End' ? tabs[2] : tabs[(index + (event.key === 'ArrowRight' ? 1 : 2)) % 3]
        setSearch((previous) => { previous.set('view', next); return previous })
        document.getElementById(`backup-tab-${next}`)?.focus()
      }} onClick={() => setSearch((previous) => { previous.set('view', item); return previous })} className={cn(buttonClass, tab === item && 'border-[var(--accent)]/40 bg-[var(--accent)]/10')}>{item === 'runs' ? 'Runs & recovery' : item[0].toUpperCase() + item.slice(1)}</button>)}
    </div>
    <AsyncState loading={state.loading} error={state.error} hasData={data !== null} isEmpty={false} loadingLabel="Loading backups…" emptyMessage="No backups configured." onRetry={state.refetch} loadingFallback={<p className="text-sm text-[var(--muted)]">Loading backups…</p>}>
      {data && <>
        {!data.restic_available && <p role="alert" className="rounded-md border border-[var(--warning)]/30 p-3 text-sm text-[var(--warning)]">Restic is unavailable on this host. Install the supported backup tools before running backups.</p>}
        <div role="tabpanel" id={`backup-panel-${tab}`} aria-labelledby={`backup-tab-${tab}`} className="space-y-4">
          {tab === 'destinations' && <BackupPanel title="Destinations" actions={<button className={primaryClass} onClick={() => setEditor({ kind: 'destination' })}>Add destination</button>}>
            {destinations.length === 0 && <p className="text-sm leading-6 text-[var(--muted)]">Start by adding a local disk, NAS, S3 bucket or Google Drive repository. Keep repository passwords and access credentials outside this host so recovery remains possible after host loss.</p>}
            <div className="space-y-3">{destinations.map((destination) => <article key={destination.id} className="space-y-3 rounded-md border border-[var(--panel-border)] p-4">
              <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="font-medium text-[var(--text)]">{destination.name}</h3><BackupStatus value={destination.status} /></div>
              <p className="break-all text-xs text-[var(--muted)]">{destination.kind.toUpperCase()} · {destination.repository}</p>
              <p className="text-xs text-[var(--muted)]">Repository data: <BackupSize value={destination.repository_size_bytes} /></p>
              {destination.kind === 'rclone' && !data.rclone_available && <p className="text-sm text-[var(--warning)]">Rclone is unavailable on this host.</p>}
              <dl className="grid gap-2 text-xs sm:grid-cols-3"><div><dt className="text-[var(--muted)]">Last successful copy</dt><dd className="mt-1 text-[var(--text)]"><BackupDate value={destination.last_success_at} /></dd></div><div><dt className="text-[var(--muted)]">Repository check {destination.last_check_policy ? `(${destination.last_check_policy})` : ''}</dt><dd className="mt-1 text-[var(--text)]"><BackupDate value={destination.last_check_at} /></dd></div><div><dt className="text-[var(--muted)]">Isolated restore test</dt><dd className="mt-1 text-[var(--text)]"><BackupDate value={destination.last_restore_test_at} /></dd></div></dl>
              {destination.error && <p className="text-sm text-[var(--danger)]">{destination.error}</p>}
              <div className="flex flex-wrap gap-2"><button className={buttonClass} disabled={pending} onClick={() => { void command(() => testBackupDestination(destination.id)) }}>Test access</button><button className={buttonClass} disabled={pending} onClick={() => setReview({ kind: 'initialize', destination })}>Initialize</button><button className={buttonClass} disabled={pending} onClick={() => { setReadData('metadata'); setCheck(destination) }}>Check repository</button><button className={buttonClass} disabled={pending} onClick={() => setRecovery(destination)}>Browse & restore</button><button className={buttonClass} disabled={pending} onClick={() => setApplyRecovery(destination)}>Apply recovered data</button><button className={buttonClass} disabled={pending} onClick={() => setEditor({ kind: 'destination', value: destination })}>Edit</button><button className={buttonClass} disabled={pending} onClick={() => setReview({ kind: 'deleteDestination', destination })}>Remove</button></div>
            </article>)}</div>
          </BackupPanel>}
          {tab === 'plans' && <BackupPanel title="Backup plans" actions={<button className={primaryClass} onClick={() => setEditor({ kind: 'plan' })}>Create plan</button>}>
            {plans.length === 0 && <p className="text-sm text-[var(--muted)]">Select stack definitions, service data or Stacklab recovery, then choose destinations and schedules.</p>}
            <div className="space-y-3">{plans.map((plan) => <article key={plan.id} className="space-y-3 rounded-md border border-[var(--panel-border)] p-4">
              <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="font-medium text-[var(--text)]">{plan.name}</h3><BackupStatus value={plan.enabled ? 'enabled' : 'disabled'} /></div>
              <p className="text-sm text-[var(--muted)]">{plan.sources.length} sources · {plan.consistency === 'stop' ? 'Stop writers during capture' : 'Live files / prepared dumps'} · {(plan.staging_max_bytes / 1024 ** 3).toLocaleString()} GiB staging budget</p>
              <ul className="space-y-1 text-xs text-[var(--muted)]">{plan.destinations.map((assignment) => <li key={assignment.destination_id}>{destinationName(assignment.destination_id)} · {assignment.enabled ? assignment.schedule.frequency : 'disabled'}{assignment.schedule.frequency !== 'manual' && assignment.enabled ? ` ${assignment.schedule.time} ${assignment.schedule.timezone || '(host timezone)'}` : ''} · keep {assignment.retention.daily} daily / {assignment.retention.weekly} weekly / {assignment.retention.monthly} monthly</li>)}</ul>
              <details><summary className="cursor-pointer text-sm text-[var(--muted)]">Selected data</summary><ul className="mt-2 space-y-2 text-sm text-[var(--text)]">{plan.sources.map((source) => <li key={source.id}><SourceLabel source={source} />{(source.excludes?.length ?? 0) > 0 && <p className="text-xs text-[var(--warning)]">Excludes: {source.excludes?.join(', ')}</p>}</li>)}</ul></details>
              <div className="flex flex-wrap gap-2"><button className={primaryClass} disabled={pending || !data.restic_available} onClick={() => setRunPlan(plan)}>Back up now</button><button className={buttonClass} disabled={pending} onClick={() => setEditor({ kind: 'plan', value: plan })}>Edit plan</button><button className={buttonClass} disabled={pending} onClick={() => setReview({ kind: 'deletePlan', plan })}>Remove plan</button></div>
            </article>)}</div>
          </BackupPanel>}
          {tab === 'runs' && <BackupPanel title="Capture and destination results">
            {runs.length === 0 && <p className="text-sm text-[var(--muted)]">No runs yet. Existing repository snapshots remain available under Destinations → Browse & restore.</p>}
            <div className="space-y-3">{runs.map((run) => {
              const expired = new Date(run.expires_at).getTime() <= clock
              const failed = run.copies.filter((copy) => copy.state !== 'succeeded')
              const active = ['running', 'queued'].includes(run.state)
              return <article key={run.id} className="space-y-3 rounded-md border border-[var(--panel-border)] p-4">
                <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="font-medium text-[var(--text)]">{run.plan_name}</h3><BackupStatus value={run.state} /></div>
                <p className="text-xs text-[var(--muted)]">Created <BackupDate value={run.created_at} /> · Capture: {run.capture_state}{run.captured_at && <> · Captured <BackupDate value={run.captured_at} /></>}</p>
                {run.error && <p className="text-sm text-[var(--danger)]">{run.error}</p>}
                <div className="space-y-2">{run.copies.map((copy) => <div key={copy.destination_id} className="rounded-md bg-[var(--bg)] p-3">
                  <div className="flex flex-wrap justify-between gap-2 text-sm text-[var(--text)]"><span>{destinationName(copy.destination_id)}</span><BackupStatus value={copy.state} /></div>
                  <p className="mt-1 text-xs text-[var(--muted)]">Verification: {copy.verification} · Retention: {copy.retention} · Uploaded <BackupDate value={copy.uploaded_at} /></p>
                  {copy.snapshot_id && <p className="mt-1 break-all font-mono text-xs text-[var(--muted)]">{copy.snapshot_id}</p>}
                  {copy.error && <p className="mt-1 text-sm text-[var(--danger)]">{copy.error}</p>}
                </div>)}</div>
                {run.manifest && <details><summary className="cursor-pointer text-sm text-[var(--muted)]">Recovery manifest and prerequisites</summary><div className="mt-2 space-y-2 text-xs text-[var(--muted)]"><p>Manifest v{run.manifest.version} · {run.manifest.consistency} · Stacklab {run.manifest.stacklab_version}</p>{run.manifest.sources.map((item) => <p key={item.saved_path} className="break-all">{item.source.service || item.source.kind}: {item.original_path} → {item.saved_path}</p>)}{run.manifest.sources.flatMap((item) => (item.consumers ?? []).map((consumer) => <p key={`${item.saved_path}:${consumer.container_id}`} className="break-all">{item.source.mount || item.source.kind} consumer: {consumer.stack_id || 'external'}/{consumer.service || consumer.container_id} · {consumer.read_only ? 'read only' : 'writer'} · {consumer.running ? 'running at capture' : 'stopped at capture'}</p>))}{run.manifest.images?.map((image) => <p key={image} className="break-all">Required image: {image}</p>)}{run.manifest.warnings?.map((warning) => <p key={warning} className="text-[var(--warning)]">{warning}</p>)}</div></details>}
                <div className="flex flex-wrap gap-2"><button className={buttonClass} onClick={() => openJob(run.job_id)}>Job details</button>{failed.length > 0 && !active && <button className={buttonClass} disabled={pending || expired || run.capture_state !== 'succeeded'} onClick={() => { void command(() => retryBackupRun(run.id, failed.map((copy) => copy.destination_id))) }}>Retry failed copies</button>}</div>
                {failed.length > 0 && !active && <p className="text-xs text-[var(--muted)]">{expired || run.capture_state !== 'succeeded' ? 'A new capture is required; start the plan again.' : <>Prepared data available for retries until <BackupDate value={run.expires_at} />. Successful copies are preserved.</>}</p>}
              </article>
            })}</div>
          </BackupPanel>}
        </div>
      </>}
    </AsyncState>
    {editor?.kind === 'destination' && <DestinationEditor destination={editor.value} onClose={() => setEditor(null)} onSaved={finishEditor} />}
    {editor?.kind === 'plan' && <PlanEditor plan={editor.value} destinations={destinations} initialStackId={search.get('stack') ?? undefined} onClose={() => setEditor(null)} onSaved={finishEditor} />}
    {recovery && <RecoveryBrowser destination={recovery} onClose={() => setRecovery(null)} onStarted={(result) => { started(result); setRecovery(null) }} />}
    {applyRecovery && <ApplyRecoveryDialog destination={applyRecovery} onClose={() => setApplyRecovery(null)} onStarted={(result) => { started(result); setApplyRecovery(null) }} />}
    {runPlan && <RunPlanDialog plan={runPlan} destinations={destinations} pending={pending} error={error} onClose={() => setRunPlan(null)} onRun={async (ids) => { if (await command(() => runBackupPlan(runPlan.id, ids))) setRunPlan(null) }} />}
    {check && <Dialog title={`Check ${check.name}`} onClose={() => { if (!pending) setCheck(null) }} preventClose={pending} busy={pending}><div className="mt-4 space-y-4"><BackupError error={error} /><Field label="Data-read policy"><select className={inputClass} value={readData} disabled={pending} onChange={(event) => setReadData(event.target.value as typeof readData)}><option value="metadata">Repository metadata only</option><option value="subset">Read a subset of stored data</option><option value="all">Read all stored data</option></select></Field><p className="text-sm text-[var(--muted)]">Reading stored data can incur provider download charges. A repository check is separate from an actual restore test.</p><div className="flex justify-end gap-2"><button className={buttonClass} disabled={pending} onClick={() => setCheck(null)}>Cancel</button><button className={primaryClass} disabled={pending} onClick={() => { void command(() => checkBackupDestination(check.id, readData)).then((ok) => { if (ok) setCheck(null) }) }}>{pending ? 'Starting…' : 'Start check'}</button></div></div></Dialog>}
    {review && <ConfirmDialog title={review.kind === 'initialize' ? 'Initialize new repository' : review.kind === 'deletePlan' ? 'Remove backup plan' : 'Remove destination'} message={review.kind === 'initialize' ? 'Create an encrypted restic repository at the configured location. Only initialize a new repository; test access to an existing one.' : 'Remove this configuration from Stacklab. Existing repository snapshots remain available with independently saved credentials.'} error={error} confirmLabel={review.kind === 'initialize' ? 'Initialize repository' : 'Remove configuration'} confirming={pending} requireText={review.kind === 'initialize' ? undefined : review.kind === 'deletePlan' ? review.plan.name : review.destination.name} items={[review.kind === 'deletePlan' ? review.plan.name : review.destination.repository]} onCancel={() => { if (!pending) setReview(null) }} onConfirm={() => {
      const action = review.kind === 'initialize' ? () => initializeBackupDestination(review.destination.id) : review.kind === 'deletePlan' ? () => deleteBackupPlan(review.plan.id) : () => deleteBackupDestination(review.destination.id)
      void command(action).then((ok) => { if (ok) setReview(null) })
    }} />}
  </div>
}

function RunPlanDialog({ plan, destinations, pending, error, onClose, onRun }: { plan: BackupPlan; destinations: BackupDestination[]; pending: boolean; error: string | null; onClose: () => void; onRun: (ids: string[]) => Promise<void> }) {
  const [selected, setSelected] = useState(plan.destinations.filter((assignment) => assignment.enabled).map((assignment) => assignment.destination_id))
  const [reviewed, setReviewed] = useState(false)
  return <Dialog title={`Back up ${plan.name}`} onClose={onClose} preventClose={pending} busy={pending}><div className="mt-4 space-y-4"><BackupError error={error} /><p className="text-sm text-[var(--muted)]">Capture {plan.sources.length} selected sources once and deliver to the selected repositories.</p><fieldset disabled={pending} className="space-y-2">{plan.destinations.filter((assignment) => assignment.enabled).map((assignment) => <label key={assignment.destination_id} className="flex items-center gap-2 text-sm text-[var(--text)]"><input type="checkbox" checked={selected.includes(assignment.destination_id)} onChange={(event) => setSelected((previous) => event.target.checked ? [...previous, assignment.destination_id] : previous.filter((id) => id !== assignment.destination_id))} />{destinations.find((destination) => destination.id === assignment.destination_id)?.name ?? assignment.destination_id}</label>)}<label className="mt-4 flex items-start gap-2 text-sm text-[var(--warning)]"><input type="checkbox" className="mt-1" checked={reviewed} onChange={(event) => setReviewed(event.target.checked)} />{plan.consistency === 'stop' ? 'I understand that running services writing selected or shared data stop during local capture and restart before upload.' : 'I reviewed live-copy consistency: active database directories require prepared dumps or stopped writers.'}</label></fieldset><div className="flex justify-end gap-2"><button className={buttonClass} disabled={pending} onClick={onClose}>Cancel</button><button className={primaryClass} disabled={pending || !reviewed || selected.length === 0} onClick={() => { void onRun(selected) }}>{pending ? 'Starting…' : 'Start backup'}</button></div></div></Dialog>
}
