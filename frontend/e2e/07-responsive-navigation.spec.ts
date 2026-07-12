import { test, expect } from '@playwright/test'
import { login } from './helpers'

test.describe('Responsive navigation', () => {
  test('opens Settings on desktop and through More on mobile', async ({ page }, testInfo) => {
    await login(page)

    if (testInfo.project.name === 'chromium-mobile-smoke') {
      const primaryNavigation = page.getByRole('navigation', { name: 'Primary' })
      await expect(primaryNavigation).toBeVisible()

      const moreButton = primaryNavigation.getByRole('button', { name: 'More navigation' })
      await moreButton.click()
      await expect(moreButton).toHaveAttribute('aria-expanded', 'true')

      const navigationDrawer = page.getByRole('dialog', { name: 'Navigation' })
      await expect(navigationDrawer).toBeVisible()
      await navigationDrawer.getByRole('link', { name: 'Settings' }).click()
      await expect(navigationDrawer).not.toBeVisible()
      await expect(moreButton).toHaveAttribute('aria-pressed', 'true')
    } else {
      await page.getByRole('link', { name: 'Settings' }).click()
    }

    await expect(page).toHaveURL(/\/settings$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Maintenance schedules' })).toBeVisible()
  })
})
