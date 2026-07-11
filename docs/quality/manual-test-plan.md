# Kompleksowy plan testów StackLab (manualne + eksploracyjne)

## Cel

Przetestować możliwie jak najwięcej ścieżek aplikacji na docelowym środowisku
(Linux + Docker), weryfikując zarówno **poprawność działania** funkcji, jak i
**UX** (czytelność stanów, komunikaty błędów, nawigacja, klawiatura,
responsywność, reconnect/sesje).

Plan uzupełnia — a nie zastępuje — testy automatyczne:

- backend: `go test ./...` (29 plików `*_test.go`, w tym integracyjne HTTP/WS i
  Docker-backed)
- frontend: `npm test` (Vitest, 28 plików), `npm run typecheck`, `npm run lint`
- E2E: Playwright (`frontend/e2e`, 6 speców: login/dashboard, editor,
  create/delete, audit, maintenance, config-workspace)

Skupiamy się na **lukach**: przepływy, których automaty nie dotykają, warstwa
UX/wizualna, ścieżki błędów, bezpieczeństwo, oraz funkcje świeżo dodane
(szablony, image updates, docker admin, self-update, notyfikacje, git workspace).

Odniesienie do kryteriów akceptacji: `docs/quality/acceptance-criteria.md`
(sekcje A–O). Ten plan rozwija je do wykonywalnych scenariuszy.

---

## 1. Środowisko testowe

### 1.1 Wymagania VM (co jest potrzebne)

- Linux `amd64` (docelowo też sprawdzenie `arm64`, jeśli dostępne), root/sudo
- Docker Engine + Compose v2 (`docker compose` lub `docker-compose`)
- Go `1.26.5`, Node.js `24.18.0` z dołączonym npm (do budowy frontendu i backendu ze
  źródeł) — **albo** zainstalowany pakiet `.deb`/tarball, jeśli testujemy tor
  produkcyjny
- Dostęp do internetu (image updates, pull obrazów, ewentualnie APT self-update)
- `git`, `setfacl`/`getfacl` (naprawa uprawnień strategią ACL), `systemd`
  (docker admin, self-update, host logs)

### 1.2 Co potrzebuję od Ciebie (Bob)

1. **Dostęp do VM** — SSH (host, user, klucz/hasło) albo uruchomienie Claude
   Code bezpośrednio na VM. Do testów Docker-backed, `/host`, docker admin i
   self-update potrzebny jest realny Linux — macOS jest niereprezentatywny.
2. **Tryb instalacji do przetestowania** — czy testujemy (a) uruchomienie ze
   źródeł (`go run`), (b) tarball, (c) pakiet `.deb` z APT. Rekomenduję zacząć
   od (a) dla szybkiej iteracji, a tor (b)/(c) domknąć w sekcji 8 (deployment).
3. **Rejestr prywatny** (opcjonalnie) — dane logowania do dowolnego prywatnego
   registry, żeby przetestować Docker registry auth i pull prywatnego obrazu.
   Jeśli brak — pominiemy scenariusze R-*.
4. **Kanał powiadomień** (opcjonalnie) — webhook URL (np. z webhook.site) i/lub
   token+chat_id Telegrama, żeby zweryfikować realną wysyłkę notyfikacji.
   Bez tego testujemy tylko walidację konfiguracji i przycisk „Test".
5. **Zgoda na akcje destrukcyjne** — plan tworzy/usuwa stacki, wolumeny, sieci,
   robi `docker prune`, restartuje daemon Dockera (docker admin) i potencjalnie
   restartuje usługę StackLab (self-update). Wszystko na **izolowanej VM**, nie
   na produkcji `stacklab.bobinski.net`.

### 1.3 Uruchomienie (tor deweloperski)

```bash
# backend (z korzenia repo)
export STACKLAB_ROOT="$PWD/.local/stacklab"
export STACKLAB_DATA_DIR="$PWD/.local/var/lib/stacklab"
export STACKLAB_HTTP_ADDR="0.0.0.0:8080"        # dostęp z LAN do testów UI
export STACKLAB_LOG_LEVEL="debug"
export STACKLAB_BOOTSTRAP_PASSWORD="change-me-test-password"
# (opcjonalnie helpery — patrz sekcja 9)
go run ./cmd/stacklab

# frontend zbudowany i serwowany przez backend:
cd frontend && npm ci && npm run build
# → otwórz http://<vm-ip>:8080

# ALBO dev z hot-reload (dwa procesy):
cd frontend && npm run dev      # proxy /api → backend
```

