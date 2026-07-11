# Plan remediacji po przeglądzie — 2026-07-11

## Cel

Ten dokument zamienia ustalenia z przeglądu kodu, bezpieczeństwa, operacji oraz
UX/UI w wykonawczy backlog. Priorytetem jest bezpieczne wydanie stabilne bez
rozszerzania zakresu produktu poza model single-host i Compose-first.

## Zasady realizacji

- każdy wiersz planu jest osobnym, logicznym krokiem;
- każdy krok trafia do osobnego commita;
- commit zawiera test regresji albo opis weryfikacji, jeśli automatyzacja nie jest
  możliwa;
- nie łączymy refaktoryzacji z poprawką zachowania, chyba że refaktoryzacja jest
  niezbędna do bezpiecznego wdrożenia poprawki;
- zmiana publicznego kontraktu aktualizuje OpenAPI i odpowiednią dokumentację;
- status `done` oznacza przejście testów właściwych dla danego obszaru;
- po zamknięciu P0/P1 wykonywany jest pełny test release candidate na Linuksie z
  prawdziwym Dockerem i systemd.

## Statusy

- `planned` — krok jeszcze nierozpoczęty;
- `in_progress` — implementacja trwa;
- `done` — zmiana zintegrowana i zweryfikowana;
- `deferred` — świadomie odłożona z zapisaną przyczyną.

## P0 — ochrona sekretów i danych operatora

| ID | Status | Krok | Kryterium odbioru | Planowany commit |
| --- | --- | --- | --- | --- |
| SEC-01 | done | Ograniczyć prawa katalogu runtime i SQLite | data dir `0700`; DB, WAL i SHM `0600`; istniejąca instalacja jest naprawiana przy starcie/upgrade; test trybów plików | `fix(security): restrict runtime state permissions` |
| SEC-02 | done | Ograniczyć prawa plików zawierających sekrety | systemowy env nie jest world-readable; nowe i istniejące stackowe `.env` mają `0600`; inne zapisy nadal zachowują istniejący mode; test pakietu i writerów | `fix(security): protect environment files` |

## P1 — blokery wydania stabilnego

### Bezpieczeństwo i uwierzytelnianie

| ID | Status | Krok | Kryterium odbioru | Planowany commit |
| --- | --- | --- | --- | --- |
| AUTH-01 | done | Ograniczyć współbieżne hashowanie haseł | globalny limit Argon2, limit in-flight per klient, mały limit body loginu, bounded cleanup liczników i test równoległego burstu | `fix(auth): bound concurrent login work` |
| AUTH-02 | done | Uporządkować model rate limitingu za proxy | udokumentowany i testowany model direct peer/XFF; brak globalnego lockoutu za prawidłowo skonfigurowanym proxy; brak możliwości prostego spoofingu z portu lokalnego | `fix(auth): harden proxied login rate limiting` |
| AUTH-03 | done | Unieważniać sesje po zmianie hasła | wersja hasła jest atomowo zwiększana; poprzednie sesje tracą ważność; UI przechodzi do ponownego logowania | `fix(auth): revoke sessions on password change` |
| AUTH-04 | done | Zamykać aktywne WS i terminale po revocation/expiry | logout, zmiana hasła i absolute lifetime zamykają WS oraz PTY; test integracyjny obejmuje połączenie w locie | `fix(auth): enforce session lifetime on websockets` |
| SEC-03 | done | Zablokować SSRF w registry token challenge | polityka HTTPS i adresów, kontrola redirectów, limit odpowiedzi, bezpieczne query; prywatny registry jest dozwolony wyłącznie przez dokładny endpoint jawnie użyty w `image_ref`; testy loopback/link-local/redirect | `fix(security): constrain registry token endpoints` |

### Bezpieczeństwo operacji i lifecycle

