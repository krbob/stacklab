# UI Review 2026-07 + Final Design "Amber Console v2"

> **Status: dated review snapshot.** Findings were evaluated against the build
> named below. The current remediation plan and frontend tests supersede its
> open/closed status.

Audyt live (stacklab.bobinski.net, build 2026.07.0~nightly20260704) — 14 ekranów,
desktop 1440 i mobile 390, Playwright na produkcji. Pełna wersja z mockupami:
artefakt "StackLab — przegląd UI i finalny design" (link w rozmowie z 2026-07-04).

## Bugi P1 (naprawić przed pracami designowymi)

1. **Ucinany output kroków (Maintenance progress + Audit → details)** —
   `step-cards.tsx:74-96`: zwinięty podgląd = ostatnie 2 linie *logiczne* przycięte
   twardo do `max-h-14`; jedna długa linia (docker pull) zawija się na 6+ linii
   wizualnych i jest cięta w połowie, a "Show all" pojawia się tylko przy >2 liniach
   logicznych → pojedynczej długiej linii nie da się rozwinąć w ogóle.
   Fix: toggle na podstawie zmierzonego `scrollHeight`, fade-out zamiast twardego
   cięcia, `overflow-wrap: anywhere`.
2. **Mobile: Config bez drzewa plików** — `config-page.tsx:257`: lewy panel
   `hidden … lg:flex` bez mobilnej alternatywy; empty state odsyła do panelu,
   którego nie ma. Docelowo bottom-sheet z drzewem.
3. **Mobile: poziomy overflow strony** — `maintenance-page.tsx:95-107` (rząd
   zakładek bez `overflow-x-auto`/wrap) oraz CodeMirror w stack editor i Create
   stack (brak `min-w-0`; edytor wymusza ~1600px na 390px viewportu).
4. **Mobile: treść widoczna nad toolbarem** — `root-layout.tsx:75`: header
   `bg-[var(--bg)]/95 backdrop-blur`, brak `env(safe-area-inset-top)` przy
   `viewport-fit=cover`. Fix: nieprzezroczyste tło + safe-area padding.
5. **Host: overlap wersji na hostname** — `host-page.tsx:130`: długi string wersji
   w `text-lg` bez `min-w-0`/`break-all` wypływa z komórki grida na sąsiednią kartę.

## P2/P3

- Audit: sztywne szerokości kolumn (`audit-table.tsx:46-50`) ucinają nazwy akcji
  ("update_maintenan…"); "View detail" niespójnie tylko na części wierszy.
- Stack overview: status "Running" zdublowany (layout + strona).
- Stats: "Waiting for stats..." bez timeoutu/stanu błędu.
- Obce akcenty kolorystyczne: sky-400 (running), cyan (serwisy w logach), fiolet
  (meter Memory na Host), niebieskie natywne checkboxy (brak `accent-*`),
  One Dark (granat) w CodeMirror.
- Geometria: mieszanka rounded-full / rounded-lg / rounded-2xl.
- Nagłówki stron niespójne (kicker/rozmiar/układ różne per ekran).
- Puste stany bez treści i akcji; destrukcyjne akcje (Down/Remove) w jednym
  rzędzie ze zwykłymi.

## Finalny design — zasady (v2; paleta Amber Console bez zmian)

- **Z1 Gęstość**: siatki zamiast list 1-kolumnowych; ekran pokazuje stan bez klikania.
- **Z2 Amber = tożsamość, nie status**: statusy to zamknięta czwórka ok/warn/err/off;
  "running/w toku" przechodzi na amber (koniec sky-400).
- **Z3 Mono niesie dane**: wartości (nazwy, wersje, czasy, ścieżki, logi) w mono
  z tabular-nums; UI w sans.
- **Z4 Jedna geometria**: radius karty 8 / kontrolki 6 / chipy 4; pigułki tylko
  dla chipów statusu; hierarchia borderem, nie cieniem.
- **Z5 Klawiatura to feature**: hotkeye 1-7 (nav, podpisane w UI), `/` filtr,
  `⌘K` paleta, `j/k` po kafelkach.

