import { chromium } from '@playwright/test'

const BASE_URL = process.env.STACKLAB_URL ?? 'http://127.0.0.1:18080'
const PASSWORD = process.env.STACKLAB_PASSWORD ?? 'stacklab-smoke'
const OUTPUT_DIR = 'screenshots'

const variants: Record<string, string> = {
  current: '', // no overrides

  'A-mission-control': `
    :root {
      --bg: #0A0A0B;
      --panel: #111113;
      --panel-border: rgba(255, 255, 255, 0.06);
      --text: #EDEDEF;
      --muted: #71717A;
      --accent: #22C55E;
      --accent-strong: #16A34A;
      --warning: #EAB308;
      --danger: #EF4444;
      --shadow: none;
      --font-sans: "Inter", ui-sans-serif, system-ui, sans-serif;
      --font-mono: "JetBrains Mono", ui-monospace, monospace;
    }
    body {
      background: #0A0A0B !important;
    }
    /* Tighten border-radius */
    [class*="rounded-[28px]"] { border-radius: 8px !important; }
    [class*="rounded-[24px]"] { border-radius: 6px !important; }
    [class*="rounded-[20px]"] { border-radius: 6px !important; }
    [class*="rounded-[16px]"] { border-radius: 4px !important; }
    [class*="rounded-[32px]"] { border-radius: 8px !important; }
    [class*="rounded-2xl"] { border-radius: 6px !important; }
    [class*="rounded-full"] { border-radius: 6px !important; }
    /* Noise texture */
    body::after {
      content: '';
      position: fixed;
      inset: 0;
      background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)' opacity='0.03'/%3E%3C/svg%3E");
      pointer-events: none;
      z-index: 9999;
    }
    /* Remove decorative shadows */
    [class*="shadow-"] { box-shadow: none !important; }
  `,

  'B-terminal-noir': `
    :root {
      --bg: #0B0B0F;
      --panel: #12121A;
      --panel-border: rgba(255, 255, 255, 0.08);
      --text: #E4E4ED;
      --muted: #6E6E80;
      --accent: #818CF8;
      --accent-strong: #6366F1;
      --warning: #EAB308;
      --danger: #F43F5E;
      --shadow: none;
      --font-sans: "Inter", ui-sans-serif, system-ui, sans-serif;
      --font-mono: "JetBrains Mono", ui-monospace, monospace;
    }
    body {
      background:
        radial-gradient(circle at 20% 20%, rgba(129, 140, 248, 0.06), transparent 40%),
        #0B0B0F !important;
    }
    /* Dot grid */
    body::before {
      content: '';
      position: fixed;
      inset: 0;
      background-image: radial-gradient(circle, rgba(255,255,255,0.04) 1px, transparent 1px);
      background-size: 24px 24px;
      pointer-events: none;
      z-index: 0;
    }
    /* Tighten border-radius */
    [class*="rounded-[28px]"] { border-radius: 8px !important; }
    [class*="rounded-[24px]"] { border-radius: 8px !important; }
    [class*="rounded-[20px]"] { border-radius: 6px !important; }
    [class*="rounded-[16px]"] { border-radius: 4px !important; }
    [class*="rounded-[32px]"] { border-radius: 8px !important; }
    [class*="rounded-2xl"] { border-radius: 6px !important; }
    [class*="rounded-full"] { border-radius: 6px !important; }
    /* Card top-edge sheen */
    [class*="bg-[var(--panel)]"] {
      position: relative;
    }
    /* Glow on accent elements */
    [class*="bg-[rgba(79,209,197"] {
      background: rgba(129, 140, 248, 0.14) !important;
      border-color: rgba(129, 140, 248, 0.35) !important;
      box-shadow: 0 0 12px rgba(129, 140, 248, 0.1) !important;
    }
    [class*="text-[var(--accent)]"] {
      color: #818CF8 !important;
    }
    [class*="border-[rgba(79,209,197"] {
      border-color: rgba(129, 140, 248, 0.35) !important;
    }
    /* Remove old shadows */
    [class*="shadow-[var(--shadow)]"] { box-shadow: none !important; }
  `,

  'C-monochrome-ops': `
    :root {
      --bg: #09090B;
      --panel: #18181B;
      --panel-border: rgba(255, 255, 255, 0.10);
      --text: #FAFAFA;
      --muted: #71717A;
      --accent: #FAFAFA;
      --accent-strong: #E4E4E7;
      --warning: #EAB308;
      --danger: #EF4444;
      --shadow: none;
      --font-sans: "Inter", ui-sans-serif, system-ui, sans-serif;
      --font-mono: "JetBrains Mono", ui-monospace, monospace;
    }
    body {
      background: #09090B !important;
    }
    /* Tighten border-radius */
    [class*="rounded-[28px]"] { border-radius: 8px !important; }
    [class*="rounded-[24px]"] { border-radius: 8px !important; }
    [class*="rounded-[20px]"] { border-radius: 6px !important; }
    [class*="rounded-[16px]"] { border-radius: 4px !important; }
    [class*="rounded-[32px]"] { border-radius: 8px !important; }
    [class*="rounded-2xl"] { border-radius: 6px !important; }
    [class*="rounded-full"] { border-radius: 6px !important; }
    /* White accent buttons */
    [class*="bg-[rgba(79,209,197"] {
      background: rgba(255, 255, 255, 0.08) !important;
      border-color: rgba(255, 255, 255, 0.15) !important;
    }
    [class*="bg-[linear-gradient(135deg"] {
      background: #FAFAFA !important;
      color: #09090B !important;
    }
    [class*="text-[var(--accent)]"] {
      color: #A1A1AA !important;
    }
    [class*="border-[rgba(79,209,197"] {
      border-color: rgba(255, 255, 255, 0.15) !important;
    }
    /* Remove all shadows */
    [class*="shadow-"] { box-shadow: none !important; }
  `,

  'D-cyber-homelab': `
    :root {
      --bg: #0A0A0F;
      --panel: #111118;
      --panel-border: rgba(255, 255, 255, 0.07);
      --text: #E8E8F0;
      --muted: #6E6E7A;
      --accent: #4FD1C5;
      --accent-strong: #14B8A6;
      --warning: #F59E0B;
      --danger: #F97316;
      --shadow: none;
      --font-sans: "Inter", "Space Grotesk", ui-sans-serif, system-ui, sans-serif;
      --font-mono: "JetBrains Mono", ui-monospace, monospace;
    }
    body {
      background: #0A0A0F !important;
    }
    /* Noise texture */
    body::after {
      content: '';
      position: fixed;
      inset: 0;
      background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)' opacity='0.025'/%3E%3C/svg%3E");
      pointer-events: none;
      z-index: 9999;
    }
    /* Tighten border-radius */
    [class*="rounded-[28px]"] { border-radius: 8px !important; }
    [class*="rounded-[24px]"] { border-radius: 8px !important; }
    [class*="rounded-[20px]"] { border-radius: 6px !important; }
    [class*="rounded-[16px]"] { border-radius: 4px !important; }
    [class*="rounded-[32px]"] { border-radius: 8px !important; }
    [class*="rounded-2xl"] { border-radius: 6px !important; }
    [class*="rounded-full"] { border-radius: 6px !important; }
    /* Subtle glow on accent interactive elements */
    [class*="bg-[rgba(79,209,197"] {
      box-shadow: 0 0 10px rgba(79, 209, 197, 0.08) !important;
    }
    [class*="bg-[linear-gradient(135deg"] {
      box-shadow: 0 0 16px rgba(79, 209, 197, 0.12) !important;
    }
    /* Remove old shadows, keep glow */
    [class*="shadow-[var(--shadow)]"] { box-shadow: none !important; }
  `,
}

