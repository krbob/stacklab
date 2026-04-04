# Visual Revamp Proposal

## Current State

The current UI is clean, functional, and consistent. Design tokens are well-defined (teal accent, dark blue-gray base, Space Grotesk + JetBrains Mono). Rounded panels with glass-like borders.

What works:
- layout and information architecture are solid
- responsiveness is handled
- empty states and loading states exist
- dark theme is already the default

What could be better:
- panels feel "floaty" — large border-radius (28px) gives a soft/consumer feel, not an ops tool
- teal-on-dark-blue palette is pleasant but generic — could be any dashboard
- no visual texture or depth — large dark surfaces feel flat
- typography doesn't differentiate enough between UI text and data values
- progress/status indicators are functional but not distinctive

## Design Principles For The Revamp

1. **Density communicates competence** — smaller fonts, tighter spacing, more data visible. Homelab operators are power users.
2. **Semantic decoration only** — every visual element (color, glow, monospace, animation) carries meaning. Nothing is purely decorative.
3. **Darkness with depth** — near-black base with barely perceptible surface layers. Hierarchy through lightness, not shadows.
4. **Monospace for data, sans-serif for UI** — strict separation signals "this is a literal value" vs "this is a label."

## Proposed Variants

---

### Variant A: "Mission Control"

Inspired by: Grafana dark, monitoring dashboards, NASA mission control screens.

**Palette:**
```
bg-base:      #0A0A0B    (near-black, neutral)
bg-surface:   #111113
bg-raised:    #1A1A1F
border:       rgba(255, 255, 255, 0.06)
text-primary: #EDEDEF
text-muted:   #71717A
accent:       #22C55E    (green — everything healthy is green)
accent-alt:   #3B82F6    (blue — for interactive/info)
```

**Character:**
- Green-on-black evoking server health dashboards
- Status dots with subtle glow for live states
- Monospace used heavily — timestamps, IDs, paths, metrics all in mono
- Tight grid layout, minimal border-radius (6-8px)
- Column headers in uppercase, 11px, letter-spacing
- Noise texture overlay at 3% opacity for surface depth
- No decorative gradients — color only for meaning

**Typography:**
- UI: Inter 13px
- Data: JetBrains Mono 12.5px
- Headers: Inter semibold 14px (no large headings inside app)
- Section labels: 11px uppercase tracking-wide, muted

**Cards:**
```
border-radius: 8px
border: 1px solid rgba(255, 255, 255, 0.06)
background: #111113
```
No large rounded corners. No glass effect. Clean, dense, precise.

**When to choose:** If operator efficiency and information density are top priority. Feels like a tool built by someone who runs 30 containers.

---

### Variant B: "Terminal Noir"

Inspired by: Warp terminal, Ghostty, retro-futuristic sci-fi interfaces.

**Palette:**
```
bg-base:      #0B0B0F    (near-black with slight blue)
bg-surface:   #12121A
bg-raised:    #1C1C28
border:       rgba(255, 255, 255, 0.08)
text-primary: #E4E4ED
text-muted:   #6E6E80
accent:       #818CF8    (indigo/violet)
accent-glow:  rgba(129, 140, 248, 0.15)
```

**Character:**
- Violet/indigo accent — stands out from typical green/blue ops tools
- Subtle glow effects on focused/active elements (inputs, primary buttons)
- Gradient top-edge sheen on cards (1px highlight line at card top)
- Terminal-inspired input fields — bottom border only, no full box
- Animated cursor blink on active terminal
- Faint dot-grid background on page base (24px spacing, 5% opacity)

**Typography:**
- UI: Inter 13px
- Data/code: JetBrains Mono 13px
- Headings: Space Grotesk 16px semibold (keep current font for personality)
- Status labels: mono, colored

**Cards:**
```
border-radius: 8px
border: 1px solid rgba(255, 255, 255, 0.08)
background: #12121A
position: relative
```
Plus a top-edge gradient highlight:
```css
.card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 20%;
  right: 20%;
  height: 1px;
  background: linear-gradient(90deg, transparent, rgba(129, 140, 248, 0.3), transparent);
}
```

**When to choose:** If you want a distinctive visual identity that feels "nerdy premium" — recognizable at a glance as a tool for enthusiasts.

---

### Variant C: "Monochrome Ops"

Inspired by: Vercel dashboard, Linear, minimalist Japanese design.

**Palette:**
```
bg-base:      #09090B    (zinc-950)
bg-surface:   #18181B    (zinc-900)
bg-raised:    #27272A    (zinc-800)
border:       rgba(255, 255, 255, 0.10)
text-primary: #FAFAFA
text-muted:   #71717A
accent:       #FAFAFA    (white — buttons are white-on-black)
status only:  green/red/yellow/blue for status indicators
```

**Character:**
- Almost no accent color — white is the accent
- Color appears ONLY for status (green=healthy, red=error, yellow=warning, blue=info)
- Extremely clean, confident, "I don't need color to look good"
- Primary buttons are white text on white-ish background, secondary are ghost
- Data visualization uses desaturated colors
- Very precise spacing — 4px grid system

