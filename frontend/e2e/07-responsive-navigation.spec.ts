import { test, expect } from '@playwright/test'
import { login } from './helpers'

test.describe('Responsive navigation', () => {
  test('opens Settings on desktop and through More on mobile', async ({ page }, testInfo) => {
    await login(page)

    if (testInfo.project.name === 'chromium-mobile-smoke') {
      await page.setViewportSize({ width: 320, height: 720 })
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

    await expect(page).toHaveURL(/\/settings\/security$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible()

    const settingsNavigation = page.getByRole('navigation', { name: 'Settings sections' })
    const settingsLinks = settingsNavigation.getByRole('link')
    await expect(settingsLinks).toHaveCount(5)
    await expect(settingsNavigation.getByRole('link', { name: 'Security' })).toHaveAttribute('aria-current', 'page')
    for (const link of await settingsLinks.all()) {
      await expect(link).toBeVisible()
      expect((await link.boundingBox())?.height).toBeGreaterThanOrEqual(44)
    }

    if (testInfo.project.name === 'chromium-mobile-smoke') {
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
    }

    await settingsNavigation.getByRole('link', { name: 'Automation' }).click()
    await expect(page).toHaveURL(/\/settings\/automation$/)
    await expect(settingsNavigation.getByRole('link', { name: 'Automation' })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByRole('heading', { name: 'Maintenance schedules' })).toBeVisible()
  })
})