Alternatywa: gotowy harness E2E z fixturami (`scripts/e2e/run-backend.sh`) —
stawia backend na `127.0.0.1:18081` z fixturą `test/fixtures/e2e/root`
(zawiera stack `demo` + celowo zablokowany plik `config/blocked-fixture/`).
Ustaw `STACKLAB_E2E_ENABLE_WORKSPACE_REPAIR=1`, żeby zbudować i włączyć helper
naprawy uprawnień.

### 1.4 Dane testowe (seed stacków)

Przygotować pod `$STACKLAB_ROOT/stacks/` zestaw pokrywający warianty
(por. `docs/ops/local-dev.md`):

| Stack | Charakterystyka | Do czego |
|---|---|---|
| `web-simple` | pojedynczy serwis `image:` (np. nginx), 1 port | overview, up/down, logi, stats, terminal |
| `web-build` | serwis z `build:` + `Dockerfile` | build, tryb build vs image, pliki stacka |
| `db-health` | serwis z `healthcheck` (np. postgres) | health summary, kolorowanie stanu |
| `multi` | 2–3 serwisy, zależności `depends_on` | logi/stats per-serwis, kolejność |
| `stopped` | poprawny, celowo nieuruchomiony | stan `stopped`, empty states diagnostyki |
| `invalid` | błędny YAML / błędny `compose config` | walidacja edytora, blokada deploy |
| `orphaned` | uruchomić, potem usunąć `compose.yaml` z dysku | zachowanie orphaned (sekcja D) |

Seed audytu/retencji (job detail drawer, retention notice):

```bash
go run ./scripts/dev/seed-retention-fixtures.go \
  --db "$STACKLAB_DATA_DIR/stacklab.db" --run-prune
```

---

## 2. Macierz pokrycia (auto vs. manualne)

| Obszar | Auto (unit/integracja/E2E) | Główny cel testów manualnych |
|---|---|---|
| Auth/sesja | ✅ unit + E2E login | lockout, wygaśnięcie, 401 w locie, zmiana hasła |
| Dashboard/discovery | ✅ E2E + unit | filtry/chipy, auto-refresh, empty states, live discovery z FS |
| Stack detail | ✅ unit | mapowanie runtime↔serwis, health, porty/mounty na realnym Dockerze |
| Editor | ✅ E2E + unit | preview draft, lint warnings, save&deploy chain, drift |
| Akcje lifecycle | ✅ Docker-integ. | locking współbieżny, progress na żywo, wszystkie 7 akcji z UI |
| Job progress | ✅ hook testy | recovery po nawigacji/reconnect, step cards, stany terminalne |
| Logs | ✅ hook test | filtry serwisów, pauza, reconnect, duże bufory |
| Stats | ✅ hook test | wartości na żywo, historia 5 min, empty gdy brak running |
| Terminal | ✅ unit | realne PTY, resize, limit sesji, idle timeout, reattach |
| Audit | ✅ E2E + contract | linki do job detail, retencja, filtry globalne |
| Config workspace | ✅ E2E + unit | edycja, git diff/commit/push, blocked files |
| Git workspace | częściowo | commit selektywny per-plik, push, konflikt |
| Maintenance | ✅ E2E (prune) + unit | bulk update, images/networks/volumes CRUD, prune preview |
| Image updates | ✅ unit | realne sprawdzenie digestów, rollup na kaflach |
| Templates | ✅ contract | wybór szablonu w kreatorze, treść, deploy |
| Docker admin | ✅ unit | realny validate/apply daemon.json, rejestry login/logout |
| Host observability | ✅ unit | metryki, viewer logów, follow |
| Notifications | ✅ unit | realna wysyłka webhook/Telegram, test, harmonogramy |
| Self-update | ✅ unit | realny apply przez helper (tylko instalacja pakietowa) |
| Bezpieczeństwo | częściowo | path traversal, origin, XSS w terminalu/logach, auth na WS |
| Deployment | smoke skrypty | systemd, reverse proxy, upgrade/rollback |
| Cross-cutting UX | — | responsywność, klawiatura, ⌘K, dostępność, motyw |

---

## 3. Scenariusze — Uwierzytelnianie i sesja (A)

> REST: `/api/session`, `/api/auth/login`, `/api/auth/logout`,
> `/api/settings/password`. Cookie `stacklab_session` (HttpOnly, SameSite=Strict,
> Secure wg configu). Argon2id. Lockout: 5 porażek / okno 5 min → blokada 5 min.

