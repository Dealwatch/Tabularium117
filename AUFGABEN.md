# Aufgabenliste „Tabularium 117“

> Arbeitstitel „Pax Live“ → endgültiger Name seit 2026-09-22: **Tabularium 117** (T8.3).

Jede Aufgabe ist so geschnitten, dass sie in **einer Arbeitssitzung** machbar ist.
Reihenfolge einhalten – spätere Phasen bauen auf früheren auf. Referenz: `KONZEPT.md`.

**Arbeitsweise**
- Pro Aufgabe: Aufgabe nennen („Bearbeite T1.3 aus AUFGABEN.md“), Plan zeigen lassen, dann umsetzen.
- Nach jeder Aufgabe: Tests laufen lassen, committen, Checkbox abhaken.
- Bei Protokoll-Unklarheiten **nicht raten** – Rohdaten ansehen (Record-Modus).

---

## Phase 0 – Klären, was die Pipe wirklich liefert *(wichtigste Phase!)*

- [x] **T0.1 Protokoll analysieren.** Das Repo `UbisoftMainzAnno/anno117_pipe_example` klonen,
  `src/pipe.h` und `src/pipe.cpp` lesen. Dokumentieren in `docs/protocol.md`:
  Pipe-Name, Verbindungsablauf (wer ist Server, wer Client), Nachrichtenrahmen (Länge? Trennzeichen?),
  Serialisierung (Binär? JSON?), wie die `unordered_map`s kodiert sind, Versionsfeld, Sendeintervall.
  *Ergebnis:* `docs/protocol.md` vollständig, offene Fragen explizit gelistet.
- [x] **T0.2 Beispieldaten sichern.** Aus `anno-mods/anno117-game-connector` die Dateien
  `docs/example_responses.txt` und `docs/test-replay-data.jsonl` ansehen (Lizenz prüfen, ggf. als
  Test-Fixtures übernehmen mit Quellenangabe). Welche Felder gibt es über `ProductionEntryData` hinaus
  (Inselname, `sessionGUID`, `areaIndex`, Zeitstempel)? Ergänzen in `docs/protocol.md`.
- [x] **T0.4 Scope-Entscheidung.** Anhand der Ergebnisse MVP-Features in `KONZEPT.md` bestätigen oder
  anpassen. Sprachentscheidung Go vs. C# final treffen. *Ergebnis:* `KONZEPT.md` §12 – Go bestätigt,
  MVP-Umfang bestätigt (vorbehaltlich T0.3), T0.3 hinter Phase 1 verschoben.

## Phase 1 – Grundgerüst & Datenfluss

- [x] **T1.1 Projekt aufsetzen.** Go-Modul, Ordnerstruktur laut Konzept, `Makefile` bzw. `build.ps1`,
  `.gitignore`, MIT-`LICENSE`, `README.md`-Skelett, `CONTRIBUTING.md` mit Projektregeln.
- [x] **T1.2 Interne Datentypen.** `internal/model` mit `IslandKey`, `ProductStat`, `IslandSnapshot`.
- [x] **T1.3 Protokoll-Decoder.** `internal/protocol` dekodiert Rohnachrichten → `model`.
  Unit-Tests mit den Fixtures aus T0.2. Unbekannte Version → definierter Fehler.
- [x] **T1.4 Replay-Quelle.** `internal/replay` liest JSONL und spielt Nachrichten mit
  einstellbarem Tempo ab. Gemeinsames Interface `Source` für Pipe und Replay.
- [x] **T1.5 Pipe-Client.** `internal/pipe` mit `go-winio`: verbinden, lesen, Reconnect mit
  exponentiellem Backoff (max. 10 s), sauberes Beenden bei Ctrl+C. Status-Events nach außen.
- [x] **T1.6 Record-Modus.** `--record datei.jsonl` schreibt jede Rohnachricht mit Zeitstempel mit.
- [x] **T1.7 Aktueller Zustand.** In-Memory-State, threadsicher, hält den neuesten Snapshot je Insel.
- [x] **T1.8 CLI.** `cmd/tabularium117` mit Flags `--replay`, `--record`, `--port` (Standard 53118, um
  nicht mit dem Connector auf 53117 zu kollidieren), `--data-dir`, `--no-browser`, `--lan`.
  *Abnahme:* `tabularium117 --replay testdata/<fixture>.jsonl` gibt Inseln und Warenanzahl auf der Konsole aus.
