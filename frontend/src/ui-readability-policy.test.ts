/// <reference types="node" />

import { readFileSync, readdirSync } from 'node:fs'
import { extname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const sourceRoot = join(process.cwd(), 'src')
const indexCSS = readFileSync(join(sourceRoot, 'index.css'), 'utf8')

describe('UI readability policy', () => {
  it('keeps semantic text tokens at AA contrast on both base surfaces', () => {
    const colors = parseColorTokens(indexCSS)
    for (const foreground of ['text', 'muted', 'dim', 'accent', 'ok', 'warning', 'danger']) {
      expect(contrast(colors[foreground], colors.bg), `${foreground} on bg`).toBeGreaterThanOrEqual(4.5)
      expect(contrast(colors[foreground], colors.panel), `${foreground} on panel`).toBeGreaterThanOrEqual(4.5)
    }
  })

  it('does not introduce utility text below the 12px readability floor', () => {
    const violations: string[] = []
    for (const path of sourceFiles(sourceRoot)) {
      if (path.endsWith('.test.ts') || path.endsWith('.test.tsx')) continue
      const source = readFileSync(path, 'utf8')
      for (const match of source.matchAll(/text-\[(\d+(?:\.\d+)?)px\]/g)) {
        if (Number(match[1]) < 12) violations.push(`${path.slice(sourceRoot.length + 1)}: ${match[0]}`)
      }
    }
    expect(violations).toEqual([])
  })

  it('ships local fonts and honors reduced-motion and forced-color preferences', () => {
    expect(indexCSS).toContain('@fontsource-variable/inter/index.css')
    expect(indexCSS).toContain('@fontsource-variable/space-grotesk/index.css')
    expect(indexCSS).toContain('@fontsource-variable/jetbrains-mono/index.css')
    expect(indexCSS).toContain('@media (prefers-reduced-motion: reduce)')
    expect(indexCSS).toContain('@media (forced-colors: active)')
    expect(indexCSS).toContain("opacity='0.015'")
  })
})

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return sourceFiles(path)
    return ['.css', '.ts', '.tsx'].includes(extname(path)) ? [path] : []
  })
}

function parseColorTokens(css: string): Record<string, string> {
  const tokens: Record<string, string> = {}
  for (const match of css.matchAll(/--([a-z-]+):\s*(#[0-9A-Fa-f]{6});/g)) {
    tokens[match[1]] = match[2]
  }
  return tokens
}

function contrast(first: string, second: string): number {
  const firstLuminance = luminance(first)
  const secondLuminance = luminance(second)
  return (Math.max(firstLuminance, secondLuminance) + 0.05) / (Math.min(firstLuminance, secondLuminance) + 0.05)
}

function luminance(hex: string): number {
  const channels = [1, 3, 5].map((offset) => Number.parseInt(hex.slice(offset, offset + 2), 16) / 255)
    .map((channel) => channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4)
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2]
}
