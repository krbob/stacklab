import { test, expect } from '@playwright/test'
import { login } from './helpers'

test.describe('Settings schedules', () => {
  test('saves a selected-stack update schedule and reloads it', async ({ page }) => {
    await login(page)
    await page.goto('/settings/automation')

    await expect(page).toHaveURL(/\/settings\/automation$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Maintenance schedules' })).toBeVisible()

    await page.getByLabel('Scheduled stack update').check()
    await page.getByLabel('Selected stacks').check()
    await page.getByRole('checkbox', { name: 'demo' }).check()

    const saveResponse = page.waitForResponse((response) =>
      response.url().endsWith('/api/settings/maintenance-schedules')
      && response.request().method() === 'PUT',
    )
    await page.getByRole('button', { name: 'Save schedules' }).click()

    const response = await saveResponse
    expect(response.ok()).toBeTruthy()
    const saved = await response.json()
    expect(saved.update.enabled).toBe(true)
    expect(saved.update.target).toMatchObject({ mode: 'selected', stack_ids: ['demo'] })
    await expect(page.getByText('Saved', { exact: true })).toBeVisible()

    await page.reload()
    await expect(page.getByLabel('Scheduled stack update')).toBeChecked()
    await expect(page.getByLabel('Selected stacks')).toBeChecked()
    await expect(page.getByRole('checkbox', { name: 'demo' })).toBeChecked()
  })
})
