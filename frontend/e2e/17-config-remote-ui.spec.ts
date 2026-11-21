import { expect, test, type Page } from '@playwright/test'

const review = {
  available: true, branch: 'main', upstream_name: 'origin/main', head_commit: 'a'.repeat(40), target_commit: 'b'.repeat(40),
  ahead_count: 0, behind_count: 1, checked_at: '2026-11-21T12:00:00Z', can_pull: true, expected_state: 'c'.repeat(64),
  files: [{ path: 'config/demo/application-with-a-long-configuration-name.conf', status: 'M', stack_id: 'demo', managed: true }, { path: 'README.md', status: 'M', stack_id: null, managed: false }],
}
const receipt = { branch: 'main', base_commit: review.head_commit, head_commit: review.target_commit, downloaded_at: review.checked_at, files: review.files }

async function fixture(page: Page, rejectDownload = false) {
  let downloaded = false
  let reloads = 0
  await page.route('**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    let json: unknown
    switch (path) {
      case '/api/session': json = { authenticated: true }; break
      case '/api/meta': json = { app: { version: 'test' }, docker: { engine_version: 'test', compose_version: 'test' } }; break
      case '/api/jobs/active': json = { items: [], summary: {} }; break
      case '/api/stacks': json = { items: [{ id: 'demo', runtime_state: 'running', config_state: 'in_sync', activity_state: 'idle' }], summary: {} }; break
      case '/api/git/workspace/reconcile': json = { draft: null }; break
      case '/api/git/workspace/incoming': json = downloaded ? { ...review, can_pull: false, behind_count: 0, files: [], blocked_reason: 'up_to_date', last_pull: receipt } : review; break
      case '/api/git/workspace/incoming/diff':
        expect(route.request().postDataJSON().expected_state).toBe(review.expected_state)
        json = { path: review.files[0].path, diff: '@@ -1 +1 @@\n-listen=8080\n+listen=9090', truncated: false }; break
      case '/api/git/workspace/pull':
        expect(route.request().postDataJSON()).toEqual({ expected_state: review.expected_state })
        if (rejectDownload) { await route.fulfill({ status: 409, json: { error: { code: 'edit_conflict', message: 'The reviewed commits changed. Check remote updates again.' } } }); return }
        downloaded = true; json = { updated: true, receipt }; break
      case '/api/stacks/demo/actions/recreate': reloads++; json = { job: { id: 'config-reload', state: 'running', action: 'recreate' } }; break
      case '/api/jobs/config-reload': json = { job: { id: 'config-reload', state: 'succeeded', action: 'recreate', stack_id: 'demo' } }; break
      case '/api/jobs/config-reload/events': json = { items: [] }; break
      default: await route.fulfill({ status: 404, json: { error: { message: 'Not part of this UI fixture' } } }); return
    }
    await route.fulfill({ json })
  })
  return { reloads: () => reloads }
}

test.describe('Remote configuration review', () => {
  for (const width of [1440, 390, 320]) {
    test(`reviews, downloads and reloads configuration at ${width}px`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: 1000 })
      const state = await fixture(page)
      await page.goto('/config/remote')
      await expect(page.getByRole('heading', { name: 'Remote updates', exact: true })).toBeVisible()
      await page.getByRole('button', { name: /application-with-a-long-configuration/ }).click()
      await expect(page.getByText('+listen=9090', { exact: true })).toBeVisible()
      await page.evaluate(() => document.fonts.ready)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath('remote-review.png'), fullPage: true })
      await page.getByRole('button', { name: 'Download reviewed changes' }).click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.getByText('README.md', { exact: true })).toBeVisible()
      await dialog.getByRole('button', { name: 'Download configuration', exact: true }).click()
      const reload = page.getByRole('button', { name: 'Reload configuration', exact: true })
      await expect(reload).toBeEnabled()
      expect(state.reloads()).toBe(0)
      await page.reload()
      await expect(reload).toBeEnabled()
      await reload.click()
      await expect(dialog).toContainText('Running services will be interrupted')
      await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
      expect(state.reloads()).toBe(0)
      await reload.click()
      await dialog.getByRole('button', { name: 'Apply configuration', exact: true }).click()
      await expect(page.getByText('Succeeded', { exact: true })).toBeVisible()
      expect(state.reloads()).toBe(1)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath('configuration-applied.png'), fullPage: true })
    })
  }

  test('keeps stale review errors visible and never automatically deploys', async ({ page }) => {
    const state = await fixture(page, true)
    await page.goto('/config/remote')
    await page.getByRole('button', { name: 'Download reviewed changes' }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Download configuration', exact: true }).click()
    await expect(page.getByRole('dialog')).toContainText('The reviewed commits changed.')
    expect(state.reloads()).toBe(0)
    await expect(page.getByRole('button', { name: 'Reload configuration', exact: true })).toHaveCount(0)
  })
})
