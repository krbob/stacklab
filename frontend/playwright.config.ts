import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  retries: 0,
  workers: 1,
  fullyParallel: false,
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'playwright-report' }],
  ],
  use: {
    baseURL: process.env.STACKLAB_E2E_URL ?? 'http://127.0.0.1:18081',
    headless: true,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium-desktop', use: { browserName: 'chromium' } },
    {
      name: 'chromium-mobile-smoke',
      testMatch: '**/07-responsive-navigation.spec.ts',
      use: { ...devices['Pixel 7'], browserName: 'chromium' },
    },
  ],
})