- [x] **T0.3 Manueller Praxistest** *(Owner-Aufgabe, bewusst hinter T1.8 gezogen)*. Anno mit `/pipe`
  starten, `tabularium117 --record docs/capture-<datum>.jsonl` laufen lassen und dabei die offenen Fragen
  aus `docs/protocol.md` abarbeiten: Kommen Daten für **alle** Inseln (auch nicht besuchte Regionen)?
  Sendeintervall? Was passiert beim Laden eines Spielstands, im Hauptmenü, bei Pause? Zweiter Client
  (Connector) parallel möglich? Antworten in `docs/protocol.md` von **[open]** auf **[capture]**
  heben; die Aufzeichnung (ohne Ingame-Währung, wenn gewünscht) nach `testdata/` übernehmen.
  *Ergebnis (2026-09-22):* 45 Frames live mitgeschnitten und ausgewertet, `docs/protocol.md` aktualisiert
  (1 Frame/s, Tick ≈ 2 min, alle eigenen Inseln aller Regionen, `timeStamp` = Spielzeit in ms,
  `Average = Summed / Buildings × 100`, Umlaute → `_`, leerer Tick nach Reload); reduzierte, anonymisierte
  Fixture `testdata/live-2026-09-22.jsonl`. Offen bleibt nur noch der Test mit zwei Pipe-Clients.

## Phase 2 – Katalog (GUID → Namen)

- [x] **T2.1 Katalog-Generator.** `tools/gen-catalog` liest die Parameterdaten aus einer gepinnten
  Revision von `anno-mods/anno-117-calculator` und erzeugt `internal/catalog/catalog.json`
  (Produkte, Gebäude, Arbeitskräfte; Namen DE/EN; Kategorie; Region).
- [x] **T2.2 Katalog-Paket.** Laden per `embed`, Lookup-Funktionen, Fallback `#GUID` für Unbekannte
  plus einmaliges Log je unbekannter GUID.
- [x] **T2.3 Lizenzhinweise.** `THIRD_PARTY_NOTICES.md` mit Calculator-Revision, MIT-Text,
  Hinweis „Spielinhalte © Ubisoft“.
- [x] **T2.4 Abgleich.** Test: Anteil der GUIDs aus den Fixtures, die im Katalog aufgelöst werden
  (Ziel: > 95 %). Fehlende auflisten.

## Phase 3 – Speicher & Verlauf