- **A1** Wejście na dowolny URL bez sesji → redirect na `/login` (AuthGuard),
  po zalogowaniu powrót do żądanej ścieżki (`state.from`).
- **A2** Logowanie poprawnym hasłem → `/stacks`. Cookie ustawione, `HttpOnly`.
- **A3** Błędne hasło → czytelny błąd, **bez** ujawniania szczegółów. Pole nie
  gubi fokusa; brak stack trace.
- **A4** Lockout: 5× błędne hasło z jednego IP → komunikat „too many attempts";
  poprawne hasło w oknie blokady dalej odrzucane; po 5 min znów działa.
- **A5** Logout → sesja zniszczona, powrót na `/login`, cofnięcie w przeglądarce
  nie przywraca dostępu (ponowny redirect).
- **A6** Wygaśnięcie idle (skróć `STACKLAB_SESSION_IDLE_TIMEOUT`, np. `1m`):
  po bezczynności następne żądanie REST → 401 → redirect na `/login`.
- **A7** Wygaśnięcie w locie na WS: gdy sesja padnie przy otwartym Logs/Terminal,
  WS zamyka się (1008/4401), fetch `/api/session` = 401 → redirect na login,
  bez pętli reconnectu.
- **A8** Absolute lifetime (`STACKLAB_SESSION_ABSOLUTE_LIFETIME`): sesja wygasa
  mimo aktywności po przekroczeniu limitu.
- **A9** Zmiana hasła (`/settings`): złe „current" → błąd; nowe ≠ confirm → błąd;
  poprawna zmiana → stare hasło przestaje działać, nowe działa.
- **A10** Nieuwierzytelniony dostęp do chronionego REST (np. `curl /api/stacks`
  bez cookie) → 401. `/api/live`, `/api/ready` i zgodnościowe `/api/health`
  dostępne bez auth; zasymulowana awaria DB daje `live=200`, `ready=503`.
- **A11** Cookie `Secure`: przy `STACKLAB_COOKIE_SECURE=true` cookie nie leci po
  czystym HTTP (weryfikacja atrybutów w DevTools / za reverse proxy TLS).

---

## 4. Scenariusze — Dashboard i discovery (B)

> `GET /api/stacks?q=&sort=`. Auto-refresh co 10 s gdy karta widoczna.
> Chipy: All / Problems / Updates. Skrót `/` = fokus filtra, `⌘K` = paleta.

- **B1** Discovery z FS: dodaj katalog stacka z `compose.yaml` ręcznie na dysku →
  pojawia się na dashboardzie bez seedowania DB (max po auto-refresh).
- **B2** Usunięcie `compose.yaml` z dysku → stack znika z normalnej listy
  (lub przechodzi w orphaned, jeśli ma running kontenery — patrz D).
- **B3** Poprawność `display_state` / `config_state` / `activity_state` dla:
  running, stopped, error, defined-only, orphaned.
- **B4** Filtr tekstowy (`stacks-filter`, skrót `/`) — filtruje po nazwie;
  „No stacks match" gdy brak trafień.
- **B5** Chip „Problems" → tylko stacki z błędem/niezdrowe; „Updates" → tylko z
  dostępną aktualizacją obrazu (spójne z rollupem image-updates).
- **B6** Empty state: pusty root → „No stacks found" + CTA „Create your first
  stack".
- **B7** Auto-refresh: zmiana stanu kontenera z CLI (`docker stop`) odbija się na
  kaflu w ≤ ~10 s; przełączenie karty w tło wstrzymuje odświeżanie.
- **B8** Kafel z metadanymi (ikona, linki zewnętrzne) — stretched-link nie łapie
  klików w link zewnętrzny; link otwiera właściwy URL.
- **B9** „Check updates" (sekcja Image updates, patrz N) — progres „Checking X/Y",
  po zakończeniu lista/rollup odświeżone.
- **B10** Błąd ładowania listy (np. ubij backend) → czytelny komunikat błędu,
  nie biały ekran.

---

## 5. Scenariusze — Szczegóły stacka (C, D)

> Zakładki warunkowane `capabilities`; niedostępne = wyszarzone z tooltipem.

- **C1** Overview pokazuje wszystkie serwisy z `compose.yaml`; mapowanie
  running kontenerów do serwisów poprawne (kropka statusu).
- **C2** Tryb image vs build wyświetlany per-serwis zgodnie z definicją.
- **C3** Porty i kluczowe mounty widoczne; „Not created" dla serwisu bez
  kontenera.
