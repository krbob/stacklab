import { expect, test, type Page } from '@playwright/test'

const source = {
  id: 'database',
  kind: 'service_mount',
  stack_id: 'demo',
  service: 'db',
  mount: '/data',
  path: 'demo_data',
}
const destination = {
  id: 'nas',
  name: 'NAS',
  kind: 'local',
  repository: '/mnt/backup',
  expected_filesystem: '2',
  status: 'available',
  metadata_check: { status: 'unknown', scope: 'unknown' },
  data_check: { status: 'unknown', scope: 'unknown' },
  credentials_configured: true,
  check_schedule: { frequency: 'manual' },
  prune_schedule: { frequency: 'manual' },
  check_read_data: 'metadata',
}
const catalog = {
  items: [
    {
      path: 'record.txt',
      type: 'file',
      size_bytes: 12,
      mode: 384,
      uid: 1000,
      gid: 1000,
      modified_at: '2026-10-01T00:00:00Z',
    },
  ],
  sources: [{ source, original_path: '/data', saved_path: 'sources/database.tar', size_bytes: 1024, archive: true }],
  path: '',
  total: 1,
  offset: 0,
  limit: 50,
  index_available: true,
  transfer_bytes: 1024,
}

async function mockBackupUI(page: Page, waitForSearch?: () => Promise<void>) {
  // UI regression fixtures never submit jobs or mutate repositories on the test host.
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    let body: unknown
    switch (url.pathname) {
      case '/api/session':
        body = { authenticated: true }
        break
      case '/api/meta':
        body = { app: { version: 'test' }, docker: { engine_version: 'test', compose_version: 'test' } }
        break
      case '/api/jobs/active':
        body = { items: [], summary: {} }
        break
      case '/api/backups':
        body = {
          destinations: [destination],
          plans: [],
          runs: [],
          protection: [],
          helper_ready: true,
          helper_configured: true,
          restic_available: true,
          rclone_available: true,
          staging_directory: '/staging',
        }
        break
      case '/api/backups/policy':
        body = { paused: false, effective_paused: false, pause_reasons: [], host_review_required: false, overdue_grace_minutes: 60, heartbeat_configured: false }
        break
      case '/api/backups/coverage':
        body = { stacks: [] }
        break
      case '/api/backups/restores':
        body = { items: [] }
        break
      case '/api/backups/destinations/nas/snapshots':
        body = {
          items: [
            {
              id: 'snapshot',
              short_id: 'snapshot',
              time: '2026-10-01T00:00:00Z',
              hostname: 'stacklab',
              tags: [],
              paths: [],
            },
          ],
        }
        break
      case '/api/backups/destinations/nas/snapshots/snapshot/files':
        if (url.searchParams.get('search')) await waitForSearch?.()
        body = catalog
        break
      case '/api/backups/schedule-preview':
        body = { timezone: 'America/New_York', next_occurrences: ['2026-10-01T06:00:00Z'] }
        break
      default:
        await route.fulfill({
          status: 404,
          json: { error: { message: 'UI fixture does not implement this endpoint' } },
        })
        return
    }
    await route.fulfill({ json: body })
  })
}

