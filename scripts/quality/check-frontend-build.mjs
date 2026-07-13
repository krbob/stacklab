import { readdir, readFile } from 'node:fs/promises'
import { join } from 'node:path'

const assetsDirectory = process.argv[2] ?? 'frontend/dist/assets'
const entries = await readdir(assetsDirectory, { withFileTypes: true })
const cssFiles = entries.filter((entry) => entry.isFile() && entry.name.endsWith('.css'))
const fontFiles = entries.filter((entry) => entry.isFile() && /\.(?:woff2?|ttf|otf)$/i.test(entry.name))

if (cssFiles.length === 0) {
  throw new Error(`No CSS assets found in ${assetsDirectory}`)
}
if (fontFiles.length === 0) {
  throw new Error(`No external font assets found in ${assetsDirectory}`)
}

const inlineFontFiles = []
for (const cssFile of cssFiles) {
  const css = await readFile(join(assetsDirectory, cssFile.name), 'utf8')
  if (/@font-face\s*\{[^}]*\bsrc\s*:[^}]*url\(\s*["']?data:/i.test(css)) inlineFontFiles.push(cssFile.name)
}

if (inlineFontFiles.length > 0) {
  throw new Error(`Inline font data violates the Content Security Policy: ${inlineFontFiles.join(', ')}`)
}
