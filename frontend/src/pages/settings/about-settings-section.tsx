import { useEffect, useState } from 'react'
import { getMeta } from '@/lib/api-client'
import type { MetaResponse } from '@/lib/api-types'
import { SettingsCard } from '@/pages/settings/settings-card'

export function AboutSettingsSection() {
  const [meta, setMeta] = useState<MetaResponse | null>(null)

  useEffect(() => {
    getMeta().then(setMeta).catch(() => {})
  }, [])

  if (!meta) return null

  return (
    <SettingsCard>
      <h2 className="text-sm font-medium text-[var(--text)]">About</h2>
      <div className="mt-3 grid gap-2 text-sm text-[var(--muted)]">
        <div>Stacklab {meta.app.version}</div>
        <div>Docker Engine {meta.docker.engine_version}</div>
        <div>Docker Compose {meta.docker.compose_version}</div>
        <div>Stack root: <code className="font-mono text-[var(--text)]">{meta.environment.stack_root}</code></div>
      </div>
    </SettingsCard>
  )
}
