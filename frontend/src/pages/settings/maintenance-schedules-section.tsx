import { useCallback, useEffect, useMemo, useState } from 'react'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { StatusMessage } from '@/components/status-message'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { getMaintenanceSchedules, getStacks, getStack, updateMaintenanceSchedules } from '@/lib/api-client'
import type { MaintenanceSchedulesResponse, ScheduleFrequency, ScheduleWeekday, StackListItem } from '@/lib/api-types'
import {
  describeSchedule,
  filteredExcludedServices,
  hasExcludedServices,
} from '@/pages/settings/maintenance-schedule-utils'
import { FrequencyToggle, ScheduleStatusFooter, WeekdayPicker } from '@/pages/settings/maintenance-schedule-controls'
import { SettingsLoadError } from '@/pages/settings/settings-card'

export function MaintenanceSchedulesSection() {
  const { openJob } = useJobDrawer()
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [data, setData] = useState<MaintenanceSchedulesResponse | null>(null)
  const [stackOptions, setStackOptions] = useState<StackListItem[]>([])
  const [serviceOptions, setServiceOptions] = useState<Record<string, string[]>>({})
  const [serviceLoading, setServiceLoading] = useState<Record<string, boolean>>({})
  const [expandedServiceStacks, setExpandedServiceStacks] = useState<Set<string>>(new Set())
  const [savingSchedules, setSavingSchedules] = useState(false)
  const [saveResult, setSaveResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [confirmVolumeCleanup, setConfirmVolumeCleanup] = useState(false)

  // Update policy
  const [updateEnabled, setUpdateEnabled] = useState(false)
  const [updateFreq, setUpdateFreq] = useState<ScheduleFrequency>('weekly')
  const [updateTime, setUpdateTime] = useState('03:30')
  const [updateWeekdays, setUpdateWeekdays] = useState<ScheduleWeekday[]>(['sat'])
  const [updateTargetMode, setUpdateTargetMode] = useState<'all' | 'selected'>('all')
  const [updateTargetStacks, setUpdateTargetStacks] = useState<string[]>([])
  const [updateExcludedServices, setUpdateExcludedServices] = useState<Record<string, string[]>>({})
  const [updatePull, setUpdatePull] = useState(true)
  const [updateBuild, setUpdateBuild] = useState(true)
  const [updateOrphans, setUpdateOrphans] = useState(true)
  const [updatePrune, setUpdatePrune] = useState(false)
  const [updatePruneVol, setUpdatePruneVol] = useState(false)

  // Prune policy
  const [pruneEnabled, setPruneEnabled] = useState(false)
  const [pruneFreq, setPruneFreq] = useState<ScheduleFrequency>('weekly')
  const [pruneTime, setPruneTime] = useState('04:30')
  const [pruneWeekdays, setPruneWeekdays] = useState<ScheduleWeekday[]>(['sun'])
  const [pruneImages, setPruneImages] = useState(true)
  const [pruneBuildCache, setPruneBuildCache] = useState(true)
  const [pruneStopped, setPruneStopped] = useState(true)
  const [pruneVolumes, setPruneVolumes] = useState(false)

  const loadSchedules = useCallback(() => {
    setLoading(true)
    setLoadError(null)
    setSaveResult(null)
    Promise.allSettled([getMaintenanceSchedules(), getStacks()])
      .then(([schedulesResult, stacksResult]) => {
        if (schedulesResult.status === 'fulfilled') {
          const s = schedulesResult.value
          setData(s)
          setUpdateEnabled(s.update.enabled)
          setUpdateFreq(s.update.frequency)
          setUpdateTime(s.update.time)
          setUpdateWeekdays(s.update.weekdays ?? ['sat'])
          setUpdateTargetMode(s.update.target.mode)
          setUpdateTargetStacks(s.update.target.stack_ids ?? [])
          setUpdateExcludedServices(s.update.target.excluded_services ?? {})
          setUpdatePull(s.update.options.pull_images)
          setUpdateBuild(s.update.options.build_images)
          setUpdateOrphans(s.update.options.remove_orphans)
          setUpdatePrune(s.update.options.prune_after)
          setUpdatePruneVol(s.update.options.include_volumes)
          setPruneEnabled(s.prune.enabled)
          setPruneFreq(s.prune.frequency)
          setPruneTime(s.prune.time)
          setPruneWeekdays(s.prune.weekdays ?? ['sun'])
          setPruneImages(s.prune.scope.images)
          setPruneBuildCache(s.prune.scope.build_cache)
          setPruneStopped(s.prune.scope.stopped_containers)
          setPruneVolumes(s.prune.scope.volumes)
        } else {
          setData(null)
          setLoadError(schedulesResult.reason instanceof Error ? schedulesResult.reason.message : 'Failed to load maintenance schedules')
        }
        if (stacksResult.status === 'fulfilled') {
          setStackOptions(stacksResult.value.items)
        } else {
          setStackOptions([])
          setLoadError(stacksResult.reason instanceof Error ? stacksResult.reason.message : 'Failed to load stack list')
        }
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { loadSchedules() }, [loadSchedules])

  const visibleUpdateStackIds = useMemo(() => (
    updateTargetMode === 'selected'
      ? updateTargetStacks
      : stackOptions.map((stack) => stack.id)
  ), [stackOptions, updateTargetMode, updateTargetStacks])
  const hasVisibleExcludedServices = useMemo(() => (
    hasExcludedServices(filteredExcludedServices(updateExcludedServices, visibleUpdateStackIds) ?? {})
  ), [updateExcludedServices, visibleUpdateStackIds])
  const volumeCleanupSummary = useMemo(() => {
    const summary: string[] = []
    if (updateEnabled && updatePrune && updatePruneVol) {
      const target = updateTargetMode === 'all' ? 'all stacks' : `${updateTargetStacks.length} selected stack(s)`
      summary.push(`After stack update: ${describeSchedule(updateFreq, updateTime, updateWeekdays)}; ${target}`)
    }
    if (pruneEnabled && pruneVolumes) {
      summary.push(`Scheduled cleanup: ${describeSchedule(pruneFreq, pruneTime, pruneWeekdays)}`)
    }
    if (summary.length > 0) summary.push('Scope: unused Docker volumes and their data')
    return summary
  }, [pruneEnabled, pruneFreq, pruneTime, pruneVolumes, pruneWeekdays, updateEnabled, updateFreq, updatePrune, updatePruneVol, updateTargetMode, updateTargetStacks.length, updateTime, updateWeekdays])

  const ensureServicesLoaded = useCallback((stackId: string) => {
    if (serviceOptions[stackId] || serviceLoading[stackId]) return
    setServiceLoading((current) => ({ ...current, [stackId]: true }))
    getStack(stackId)
      .then((detail) => {
        setServiceOptions((current) => ({
          ...current,
          [detail.stack.id]: detail.stack.services.map((service) => service.name).sort(),
        }))
      })
      .catch(() => {
        setServiceOptions((current) => ({ ...current, [stackId]: [] }))
      })
      .finally(() => {
        setServiceLoading((current) => {
          const next = { ...current }
          delete next[stackId]
          return next
        })
      })
  }, [serviceLoading, serviceOptions])

  useEffect(() => {
    const visible = new Set(visibleUpdateStackIds)
    const stackIds = Object.keys(updateExcludedServices).filter((stackId) => visible.has(stackId))
    if (stackIds.length === 0) return
    setExpandedServiceStacks((current) => new Set([...current, ...stackIds]))
    for (const stackId of stackIds) {
      ensureServicesLoaded(stackId)
    }
  }, [ensureServicesLoaded, updateExcludedServices, visibleUpdateStackIds])

  const toggleServiceStack = useCallback((stackId: string) => {
    const willExpand = !expandedServiceStacks.has(stackId)
    setExpandedServiceStacks((current) => {
      const next = new Set(current)
      if (next.has(stackId)) {
        next.delete(stackId)
      } else {
        next.add(stackId)
      }
      return next
    })
    if (willExpand) {
      ensureServicesLoaded(stackId)
    }
  }, [ensureServicesLoaded, expandedServiceStacks])

  const toggleExcludedService = useCallback((stackId: string, serviceName: string, excluded: boolean) => {
    setUpdateExcludedServices((current) => {
      const existing = current[stackId] ?? []
      const nextForStack = excluded
        ? Array.from(new Set([...existing, serviceName])).sort()
        : existing.filter((item) => item !== serviceName)
      const next = { ...current }
      if (nextForStack.length > 0) {
        next[stackId] = nextForStack
      } else {
        delete next[stackId]
      }
      return next
    })
    if (excluded) {
      setUpdateOrphans(false)
    }
  }, [])

  const saveSchedules = useCallback(async () => {
    setSavingSchedules(true)
    setSaveResult(null)
    try {
      const result = await updateMaintenanceSchedules({
        update: {
          enabled: updateEnabled,
          frequency: updateFreq,
          time: updateTime,
          weekdays: updateFreq === 'weekly' ? updateWeekdays : undefined,
          target: {
            mode: updateTargetMode,
            stack_ids: updateTargetMode === 'selected' ? updateTargetStacks : undefined,
            excluded_services: filteredExcludedServices(updateExcludedServices, visibleUpdateStackIds),
          },
          options: {
            pull_images: updatePull,
            build_images: updateBuild,
            remove_orphans: updateOrphans,
            prune_after: updatePrune,
            include_volumes: updatePrune ? updatePruneVol : false,
          },
        },
        prune: {
          enabled: pruneEnabled,
          frequency: pruneFreq,
          time: pruneTime,
          weekdays: pruneFreq === 'weekly' ? pruneWeekdays : undefined,
          scope: {
            images: pruneImages,
            build_cache: pruneBuildCache,
            stopped_containers: pruneStopped,
            volumes: pruneVolumes,
          },
        },
      })
      setData(result)
      setSaveResult({ type: 'success', text: 'Saved' })
    } catch (err) {
      setSaveResult({ type: 'error', text: err instanceof Error ? err.message : 'Save failed' })
    } finally {
      setSavingSchedules(false)
    }
  }, [updateEnabled, updateFreq, updateTime, updateWeekdays, updateTargetMode, updateTargetStacks, visibleUpdateStackIds, updateExcludedServices, updatePull, updateBuild, updateOrphans, updatePrune, updatePruneVol, pruneEnabled, pruneFreq, pruneTime, pruneWeekdays, pruneImages, pruneBuildCache, pruneStopped, pruneVolumes])

  const handleSave = useCallback(() => {
    if (loadError) return
    if (updateTargetMode === 'selected' && updateTargetStacks.length === 0) {
      setSaveResult({ type: 'error', text: 'Select at least one stack for scheduled updates' })
      return
    }
    if (volumeCleanupSummary.length > 0) {
      setConfirmVolumeCleanup(true)
      return
    }
    void saveSchedules()
  }, [loadError, saveSchedules, updateTargetMode, updateTargetStacks.length, volumeCleanupSummary.length])

  if (loading) {
    return (
      <div aria-busy="true">
        <h2 className="text-sm font-medium text-[var(--text)]">Maintenance schedules</h2>
        <div className="mt-3 h-24 animate-pulse rounded-md bg-[rgba(255,255,255,0.03)]"><span className="sr-only" role="status" aria-live="polite">Loading maintenance schedules...</span></div>
      </div>
    )
  }

  if (loadError) {
    return <SettingsLoadError title="Maintenance schedules" message={loadError} onRetry={loadSchedules} />
  }

  return (
    <div>
      <h2 className="text-sm font-medium text-[var(--text)]">Maintenance schedules</h2>
      <p className="mt-1 text-xs text-[var(--muted)]">Runs in host local time. Reuses the same workflows as manual maintenance.</p>

      <div className="mt-3 max-w-lg space-y-4">
        {/* Update schedule card */}
        <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4 space-y-3">
          <label className="flex items-center gap-2 text-xs font-medium text-[var(--text)]">
            <input type="checkbox" checked={updateEnabled} onChange={(e) => setUpdateEnabled(e.target.checked)} className="rounded" />
            Scheduled stack update
          </label>

          <div className="flex flex-wrap items-center gap-2">
            <FrequencyToggle value={updateFreq} onChange={setUpdateFreq} />
            <input type="time" value={updateTime} onChange={(e) => setUpdateTime(e.target.value)} className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1 font-mono text-xs text-[var(--text)] outline-none" />
          </div>

          {updateFreq === 'weekly' && (
            <WeekdayPicker value={updateWeekdays} onChange={setUpdateWeekdays} />
          )}

          <div className="space-y-1">
            <label className="flex items-center gap-2 text-xs text-[var(--text)]">
              <input type="radio" checked={updateTargetMode === 'all'} onChange={() => setUpdateTargetMode('all')} className="accent-[var(--accent)]" />
              All stacks
            </label>
            <label className="flex items-center gap-2 text-xs text-[var(--text)]">
              <input type="radio" checked={updateTargetMode === 'selected'} onChange={() => setUpdateTargetMode('selected')} className="accent-[var(--accent)]" />
              Selected stacks
            </label>
          </div>

          {updateTargetMode === 'selected' && (
            <div className="space-y-2 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-3">
              {stackOptions.length > 0 ? (
                <div className="space-y-1.5">
                  {stackOptions.map((stack) => (
                    <label key={stack.id} className="flex items-center justify-between gap-3 text-xs text-[var(--text)]">
                      <span className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          aria-label={stack.id}
                          checked={updateTargetStacks.includes(stack.id)}
                          onChange={(e) => setUpdateTargetStacks((current) => (
                            e.target.checked
                              ? [...current, stack.id]
                              : current.filter((id) => id !== stack.id)
                          ))}
                          className="rounded"
                        />
                        <span className="font-mono">{stack.id}</span>
                      </span>
                      <span className="text-[var(--muted)]">{stack.display_state}</span>
                    </label>
                  ))}
                </div>
              ) : (
                <p className="text-xs text-[var(--muted)]">No stacks available.</p>
              )}
            </div>
          )}

          {visibleUpdateStackIds.length > 0 && (
            <div className="space-y-2 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-3">
              <div className="text-xs font-medium text-[var(--text)]">Skip services</div>
              <div className="space-y-2">
                {visibleUpdateStackIds.map((stackId) => {
                  const expanded = expandedServiceStacks.has(stackId)
                  const services = serviceOptions[stackId] ?? []
                  return (
                    <div key={stackId} className="space-y-1">
                      <button
                        type="button"
                        aria-label={`${expanded ? 'Hide' : 'Show'} services for ${stackId}`}
                        onClick={() => toggleServiceStack(stackId)}
                        className="flex w-full items-center justify-between gap-2 rounded border border-[var(--panel-border)] px-2 py-1.5 text-left text-xs text-[var(--text)]"
                      >
                        <span className="font-mono text-xs uppercase tracking-wide text-[var(--muted)]">{stackId}</span>
                        <span className="text-xs text-[var(--muted)]">{expanded ? 'Hide' : 'Show'}</span>
                      </button>
                      {expanded && (
                        <div className="flex flex-wrap gap-2 pt-1">
                          {serviceLoading[stackId] ? (
                            <span className="text-xs text-[var(--muted)]">Loading...</span>
                          ) : services.length > 0 ? (
                            services.map((serviceName) => (
                              <label key={serviceName} className="flex items-center gap-1.5 rounded border border-[var(--panel-border)] px-2 py-1 text-xs text-[var(--text)]">
                                <input
                                  type="checkbox"
                                  checked={(updateExcludedServices[stackId] ?? []).includes(serviceName)}
                                  onChange={(e) => toggleExcludedService(stackId, serviceName, e.target.checked)}
                                  className="rounded accent-[var(--accent)]"
                                />
                                <span className="font-mono">{serviceName}</span>
                              </label>
                            ))
                          ) : (
                            <span className="text-xs text-[var(--muted)]">No services.</span>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          <div className="space-y-1 text-xs text-[var(--muted)]">
            <label className="flex items-center gap-2"><input type="checkbox" checked={updatePull} onChange={(e) => setUpdatePull(e.target.checked)} className="rounded" />Pull images</label>
            <label className="flex items-center gap-2"><input type="checkbox" checked={updateBuild} onChange={(e) => setUpdateBuild(e.target.checked)} className="rounded" />Build images</label>
            <label className="flex items-center gap-2"><input type="checkbox" checked={updateOrphans} disabled={hasVisibleExcludedServices} onChange={(e) => setUpdateOrphans(e.target.checked)} className="rounded" />Remove orphans</label>
            <label className="flex items-center gap-2 text-[var(--warning)]"><input type="checkbox" checked={updatePrune} onChange={(e) => { setUpdatePrune(e.target.checked); if (!e.target.checked) setUpdatePruneVol(false) }} className="rounded" />Prune after update</label>
            {updatePrune && <label className="ml-5 flex items-center gap-2 text-[var(--danger)]"><input type="checkbox" checked={updatePruneVol} onChange={(e) => setUpdatePruneVol(e.target.checked)} className="rounded" />Include volumes</label>}
          </div>

          <ScheduleStatusFooter status={data?.update.status} onOpenJob={openJob} />
        </div>

        {/* Prune schedule card */}
        <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4 space-y-3">
          <label className="flex items-center gap-2 text-xs font-medium text-[var(--text)]">
            <input type="checkbox" checked={pruneEnabled} onChange={(e) => setPruneEnabled(e.target.checked)} className="rounded" />
            Scheduled cleanup
          </label>

          <div className="flex flex-wrap items-center gap-2">
            <FrequencyToggle value={pruneFreq} onChange={setPruneFreq} />
            <input type="time" value={pruneTime} onChange={(e) => setPruneTime(e.target.value)} className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1 font-mono text-xs text-[var(--text)] outline-none" />
          </div>

          {pruneFreq === 'weekly' && (
            <WeekdayPicker value={pruneWeekdays} onChange={setPruneWeekdays} />
          )}

          <div className="space-y-1 text-xs text-[var(--muted)]">
            <label className="flex items-center gap-2"><input type="checkbox" checked={pruneImages} onChange={(e) => setPruneImages(e.target.checked)} className="rounded" />Unused images</label>
            <label className="flex items-center gap-2"><input type="checkbox" checked={pruneBuildCache} onChange={(e) => setPruneBuildCache(e.target.checked)} className="rounded" />Build cache</label>
            <label className="flex items-center gap-2 text-[var(--warning)]"><input type="checkbox" checked={pruneStopped} onChange={(e) => setPruneStopped(e.target.checked)} className="rounded" />Stopped containers</label>
            <label className="flex items-center gap-2 text-[var(--danger)]"><input type="checkbox" checked={pruneVolumes} onChange={(e) => setPruneVolumes(e.target.checked)} className="rounded" />Unused volumes</label>
          </div>

          <ScheduleStatusFooter status={data?.prune.status} onOpenJob={openJob} />
        </div>

        {saveResult && <StatusMessage className={saveResult.type === 'success' ? 'text-xs text-[var(--ok)]' : 'text-xs text-[var(--danger)]'}>{saveResult.text}</StatusMessage>}

        <button onClick={handleSave} disabled={savingSchedules} className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-4 py-2 text-xs text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)] disabled:opacity-40">
          {savingSchedules ? 'Saving...' : 'Save schedules'}
        </button>
      </div>

      {confirmVolumeCleanup && (
        <ConfirmDialog
          title="Enable scheduled volume deletion?"
          message="Unused Docker volumes can contain persistent application data. This cleanup will run automatically without another confirmation."
          items={volumeCleanupSummary}
          confirmLabel="Save volume cleanup"
          onConfirm={() => {
            setConfirmVolumeCleanup(false)
            void saveSchedules()
          }}
          onCancel={() => setConfirmVolumeCleanup(false)}
        />
      )}
    </div>
  )
}
