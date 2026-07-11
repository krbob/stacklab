# Readability and Motion Policy

Stacklab keeps the dense Amber Console presentation while applying a small set
of non-negotiable readability rules.

## Typography

- UI text uses a 12px minimum; dense labels rely on weight, tracking, and
  monospace treatment instead of 9–11px sizes.
- Inter Variable, Space Grotesk Variable, and JetBrains Mono Variable ship as
  local WOFF2 assets through pinned Fontsource packages. The application does
  not depend on an external font CDN. Their OFL notices ship with the frontend
  at `/font-licenses.txt`.
- Native controls remain at least 16px where mobile browsers would otherwise
  zoom the viewport.

## Contrast

Semantic foreground tokens (`text`, `muted`, `dim`, `accent`, `ok`, `warning`,
and `danger`) maintain at least a 4.5:1 contrast ratio against both base and
panel surfaces. A unit test calculates these ratios directly from
`frontend/src/index.css` and rejects sub-12px arbitrary text utilities.

Disabled controls may use lower opacity because they are inactive, but status,
help, shortcut, and progress-detail text must use an AA-tested token without an
additional opacity reduction.

## Motion and texture

When `prefers-reduced-motion: reduce` is active, animations and transitions are
reduced to a single near-zero-duration frame and smooth scrolling is disabled.
The decorative noise layer is also removed. Forced-colors mode removes the
noise layer so it cannot interfere with user-selected system colors.

The normal noise texture remains intentionally subtle: three octaves and 1.5%
source opacity. It must never carry information or capture pointer events.
