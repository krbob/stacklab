import { chromium } from '@playwright/test'

const BASE_URL = process.env.STACKLAB_URL ?? 'http://127.0.0.1:18080'
const PASSWORD = process.env.STACKLAB_PASSWORD ?? 'stacklab-smoke'
const OUTPUT_DIR = 'screenshots/fonts'

const variants: Record<string, string> = {
  'current-inter': '',

  'A-mono-headings': `
    h1, h2, h3, h4,
    [class*="text-3xl"],
    [class*="text-2xl"],
    [class*="text-lg"],
    [class*="text-xs"][class*="uppercase"][class*="tracking"] {
      font-family: "JetBrains Mono", ui-monospace, monospace !important;
    }
  `,

  'B-space-grotesk-headings': `
    h1, h2, h3, h4,
    [class*="text-3xl"],
    [class*="text-2xl"],
    [class*="text-lg"] {
      font-family: "Space Grotesk", sans-serif !important;
    }
    [class*="text-xs"][class*="uppercase"][class*="tracking"] {
      font-family: "Space Grotesk", sans-serif !important;
    }
  `,

  'C-full-mono': `
    * {
      font-family: "JetBrains Mono", ui-monospace, monospace !important;
    }
  `,

  'D-mono-brand-space-titles': `
    [class*="text-xs"][class*="uppercase"][class*="tracking"] {
      font-family: "JetBrains Mono", ui-monospace, monospace !important;
    }
    h1, h2, h3,
    [class*="text-3xl"],
    [class*="text-2xl"] {
      font-family: "Space Grotesk", sans-serif !important;
    }
  `,
}

async function run() {
  const fs = await import('fs')
  fs.mkdirSync(OUTPUT_DIR, { recursive: true })

  const browser = await chromium.launch()

  for (const [variantName, css] of Object.entries(variants)) {
    console.log(`Capturing variant: ${variantName}`)

    // Login page — fresh context, no auth
    {
      const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
      const page = await ctx.newPage()
      await page.goto(`${BASE_URL}/login`)
      await page.waitForTimeout(1000)
      if (css) {
        await page.addStyleTag({ content: css })
        await page.waitForTimeout(300)
      }
      await page.screenshot({ path: `${OUTPUT_DIR}/${variantName}_login.png` })
      console.log(`  → ${variantName}_login.png`)
      await ctx.close()
    }

    // Authenticated pages — single context with login
    {
      const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
      const loginPage = await ctx.newPage()
      await loginPage.goto(`${BASE_URL}/login`)
      await loginPage.fill('[data-testid="login-password"]', PASSWORD)
      await loginPage.click('[data-testid="login-submit"]')
      await loginPage.waitForURL('**/stacks')
      await loginPage.close()

      for (const [pageName, path] of [['dashboard', '/stacks'], ['host', '/host']] as const) {
        const page = await ctx.newPage()
        await page.goto(`${BASE_URL}${path}`)
        await page.waitForTimeout(1500)
        if (css) {
          await page.addStyleTag({ content: css })
          await page.waitForTimeout(300)
        }
        await page.screenshot({ path: `${OUTPUT_DIR}/${variantName}_${pageName}.png` })
        console.log(`  → ${variantName}_${pageName}.png`)
        await page.close()
      }

      await ctx.close()
    }
  }

  await browser.close()
  console.log('Done!')
}

run().catch((err) => {
  console.error(err)
  process.exit(1)
})