- **C4** Health summary (♥/!/~) dla stacka `db-health`; brak health → brak
  fałszywego wskaźnika.
- **C5** Zakładki renderują się wg capabilities — dla stacka bez uprawnienia
  Terminal/Editor zakładka wyszarzona, tooltip „not available", nieklikalna.
- **D1** Orphaned: uruchom stack, usuń `compose.yaml` z dysku → prezentowany jako
  `orphaned`, banner widoczny.
- **D2** Orphaned: zakładka Editor wyłączona; Logs/Stats/Terminal/History nadal
  działają.
- **D3** Orphaned: dostępne tylko bezpieczne akcje (zgodnie z `available_actions`
  z backendu — brak np. build).

---

## 6. Scenariusze — Edytor Compose (E)

> `GET/PUT /api/stacks/{id}/definition`, `GET/POST .../resolved-config`.
> Zakładki compose.yaml / .env. Przyciski: Preview, Discard, Save
> (`editor-save`), Save & Deploy (`editor-save-deploy`).

- **E1** Edytor ładuje `compose.yaml` z dysku; CodeMirror z podświetlaniem YAML,
  Tab wcina, motyw Amber Console.
- **E2** `.env` obecny → ładuje; nieobecny → zakładka „.env (new)", zapis tworzy
  plik.
- **E3** Zmiana treści → „· Unsaved changes"; Discard przywraca; nawigacja z
  niezapisanymi zmianami (sprawdź, czy nie gubi cicho danych).
- **E4** Save niepoprawnego configu → zapis **przechodzi** (praca w toku),
  ale status „✗ error" i Save & Deploy zablokowany.
- **E5** Preview z zapisanych plików (`source=current`) → resolved config w
  panelu split.
- **E6** Preview z **niezapisanego** draftu (POST resolved-config) → resolved bez
  zapisu na dysk (zweryfikuj, że plik na dysku niezmieniony).
- **E7** Lint warnings (advisory) — np. brak healthcheck / restart policy /
  publiczny `0.0.0.0` bind → pokazane jako ostrzeżenia, **nie** blokują Save ani
  Deploy.
- **E8** Save & Deploy (poprawny config): zapisuje → job → `up`; ProgressPanel na
  żywo; po sukcesie stan stacka zaktualizowany, drift baseline odświeżony.
- **E9** `resolved-config?source=last_valid` → przed pierwszym deployem `409`;
  po udanym deployu zwraca ostatnią wdrożoną konfigurację nawet po edycji draftu.
- **E10** Komunikaty walidacji wystarczają do zlokalizowania błędu (linia/opis).

---

## 7. Scenariusze — Akcje lifecycle i joby (F, G)

> `POST /api/stacks/{id}/actions/{action}`; akcje: validate, up, down, stop,
> restart, pull, build, recreate. Locking per-stack. Joby async, WS `jobs.*`.

- **F1** Każda z akcji z ActionBara (Deploy/Restart/Stop/Down/Pull/Build) na
  `web-simple` → tworzy job, ProgressPanel, końcowy stan zgodny z rzeczywistością
  (`docker ps`).
- **F2** Recreate — kontenery odtworzone; weryfikacja przez nowe ID kontenerów.
- **F3** Build na `web-build` — realny build, log budowania w progresie.
- **F4** Pull na `web-simple` — pobranie obrazu, progres warstw (JSON progress,
  fallback na plain gdy brak `--progress json`).
- **F5** Locking: uruchom długą akcję (np. pull ciężkiego obrazu), spróbuj drugiej
  akcji na tym samym stacku → odrzucona (409/„locked"), przyciski zablokowane,
  kafel „Working…".
- **F6** Równoległość: akcje na dwóch różnych stackach jednocześnie → obie
  przechodzą niezależnie.
- **F7** Down z opcjami usunięcia — patrz delete dialog (F12–F14).
- **F8** Progress recovery: uruchom akcję, przejdź na inną zakładkę/stronę,
  wróć → job dalej widoczny, streaming wznowiony (nie zgubiony).
- **F9** Reconnect w trakcie joba: zerwij sieć/WS podczas akcji → po reconnekcie
  stan joba odtworzony (replay eventów), streaming kontynuowany.
- **F10** Stany joba: zaobserwuj queued → running → succeeded; wymuś failed
  (np. build błędnego Dockerfile) → progres pokazuje failed + błąd; „with
  warnings" dla akcji z ostrzeżeniami.
- **F11** Job detail drawer (z Audytu/aktywności): metadata, Events lub StepCards,
  zamykanie Escape/backdrop; dla starego joba (seed retencji) — „Detailed output
  … no longer retained.".
- **F12** Delete dialog: przycisk disabled gdy nic nie zaznaczone.
- **F13** Default remove (tylko runtime/definition) — **nie** usuwa config ani
  data (sprawdź katalogi na dysku).
- **F14** Remove z „Delete stack data" (danger, „irreversible") — usuwa dane po
  świadomym wyborze; potwierdź ostrzeżenie wizualne.

