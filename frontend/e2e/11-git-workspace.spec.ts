import { test, expect, type Page } from '@playwright/test'
import { login } from './helpers'

const BASE_URL = process.env.STACKLAB_E2E_URL ?? 'http://127.0.0.1:18081'
const CONFIG_PATH = 'demo/.gitkeep'
const GIT_PATH = `config/${CONFIG_PATH}`
const COMMIT_MESSAGE = 'Update E2E config fixture'

async function getConfigFile(page: Page) {
  const response = await page.request.get(
    `${BASE_URL}/api/config/workspace/file?path=${encodeURIComponent(CONFIG_PATH)}`,
  )
  expect(response.ok()).toBeTruthy()
  return await response.json() as { content: string | null; modified_at: string }
}

async function saveConfigFile(page: Page, content: string, expectedModifiedAt?: string) {
  const response = await page.request.put(`${BASE_URL}/api/config/workspace/file`, {
    data: {
      path: CONFIG_PATH,
      content,
      create_parent_directories: false,
      expected_modified_at: expectedModifiedAt,
    },
  })
  expect(response.ok()).toBeTruthy()
}

async function restoreDirtyFixture(page: Page, baselineContent: string) {
  const statusResponse = await page.request.get(`${BASE_URL}/api/git/workspace/status`)
  if (!statusResponse.ok()) return

  const status = await statusResponse.json() as { items?: Array<{ path: string }> }
  if (status.items?.some((item) => item.path === GIT_PATH)) {
    await saveConfigFile(page, baselineContent)
  }
}

test.describe('Git workspace', () => {
  let baselineContent = ''
  let updatedContent = ''

  test.beforeEach(async ({ page }) => {
    await login(page)

    const original = await getConfigFile(page)
    baselineContent = original.content ?? ''
    updatedContent = baselineContent === 'stacklab-e2e-git-a\n'
      ? 'stacklab-e2e-git-b\n'
      : 'stacklab-e2e-git-a\n'
    await saveConfigFile(page, updatedContent, original.modified_at)
  })

  test.afterEach(async ({ page }) => {
    await restoreDirtyFixture(page, baselineContent)
  })

  test('reviews, commits, and pushes a config change', async ({ page }) => {
    await page.goto('/config')
    await page.getByRole('button', { name: /^Changes/ }).click()

    await expect(page.getByText('main', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'demo (1)', exact: true })).toBeVisible()

    await page.getByRole('button', { name: /\.gitkeep$/ }).click()
    await expect(page.getByText(`+${updatedContent.trimEnd()}`, { exact: true })).toBeVisible()

    await page.getByRole('button', { name: 'demo (1)', exact: true }).click()
    await page.getByRole('button', { name: 'Commit', exact: true }).click()
    await page.getByTestId('git-commit-message').fill(COMMIT_MESSAGE)

    // The desktop sidebar is narrower than the default text input plus buttons.
    // Check actual layout before Playwright can scroll a clipped submit into view.
    await page.evaluate(() => document.fonts.ready)
    const commitLayout = await page.getByTestId('git-commit-message').evaluate((input) => {
      const form = input.closest('form')!
      const bar = form.parentElement!
      const bounds = bar.getBoundingClientRect()
      const controls = Array.from(form.querySelectorAll('input, button'))
      return {
        overflow: bar.scrollWidth - bar.clientWidth,
        controlsFit: controls.every((control) => {
          const rect = control.getBoundingClientRect()
          return rect.left >= bounds.left - 1 && rect.right <= bounds.right + 1
        }),
      }
    })
    expect(commitLayout.overflow).toBeLessThanOrEqual(1)
    expect(commitLayout.controlsFit).toBe(true)

    const commitResponse = page.waitForResponse((response) =>
      response.url().endsWith('/api/git/workspace/commit')
      && response.request().method() === 'POST',
    )
    await page.getByTestId('git-commit-submit').click()

    const commitResult = await commitResponse
    expect(commitResult.ok()).toBeTruthy()
    expect(commitResult.request().postDataJSON()).toEqual({
      message: COMMIT_MESSAGE,
      paths: [GIT_PATH],
    })
    expect(await commitResult.json()).toMatchObject({
      committed: true,
      summary: COMMIT_MESSAGE,
      paths: [GIT_PATH],
      remaining_changes: 0,
    })

    await expect(page.getByText('Working tree clean', { exact: true })).toBeVisible()
    await expect(page.getByTestId('git-push')).toHaveText('Push (1 ahead)')

    const pushResponse = page.waitForResponse((response) =>
      response.url().endsWith('/api/git/workspace/push')
      && response.request().method() === 'POST',
    )
    await page.getByTestId('git-push').click()

    const pushResult = await pushResponse
    expect(pushResult.ok()).toBeTruthy()
    expect(await pushResult.json()).toMatchObject({
      pushed: true,
      remote: 'origin',
      branch: 'main',
      upstream_name: 'origin/main',
      ahead_count: 0,
      behind_count: 0,
    })

    await expect(page.getByTestId('git-push')).not.toBeVisible()
    await expect.poll(async () => {
      const response = await page.request.get(`${BASE_URL}/api/git/workspace/status`)
      if (!response.ok()) return { status: response.status() }
      const status = await response.json()
      return {
        available: status.available,
        clean: status.clean,
        hasUpstream: status.has_upstream,
        upstream: status.upstream_name,
        ahead: status.ahead_count ?? 0,
        behind: status.behind_count ?? 0,
        items: status.items ?? [],
      }
    }).toEqual({
      available: true,
      clean: true,
      hasUpstream: true,
      upstream: 'origin/main',
      ahead: 0,
      behind: 0,
      items: [],
    })
  })
})

