import { chromium } from '@playwright/test'

const BASE_URL = process.env.STACKLAB_URL ?? 'http://127.0.0.1:18080'
const PASSWORD = process.env.STACKLAB_PASSWORD ?? 'stacklab-smoke'
const OUTPUT_DIR = 'screenshots/fonts'

const variants: Record<string, string> = {
  'current-inter': '',

  'A-mono-headings': `
    /* JetBrains Mono for all headings and branding */
    h1, h2, h3, h4,
    [class*="text-3xl"],
    [class*="text-2xl"],
    [class*="text-lg"],
    [class*="text-xs"][class*="uppercase"][class*="tracking"] {
      font-family: "JetBrains Mono", ui-monospace, monospace !important;
    }
  `,

  'B-space-grotesk-headings': `
    /* Space Grotesk for headings, Inter for body */
    h1, h2, h3, h4,
    [class*="text-3xl"],
    [class*="text-2xl"],
    [class*="text-lg"] {
      font-family: "Space Grotesk", sans-serif !important;
    }
    /* Keep branding label in Space Grotesk too */
    [class*="text-xs"][class*="uppercase"][class*="tracking"] {
      font-family: "Space Grotesk", sans-serif !important;
    }
  `,

  'C-full-mono-ui': `
    /* Everything in JetBrains Mono — maximum nerd */
    * {
      font-family: "JetBrains Mono", ui-monospace, monospace !important;
    }
  `,

  'D-mono-brand-space-titles': `
    /* JetBrains Mono ONLY for branding label */
    [class*="text-xs"][class*="uppercase"][class*="tracking"] {
      font-family: "JetBrains Mono", ui-monospace, monospace !important;
    }
    /* Space Grotesk for page titles */
    h1, h2, h3,
    [class*="text-3xl"],
    [class*="text-2xl"] {
      font-family: "Space Grotesk", sans-serif !important;
    }
    /* Inter stays for everything else */
  `,
}

const pages = [
  { name: 'login', path: '/login', needsAuth: false },
  { name: 'dashboard', path: '/stacks', needsAuth: true },
  { name: 'host', path: '/host', needsAuth: true },
]

async function run() {
  const fs = await import('fs')
  fs.mkdirSync(OUTPUT_DIR, { recursive: true })

  const browser = await chromium.launch()

  for (const [variantName, css] of Object.entries(variants)) {
    console.log(`Capturing variant: ${variantName}`)

    const context = await browser.newContext({ viewport: { width: 1440, height: 900 } })

    for (const { name: pageName, path, needsAuth } of pages) {
      const page = await context.newPage()

      if (needsAuth) {
        await page.goto(`${BASE_URL}/login`)
        await page.fill('[data-testid="login-password"]', PASSWORD)
        await page.click('[data-testid="login-submit"]')
        await page.waitForURL('**/stacks')
      }

      await page.goto(`${BASE_URL}${path}`)
      await page.waitForTimeout(1500)

      if (css) {
        await page.addStyleTag({ content: css })
        await page.waitForTimeout(300)
      }

      const filename = `${OUTPUT_DIR}/${variantName}_${pageName}.png`
      await page.screenshot({ path: filename, fullPage: false })
      console.log(`  → ${filename}`)
      await page.close()
    }

    await context.close()
  }

  await browser.close()
  console.log('Done!')
}

run().catch((err) => {
  console.error(err)
  process.exit(1)
})
