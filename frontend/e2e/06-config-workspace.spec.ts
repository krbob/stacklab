import { test, expect } from '@playwright/test'
import { createStackViaApi, deleteStackViaApi, login } from './helpers'

const STACK_ID = 'e2e-config'
const COMPOSE = `services:
  app:
    image: alpine:latest
`

test.describe('Config Workspace', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
    await createStackViaApi(page, STACK_ID, COMPOSE)
  })

  test.afterEach(async ({ page }) => {
    await deleteStackViaApi(page, STACK_ID)
  })

  test('browses config tree and opens a file', async ({ page }) => {
    const cookies = await page.context().cookies()
    const sessionCookie = cookies.find((c) => c.name.startsWith('stacklab'))
    const headers = sessionCookie ? { Cookie: `${sessionCookie.name}=${sessionCookie.value}` } : {}
    const baseURL = process.env.STACKLAB_E2E_URL ?? 'http://127.0.0.1:18081'

    await page.request.put(`${baseURL}/api/config/workspace/file`, {
      data: { path: `${STACK_ID}/test.conf`, content: 'listen 8080;\n', create_parent_directories: true },
      headers,
    })

    await page.goto('/config')

    // Navigate into stack directory
    await page.getByRole('button', { name: STACK_ID }).click()

    // Open file
    await page.getByRole('button', { name: 'test.conf' }).click()

    // Editor should show content
    await expect(page.locator('.cm-content')).toBeVisible({ timeout: 10_000 })
    await expect(page.locator('.cm-content')).toContainText('listen 8080')
  })

  test('saves a config file and sees audit entry', async ({ page }) => {
    const cookies = await page.context().cookies()
    const sessionCookie = cookies.find((c) => c.name.startsWith('stacklab'))
    const headers = sessionCookie ? { Cookie: `${sessionCookie.name}=${sessionCookie.value}` } : {}
    const baseURL = process.env.STACKLAB_E2E_URL ?? 'http://127.0.0.1:18081'

    await page.request.put(`${baseURL}/api/config/workspace/file`, {
      data: { path: `${STACK_ID}/editable.conf`, content: 'original;\n', create_parent_directories: true },
      headers,
    })

    await page.goto('/config')

    // Navigate to file
    await page.getByRole('button', { name: STACK_ID }).click()
    await page.getByRole('button', { name: 'editable.conf' }).click()

    // Wait for editor
    await expect(page.locator('.cm-content')).toBeVisible({ timeout: 10_000 })

    // Modify content
    const editor = page.locator('.cm-content')
    await editor.click()
    await page.keyboard.press('Meta+a')
    await page.keyboard.type('modified;\n')

    // Save
    await page.getByTestId('config-save').click()
    await expect(page.getByText('Saved')).toBeVisible({ timeout: 10_000 })

    // Check audit — use .first() since multiple entries may exist
    await page.goto('/audit')
    await expect(page.getByText('save_config_file').first()).toBeVisible({ timeout: 10_000 })
  })

  test('creates a new file in config workspace', async ({ page }) => {
    const cookies = await page.context().cookies()
    const sessionCookie = cookies.find((c) => c.name.startsWith('stacklab'))
    const headers = sessionCookie ? { Cookie: `${sessionCookie.name}=${sessionCookie.value}` } : {}
    const baseURL = process.env.STACKLAB_E2E_URL ?? 'http://127.0.0.1:18081'

    // Ensure directory exists
    await page.request.put(`${baseURL}/api/config/workspace/file`, {
      data: { path: `${STACK_ID}/.gitkeep`, content: '', create_parent_directories: true },
      headers,
    })

    await page.goto('/config')
    await page.getByRole('button', { name: STACK_ID }).click()

    // Click New file
    await page.getByRole('button', { name: 'New file' }).click()

    // Type filename and press Enter
    const input = page.getByPlaceholder('filename')
    await input.fill('new-config.yml')
    await input.press('Enter')

    // Should open the new file — check heading specifically
    await expect(page.getByRole('heading', { name: 'new-config.yml' })).toBeVisible({ timeout: 10_000 })
    await expect(page.locator('.cm-content')).toBeVisible()
  })
})