test.describe('Backup UI failure regressions', () => {
  test.use({ timezoneId: 'Europe/Warsaw', locale: 'en-US', serviceWorkers: 'block' })

  test('keeps typing focus during an index request and preserves Recovery across tabs', async ({ page }) => {
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    await mockBackupUI(page, () => gate)
    await page.goto('/backups?view=recovery')
    await page.getByLabel('Recovery set', { exact: true }).selectOption('snapshot')
    await page.getByLabel('Snapshot source').selectOption('database')
    const search = page.getByLabel('Search snapshot files')
    const requested = page.waitForRequest((request) => new URL(request.url()).searchParams.get('search') === 'record')
    await search.fill('record')
    await requested
    await expect(search).toBeFocused()
    await page.keyboard.type('2')
    await expect(search).toHaveValue('record2')
    await expect(page).toHaveURL(/\/backups\?view=recovery$/)
    release()
    await page.getByRole('tab', { name: 'History', exact: true }).click()
    await page.getByLabel('Filter by result').selectOption('failed')
    await page.getByRole('tab', { name: 'Recovery', exact: true }).click()
    await expect(page.getByLabel('Recovery set', { exact: true })).toHaveValue('snapshot')
    await expect(search).toHaveValue('record2')
    await page.getByRole('tab', { name: 'History', exact: true }).click()
    await expect(page.getByLabel('Filter by result')).toHaveValue('failed')
  })

  test('shows the schedule zone in a browser with a different timezone', async ({ page }) => {
    await mockBackupUI(page)
    await page.goto('/backups?view=destinations')
    await page.getByRole('button', { name: 'Edit', exact: true }).click()
    await page.getByText('Repository maintenance schedules', { exact: true }).click()
    await page.getByLabel('Repository check schedule', { exact: true }).selectOption('daily')
    await page.getByLabel('Timezone (Repository check schedule)').fill('America/New_York')
    await expect(page.getByText('Next occurrences (America/New_York):')).toBeVisible()
    await expect(page.getByText('10/1/2026, 2:00:00 AM EDT', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Generate password', exact: true })).toHaveCount(0)
  })
})

test.describe('Backup round two recovery boundaries', () => {
  test.use({ timezoneId: 'Europe/Warsaw', locale: 'en-US', serviceWorkers: 'block' })

  test('keeps evidence warnings during activity and refreshes a selected Recovery list', async ({ page }) => {
    await mockBackupUI(page)
    await page.route('**/api/backups', (route) => route.fulfill({ json: {
      destinations: [destination], plans: [{ id: 'plan', name: 'Database plan', enabled: true, consistency: 'stop', sources: [source], destinations: [], staging_max_bytes: 1024 ** 3, staging_ttl_hours: 24, stop_timeout_seconds: 300 }], runs: [],
      protection: [{ plan_id: 'plan', plan_name: 'Database plan', state: 'attention', activity: 'running', current_copies: 0, expected_copies: 1, copies: [{ destination_id: 'nas', state: 'integrity_failed', activity: 'running', evidence_usable: false, snapshot_id: 'snapshot' }] }],
      restic_available: true, rclone_available: true, helper_ready: true, staging_directory: '/staging',
    } }))
    await page.route('**/api/backups/policy', (route) => route.fulfill({ json: { paused: false, effective_paused: true, pause_reasons: [{ code: 'service_recovery_required', message: 'Review interrupted service recovery.' }], host_review_required: false, overdue_grace_minutes: 60, heartbeat_configured: false } }))
    let newSnapshot = false
    await page.route('**/api/backups/destinations/nas/snapshots', (route) => route.fulfill({ json: { items: [
      { id: 'snapshot', short_id: 'snapshot', time: '2026-10-01T00:00:00Z', hostname: 'stacklab', tags: [], paths: [] },
      ...(newSnapshot ? [{ id: 'new-snapshot', short_id: 'new-snapshot', time: '2026-10-02T00:00:00Z', hostname: 'stacklab', tags: [], paths: [] }] : []),
    ] } }))
    await page.goto('/backups?view=protection')
    await expect(page.getByText('Copies needing attention').locator('..')).toContainText('1')
    await expect(page.getByText('A repository integrity check failed.', { exact: false })).toBeVisible()
    await expect(page.getByText('Automatic backups, integrity checks and retention cleanup are paused.', { exact: false })).toBeVisible()
    await page.getByRole('button', { name: 'Recover data', exact: true }).click()
    await page.getByLabel('Recovery set', { exact: true }).selectOption('snapshot')
    newSnapshot = true
    await page.getByRole('button', { name: 'Refresh', exact: true }).click()
    await expect(page.getByLabel('Recovery set', { exact: true }).getByRole('option', { name: /new-snapshot/ })).toHaveCount(1)
    await expect(page.getByLabel('Recovery set', { exact: true })).toHaveValue('snapshot')
  })

  for (const viewport of [{ name: 'desktop', width: 1440, height: 1000 }, { name: 'mobile', width: 390, height: 844 }]) {
    test(`requires an explicit safe recovery choice for exclusions on ${viewport.name}`, async ({ page }) => {
      await page.setViewportSize(viewport)
      await mockBackupUI(page)
      await page.route('**/api/backups/restores', (route) => route.fulfill({ json: { items: [{ id: 'restore', recovery_status: 'complete', destination_id: 'nas', snapshot_id: 'snapshot', target: '/private/restore', available: true, test: false, created_at: '2026-10-01T12:00:00Z', source_ids: ['database'], manifest: { version: 1, run_id: 'run', plan_id: 'plan', consistency: 'stop', captured_at: '2026-10-01T00:00:00Z', stacklab_version: 'test', sources: [{ ...catalog.sources[0], source: { ...source, excludes: ['private-uploads'] } }] } }] } }))
      await page.route('**/api/backups/restores/restore/files?**', (route) => route.fulfill({ json: catalog }))
      await page.goto('/backups?view=recovery')
      await page.getByLabel('Completed isolated restore').selectOption('restore')
      await page.getByRole('button', { name: 'Review applying recovered data' }).click()
      await page.getByRole('checkbox', { name: /db → \/data/ }).check()
      await page.getByRole('button', { name: 'Use original location' }).click()
      await expect(page.getByLabel('Recovery mode for db')).toHaveValue('replace')
      await expect(page.getByRole('button', { name: 'Preview recovery' })).toBeDisabled()
      await expect(page.getByText('Replacement is blocked', { exact: false })).toBeVisible()
      await page.getByLabel('Recovery mode for db').selectOption('merge')
      await expect(page.getByRole('button', { name: 'Preview recovery' })).toBeEnabled()
      expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(false)
    })
  }
})


test.describe('Backup round three freshness and action boundaries', () => {
  test.use({ timezoneId: 'Europe/Warsaw', locale: 'en-US', serviceWorkers: 'block' })

  test('discovers scheduled copies while idle and explains a removed selection', async ({ page }) => {
    await mockBackupUI(page)
    await page.clock.install()
    let finished = false
    let removed = false
    let listings = 0
    await page.route('**/api/backups', (route) => route.fulfill({ json: {
      destinations: [destination], plans: [], runs: finished ? [{ id: 'scheduled', job_id: 'scheduled-job', plan_id: 'plan', plan_name: 'Database', state: 'running', capture_state: 'succeeded', phase: 'upload', created_at: '2026-10-01T00:00:00Z', expires_at: '2026-10-02T00:00:00Z', copies: [
        { destination_id: 'nas', state: 'succeeded', snapshot_id: 'new', verification: 'recovery_objects_verified', retention: 'succeeded' },
        { destination_id: 'cloud', state: 'running', verification: 'pending', retention: 'pending' },
      ] }] : [], protection: [], helper_ready: true, helper_configured: true, restic_available: true, rclone_available: true, staging_directory: '/staging',
    } }))
    await page.route('**/api/backups/destinations/nas/snapshots', (route) => {
      listings += 1
      return route.fulfill({ json: { items: [
        ...(!removed ? [{ id: 'snapshot', short_id: 'snapshot', time: '2026-10-01T00:00:00Z', hostname: 'host', tags: [], paths: [], recovery_status: 'complete' }] : []),
        ...(finished ? [{ id: 'new', short_id: 'new', time: '2026-10-02T00:00:00Z', hostname: 'host', tags: [], paths: [], recovery_status: 'complete' }] : []),
      ] } })
    })
    await page.goto('/backups?view=recovery')
    await page.getByLabel('Recovery set', { exact: true }).selectOption('snapshot')
    const initialListings = listings
    finished = true
    await page.clock.fastForward(60000)
    await expect(page.getByRole('option', { name: /new/ })).toHaveCount(1)
    expect(listings).toBe(initialListings + 1)
    await expect(page.getByLabel('Recovery set', { exact: true })).toHaveValue('snapshot')
    removed = true
    await page.getByRole('button', { name: 'Refresh snapshots', exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: 'The selected snapshot snapshot is no longer' })).toBeVisible()
    await expect(page.getByLabel('Recovery set', { exact: true })).toHaveValue('snapshot')
    const refreshedListings = listings
    await page.getByRole('tab', { name: 'Destinations', exact: true }).click()
    await page.getByRole('tab', { name: 'Recovery', exact: true }).click()
    expect(listings).toBe(refreshedListings)
    await expect(page.getByText('The selected snapshot snapshot is no longer', { exact: false })).toBeVisible()
  })

  for (const viewport of [{ name: 'desktop', width: 1440, height: 1000 }, { name: 'mobile', width: 390, height: 844 }]) {
    test(`blocks capture and apply but keeps isolated retrieval on ${viewport.name}`, async ({ page }) => {
      await page.setViewportSize(viewport)
      await mockBackupUI(page)
      await page.route('**/api/backups', (route) => route.fulfill({ json: {
        destinations: [destination], plans: [{ id: 'plan', name: 'Database', enabled: true, consistency: 'stop', sources: [source], destinations: [], staging_max_bytes: 1024 ** 3, staging_ttl_hours: 24, stop_timeout_seconds: 300 }], runs: [], protection: [], helper_ready: true, helper_configured: true, recovery_error: 'Interrupted service recovery', restic_available: true, rclone_available: true, staging_directory: '/staging',
      } }))
      await page.route('**/api/backups/restores', (route) => route.fulfill({ json: { items: [{ id: 'restore', recovery_status: 'complete', destination_id: 'nas', snapshot_id: 'snapshot', target: '/private/restore', available: true, test: true, created_at: '2026-10-01T12:00:00Z', source_ids: ['database'], manifest: { version: 1, run_id: 'run', plan_id: 'plan', consistency: 'stop', captured_at: '2026-10-01T00:00:00Z', stacklab_version: 'test', sources: catalog.sources } }] } }))
      await page.route('**/api/backups/restores/restore/files?**', (route) => route.fulfill({ json: catalog }))
      await page.goto('/backups?view=plans')
      await expect(page.getByRole('button', { name: 'Back up now', exact: true })).toBeDisabled()
      await expect(page.getByText('New captures are blocked until', { exact: false })).toBeVisible()
      await page.getByRole('tab', { name: 'Recovery', exact: true }).click()
      await page.getByLabel('Recovery set', { exact: true }).selectOption('snapshot')
      await expect(page.getByRole('button', { name: 'Retrieve complete set for offline recovery' })).toBeEnabled()
      await page.getByLabel('Completed isolated restore').selectOption('restore')
      await expect(page.getByRole('button', { name: 'Review applying recovered data' })).toBeDisabled()
      await expect(page.getByRole('button', { name: 'Review applying recovered data' })).toHaveAccessibleDescription('Interrupted service recovery')
      expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(false)
    })
  }
})


test.describe('Backup round four repository decisions', () => {
  test('requires cleanup risk acknowledgment and defaults recovery checks to all data', async ({ page }) => {
    await mockBackupUI(page)
    await page.route('**/api/backups', route => route.fulfill({ json: {
      destinations: [{ ...destination, maintenance_review_token: 'proof-a', data_check: { status: 'failed', scope: 'all', error: 'Damaged pack' }, last_check_attempt_error: 'Damaged pack', check_error: 'Damaged pack' }], plans: [], runs: [], protection: [], helper_ready: true, helper_configured: true, restic_available: true, rclone_available: true, staging_directory: '/staging',
    } }))
    await page.goto('/backups?view=destinations')
    await expect(page.getByText('Damaged pack', { exact: true })).toHaveCount(1)
    await page.getByRole('button', { name: 'Check repository', exact: true }).click()
    await expect(page.getByLabel('Data-read policy')).toHaveValue('all')
    await page.getByLabel('Data-read policy').selectOption('metadata')
    await expect(page.getByRole('dialog')).toContainText('data-failure evidence unchanged')
    await page.getByRole('button', { name: 'Cancel', exact: true }).click()
    await page.getByRole('button', { name: 'Run retention cleanup', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Start cleanup', exact: true })).toBeDisabled()
    await page.getByRole('textbox').fill('NAS')
    await expect(page.getByRole('button', { name: 'Start cleanup', exact: true })).toBeEnabled()
    await page.getByRole('button', { name: 'Cancel', exact: true }).click()
    await page.getByRole('button', { name: 'Remove stale locks', exact: true }).click()
    await expect(page.getByRole('dialog')).toContainText('refuses live, uncertain or foreign-host owners')
    await expect(page.getByRole('dialog')).toContainText('at least 30 minutes old')
  })

  test('refreshes a raced cleanup review and sends the newly acknowledged fingerprint on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await mockBackupUI(page)
    let changed = false
    const requests: unknown[] = []
    await page.route('**/api/backups', route => route.fulfill({ json: {
      destinations: [{ ...destination, ...(changed ? { maintenance_review_token: 'proof-b', check_hold: { scope: 'metadata', error: 'Index discovery was inconclusive', checked_at: '2026-10-15T09:00:00Z' } } : {}) }], plans: [], runs: [], protection: [], helper_ready: true, helper_configured: true, restic_available: true, rclone_available: true, staging_directory: '/staging',
    } }))
    await page.route('**/api/backups/destinations/nas/prune', async route => {
      requests.push(route.request().postDataJSON())
      changed = true
      await route.fulfill({ status: 409, json: { error: { code: requests.length === 1 ? 'maintenance_acknowledgement_required' : 'busy', message: requests.length === 1 ? 'Raw acknowledgement contract' : 'Repository busy' } } })
    })
    await page.goto('/backups?view=destinations')
    await page.getByRole('button', { name: 'Run retention cleanup', exact: true }).click()
    await page.getByRole('button', { name: 'Start cleanup', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toContainText('Review the latest evidence and confirm')
    await expect(dialog.getByRole('alert').filter({ hasText: 'Cleanup can remove recovery material' })).toContainText('Index discovery was inconclusive')
    await expect(dialog.getByRole('button', { name: 'Start cleanup', exact: true })).toBeDisabled()
    await dialog.getByRole('textbox').fill('NAS')
    await dialog.getByRole('button', { name: 'Start cleanup', exact: true }).click()
    await expect(dialog).toContainText('Repository busy')
    expect(requests).toEqual([{}, { acknowledge_integrity_failure: true, acknowledgement_review_token: 'proof-b' }])
    expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(false)
  })
})

test.describe('Backup review polling interaction', () => {
  for (const width of [1440, 390]) {
    test(`preserves typing and focus during polling, but resets changed evidence at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 })
      await page.clock.install()
      await mockBackupUI(page)
      let token = 'proof-a'
      let release: (() => void) | undefined
      let requested: (() => void) | undefined
      let gate: Promise<void> | undefined
      await page.route('**/api/backups', async route => {
        requested?.()
        await gate
        await route.fulfill({ json: {
          destinations: [{ ...destination, maintenance_review_token: token, check_hold: { scope: 'all', error: 'Review repository evidence' } }], plans: [], runs: [], protection: [], helper_ready: true, helper_configured: true, restic_available: true, rclone_available: true, staging_directory: '/staging',
        } })
      })
      await page.goto('/backups?view=destinations')
      await page.getByRole('button', { name: 'Run retention cleanup', exact: true }).click()
      const input = page.getByLabel('Type NAS to confirm', { exact: true })
      await expect(input).toBeFocused()
      await expect(input).toBeEditable()
      await input.fill('NA')
      for (let tick = 0; tick < 2; tick++) {
        gate = new Promise<void>(resolve => { release = resolve })
        const request = new Promise<void>(resolve => { requested = resolve })
        await page.clock.runFor(5000)
        await request
        await expect(input).toBeFocused()
        await expect(input).toBeEditable()
        await expect(page.getByText('Refreshing repository evidence. Cleanup is disabled until this finishes.')).toHaveCount(0)
        if (tick === 0) await page.keyboard.type('S')
        await expect(input).toHaveValue('NAS')
        if (tick === 1) token = 'proof-b'
        release!()
        gate = undefined
        if (tick === 0) await expect(page.getByRole('button', { name: 'Start cleanup' })).toBeEnabled()
      }
      await expect(input).toHaveValue('')
      await expect(input).toBeFocused()
      await expect(page.getByRole('dialog')).toContainText('Typed confirmation was reset')
      await expect(page.getByRole('button', { name: 'Start cleanup' })).toBeDisabled()
    })
  }
})