**Typography:**
- UI: Inter 13px
- Data: Geist Mono or JetBrains Mono 12.5px
- Headings: Inter medium 15px
- Everything lowercase in headers (no uppercase tracking)

**Cards:**
```
border-radius: 8px
border: 1px solid rgba(255, 255, 255, 0.10)
background: #18181B
```
No effects, no gradients, no glow. Precision.

**When to choose:** If you want the most "mature" and timeless look. Risks feeling cold, but ages well.

---

### Variant D: "Cyber Homelab" (hybrid — recommended)

Takes the best elements from A, B, and C.

**Palette:**
```
bg-base:      #0A0A0F    (near-black, slight blue tint)
bg-surface:   #111118
bg-raised:    #1A1A24
border:       rgba(255, 255, 255, 0.07)
text-primary: #E8E8F0
text-muted:   #6E6E7A
accent:       #4FD1C5    (keep current teal — it's already distinctive)
accent-glow:  rgba(79, 209, 197, 0.12)
secondary:    #818CF8    (indigo for interactive highlights)
```

**Character:**
- Keeps the teal identity but darkens and tightens the base
- Teal accent reserved for: active nav, primary buttons, status "running", accent borders
- Indigo used for: focus rings, selected states, diff highlights
- Smaller border-radius (8px cards, 6px buttons, 4px inputs) — ops tool, not consumer app
- Noise texture at 3% opacity on base
- Subtle glow on primary CTA and focused inputs only
- Dot-grid background on empty states only (not on data-heavy pages)
- Mono for all data values (container IDs, paths, timestamps, metrics, commit hashes)
- Tight 4px baseline grid
- Card top-edge sheen (1px gradient highlight) using teal

**Typography:**
- UI labels: Inter 13px
- Data values: JetBrains Mono 12.5px (already used, expand to more places)
- Page titles: Space Grotesk 18px semibold (personality, keep)
- Section headers: Inter medium 13px
- Table headers: 11px uppercase tracking-wider muted
- Sidebar nav: 13px

**Specific improvements over current:**
- `border-radius: 28px` panels → `8px` — tighter, denser, more serious
- Teal radial gradient blobs on body → removed or replaced with subtle noise texture
- Panel glass effect → flat surface with barely visible border
- Large panel shadows → removed, rely on background-color steps
- "Dashboard" heading → smaller, tighter, semibold not 3xl
- Stack cards → denser rows with mono service counts
- Sidebar → narrower (48px collapsed / 200px expanded), icon-only on tablet

**Key additions:**
1. Noise overlay (3% opacity SVG) on body for anti-banding
2. Card top-edge gradient sheen (teal, 1px, centered)
3. Focused input glow: `box-shadow: 0 0 0 1px var(--accent), 0 0 12px var(--accent-glow)`
4. Status dots with live pulse animation
5. Terminal cursor blink on active terminal tab indicator
6. Key-value pairs in mono grid format (Host page, Stack overview)

---

## Comparison Matrix

| Aspect | A: Mission Control | B: Terminal Noir | C: Monochrome Ops | D: Cyber Homelab |
|---|---|---|---|---|
| Accent color | Green | Indigo/Violet | White (no accent) | Teal (current) + Indigo |
| Border-radius | 6-8px | 8px | 8px | 8px |
| Decorative effects | Noise only | Glow + dot-grid + card sheen | None | Noise + card sheen + glow on focus |
| Monospace usage | Heavy | Medium | Medium | Medium-heavy |
| Typography | Inter only | Inter + Space Grotesk | Inter only | Inter + Space Grotesk + JetBrains Mono |
| Info density | Highest | Medium-high | Medium | Medium-high |
| Risk | Too clinical | Too flashy | Too cold | Balanced |
| Personality | "Ops center" | "Hacker workstation" | "Mature SaaS" | "Enthusiast homelab" |

## Recommendation

**Variant D: Cyber Homelab.**

It preserves the teal identity you already have, tightens the visual density to feel like a real ops tool, adds just enough texture and effects to make it visually distinctive without crossing into cheesy territory. The hybrid approach lets you tune the "nerdiness dial" — more glow/noise for personality, or dial it back toward C for sobriety.

## Implementation Approach

Revamp should be CSS/token-level, not structural:

1. Update `index.css` design tokens
2. Reduce border-radius across all panels/cards/buttons
3. Add noise overlay
4. Update card styles (flat surface + top-edge sheen)
5. Tighten typography scale
6. Expand mono usage in data-heavy areas
7. Add focus glow to inputs/buttons
8. Update sidebar to be narrower/tighter

No layout changes. No component restructuring. No route changes.

## Next Steps

1. Owner picks a variant (or mix)
2. I implement token changes + CSS in one commit
3. Review on live instance
4. Iterate on details
