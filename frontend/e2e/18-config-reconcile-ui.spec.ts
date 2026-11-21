import { expect, test, type Page } from '@playwright/test'

const path = 'config/demo/application-with-a-long-configuration-name.conf'
const files = [{ path, status: 'M', stack_id: 'demo', managed: true }]
const incoming = { available: true, branch: 'main', upstream_name: 'origin/main', head_commit: 'a'.repeat(40), target_commit: 'b'.repeat(40), ahead_count: 1, behind_count: 1, local_changes: 0, can_pull: false, can_reconcile: true, blocked_reason: 'diverged', expected_state: 'c'.repeat(64), checked_at: '2026-11-21T12:00:00Z', files }
const version = (text: string) => ({ exists: true, mode: '100644', size: text.length, text })
const receipt = { branch: 'main', base_commit: incoming.head_commit, head_commit: 'd'.repeat(40), downloaded_at: incoming.checked_at, files }

async function fixture(page: Page, rejectApply = false) {
  let prepared = false
  let resolved = false
  let applied = false
  let fetches = 0
  let deployments = 0
  const draft = () => ({ id: 'merge-draft', revision: resolved ? 2 : 1, expected_state: incoming.expected_state, branch: 'main', local_commit: incoming.head_commit, remote_commit: incoming.target_commit, created_at: incoming.checked_at, files, conflicts: [{ path, resolved }], ready: resolved, applied })
  await page.route('**/api/**', async (route) => {
    const endpoint = new URL(route.request().url()).pathname
    let json: unknown
    switch (endpoint) {
      case '/api/session': json = { authenticated: true }; break
      case '/api/meta': json = { app: { version: 'test' }, docker: { engine_version: 'test', compose_version: 'test' } }; break
      case '/api/jobs/active': json = { items: [], summary: {} }; break
      case '/api/config/workspace/tree': json = { workspace_root: '/srv/homelab/config', current_path: '', parent_path: null, items: [] }; break
      case '/api/stacks': json = { items: [{ id: 'demo', runtime_state: 'running', config_state: 'in_sync', activity_state: 'idle' }], summary: {} }; break
      case '/api/git/workspace/incoming': fetches++; json = applied ? { ...incoming, head_commit: receipt.head_commit, behind_count: 0, ahead_count: 2, can_reconcile: false, expected_state: 'new', blocked_reason: 'up_to_date', last_pull: receipt, files: [] } : incoming; break
      case '/api/git/workspace/reconcile': json = { draft: prepared ? draft() : null }; break
      case '/api/git/workspace/reconcile/prepare':
        expect(route.request().postDataJSON()).toEqual({ expected_state: incoming.expected_state })
        prepared = true; json = draft(); break
      case '/api/git/workspace/reconcile/file': json = { path, base: version('port=8080\n'), local: version('port=8081\n'), remote: version('port=9090\n'), editable: true, choice: resolved ? 'edit' : '', content: resolved ? 'port=9090\nlocal=on\n' : '', diff: resolved ? '@@ -1 +1,2 @@\n-port=8081\n+port=9090\n+local=on' : '', truncated: false }; break
      case '/api/git/workspace/reconcile/resolve':
        expect(route.request().postDataJSON()).toEqual({ id: 'merge-draft', revision: 1, path, choice: 'edit', content: 'port=9090\nlocal=on\n' })
        resolved = true; json = draft(); break
      case '/api/git/workspace/reconcile/apply':
        expect(route.request().postDataJSON()).toEqual({ id: 'merge-draft', revision: 2 })
        if (rejectApply) { await route.fulfill({ status: 409, json: { error: { code: 'draft_stale', message: 'New local edits invalidate this draft. Preserve them before preparing another.' } } }); return }
        applied = true; json = { updated: true, receipt }; break
      default:
        if (endpoint.includes('/actions/')) deployments++
        await route.fulfill({ status: 404, json: { error: { message: 'Not part of this fixture' } } }); return
    }
    await route.fulfill({ json })
  })
  return { fetches: () => fetches, deployments: () => deployments }
}

test.describe('Proactive Git checks and conflict resolution', () => {
  for (const width of [1440, 390, 320]) {
    test(`checks Config and reconciles without deployment at ${width}px`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: 1000 })
      const state = await fixture(page)
      await page.goto('/config')
      await expect(page.getByRole('region', { name: 'Git synchronization' })).toContainText('reconciliation needed')
      expect(state.fetches()).toBe(1)
      await page.evaluate(() => window.dispatchEvent(new Event('focus')))
      expect(state.fetches()).toBe(1)
      await page.getByRole('link', { name: 'Remote updates →' }).click()
      await page.getByRole('button', { name: 'Prepare reconciliation' }).click()
      const panel = page.getByRole('region', { name: 'Reconcile local and remote changes' })
      await panel.getByRole('button', { name: /application-with-a-long-configuration/ }).click()
      await expect(panel.getByRole('textbox', { name: 'Edited merge result' })).toHaveValue('port=8081\n')
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath('merge-conflict.png'), fullPage: true })
      await panel.getByRole('textbox', { name: 'Edited merge result' }).fill('port=9090\nlocal=on\n')
      await panel.getByRole('button', { name: 'Save edited result' }).click()
      await expect(panel).toContainText('All conflicts resolved')
      await page.reload()
      await expect(panel).toContainText('Saved draft revision 2')
      await panel.getByRole('button', { name: /application-with-a-long-configuration/ }).click()
      await expect(panel.getByText('+local=on', { exact: true })).toBeVisible()
      await panel.getByRole('button', { name: 'Review and apply merge' }).click()
      const dialog = page.getByRole('dialog')
      await expect(dialog).toContainText('Mounted configuration may react immediately')
      await dialog.getByRole('button', { name: 'Apply merged configuration', exact: true }).click()
      await expect(panel).toContainText('This merge was applied')
      await expect(page.getByRole('button', { name: 'Reload configuration', exact: true })).toBeEnabled()
      expect(state.deployments()).toBe(0)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath('merge-applied.png'), fullPage: true })
    })
  }

  test('retains an approval error and the saved resolutions', async ({ page }) => {
    const state = await fixture(page, true)
    await page.goto('/config/remote')
    await page.getByRole('button', { name: 'Prepare reconciliation' }).click()
    const panel = page.getByRole('region', { name: 'Reconcile local and remote changes' })
    await panel.getByRole('button', { name: /application-with-a-long-configuration/ }).click()
    await panel.getByRole('textbox', { name: 'Edited merge result' }).fill('port=9090\nlocal=on\n')
    await panel.getByRole('button', { name: 'Save edited result' }).click()
    await panel.getByRole('button', { name: 'Review and apply merge' }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Apply merged configuration', exact: true }).click()
    await expect(page.getByRole('dialog')).toContainText('New local edits invalidate this draft')
    expect(state.deployments()).toBe(0)
    await page.reload()
    await expect(panel).toContainText('Saved draft revision 2')
  })
})
