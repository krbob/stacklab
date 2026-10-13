import type { BackupPlan, BackupSource } from '@/lib/backup-api'

export function sourceCoverage(source: BackupSource, plans: BackupPlan[]) {
  const selected = plans.flatMap((plan) =>
    plan.sources
      .filter(
        (item) =>
          item.id === source.id ||
          (source.kind === 'service_mount' &&
            item.kind === 'service_mount' &&
            source.path &&
            item.path === source.path),
      )
      .map((item) => ({ plan, source: item })),
  )
  const active = selected.filter(
    ({ plan }) => plan.enabled && plan.destinations.some((destination) => destination.enabled),
  )
  const current = active.filter((selection) => selection.source.path === source.path)
  return {
    selected,
    state:
      current.length > 0
        ? 'configured'
        : active.length > 0
          ? source.kind === 'managed_config'
            ? 'partial'
            : 'source_changed'
          : selected.length > 0
            ? 'disabled'
            : 'unconfigured',
  }
}