test('deletes stack backups absent from Files on desktop and mobile', async ({ page }) => {
  await login(page)
  for (const [name, width] of [['compose.yaml.bak-20260806T1108', 1280], ['compose.yaml.bak-pre-65ec012', 390]] as const) {
    const path = `stacks/demo/${name}`
    const created = await page.request.put('/api/stacks/demo/workspace/file', {
      data: { path: name, content: '# obsolete backup\n', create_parent_directories: false },
    })
    expect(created.ok()).toBeTruthy()
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/config')
    if (width < 1024) await page.getByTestId('config-open-tree').click()
    await expect(page.getByRole('button', { name: new RegExp(`${name}$`) })).toHaveCount(0)
    await page.getByRole('button', { name: /^Changes/ }).click()
    await page.getByRole('button', { name: new RegExp(`${name}$`) }).click()
    const remove = page.getByRole('button', { name: 'Delete file', exact: true })
    await expect(remove).toBeVisible()
    const bounds = await remove.boundingBox()
    expect(bounds).not.toBeNull()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width)
    await remove.click()
    const dialog = page.getByRole('dialog', { name: `Delete "${name}"?` })
    await expect(dialog.getByText(path, { exact: true })).toBeVisible()
    await expect(dialog.getByText(/This file is untracked/)).toBeVisible()
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
    expect((await page.request.get(`/api/stacks/demo/workspace/file?path=${name}`)).ok()).toBeTruthy()
    await remove.click()
    const deletion = page.waitForResponse((response) => response.url().endsWith('/api/git/workspace/file') && response.request().method() === 'DELETE')
    await dialog.getByRole('button', { name: 'Delete file', exact: true }).click()
    expect((await deletion).ok()).toBeTruthy()
    await expect(dialog).not.toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: `Deleted ${path}` })).toBeVisible()
    const statusResponse = await page.request.get('/api/git/workspace/status')
    expect(statusResponse.ok()).toBeTruthy()
    const status = await statusResponse.json()
    expect(status.available).toBe(true)
    expect((status.items ?? []).some((item: { path: string }) => item.path === path)).toBe(false)
    expect((await page.request.get(`/api/stacks/demo/workspace/file?path=${name}`)).status()).toBe(404)
  }
})
