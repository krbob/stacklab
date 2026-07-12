import { test, expect, type Page } from '@playwright/test'
import { login } from './helpers'

const BASE_URL = process.env.STACKLAB_E2E_URL ?? 'http://127.0.0.1:18081'
const STACK_ID = 'demo'
const FILE_PATH = 'config/app.yaml'
const ORIGINAL_CONTENT = 'port: 8080\nmode: development\n'
const UPDATED_CONTENT = 'port: 9090\nmode: production\n'

async function saveFixtureFile(page: Page, content: string) {
  const cookies = await page.context().cookies()
  const sessionCookie = cookies.find((cookie) => cookie.name.startsWith('stacklab'))
  const headers = sessionCookie ? { Cookie: `${sessionCookie.name}=${sessionCookie.value}` } : {}
  const response = await page.request.put(
    `${BASE_URL}/api/stacks/${STACK_ID}/workspace/file`,
    {
      data: {
        path: FILE_PATH,
        content,
        create_parent_directories: false,
      },
      headers,
    },
  )

  expect(response.ok()).toBeTruthy()
}

async function openFixtureFile(page: Page) {
  await page.goto(`/stacks/${STACK_ID}/files`)
  await page.getByRole('button', { name: 'config', exact: true }).click()
  await page.getByRole('button', { name: 'app.yaml', exact: true }).click()
  await expect(page.locator('.cm-content')).toBeVisible()
}

test.describe('Stack Files', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
    await saveFixtureFile(page, ORIGINAL_CONTENT)
  })

  test.afterEach(async ({ page }) => {
    await saveFixtureFile(page, ORIGINAL_CONTENT)
  })

  test('saves an auxiliary stack file and reloads the persisted content', async ({ page }) => {
    await openFixtureFile(page)

    await expect(page.getByRole('heading', { level: 1, name: STACK_ID })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Files', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByRole('button', { name: '.. (up)', exact: true })).toBeVisible()

    const editor = page.locator('.cm-content')
    await expect(editor).toContainText('port: 8080')
    await editor.click()
    await page.keyboard.press('ControlOrMeta+A')
    await page.keyboard.insertText(UPDATED_CONTENT)
    await expect(page.getByText('Unsaved changes', { exact: true })).toBeVisible()

    const saveResponse = page.waitForResponse((response) =>
      response.url().endsWith(`/api/stacks/${STACK_ID}/workspace/file`)
      && response.request().method() === 'PUT',
    )
    await page.getByRole('button', { name: 'Save', exact: true }).click()

    const response = await saveResponse
    expect(response.ok()).toBeTruthy()
    expect(response.request().postDataJSON()).toMatchObject({
      path: FILE_PATH,
      content: UPDATED_CONTENT,
      create_parent_directories: false,
      expected_modified_at: expect.any(String),
    })
    expect(await response.json()).toMatchObject({
      saved: true,
      stack_id: STACK_ID,
      path: FILE_PATH,
      audit_action: 'save_stack_file',
    })
    await expect(page.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible()

    const persistedResponse = await page.request.get(
      `${BASE_URL}/api/stacks/${STACK_ID}/workspace/file?path=${encodeURIComponent(FILE_PATH)}`,
    )
    expect(persistedResponse.ok()).toBeTruthy()
    expect((await persistedResponse.json()).content).toBe(UPDATED_CONTENT)

    await page.reload()
    await page.getByRole('button', { name: 'config', exact: true }).click()
    await page.getByRole('button', { name: 'app.yaml', exact: true }).click()
    await expect(page.locator('.cm-line')).toHaveText([
      'port: 9090',
      'mode: production',
      '',
    ])
  })
})