---

## 8. Scenariusze — Logi, Stats, Terminal (H, I, J)

> WS: `logs.subscribe`, `stats.subscribe`, `terminal.*`. Reconnect backoff
> `[1,2,5,10,20,30]s`.

**Logi (H)**
- **H1** Subskrypcja wszystkich serwisów; linie z timestampem i identyfikacją
  serwisu, kolor per serwis.
- **H2** Filtr chipów serwisów (All + per serwis) i filtr tekstowy.
- **H3** Pause/Resume — pauza buforuje, resume dolewa; Clear czyści.
- **H4** Auto-scroll + „Scroll to bottom"; licznik linii.
- **H5** Reconnect: zerwij WS → „Stream disconnected. Reconnecting…" → po
  powrocie streaming wznowiony **bez** duplikacji (tail=0 po reconnekcie).
- **H6** Duży bufor: serwis generujący dużo logów → UI płynne, cap 5000 linii.
- **H7** Empty: stack `stopped` → „No logs available".

**Stats (I)**
- **I1** Karty Stack CPU/RAM/Net + per-container; wartości sensowne vs
  `docker stats`.
- **I2** Aktualizacja ciągła gdy kontenery działają; sparkline rośnie.
- **I3** Historia lokalna ~5 min / do 150 ramek; po przekroczeniu okno się
  przesuwa.
- **I4** Reconnect stats — po powrocie WS dane znów napływają.
- **I5** Empty: brak running kontenerów → empty state.

**Terminal (J)**
- **J1** Otwórz shell w running kontenerze; `/bin/sh` domyślnie działa.
- **J2** `/bin/bash` oferowany tylko gdy dostępny/dozwolony; wybór złego shella →
  `ErrInvalidShell` czytelnie.
- **J3** Wejście, wyjście i **resize** działają (zmień rozmiar okna → PTY reaguje,
  FitAddon + ResizeObserver).
- **J4** Limit sesji: otwórz sesje do `MaxSessionsPerOwner` (domyślnie 5); kolejna
  → „session limit exceeded" w UI.
- **J5** Idle timeout: bezczynna sesja zamknięta z reason `idle_timeout`,
  komunikat poprawny.
- **J6** Reattach po reconnekcie: jeśli PTY żyje → sesja kontynuowana; jeśli nie
  → „Session ended. Start a new session?", lokalny scrollback zachowany.
- **J7** Izolacja sesji: druga karta/sesja nie może przejąć terminala pierwszej
  (własność per session — `ErrSessionNotFound`).
- **J8** Terminal na kontenerze nie-running → niedostępny (walidacja
  `CanOpenTerminal` + status running).

---

## 9. Scenariusze — Workspace, Git, uprawnienia (config + per-stack)

> Config: `/api/config/workspace/*`. Per-stack: `/api/stacks/{id}/workspace/*`.
> Git: `/api/git/workspace/{status,diff,commit,push}`. Naprawa uprawnień przez
> helper `stacklab-workspace-admin-helper` (sudo, strategia ownership/acl).

- **W1** `/config` Files: nawigacja drzewem, otwarcie pliku tekstowego, edycja,
  Save (`config-save`), Discard; komunikat sukcesu **inline** (brak globalnego
  toasta — sprawdzaj przy akcji).
- **W2** „New file" (inline input, Enter/Escape) tworzy plik; nazwy zarezerwowane
  (compose.yaml/.env w stacku) przekierowują do Editora.
- **W3** Plik binarny/nieznany → placeholder, brak edycji.
- **W4** Zablokowany plik (fixtura `config/blocked-fixture/blocked.env`, chmod
  000) → `BlockedFileCard` z „Repair access" (opcjonalnie recursive); po naprawie
  wynik before/after, plik edytowalny. Wymaga włączonego helpera
  (`STACKLAB_E2E_ENABLE_WORKSPACE_REPAIR=1` lub konfiguracja helpera).