Tokeny — delta: `--status-warn: #F97316` (warn przestaje dziedziczyć amber —
ryzyko z round-2 się zmaterializowało), `--status-run: amber pulsujący`,
radius 8/6/4, własny theme CodeMirror na tokenach (bg #171107, klucze amber),
wykresy/metery: amber + desaturowane pochodne (koniec fioletu/cyanu).

## Struktura ekranów

- **Stacks**: staggered grid (CSS columns, bez JS) kafelków z telemetrią —
  status na lewej krawędzi (3px), uptime, obraz, cpu/mem metery, lista serwisów
  przy >1, badge UPDATE (dostępna aktualizacja obrazu). Toolbar: filtr `/`,
  chipy Wszystkie/Problemy/Do aktualizacji.
- **Maintenance/Audit progress**: fix podglądu kroku (P1.1); panel Progress
  w bezczynności pokazuje historię ostatnich przebiegów zamiast pustki.
- **Settings**: staggered grid kart-sekcji (Bezpieczeństwo / Powiadomienia /
  Harmonogramy / Self-update / About), każda z własnym zapisem i statusem zapisu.
- **Audit**: elastyczna tabela, każdy wiersz klikalny, filtry (akcja/stack/status/
  zakres dat), paginacja.
- **Host**: 2 kolumny, logi na pełną wysokość, metery amber.
- **Stack detail**: status raz przy tytule; Down/Remove za separatorem lub w "⋯";
  zakładki jako tab bar (podkreślenie), nie pigułki.
- **Globalnie**: host-strip (k9s-style) `homelab · docker 26.1.5 · 19/19 up · disk…`
  nad treścią; nav z podpisami hotkeyów.

## Mobile — strategia

- Bottom nav (Stacks/Host/Maint/Audit/Więcej) + safe-area; hamburger tylko
  w "Więcej". Header nieprzezroczysty z safe-area-top, w nim host-strip.
- Stacks jako zwarte wiersze (pip statusu · nazwa · cpu/mem), nie karty.
- Drzewa plików (Config, Files) jako bottom-sheet nad pełnoekranowym edytorem.
- Zakładki poziomo scrollowane; tabele → karty; edytor `min-w-0`, font ≥16px
  (inaczej iOS zoomuje focus).

## Backlog → design (uzupełnienie 2026-07-04)

Przegląd `docs/roadmap.md` (near-term + backlog candidates) pod kątem rzeczy,
które finalny design powinien uwzględnić od razu:

- **Strukturalny progres zamiast log-dumpów** (roadmap: "richer operation
  progress"): step cards dostają pasek postępu i licznik jednostek
  (`7/12 layers · extracting 45MB/120MB`) zasilany z nowego pola `progress`
  na eventach jobów. Logi zostają jako zwijany szczegół, nie główny widok.
  Kontrakt: docs/api/dashboard-read-model-and-progress.md, Slice C.
- **Widoczność pracy w tle**: host-strip pokazuje żywy chip aktywnego joba
  ("1 job · pull traefik 30/40") + tray z aktywnymi jobami; kafelki stacków
  dostają badge aktywności. Zasilanie: push `activity.subscribe` po istniejącym
  WS (Slice D), REST zostaje jako fallback.
- **Drift i health na kafelkach za darmo**: `config_state: drifted`,
  `runtime_state: partial/error` i `health_summary` już są w `GET /api/stacks` —
  frontend ich nie konsumuje. Kafelki: badge DRIFT (amber outline), stany
  partial (podwójny pip), unhealthy (warn). Diff "draft vs last valid"
  w edytorze po domknięciu deploy-baseline (backlog, Slice E).
- **Ikony i linki stacków** (roadmap: custom project metadata): `x-stacklab`
  w compose.yaml (icon + links) → kafelek z ikoną aplikacji i szybkim linkiem
  do web UI usługi. Compose-first, git-versioned (Slice A2).
- **Badge UPDATE naprawdę**: nie rozszerzenie inventory, tylko nowy job
  `check_image_updates` (digest lokalny vs rejestr, harmonogramowalny,
  notyfikacja przy przejściu na "available") + rollup per stack (Slice B/A3).
- **Create stack z szablonami** (roadmap goal #1): picker lokalnych szablonów
  zamiast pustego CodeMirrora (Slice F).
- **Lint Compose w edytorze** (backlog): `warnings[]` w validate — gutter
  w edytorze + chip warn na kafelku (brak healthchecka, restart policy,
  bind 0.0.0.0). Nigdy nie blokuje deployu (Slice E).
- **Wykluczenia serwisów z harmonogramów** (backlog): checklist per serwis
  w karcie harmonogramu w Settings; pominięte cele raportowane w progresie.
- **Theme toggle z roadmapy — de-priorytet**: sprzeczny z decyzją round-2
  ("dark themes only"); Amber Console v2 zostaje dark-only, ewentualny drugi
  wariant ciemny (np. Catppuccin) jako accent-swap kiedyś, nie teraz.

## Plan wdrożenia (zrewidowany)

1. **Etap 1 — bugfixy P1** (małe izolowane PR-y, bez zmian designu).
2. **Etap 2 — tokeny v2** (warn/run/radius, czystka obcych kolorów, theme CM,
   wspólny nagłówek stron) — jeden PR.
3. **Etap 3 — layouty**: stacks grid (konsumpcja drift/health/partial + API
   Slice A1/A2) → settings grid → audit tabela → mobile bottom nav + sheety.
4. **Etap 4 — progres i tło**: strukturalny progres (Slice C) → activity push
   (Slice D) → host-strip + tray + hotkeye (Z5).
5. **Etap 5 — funkcje**: update checks + badge (Slice B/A3) → lint + diff
   last_valid (Slice E) → szablony create stack (Slice F) → wykluczenia
   serwisów w harmonogramach.

Zmiany API zaprojektowane i sekwencjonowane w
`docs/api/dashboard-read-model-and-progress.md` (wszystkie addytywne).
