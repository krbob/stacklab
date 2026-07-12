import { test, expect } from '@playwright/test'
import { login } from './helpers'
import { startRuntimeStack, stopRuntimeStack } from './runtime-fixture'

const STACK_ID = 'e2e-runtime-stats'

test.describe('Stack stats', () => {
  test.describe.configure({ timeout: 45_000 })

  test.beforeEach(async ({ page }) => {
    await login(page)
    await startRuntimeStack(page, STACK_ID)
  })

  test.afterEach(async ({ page }) => {
    await stopRuntimeStack(page, STACK_ID)
  })

  test('renders live aggregate and container metrics', async ({ page }) => {
    await page.goto(`/stacks/${STACK_ID}/stats`)

    await expect(page.getByRole('link', { name: 'Stats', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByText('Session history: last ~5 min, collected in this browser while this view is open.', { exact: true })).toBeVisible({ timeout: 20_000 })

    await expect(page.getByText('Stack CPU', { exact: true })).toBeVisible()
    await expect(page.getByText('Stack RAM', { exact: true })).toBeVisible()
    await expect(page.getByText('Stack Net', { exact: true })).toBeVisible()

    const cpu = page.getByRole('progressbar', { name: 'probe CPU usage' })
    const memory = page.getByRole('progressbar', { name: 'probe memory usage' })
    await expect(cpu).toHaveAttribute('aria-valuenow', /^\d+(\.\d+)?$/)
    await expect(memory).toHaveAttribute('aria-valuenow', /^\d+(\.\d+)?$/)
  })
})