- **W5** Naprawa poza managed roots niemożliwa (helper ograniczony do
  `STACKLAB_ROOT`) — próba path traversal odrzucona.
- **G-git1** `/config` → Changes: po edycji pliku w stacku pojawia się w grupie
  per-stack z diffem (`DiffView`, +/-/@@, obsługa `truncated`).
- **G-git2** Selektywny commit: zaznacz pliki tylko jednego stacka, commit z
  wiadomością (`git-commit-message` → `git-commit-submit`) — commit zawiera
  **tylko** wybrane pliki (weryfikacja `git log`/`git show` na VM).
- **G-git3** Push (`git-push`) do skonfigurowanego remote → sukces; bez remote/
  uprawnień → czytelny błąd.
- **G-git4** Plik z `!commit_allowed` (np. zablokowany) → checkbox disabled + ⚠.
- **G-git5** „Not a Git repository" — gdy root nie jest repo, sekcja Changes
  pokazuje powód zamiast błędu.
- **W6** Per-stack Files (`/stacks/:id/files`): edycja np. `Dockerfile`;
  compose.yaml/.env przekierowują do Editora.

---

## 10. Scenariusze — Maintenance (bulk update + zasoby Docker)

> `/api/maintenance/*`. Zakładki: Update / Images / Networks / Volumes / Cleanup.

- **M1** Update: wybór stacków, opcje pull/build/remove-orphans/prune-after →
  job wieloetapowy; StepCards pokazują per-krok progres, expand/collapse logów.
- **M2** Update z jednym błędnym stackiem → krok oznaczony failed **przed**
  przejściem terminalnym joba; reszta kroków wg polityki.
- **M3** Images: lista z filtrami `q`/`usage`/`origin`; dangling/unused
  oznaczone.
- **M4** Networks: create → pojawia się; delete używanej → odrzucone czytelnie;
  delete nieużywanej → znika; refresh.
- **M5** Volumes: create/delete/refresh analogicznie; delete używanego wolumenu
  odrzucone.
- **M6** Cleanup: Prune preview pokazuje reclaimable bytes; Prune (`maintenance-prune`)
  z flagami images/build_cache/stopped_containers/volumes → realnie zwalnia
  miejsce; potwierdź na `docker system df`.
- **M7** Prune volumes (danger) — świadome potwierdzenie, nie usuwa wolumenów w
  użyciu.

---

## 11. Scenariusze — Image updates, Templates (N, świeże funkcje)

> `GET /api/maintenance/image-updates`, `POST .../check`. `GET /api/templates`.

- **N1** „Check updates" (dashboard) → detached job (niezależny od requestu),
  progres „Checking X/Y", stan terminalny nawet po odświeżeniu strony.
- **N2** Stack z nieaktualnym tagiem (tag wskazuje nowy digest w registry) →
  oznaczony jako „update available"; rollup na kaflu i chip „Updates".
- **N3** Po `pull` + redeploy → oznaczenie aktualizacji znika.
- **N4** Prywatny obraz — sprawdzenie updates działa po zalogowaniu do registry
  (patrz R-*); bez auth — czytelny błąd, nie crash.
- **T1** Kreator `/stacks/new`: lista szablonów z `GET /api/templates`; wybór
  szablonu wypełnia edytor compose.
- **T2** Walidacja nazwy: regex `^[a-z0-9]+(?:-[a-z0-9]+)*$` — złe znaki/spacje/
  wielkie litery odrzucone z komunikatem.
- **T3** Podgląd „Will create a new stack definition for …"; checkbox „Deploy
  immediately" → po utworzeniu od razu `up`, ProgressPanel.
- **T4** Utworzenie bez deploy → stack w stanie defined/stopped, katalogi
  config/data utworzone wg checkboxów.

---

## 12. Scenariusze — Docker admin i rejestry (O, R)

> `/api/docker/admin/*`, `/api/docker/registries/*`. Wymaga helpera
> `stacklab-docker-admin-helper` (sudo) na Linux. Restartuje `docker.service`.

- **DA1** Overview daemon pokazuje aktualny stan/konfigurację.
- **DA2** Edycja `daemon.json` → Validate: poprawny JSON/klucze → OK; błędny →
  czytelny błąd walidacji, brak zapisu.
- **DA3** Apply & Restart (poprawna zmiana) → backup utworzony, daemon
  zrestartowany, nowa konfiguracja aktywna; job w audycie.