| ID | Status | Krok | Kryterium odbioru | Planowany commit |
| --- | --- | --- | --- | --- |
| JOB-01 | done | Odłączyć delete stacka od requestu HTTP | endpoint zwraca `202` z jobem przed pracą; operacja używa app contextu i osobnego contextu finalizacji; progres jest widoczny od początku | `fix(jobs): detach stack deletion from requests` |
| JOB-02 | done | Naprawić kolejność graceful shutdown | zatrzymanie nowych operacji, cancel background, zamknięcie WS, oczekiwanie na workery, dopiero potem DB close; test lifecycle | `fix(runtime): wait for graceful shutdown` |
| JOB-03 | done | Wprowadzić typowane globalne zasoby locków | co najmniej `global`, `docker-daemon`, `docker-registry`, `self-update`, `stack:<id>`; operacja bez stacka nie może pozostać bez locka | `refactor(jobs): add typed resource locks` |
| JOB-04 | done | Drenować operacje przed self-update | self-update startuje tylko bez kolidujących mutacji i blokuje nowe do restartu/finalizacji | `fix(selfupdate): drain mutating jobs before upgrade` |
| JOB-05 | done | Atomizować utworzenie joba i workflow | błąd eventu/workflow nie zostawia joba `running`; start, workflow i pierwszy event są jedną transakcją | `fix(jobs): atomically initialize workflows` |
| JOB-06 | planned | Atomizować przejścia stanu i sekwencje eventów | cancel i worker nie wybierają tego samego numeru eventu; stan oraz event zapisują się razem | `fix(jobs): serialize state transitions and events` |

### UX zapobiegający utracie danych

| ID | Status | Krok | Kryterium odbioru | Planowany commit |
| --- | --- | --- | --- | --- |
| UX-01 | done | Wymagać poprawnego preview przed cleanupem | loading/error/brak preview blokuje wykonanie; błąd i Retry są widoczne; dialog nigdy nie twierdzi, że lista jest pusta bez udanego odczytu | `fix(ui): require cleanup preview before prune` |
| UX-02 | done | Zablokować pusty zapis po błędzie ładowania edytora | definicja i resolved preview ładują się niezależnie; Save wymaga poprawnie załadowanej rewizji; błąd ma Retry | `fix(ui): block editor saves until definition loads` |
| UX-03 | done | Objąć drafty pełną ochroną nawigacji | Back/Forward, link, hotkey, command palette, zmiana pliku/katalogu/trybu i programmatic navigation wymagają decyzji; testy wszystkich dróg | `fix(ui): guard all unsaved draft transitions` |
| UX-04 | done | Ujednolicić potwierdzenia Stop/Down | dialog pokazuje stack, liczbę kontenerów i wpływ na dane; destrukcyjne akcje są wizualnie oddzielone | `fix(ui): confirm disruptive stack actions` |
| UX-05 | done | Potwierdzać harmonogram usuwający wolumeny | zapis cyklicznego prune volumes wymaga jawnego review; podsumowanie ustawień pozostaje widoczne | `fix(ui): confirm scheduled volume cleanup` |
| DATA-01 | done | Zapisywać compose i `.env` jako jedną rewizję | staging obu plików, rollback przy drugim błędzie i fault-injection test; create usuwa częściowy stan | `fix(stacks): commit definition files transactionally` |

### Release engineering

| ID | Status | Krok | Kryterium odbioru | Planowany commit |
| --- | --- | --- | --- | --- |
| REL-01 | done | Zablokować release bez pełnego quality gate | stable/nightly/hotfix weryfikują dokładny SHA przez testy backend/frontend, hygiene i wymagane integration smoke | `ci: gate releases on verified source revisions` |
| REL-02 | planned | Dodać prawdziwy package smoke | test uruchamia artefakt jako `stacklab` pod systemd, sprawdza readiness, frontend, login i upgrade A→B | `test(release): run package smoke under systemd` |
| REL-03 | done | Ograniczyć uprawnienia workflowów | domyślnie `contents: read`; write tylko w publish; checkout bez utrwalonych credentials poza świadomym pushem | `ci: minimize release workflow permissions` |
| REL-04 | done | Przypiąć Actions i narzędzia analizy | Actions do pełnych SHA, staticcheck/govulncheck do wersji; Renovate nadal może proponować aktualizacje | `ci: pin workflow dependencies` |
| REL-05 | done | Wyłączyć automerge zmian wysokiego ryzyka | major, Actions, SQLite, WebSocket i PTY wymagają review i rozszerzonych testów | `chore(deps): require review for high-risk updates` |
| REL-06 | done | Serializować publikację APT/Pages | wszystkie ścieżki publikacji używają wspólnej blokady concurrency i idempotentnego publish | `ci: serialize apt repository publication` |
| REL-07 | done | Weryfikować tarball przed instalacją | pobrany i lokalny artefakt wymaga zgodnego SHA256 lub podpisanego manifestu przed ekstrakcją | `fix(upgrade): verify release archives before install` |

## P2 — hardening następnej iteracji

### Dane, filesystem i Git

