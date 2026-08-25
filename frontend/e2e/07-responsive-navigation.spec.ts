import { test, expect, type Page } from '@playwright/test'
import { login } from './helpers'

const LONG_NIGHTLY_VERSION = '2026.09.0~nightly20260806+r133.g4720e3e'

async function expectMobileShellEdgesToMeet(page: Page) {
  const primaryNavigation = page.getByRole('navigation', { name: 'Primary' })
  await expect(primaryNavigation).toBeVisible()
  await page.evaluate(() => document.fonts.ready)

  await expect.poll(async () => primaryNavigation.evaluate((navigation) => {
    const navigationRect = navigation.getBoundingClientRect()
    const viewportBottom = window.visualViewport
      ? window.visualViewport.offsetTop + window.visualViewport.height
      : window.innerHeight
    return Math.abs(navigationRect.bottom - viewportBottom)
  })).toBeLessThanOrEqual(1)

  await expect.poll(async () => primaryNavigation.evaluate((navigation) => {
    const scrollRegion = navigation.previousElementSibling
    if (!(scrollRegion instanceof HTMLElement)) {
      throw new Error('Mobile scroll region was not found immediately before the primary navigation')
    }
    return Math.abs(scrollRegion.getBoundingClientRect().bottom - navigation.getBoundingClientRect().top)
  })).toBeLessThanOrEqual(1)
}

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

  test('keeps the mobile bottom navigation attached to the content and viewport after entry and reload', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium-mobile-smoke', 'Mobile app-shell regression')

    await page.setViewportSize({ width: 320, height: 720 })
    await login(page)

    await expectMobileShellEdgesToMeet(page)

    await page.reload()
    await expect(page).toHaveURL(/\/stacks$/)
    await expectMobileShellEdgesToMeet(page)
  })

  test('keeps a long nightly version clear of its health label without horizontal overflow', async ({ page }, testInfo) => {
    if (testInfo.project.name === 'chromium-mobile-smoke') {
      await page.setViewportSize({ width: 320, height: 720 })
    } else {
      await page.setViewportSize({ width: 1280, height: 720 })
    }
    await page.route('**/api/ready', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          status: 'ok',
          version: LONG_NIGHTLY_VERSION,
          checks: {
            database: { status: 'ok' },
            frontend: { status: 'ok' },
            runtime: { status: 'ok' },
          },
        }),
      })
    })

    await login(page)
    await page.getByRole('link', { name: /^Host(?:\s+2)?$/ }).click()
    await expect(page).toHaveURL(/\/host$/)

    const backendHealth = page.getByRole('article', { name: 'Backend health' })
    await expect(backendHealth.getByText(LONG_NIGHTLY_VERSION, { exact: true })).toBeVisible()
    await page.evaluate(() => document.fonts.ready)

    const geometry = await backendHealth.evaluate((article, expectedVersion) => {
      const label = Array.from(article.querySelectorAll('dt'))
        .find((element) => element.textContent?.trim() === 'Version')
      const value = Array.from(article.querySelectorAll('dd'))
        .find((element) => element.textContent?.trim() === expectedVersion)
      if (!(label instanceof HTMLElement) || !(value instanceof HTMLElement)) {
        throw new Error('Backend version label or value was not found')
      }

      const textRects = (element: HTMLElement) => {
        const range = document.createRange()
        range.selectNodeContents(element)
        return Array.from(range.getClientRects(), (rect) => ({
          left: rect.left,
          right: rect.right,
          top: rect.top,
          bottom: rect.bottom,
        }))
      }
      const labelRects = textRects(label)
      const valueRects = textRects(value)
      const overlapTolerance = 0.5
      const overlaps = labelRects.some((labelRect) => valueRects.some((valueRect) => (
        labelRect.left < valueRect.right - overlapTolerance
        && labelRect.right > valueRect.left + overlapTolerance
        && labelRect.top < valueRect.bottom - overlapTolerance
        && labelRect.bottom > valueRect.top + overlapTolerance
      )))

      return {
        labelRectCount: labelRects.length,
        valueRectCount: valueRects.length,
        overlaps,
        valueOverflow: value.scrollWidth - value.clientWidth,
        pageOverflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      }
    }, LONG_NIGHTLY_VERSION)

    expect(geometry.labelRectCount).toBeGreaterThan(0)
    expect(geometry.valueRectCount).toBeGreaterThan(0)
    expect(geometry.overlaps).toBe(false)
    expect(geometry.valueOverflow).toBeLessThanOrEqual(1)
    expect(geometry.pageOverflow).toBeLessThanOrEqual(1)
  })
})