- **DA4** Apply z konfiguracją psującą daemon → helper wykrywa nieudany restart →
  **rollback** do backupu, `rolled_back=true`, daemon znów aktywny; UI pokazuje
  wynik i ostrzeżenia. (Test wysokiego ryzyka — tylko na VM jednorazowej.)
- **DA5** Brak skonfigurowanego helpera → funkcja wyłączona/niedostępna z
  czytelnym komunikatem (nie 500).
- **R1** Registry login (prywatny registry) → status „logged in"; pull
  prywatnego obrazu w stacku działa.
- **R2** Registry logout → status wraca; pull prywatnego obrazu znów wymaga
  logowania.
- **R3** Błędne dane logowania → czytelny błąd, timeout wg
  `STACKLAB_DOCKER_REGISTRY_AUTH_TIMEOUT`.

---

## 13. Scenariusze — Host, Notyfikacje, Harmonogramy, Self-update

**Host (`/host`)**
- **HO1** Overview hosta: metryki (CPU/RAM/dysk/uptime itp.) sensowne na Linux.
- **HO2** Viewer logów StackLab: filtr poziomu, Follow (na żywo), Refresh;
  na Linux z systemd czyta journal jednostki `STACKLAB_SYSTEMD_UNIT`.

**Notyfikacje (`/settings`)**
- **NT1** Konfiguracja webhook: zapis, „Test" → realny POST na endpoint
  (zweryfikuj np. na webhook.site).
- **NT2** Telegram: token + chat_id, „Test" → wiadomość dociera; złe dane →
  czytelny błąd.
- **NT3** Zdarzenia: wywołaj job/awarię runtime → notyfikacja wysłana zgodnie z
  konfiguracją (job/maintenance/runtime/self-health).
- **NT4** Walidacja pól (pusty URL, zły token) — komunikaty inline.

**Harmonogramy (`/settings`)**
- **SC1** Ustaw harmonogram update i prune; zapis persystuje po restarcie
  backendu.
- **SC2** (jeśli wykonalne w oknie testu) harmonogram wyzwala job o czasie;
  alternatywnie weryfikacja przez skrócony interwał/logi schedulera.

**Self-update (`/settings`, tylko instalacja pakietowa)**
- **SU1** Overview: aktualna vs dostępna wersja z APT.
- **SU2** Apply → helper `stacklab-self-update-helper run` instaluje pakiet,
  health-check `STACKLAB_SELF_UPDATE_HEALTH_URL`, `pending_finalize`, usługa
  wraca zdrowa; job/audit obecny.
- **SU3** Brak helpera / tor tarball → funkcja niedostępna czytelnie.

---

## 14. Scenariusze — Audyt i aktywność globalna (K)

> `/api/stacks/{id}/audit`, `/api/audit`. Global activity + host strip.

- **K1** Każda mutująca akcja tworzy wpis audytu (per-stack i globalny).
- **K2** Wiersze: action, result, timestamps, duration.
- **K3** Wiersz stack-joba linkuje do job detail (`job_id`) → otwiera drawer.
- **K4** Retencja: stary job (seed) → „retained-summary-only" w drawerze.
- **K5** Globalny audyt: filtry `stack_id`, paginacja `cursor`/`limit`.
- **K6** Terminal — zdarzenia metadanych audytowalne **bez** treści komend
  (sprawdź, że wpis nie zawiera wpisanych poleceń).
- **K7** Global activity indicator: aktywny job widoczny w sidebarze, „lingering"
  5 s po zakończeniu; klik → drawer. Host strip pokazuje wersje + chip aktywnego
  joba.

---

## 15. Scenariusze — Bezpieczeństwo (L)

- **S1** Path traversal: próby `../` w `stackId`, `path` (workspace tree/file),
  nazwach sieci/wolumenów → odrzucone (nie wychodzą poza managed root).
- **S2** WS wymaga auth: połączenie `/api/ws` bez cookie → 401 przed upgrade.
- **S3** Origin/CSRF: żądanie login/logout i WS z obcym `Origin` (host ≠ Host) →
  403.
- **S4** XSS w terminalu: wypisz w kontenerze sekwencje/`<script>` → renderowane
  przez emulację XTerm, **nie** jako HTML.
- **S5** XSS w logach: log z `<img onerror>`/HTML → escapowany, brak wykonania.
- **S6** Brak generycznego shell execu z UI (host shell wyłączony,
  `features.host_shell=false` w hello).
- **S7** Hasło w DB tylko jako hash Argon2id (podejrzyj `stacklab.db` — brak
  plaintext).
