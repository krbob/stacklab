import { test, expect } from '@playwright/test'
import { login } from './helpers'
import {
  RUNTIME_LOG_MARKER,
  startRuntimeStack,
  stopRuntimeStack,
} from './runtime-fixture'

const STACK_ID = 'e2e-runtime-logs'

test.describe('Stack logs', () => {
  test.describe.configure({ timeout: 45_000 })

  test.beforeEach(async ({ page }) => {
    await login(page)
    await startRuntimeStack(page, STACK_ID)
  })

  test.afterEach(async ({ page }) => {
    await stopRuntimeStack(page, STACK_ID)
  })

  test('streams and filters output from a running container', async ({ page }) => {
    await page.goto(`/stacks/${STACK_ID}/logs`)

    await expect(page.getByRole('link', { name: 'Logs', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByRole('button', { name: 'probe', exact: true })).toBeVisible()

    const markerLine = page.getByTestId('log-line').filter({ hasText: RUNTIME_LOG_MARKER })
    await expect(markerLine).toBeVisible({ timeout: 20_000 })

    const filter = page.getByPlaceholder('Filter...')
    await filter.fill('no-such-runtime-line')
    await expect(page.getByText('No log lines match the current filter.', { exact: true })).toBeVisible()

    await filter.clear()
    await expect(markerLine).toBeVisible()
  })
})