const pages = [
  { name: 'dashboard', path: '/stacks' },
  { name: 'host', path: '/host' },
  { name: 'maintenance', path: '/maintenance' },
]

async function run() {
  const browser = await chromium.launch()
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
  })

  // Login once
  const loginPage = await context.newPage()
  await loginPage.goto(`${BASE_URL}/login`)
  await loginPage.fill('[data-testid="login-password"]', PASSWORD)
  await loginPage.click('[data-testid="login-submit"]')
  await loginPage.waitForURL('**/stacks')
  await loginPage.close()

  for (const [variantName, css] of Object.entries(variants)) {
    console.log(`Capturing variant: ${variantName}`)

    for (const { name: pageName, path } of pages) {
      const page = await context.newPage()
      await page.goto(`${BASE_URL}${path}`)
      await page.waitForTimeout(1500) // let data load

      if (css) {
        await page.addStyleTag({ content: css })
        await page.waitForTimeout(300) // let styles apply
      }

      const filename = `${OUTPUT_DIR}/${variantName}_${pageName}.png`
      await page.screenshot({ path: filename, fullPage: false })
      console.log(`  → ${filename}`)
      await page.close()
    }
  }

  await browser.close()
  console.log('Done!')
}

run().catch((err) => {
  console.error(err)
  process.exit(1)
})