- **S8** Mutujące REST bez sesji → 401 (nie tylko GET).
- **S9** Cookie: `HttpOnly`, `SameSite=Strict`, `Secure` gdy włączone.

---

## 16. Scenariusze — Deployment i odporność (M, N-deploy)

> Tor tarball/`.deb`/systemd. Skrypty smoke: `scripts/release/smoke-*.sh`.

- **DP1** Instalacja `.deb` z APT (stable) → usługa startuje, `/api/ready` OK.
- **DP2** systemd: start/stop/restart/status działają; stan w `/var/lib/stacklab`.
- **DP3** Reverse proxy (jeśli konfigurujemy) — dostęp przez proxy, WS działa
  (terminal/logi), cookie Secure przy TLS.
- **DP4** Upgrade pakietu (nightly→nowszy lub przez self-update) → dane/stacki
  zachowane, brak regresji.
- **DP5** Rollback (`scripts/release/upgrade.sh` / downgrade) → działa.
- **RE1** Utrata SQLite: skasuj `stacklab.db`, restart → stacki wykryte z FS na
  nowo; drift może być „unknown" do następnego udanego deploy; definicje na
  dysku nienaruszone.
- **RE2** Awaria startu (np. Docker down / zła DB) → obserwowalne w logach,
  usługa nie „znika" cicho.
- **RE3** Padnięcie reverse proxy nie ubija usługi host-native (host-native żyje
  dalej).

---

## 17. Scenariusze przekrojowe — UX / dostępność / responsywność

- **UX1** Responsywność: desktop (sidebar), tablet (dashboard/detale OK), mobile
  (bottom-nav + „More" drawer, bottom-sheet w Config). Terminal i edytor pełne od
  szerokości desktop (zgodnie z decyzją projektową).
- **UX2** Klawiatura: skróty 1–7 (sekcje), `/` (filtr), `⌘K`/`Ctrl+K` (paleta —
  strzałki/Enter/Escape, sekcje + stacki).
- **UX3** Command palette: nawigacja do sekcji i konkretnych stacków działa.
- **UX4** Spójność komunikatów: sukces/błąd inline w miejscu akcji (brak
  globalnego systemu toastów — potwierdź, że każdy błąd jest widoczny i czytelny,
  nic nie ginie).
- **UX5** Stany ładowania: spinnery/skeletony zamiast pustych ekranów przy wolnym
  backendzie.
- **UX6** Dostępność (podstawy): focus ring, nawigacja Tab po formularzach
  (login, create, settings), aria/label na przyciskach akcji, kontrast motywu
  Amber Console.
- **UX7** iOS status-bar strip / sticky header — brak wizualnych glitchy (był fix
  `ca0dd26`).
- **UX8** Masonry kafli — brak dryfu kolumn (był fix `a5e07dd`), układ stabilny
  przy różnej liczbie stacków.
- **UX9** Motyw (jeśli theme toggle dostępny wg roadmapy) — light/dark/system.
- **UX10** 404: nieznany URL → `NotFoundPage`, nie biały ekran.

---

## 18. Priorytetyzacja i kolejność wykonania

**Tura 1 — krytyczne ścieżki (blokery MVP):**
A (auth), B (dashboard/discovery), C/D (detail/orphaned), E (editor),
F/G (akcje + joby), H/I/J (logs/stats/terminal), K (audyt), S (bezpieczeństwo).

**Tura 2 — funkcje operacyjne:**
W+Git (workspace/commit/push), M (maintenance), N/T (image updates/templates).

**Tura 3 — helper-backed i wysokie ryzyko (izolowana VM):**
DA/R (docker admin/rejestry), self-update, deployment/odporność (16).

**Tura 4 — przekrojowe UX** (17) — równolegle podczas tur 1–3.

Sugerowany przebieg na VM: najpierw `go test ./...` + `npm test` (baseline
zielony), potem eksploracja manualna wg tur, dokumentując odchylenia.

---

## 19. Raportowanie wyników

Dla każdego scenariusza notować: **PASS / FAIL / BLOCKED / N-A** + dowód
(screenshot, fragment logu, `docker ps`/`git show`). Znalezione defekty
klasyfikować: `blocker` (łamie kryterium akceptacji / Non-Acceptance z sekcji A–O),
`bug`, `ux`, `polish`. Blokery mapować wprost na
`docs/quality/acceptance-criteria.md`.

Wyniki proponuję zbierać w osobnym pliku przebiegu (np.
`docs/quality/test-run-YYYY-MM-DD.md`) lub jako issues.
