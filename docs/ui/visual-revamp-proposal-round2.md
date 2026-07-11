# Visual Revamp Proposal — Round 2

> **Status: historical design decision record.** The selected Amber Console
> direction is implemented; use current frontend tokens and components for
> exact behavior.

Direction after owner feedback: **dark themes only**, nerd / hacker / homelab character.

**Decision: Variant E "Amber Console" — implemented** (July 2026). Token swap in
`frontend/src/index.css`, brand tints `rgba(34,197,94,*)` → `rgba(245,165,36,*)`,
`zinc-*` grays → warm `stone-*`, amber logo/favicon/PWA icons, left-rail active nav.
Success/running semantics stay green (`emerald-*`). G's structural items (stacks table,
keyboard navigation) remain open as a separate work item.

Round 1 (variants A–D) explored neutral-dark panels with a single accent: green (A,
implemented), indigo (B), white (C), teal+indigo (D). Round 2 differentiates on character
instead: warm industrial, the selfhosted-community pastel standard, and terminal density.

Interactive mockups (dashboard + Host, shared data, variant switcher):
open `docs/ui/visual-revamp-round2-mockups.html` in a browser.

---

## Variant E: "Amber Console"

Inspired by: instrument panels, avionics night modes, power-station control rooms.

**Palette:**
```
bg-base:      #0C0906    (warm near-black, brown undertone)
bg-surface:   #151007
bg-raised:    #201808
border:       rgba(245, 165, 36, 0.13)
text-primary: #F2EADB    (warm off-white)
text-muted:   #97907E
accent:       #F5A524    (amber)
accent-ink:   #201302    (text on amber buttons)
status-ok:    #55D47F
status-warn:  #F5A524    (inherits accent — see risk note)
status-err:   #F26D5B
status-off:   #7C7566
radius:       6px
```

**Character:**
- Amber is identity only (brand, nav, section labels, primary CTA); status semantics stay
  green/red, and nothing else on screen is green or red — statuses pop instantly.
- Active nav item gets a 2px left rail (inset box-shadow) instead of a filled pill.
- Card labels carry a `▮` marker like switchboard nameplates.
- Meters slightly thicker (5px), warm track color.
- Running/partial dots glow subtly; warm palette is easier on the eyes at night.

**Risk:** amber sits close to the yellow "warning" hue. Here `warn` inherits the accent and
relies on icon/context for disambiguation. If that proves confusing, shift warn to orange
`#F97316` and desaturate the accent slightly.

**When to choose:** maximum personality at pure token-level cost. No structural changes.

---

## Variant F: "Catppuccin Den"

Inspired by: Catppuccin Mocha — the de facto theme standard of the selfhosted /
r/unixporn community.

**Palette (Catppuccin Mocha mapping):**
```
bg-base:      #11111B    (crust)
bg-surface:   #181825    (mantle)
bg-raised:    #1E1E2E    (base)
border:       #313244    (surface0, solid — hierarchy via surface layers, not shadows)
text-primary: #CDD6F4    (text)
text-muted:   #7F849C    (overlay1)
accent:       #CBA6F7    (mauve)
accent-ink:   #1E1E2E
status-ok:    #A6E3A1    (green)
status-warn:  #F9E2AF    (yellow)
status-err:   #F38BA8    (red/pink)
status-off:   #6C7086    (overlay0)
radius:       10px
```

**Character:**
- Pastel accents on layered dark surfaces; active nav = mauve text on `surface0`.
- No glows, no textures — the palette itself is the identity.
- The wider Catppuccin palette (peach, teal, blue, lavender) gives ready-made, coherent
  colors for future charts, diffs and badges.

**Why it works:** instant recognition in the target group ("built by one of us"); official
Catppuccin ports exist for Grafana, Home Assistant, terminals and editors, so Stacklab
slots into an already-themed homelab.

**Risks:** it is a borrowed identity — mainstream within the niche; pastel softness pulls
toward consumer, so radius stays at 10px (no return to 28px).

---

## Variant G: "TUI Dense"

Inspired by: k9s, lazydocker, lazygit.

**Palette:**
```
bg-base:      #0A0C0C
bg-surface:   #0A0C0C   (flat — hierarchy via borders, not surfaces)
bg-raised:    #101414   (selection / hover only)
border:       #1F2B2B   (solid, visible)
text-primary: #D8E4E4
text-muted:   #5F7373
accent:       #22D3EE   (cyan)
accent-ink:   #042A31
status-ok:    #34D399 · warn #FBBF24 · err #FB7185
radius:       0
font:         monospace everywhere (UI labels included)
```

**Character / structure (this variant is not token-only):**
- Stack list becomes a table (name / services / up / state / last job) — fits 2–3× more
  rows than cards; selected row = 2px accent rail + raised background.
- Top bar with host summary (`host: homelab-01 · docker 29.3.1 · 14/17 up`).
- Bottom keybar with real shortcuts: `[j/k] move · [enter] open · [s] start · [x] stop ·
  [l] logs · [e] edit compose · [/] filter` — forces implementing keyboard navigation,
  which is a genuine feature, not decoration.
- Status dots are small vertical bars (▮), not circles; nav items carry `[1]`–`[6]` hotkeys.

**Risks:** monospace tires on long prose (Settings, descriptions — sans is acceptable
there); the look polarizes users.

**Partial adoption:** the table view + keyboard shortcuts are worth shipping under any
palette. Treat them as a separate work item.

---

## Comparison (vs implemented A and round-1 recommendation D)

| Aspect | A (current) | D (round 1 rec.) | E Amber | F Catppuccin | G TUI |
|---|---|---|---|---|---|
| Identity | green/neutral | teal+indigo | amber/warm black | mauve/Mocha pastels | cyan/terminal |
| Density | medium | medium-high | medium-high | medium | **highest** |
| Cost | — | CSS tokens | CSS tokens | CSS tokens | tokens + table + keyboard |
| Risk | generic | safe | amber vs warn | borrowed identity | polarizing |
| Personality | ops center | enthusiast homelab | control room | cozy homelab | k9s in a browser |

## Recommendation (dark-only, hacker/homelab brief)

**G "TUI Dense" as the primary direction** — the most hacker of the set, and the only one
that ships functional wins beyond a reskin (stack table, keyboard navigation, keybar).
The palette is orthogonal to the structure: default cyan keeps a clean terminal mood, while
a **G×E hybrid (TUI structure + amber tokens)** yields a retro-terminal with the most
personality.

**E "Amber Console"** is the best pick for effect without structural work (pure token cost).
**F "Catppuccin Den"** if coherence with an already Catppuccin-themed homelab matters most.

Suggested sequencing:

1. Token pass: move remaining hardcoded values in `index.css` / components onto variables
   (prerequisite for any variant, cheap).
2. Ship the chosen palette (E / F / G-cyan) as a token-only commit; review on live instance.
3. Separately scope G's structural items: stacks table view (threshold: >8–10 stacks),
   keyboard navigation + keybar.