| ID | Status | Krok | Kryterium odbioru | Planowany commit |
| --- | --- | --- | --- | --- |
| FS-01 | done | Odrzucać symlinkowany root stacka | `Lstat` rootu, kontrola względem kanonicznego stacks root, test escape; ocena `openat2 RESOLVE_BENEATH` | `fix(workspace): reject symlinked stack roots` |
| FS-02 | done | Limitować odczyty plików i outputów | limit przed alokacją dla workspace, definicji, diffów i Compose; jawny `content_too_large`/`413` | `fix(io): bound workspace and command output` |
| GIT-01 | planned | Nie usuwać obcego `index.lock` | lock bez udowodnionej własności daje `operation_in_progress`; commit używa bezpiecznego indexu tymczasowego lub transakcji | `fix(git): preserve external index locks` |
| DATA-02 | planned | Wersjonować migracje SQLite | tabela wersji, transakcja per migracja, test upgrade z poprzedniego schematu i zgodność rollbacku | `refactor(store): add versioned migrations` |
| DATA-03 | planned | Zachowywać metadane przy atomic write | jawna polityka owner/group/mode/ACL/xattr; helper Docker wykonuje fsync pliku i katalogu; backupy są unikalne | `fix(files): preserve metadata in atomic writes` |

### Sesje, health i obserwowalność

| ID | Status | Krok | Kryterium odbioru | Planowany commit |
| --- | --- | --- | --- | --- |
| AUTH-05 | planned | Uzgodnić sliding expiry cookie i DB | aktywna sesja nie wygasa według starego cookie; touch jest throttlowany; błędy DB zwracają 5xx zamiast 401 | `fix(auth): align sliding session expiry` |
| AUTH-06 | planned | Walidować politykę haseł i parametry Argon2 | sensowne minimum hasła; parser hasha ma limity memory/iterations/parallelism i nie może panic/OOM | `fix(auth): validate password hash parameters` |
| HEALTH-01 | planned | Rozdzielić liveness i readiness | `/live` sprawdza proces; `/ready` DB, assets i wymagane subsystemy; self-update używa readiness | `feat(health): add component readiness checks` |
| OBS-01 | planned | Dodać correlation/request ID | request ID przechodzi przez log, job start i odpowiedź; możliwość skorelowania błędu UI z journalem | `feat(observability): correlate requests and jobs` |
| OBS-02 | planned | Eksportować podstawowe metryki procesu | liczba requestów/jobów/WS, czasy, błędy i readiness w lekkim endpointcie zgodnym z zakresem single-host | `feat(observability): expose service metrics` |

### Architektura i kontrakty

| ID | Status | Krok | Kryterium odbioru | Planowany commit |
| --- | --- | --- | --- | --- |
| ARCH-01 | planned | Przenieść ownership usług do composition root | `main` tworzy pojedyncze instancje; handler nie uruchamia ukrytych background workers; lifecycle jest jawny | `refactor(runtime): centralize service ownership` |
| ARCH-02 | planned | Rozdzielić handler HTTP według domen | osobne kontrolery/routes dla auth, stacks, maintenance, settings i system; bez zmiany kontraktu | seria `refactor(http): split ... handlers` |
| ARCH-03 | planned | Rozdzielić duże moduły frontendu | Settings i Host podzielone na sekcje/hooki; zachowanie pokryte testami przed przenosinami | seria `refactor(ui): extract ...` |
| API-01 | planned | Generować typy frontendu z OpenAPI | deterministyczny codegen, komenda `generate`, CI failuje przy drift; ręczne rozszerzenia są oddzielone | `build(api): generate frontend contract types` |

### Jakość i DX

| ID | Status | Krok | Kryterium odbioru | Planowany commit |
| --- | --- | --- | --- | --- |
| QA-01 | planned | Podnieść pokrycie krytycznych pakietów | coverage artifact w CI; progi dla `httpapi`, `selfupdate`, `jobs`, `store`; testy `audit`, `retention`, `fsmeta` | seria `test(...): cover ...` |
| QA-02 | planned | Rozszerzyć browser E2E | desktop i mobile Chromium; Settings, schedules, Docker Admin, Git, Files, terminal/logs/stats; preflight Docker i użyteczny trace | seria `test(e2e): cover ...` |
| QA-03 | planned | Włączyć dodatkowe bramki statyczne | actionlint, ShellCheck, secret scan, npm production audit i `eslint --max-warnings=0` | `ci: enforce repository hygiene checks` |
| QA-04 | planned | Usunąć flake testów schedulera | lokalizacja czasu jako zależność, pełne oczekiwanie na finalizację; wielokrotny race run jest stabilny | `test(scheduler): isolate local time state` |
| DX-01 | planned | Ujednolicić toolchain i jedno polecenie check | zgodne wersje Go/Node w docs i plikach narzędzi; `make check`/równoważne nie skanuje `frontend/node_modules` jako Go | `build: add reproducible developer checks` |
| DX-02 | planned | Uporządkować dokumentację i governance | aktualny indeks docs; historyczne plany oznaczone; `SECURITY.md`, `CONTRIBUTING.md`, `CODEOWNERS`, licencja po decyzji właściciela | seria `docs: ...` |

