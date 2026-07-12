import { test, expect, type Locator } from '@playwright/test'
import { login } from './helpers'
import { startRuntimeStack, stopRuntimeStack } from './runtime-fixture'

const STACK_ID = 'e2e-runtime-terminal'
const TERMINAL_COMMAND = 'echo terminal-result-$$'

async function establishShellInput(input: Locator, rows: Locator) {
  const promptCount = async () => ((await rows.textContent())?.match(/\/ #/g) ?? []).length

  for (let attempt = 0; attempt < 3; attempt += 1) {
    const initialPromptCount = await promptCount()
    await input.press('Enter')
    try {
      await expect.poll(promptCount, { timeout: 2_000 }).toBeGreaterThan(initialPromptCount)
      return
    } catch (error) {
      if (attempt === 2) throw error
    }
  }
}

test.describe('Stack terminal', () => {
  test.describe.configure({ timeout: 45_000 })

  test.beforeEach(async ({ page }) => {
    await login(page)
    await startRuntimeStack(page, STACK_ID)
  })

  test.afterEach(async ({ page }) => {
    const disconnect = page.getByRole('button', { name: 'Disconnect', exact: true })
    if (await disconnect.isVisible().catch(() => false)) {
      await disconnect.click()
      await expect(page.getByText('Disconnected', { exact: true })).toBeVisible()
    }
    await stopRuntimeStack(page, STACK_ID)
  })

  test('executes a command in an interactive shell session', async ({ page }) => {
    await page.goto(`/stacks/${STACK_ID}/terminal`)

    await expect(page.getByRole('link', { name: 'Terminal', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByLabel('Shell:')).toHaveValue('/bin/sh')
    await page.getByRole('button', { name: 'Connect', exact: true }).click()
    await expect(page.getByText('Connected', { exact: true })).toBeVisible({ timeout: 20_000 })

    const terminalRows = page.locator('.xterm-rows')
    await expect(terminalRows).toContainText('/ #', { timeout: 20_000 })
    const terminalInput = page.getByLabel('Terminal input')
    await terminalInput.focus()
    await establishShellInput(terminalInput, terminalRows)
    await terminalInput.evaluate((input, command) => {
      const clipboardData = new DataTransfer()
      clipboardData.setData('text/plain', command)
      input.dispatchEvent(new ClipboardEvent('paste', { clipboardData, bubbles: true, cancelable: true }))
    }, TERMINAL_COMMAND)
    await terminalInput.press('Enter')
    await expect(terminalRows).toContainText(/terminal-result-\d+/, { timeout: 20_000 })

    await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
    await expect(page.getByText('Disconnected', { exact: true })).toBeVisible()
  })
})