- [x] **T3.1 SQLite-Schema & Migrationen.** Schema aus Konzept Abschnitt 4, einfache
  versionierte Migrationen. DB-Datei in `%LOCALAPPDATA%\Tabularium117\` (per `--data-dir` änderbar).
- [x] **T3.2 Schreiben.** Snapshots gebündelt in einer Transaktion schreiben; Insel-Upsert über
  `(session_guid, island_id)`.
- [x] **T3.3 Abfragen.** Zeitreihe je Insel/Ware für Zeiträume 1 h / 4 h / Session.
- [x] **T3.4 Verdichtung & Aufräumen.** Hintergrund-Job: > 24 h auf 1-Minuten-Mittel, > 30 Tage löschen.
  Test mit künstlichen Daten.
  *Nachtrag nach T0.3:* Die Live-Capture 2026-09-22 zeigt einen Statistik-Tick alle zwei Minuten
  (`KONZEPT.md` §12), damit war ein „1-Minuten-Mittel“ das Mittel aus höchstens einer Messung.
  Migration 3 benennt `sample_minute` in `sample_bucket` um, gibt jeder Zeile ihre Breite
  (`bucket_ms`, Altbestand behält 60000) und speichert die Tick-Id (`game_ts`/`game_ts_max`).
  Neue Politik: **7 Tage** roh, dann **10-Minuten-Mittel**, Löschung nach **90 Tagen**
  (`DefaultCompactOptions`). Der synthetische Testdatensatz misst jetzt in diesem Takt.

## Phase 4 – HTTP-Server & API

- [x] **T4.1 Server-Grundgerüst.** `net/http`, Bindung an 127.0.0.1, statische Dateien aus `web/`
  per `embed`, Graceful Shutdown. *Ergebnis:* `internal/server` plus Platzhalterseite
  `web/index.html`; nur GET/HEAD, keine schreibenden Endpunkte.
- [x] **T4.2 REST-Endpunkte.** Laut Konzept Abschnitt 5, mit Tests (`httptest`).
  *Ergebnis:* Insel-Id ist `"<sessionGUID>-<islandID>"`, Sprache aus `?lang=`/`Accept-Language`,
  `/alerts` liefert bis Phase 6 `[]`, `/lan/qr.png` bis Phase 7 404.
- [x] **T4.3 SSE-Stream.** `/api/v1/events` mit `snapshot`, `alert`, `status`; Heartbeat alle 15 s;
  mehrere Clients gleichzeitig. *Ergebnis:* beim Verbinden sofort `status` + je ein `snapshot`
  pro Insel; langsame Clients verlieren Ereignisse, statt den Datenfluss aufzuhalten.
- [x] **T4.4 Browser öffnen.** Nach Start automatisch Standardbrowser öffnen (außer `--no-browser`).
  *Ergebnis:* Fehler beim Öffnen werden protokolliert, nie fatal; `--port 0` nimmt einen freien
  Port (für Tests), `--serve-after-replay` hält den Server nach einem Replay bis Strg+C offen.

## Phase 5 – Frontend

- [x] **T5.1 Layout & Design-Basis.** Responsives Grundlayout (Handy zuerst), Hell/Dunkel,
  Statusleiste „verbunden / warte auf Anno“. Kein Build-Schritt, ES-Module.
- [x] **T5.2 Inselübersicht.** Insel-Auswahl, Warentabelle: Name, Produktion, Verbrauch, Delta,
  Gebäude; sortierbar; Defizite oben und farblich markiert; Suchfeld. Live-Update per SSE.
- [x] **T5.3 Verlaufsansicht.** Klick auf Ware → Diagramm (uPlot) mit Produktion/Verbrauch/Delta,
  Zeitraumwahl 1 h / 4 h / Session.
- [x] **T5.4 Effizienz-Ansicht.** Liste der Waren sortiert nach „verschenkter Produktion“
  (Perfekt − Ist), mit Produktivität in %; kurze Erklärung, was das bedeutet.
- [x] **T5.5 Leerzustände & Hilfe.** Anleitung „/pipe als Startparameter setzen“ mit Schritten,
  wenn keine Verbindung besteht.
- [x] **T5.6 Sprache.** Einfaches i18n (DE/EN), Sprache aus Browser, umschaltbar.

## Phase 6 – Warnungen

- [x] **T6.1 Regel-Engine.** `internal/alerts` mit Regeln:
  `deficit` (Delta < 0 in 3 aufeinanderfolgenden Messungen),
  `productivity_drop` (Produktivität sinkt um > 20 Prozentpunkte gegenüber 5-Minuten-Mittel).
  Hysterese, damit Warnungen nicht flackern. Schwellwerte konfigurierbar.
  *Ergebnis:* Effizienz ist `Generation / PerfectGeneration` in Prozent (T0.4, `KONZEPT.md` §12),
  nicht `AverageProductivity`; Zustand je (Insel, Ware); eine Ware, die aus einem Snapshot
  verschwindet, behält ihre Warnung; `Reset()` an der Sitzungsgrenze liefert die Cleared-Events.
  *Nachtrag nach T0.3:* Eine „Messung“ ist ein Statistik-Tick, und das Spiel liefert etwa alle
  zwei Minuten einen. `DropWindow` steigt deshalb von 5 auf **15 Minuten** – in 5 Minuten lagen
  höchstens zwei Ticks, `MinSamplesForDrop` (3) wurde nie erreicht und `productivity_drop` konnte
  überhaupt nicht auslösen. Ein Test beweist das an drei Ticks mit 80 % und einem vierten mit
  50 %. `DeficitSamples` und `MinSamplesForDrop` bleiben bei 3 (≈ 6 Minuten).
- [x] **T6.2 Persistenz & API.** Warnungen in DB, `/alerts`, SSE-Event.
  *Ergebnis:* Migration 2 (`alert`-Tabelle, `KONZEPT.md` §4); Schreiben über dieselbe
  Batcher-Warteschlange wie die Snapshots, damit die Insel-Zeile vorher existiert;
  `/alerts?active=true` aus der Engine, `active=false` aus der Datenbank (ohne DB 503);
  SSE-Ereignis `alert` inkl. Nachholen beim Verbinden; `/status` zählt die offenen Warnungen.
- [x] **T6.3 UI.** Warnungsleiste, Badge-Zähler, optional Browser-Benachrichtigung und Ton
  (Opt-in, standardmäßig aus).
  *Ergebnis:* Badge in der Statusleiste, Ansicht `#/alerts` (mit `?all=1` der Verlauf),
  Warnungs-Abzeichen je Insel neben dem Defizit-Abzeichen, Markierung in der Warentabelle;
  Benachrichtigung und Ton werden erst beim Einschalten angefragt bzw. erzeugt und nur in
  diesem Browser gespeichert.

## Phase 7 – LAN / Handy