## P2 — UX/UI i propozycje produktowe

| ID | Status | Krok | Kryterium odbioru | Planowany commit |
| --- | --- | --- | --- | --- |
| UX-06 | planned | Ujednolicić async error/loading/empty | wspólny `AsyncState`, Retry, route Error Boundary i Suspense fallback; brak pustych `catch` dla działań operatora | seria `refactor(ui): standardize async states` |
| UX-07 | planned | Zbudować dostępne prymitywy overlay | Dialog/Drawer/BottomSheet z focus trap, Escape, restore focus i ARIA; migracja wszystkich modalów | seria `refactor(ui): adopt accessible ...` |
| UX-08 | planned | Dodać semantykę dynamicznych statusów | `aria-live`, `role=status`, `aria-busy`, progressbar, `aria-pressed`/tabs i dostępna command palette | `fix(a11y): announce dynamic interface state` |
| UX-09 | planned | Poprawić czytelność wizualną | kontrast AA, mniej tekstu 9–11 px, reduced motion, lokalne WOFF2, ograniczona tekstura noise | seria `fix(ui): improve ...` |
| UX-10 | planned | Poprawić nawigację mobile | poziomy tab bar stacka, aktywne `More`, poprawne `/`→`/stacks`, sticky i uporządkowane akcje | `fix(ui): refine mobile navigation` |
| UX-11 | planned | Usprawnić Maintenance | montowanie zakładek na żądanie, debounce search, widoczny status stacka, wartościowy idle state i ostatnie wykonania | seria `perf(ui): optimize maintenance ...` |
| UX-12 | planned | Uporządkować Audit i Logs | filtry serwerowe z URL state, zakres dat, poprawne empty states, zachowane `Load more`, eksport/copy/wrap | seria `feat(ui): improve diagnostics ...` |
| UX-13 | planned | Uporządkować dokument title i nagłówki | jeden `h1` per ekran, tytuł karty zawiera ekran/stack; poprawiona meta description PWA | `fix(a11y): add page titles and heading hierarchy` |
| PROD-01 | planned | Wprowadzić wspólny `Review operation` | cel, zakres, wpływ, snapshot i recovery są prezentowane jednolicie dla delete/prune/update/apply | seria `feat(ui): add operation review ...` |
| PROD-02 | planned | Dodać System Health Center | widoczny stan Backend/Docker/WS, last success, Retry i linki do diagnostyki | `feat(ui): add system health center` |
| PROD-03 | planned | Podzielić Settings według zadań | Security, Notifications, Automation, Updates i About jako odnajdywalne sekcje/subroutes, także mobile | `refactor(ui): split settings navigation` |

## Kolejność wykonania

1. `SEC-01`–`SEC-03`, `AUTH-01`–`AUTH-04`.
2. `UX-01`–`UX-05`, `DATA-01`.
3. `JOB-01`–`JOB-06`.
4. `REL-01`–`REL-07` i pełny release-candidate smoke.
5. Pozostałe P2 według zależności: dane/lifecycle → health/observability →
   architektura/codegen → accessibility/UX → produkt.

## Weryfikacja końcowa

Przed uznaniem planu P0/P1 za zamknięty muszą przejść:

- `go test -race ./cmd/... ./internal/...`;
- `go vet ./cmd/... ./internal/...` i `gofmt` check;
- frontend unit tests, typecheck, lint bez ostrzeżeń i production build;
- Docker integration tests na Linuksie;
- browser E2E desktop oraz mobile;
- świeża instalacja i upgrade pakietu pod prawdziwym systemd;
- test praw plików po świeżej instalacji i po upgrade;
- test przerwania aktywnej operacji oraz restartu procesu;
- test rollbacku self-update i zachowania bazy/configu.