- [x] **T7.1 LAN-Bindung.** Private LAN-IP ermitteln, zusätzlich darauf lauschen, nur RFC-1918-Clients.
  *Ergebnis:* `internal/lan` wählt die Adresse (192.168/16 vor 10/8 vor 172.16/12, stabil bei
  Gleichstand), `--lan-ip` überschreibt sie; der Loopback-Listener bleibt immer bestehen, der
  LAN-Listener kommt daneben auf **eine** Adresse, nie `0.0.0.0`. Welcher Listener eine Anfrage
  angenommen hat, entscheidet `ConnContext` – nie ein Header. Ohne Kandidat bleibt der Modus aus
  und `GET /api/v1/lan` nennt den Grund.
- [x] **T7.2 Token.** Zufallstoken je Start, Token in URL → Cookie, alle API-Aufrufe aus dem LAN prüfen.
  *Ergebnis:* 128 Bit aus `crypto/rand`, base64url; `?token=` an `/` wird gegen ein
  HttpOnly-Sitzungs-Cookie getauscht (303 auf `/`, damit das Token aus dem Verlauf verschwindet),
  Vergleich in konstanter Zeit. Ohne gültiges Token: `/api/*` → 401 JSON, sonst eine minimale
  401-Seite DE/EN. Das Token steht nie im Log, und jede Antwort trägt `Referrer-Policy: no-referrer`.
- [x] **T7.3 QR-Code.** Endpunkt + Anzeige in der PC-UI (nur localhost), Umschalter LAN an/aus
  (nur von localhost bedienbar).
  *Ergebnis:* `GET /api/v1/lan/qr.png` (256 px, `no-store`) nur über den Loopback-Listener und nur
  bei eingeschaltetem LAN-Modus; `POST /api/v1/lan` ist der einzige schreibende Endpunkt des
  Projekts (nur Loopback, nur `application/json`, fremder `Origin` → 403, belegter Port → 409).
  Das Panel `#/phone` zeigt Schalter, QR-Code, Adresse zum Kopieren und den Firewall-Hinweis; auf
  einem LAN-Client gibt es stattdessen nur eine Anzeige in der Statusleiste (`status.lan.local`).
- [x] **T7.4 Handy-Feinschliff.** Auf echtem Handy testen: Touch-Ziele, Hochformat, Bildschirm
  bleibt an (Wake Lock, optional).
  *Ergebnis:* `viewport-fit=cover` plus Safe-Area-Abstände, Touch-Ziele ≥ 44 px in Statusleiste und
  Inselliste, breite Tabellen scrollen in ihrem eigenen Kasten statt die Seite zu verschieben, und
  „Bildschirm anlassen“ (Screen Wake Lock, Opt-in, in diesem Browser gespeichert, nach
  `visibilitychange` erneut angefordert) steht unter `#/help`. Geprüft wurde headless bei 390×844;
  der Test auf einem echten Handy im echten WLAN bleibt Owner-Aufgabe.

## Phase 8 – Release

- [x] **T8.1 Build-Pipeline.** GitHub Actions: Tests, `go vet`, Build windows/amd64 mit Versions-Info,
  Release bei Tag `v*` mit .exe und SHA-256. *Ergebnis:* `.github/workflows/release.yml`; läuft erst,
  sobald das Repo Actions-Kapazität hat (bis dahin `./scripts/check.sh` + `make build` lokal).
- [x] **T8.2 README.** DE + EN: Was ist es, Screenshots/GIF, Installation, `/pipe` aktivieren,
  Handy-Modus, SmartScreen-Hinweis, Datenschutz („alles lokal“), Abgrenzung zu anderen Tools, Credits.
  *Ergebnis:* `README.md`, Screenshots unter `docs/screenshots/` (aus einem Replay-Mitschnitt).
- [x] **T8.3 Namensfindung.** Endgültigen Namen ohne „Anno“ im Produktnamen festlegen.
  *Ergebnis:* Tabularium 117 (Exe tabularium117.exe, Datenordner %LOCALAPPDATA%\Tabularium117,
  Repo und Modulpfad `github.com/Dealwatch/Tabularium117`).
- [ ] **T8.4 Beta mit echten Spielern.** 3–5 Tester (z. B. anno-mods-Discord), Feedback als Issues.
  *Vorbereitet:* docs/beta.md, Issue-Templates, CHANGELOG 0.1.0-beta.1, docs/release.md.
- [ ] **T8.5 v1.0 veröffentlichen & ankündigen.**

---

## Projektregeln

Die Projektregeln stehen in `CONTRIBUTING.md`.
