# Konzept: „Tabularium 117“ – Second Screen für Anno 117

> Name entschieden: **Tabularium 117** enthält kein „Anno“ im Produktnamen (Marke von
> Ubisoft). In der Beschreibung „für Anno 117“ zu schreiben bleibt unproblematisch.

## 1. Ziel

Ein kostenloses Open-Source-Tool, das die inoffizielle **Pipe-Schnittstelle** von Anno 117 liest und
die Wirtschaftsdaten live, mit Verlauf und handyfreundlich anzeigt.

**Leitsatz:** *Herunterladen, Doppelklick, läuft.* Kein Build, kein Docker, keine Mod.

### Abgrenzung zu bestehenden Projekten

| Projekt | Was es macht | Was wir anders machen |
|---|---|---|
| `UbisoftMainzAnno/anno117_pipe_example` | Rohdaten in ImGui-Fenster | Namen, Auswertung, Verlauf, Web-UI |
| `anno-mods/anno117-game-connector` | Pipe → SSE auf localhost, bettet Calculator ein | Fertige .exe, LAN/Handy, Verlauf, Warnungen |
| `anno-mods/anno-117-calculator` | Planungstool (manuelle Eingabe) | Wir planen nicht, wir zeigen den Ist-Zustand |
| `knakeyar/anno-117-companion` | Lua-Mod + Log + Docker, sehr umfangreich | Keine Mod, kein Docker, bewusst schlank |

### Nicht-Ziele (bewusst ausgeschlossen)

- Kein Produktionsketten-Planer (das macht der Calculator)
- Kein Handelsrouten-Planer, kein KI-Berater
- Kein Cloud-Dienst, keine Accounts, keine Daten verlassen den PC (außer ins eigene LAN, wenn aktiviert)
- Keine Eingriffe ins Spiel – streng read-only
- Keine Mod-Installation

## 2. Kernfunktionen (MVP)

1. **Live-Übersicht pro Insel** – alle Waren mit Produktion, Verbrauch, Delta; Defizite oben, rot markiert.
2. **Verlauf pro Ware** – Diagramm der letzten 1 h / 4 h / 24 h / 7 d / Session für Produktion,
   Verbrauch, Delta. Die langen Bereiche sind erst mit dem 2-Minuten-Takt sinnvoll: 24 h sind nur
   rund 720 Rohpunkte je Insel und Ware (§12).
3. **Effizienz-Ansicht** – Ist-Produktion vs. `PerfectProductGeneration` pro Ware
   (`Generation / PerfectGeneration`, in der Capture wohldefiniert). Beantwortet: *Welche Kette
   läuft unter ihrem Potenzial?* Die Bedeutung der Produktivitätsfelder ist seit der Live-Capture
   2026-09-22 geklärt (§12): `AverageProductivity = SummedProductivity / AmountOfBuildings × 100`
   ist der Mittelwert der Produktivität aller Gebäude der Ware in Prozent. Die Effizienz-Ansicht
   zeigt ihn deshalb als zweite Spalte **„Produktivität“** neben der Effizienz. Beide bleiben
   getrennt: Effizienz misst den Ausstoß gegen das Optimum, Produktivität den Laufgrad der
   Gebäude – Boosts heben beide über 100 %.
4. **Warnungen** – regelbasiert, umgesetzt in `internal/alerts`:
   - `deficit` (Warnung): Delta < 0 in 3 aufeinanderfolgenden Messungen auf einer Insel, die
     **eigene Gebäude** für die Ware hat; klärt sich nach 3 Messungen mit Delta ≥ 0.
   - `no_local_production` (Stufe `info`, leise, in der UI „Importbedarf“): dieselbe Serie auf
     einer Insel **ohne** Gebäude für die Ware. Die Insel braucht die Ware von anderswo – so
     erreichen die meisten Waren die meisten Inseln. **Ob** sie tatsächlich geliefert wird, weiß
     Tabularium nicht: Handelsrouten und Schiffe stehen nicht in der Pipe, und ein Lager, das
     gerade leer gegessen wird, sieht genauso aus. Deshalb behauptet die Regel nur, was die
     Daten zeigen – verbraucht, nicht lokal produziert. Wird gelistet und in der Warentabelle
     markiert, aber nicht gezählt, nicht gefiltert, nicht angekündigt und nicht im
     Warnungsverlauf geführt. Im Mitschnitt vom 2026-09-23 waren 81 von 102 Defizit-Warnungen
     von dieser Art. Baut der Spieler das erste Gebäude, endet der Eintrag und – bei weiter
     negativem Delta – beginnt im selben Tick ein `deficit` (und umgekehrt).
   - `productivity_drop` (Warnung): die **Produktivität** der Gebäude (`AverageProductivity`,
     §12, bei 100 % gekappt) fällt um mehr als 20 Prozentpunkte unter ihr eigenes
     15-Minuten-Mittel, **während** die Insel bei der Ware im Minus ist (Delta < 0 in
     2 aufeinanderfolgenden Messungen). Klärt sich, sobald die Produktivität wieder innerhalb
     von 10 Prozentpunkten liegt, das Minus 3 Messungen lang vorbei ist oder die Gebäude weg
     sind (auch wenn die Ware ganz aus der Statistik verschwindet).

   Die Regel lief zuerst auf der Effizienz (`Generation / PerfectGeneration`). Der
   35-Minuten-Mitschnitt vom 2026-09-23 zeigte, warum das nicht trägt: Die Pipe zählt fertige
   Produktionszyklen pro Tick, die Generation eines durchlaufenden Gebäudes springt deshalb
   zwischen 0 und dem vollen Wert. Das ergab 37 Warnungen in 35 Minuten, die meisten für Gebäude
   mit 86–100 % Produktivität. Die **Kappung bei 100 %** kommt daher, dass Boni die
   Produktivität weit darüber heben (live bis 270 %) und ein nachlassender Bonus (174 % → 151 %)
   kein Stillstand ist. Die **Bedingung „im Minus“** kommt vom vollen Lager: Die Pipe liefert
   keinen Lagerbestand, ein volles Lager stoppt die Gebäude genauso wie fehlende Arbeitskräfte
   oder Rohstoffe, und eine Insel mit viel mehr Kapazität als Verbrauch steht die meiste Zeit
   so. Der Unterschied, der in den Daten steckt, ist das Delta: Ein volles Lager hält es bei
   null oder darüber, eine stockende Kette drückt es ins Minus. Im Mitschnitt geschah jeder
   Produktivitätseinbruch bei Delta ≥ 0 – die Regel meldet dort keinen. Sie warnt also nicht
   bei jedem Gebäude, das pausiert, sondern erklärt einen Engpass. Die beiden unterschiedlichen Schwellen sind die
   Hysterese: eine Warnung flackert nicht, wenn ein Wert um die Schwelle pendelt. Schwellwerte
   per `--alert-deficit-samples` und `--alert-drop-pp` einstellbar.
   **Eine „Messung“ ist ein Statistik-Tick, und das Spiel liefert etwa alle zwei Minuten einen**
   (Live-Capture 2026-09-22, §12). Drei Messungen sind also rund sechs Minuten anhaltendes Defizit
   – genau das ist gewollt. Das Fenster des gleitenden Mittels muss dagegen mindestens drei Ticks
   fassen: mit den früheren 5 Minuten lagen höchstens zwei Ticks gleichzeitig im Fenster, die
   Mindestzahl von 3 Messwerten wurde nie erreicht und `productivity_drop` konnte gar nicht
   auslösen. Daher 15 Minuten (≈ 7 Ticks, mit Reserve für den schwankenden Takt von 115–138 s).
   Anzeige in der UI + optional Browser-Benachrichtigung/Ton (Opt-in, standardmäßig aus).
5. **Handy-Modus (LAN)** – optional per Schalter; zeigt QR-Code mit URL inkl. Zugriffstoken.

### Später (nach MVP)

- Vergleich zweier Zeitpunkte („Was hat sich seit gestern geändert?“)
- Export (CSV/JSON) des Verlaufs
- Icons (Lizenzfrage klären, siehe Abschnitt 7)
- Mehrsprachigkeit (DE/EN zuerst)
- Tray-Icon statt Konsolenfenster

## 3. Architektur

```
┌──────────────┐  Named Pipe   ┌─────────────────────────── tabularium117.exe ───────────────────────────┐
│  Anno 117    │ ────────────▶ │ pipe reader → decoder → normalizer ─┬─▶ state (in-memory, aktuell) │
│  (/pipe)     │               │                                     ├─▶ store (SQLite, Verlauf)    │
└──────────────┘               │                                     └─▶ alerts (Regel-Engine)      │
                               │                                                                    │
                               │ HTTP-Server: statische UI (embedded) · REST · SSE-Live-Feed        │
                               └───────────────────────────────┬────────────────────────────────────┘
                                                               │ 127.0.0.1 (Standard) / LAN (optional)
                                                   ┌───────────┴───────────┐
                                                   │ Browser PC / Handy    │
                                                   └───────────────────────┘
```

### Technologie-Entscheidungen

| Bereich | Wahl | Begründung |
|---|---|---|
| Backend-Sprache | **Go** | Eine statische .exe, kein Runtime, einfaches Cross-Compile, `embed` für UI-Dateien |
| Named Pipe | `github.com/Microsoft/go-winio` | Etablierte Windows-Pipe-Bibliothek |
| Datenbank | SQLite via `modernc.org/sqlite` | Pure Go, kein CGO → einfacher Build |
| Live-Feed | Server-Sent Events | Einweg reicht, simpler als WebSocket, kompatibel zum Connector-Ansatz |
| Frontend | HTML + CSS + JavaScript (ES-Module), **kein Build-Schritt** | Hobbyprojekt-tauglich, leicht wartbar |
| Diagramme | uPlot (vendored) | Sehr klein und schnell, ideal für Zeitreihen |
| QR-Code | `github.com/skip2/go-qrcode` | Serverseitig als PNG |
| Lizenz | MIT | Kompatibel mit Calculator-Daten (MIT) |

> **Entschieden:** Go. Die Pipe liefert ein flaches Little-Endian-Binärformat mit
> Längenpräfix (`docs/protocol.md`), das mit `encoding/binary` trivial zu dekodieren ist. Der
> frühere Plan B (C# / .NET 8) entfällt.

### Module (Go-Pakete)

- `internal/pipe` – Verbindung zur Pipe, Reconnect-Logik (Spiel gestartet/beendet/neu geladen)
- `internal/protocol` – Dekodieren der Rohnachrichten in Go-Structs. **Einzige Stelle, die das
  Pipe-Format kennt.** Versioniert (`v2`, …), damit ein Spielpatch nur hier Änderungen erfordert.
- `internal/model` – interne, stabile Datentypen (unabhängig vom Pipe-Format)
- `internal/ingest` – Normalizer: verbindet Quelle, Decoder und Zustand; schreibt den
  Record-Mitschnitt und setzt das Versions-Gate aus §8 durch
- `internal/state` – aktueller Zustand im Speicher: neuester Snapshot je Insel, Session und
  Verbindungsstatus, threadsicher
- `internal/catalog` – GUID → Name/Kategorie/Region (aus Calculator-Daten generiert)
- `internal/store` – SQLite: Schema, Schreiben, Downsampling, Abfragen
- `internal/alerts` – Regel-Engine
- `internal/lan` – Auswahl der privaten LAN-Adresse (RFC 1918) und das
  Zugriffstoken; ohne HTTP, damit beides ohne Netz testbar ist (§6)
- `internal/server` – HTTP, REST, SSE, statische Dateien, LAN-Listener mit
  Client-Filter, Token-Prüfung und QR-Code
- `internal/source` – gemeinsames `Source`-Interface für Pipe und Replay (rohe Frames, ohne Formatwissen)
- `internal/replay` – JSONL-Replay für Entwicklung ohne laufendes Spiel
- `cmd/tabularium117` – Einstiegspunkt, Flags, Konfiguration
- `web/` – Frontend (per `embed` in die .exe)

## 4. Datenmodell

### Eindeutige Insel-Identität

**Wichtig (bekannter Stolperstein aus dem Connector-Projekt, in der Capture bestätigt):** `islandID`
+ `areaIndex` sind nicht eindeutig – `islandID` 5 und 11 kommen in beiden aufgezeichneten Sessions
vor. Schlüssel ist immer **`sessionGUID` + `islandID`**. `sessionID` und `areaIndex` werden
mitgeführt, aber nicht im Schlüssel verwendet, solange keine Evidenz dafür vorliegt.

### Interne Typen (Vorschlag)

```go
type IslandKey struct {
    SessionGUID int32
    IslandID    int32 // auf der Pipe ein uint8
}

type ProductStat struct {
    ProductGUID          int32
    Generation           float32
    Consumption          float32
    Delta                float32
    PerfectGeneration    float32
    PerfectConsumption   float32
    Buildings            int32
    Maintenance          int32
    Income               float32
    Profit               int32
    SummedProductivity   float32 // Summe der Produktivitätsfaktoren aller Gebäude
    AvgProductivity      float32 // Mittel in Prozent = Summed / Buildings × 100 (§12)
    Workforce            map[int32]int32 // WorkforceGUID -> Anzahl
    BuildingsByGUID      map[int32]int32 // BuildingGUID -> Anzahl
}

type IslandSnapshot struct {
    Key           IslandKey
    SessionID     uint8
    AreaIndex     uint8
    Name          string    // areaName, wie geliefert (nicht getrimmt)
    ReceivedAt    time.Time // Uhr des Empfängers – Basis aller Zeitreihen
    GameTimestamp int64     // timeStamp der Pipe: Spielzeit in ms, Tick-Id (§12)
    Products      []ProductStat
}
```

### SQLite-Schema (Migration 1)

Schemastand wird in `PRAGMA user_version` geführt; Migrationen liegen als Go-Konstanten in
`internal/store` und laufen je in einer Transaktion. Alle Zeitstempel sind Unix-Millisekunden
der Empfängeruhr.

```sql
CREATE TABLE game_session (       -- eine „Spielsitzung“ = ein SessionStart der Pipe
  id INTEGER PRIMARY KEY, started_at INTEGER NOT NULL, ended_at INTEGER,
  protocol_version INTEGER NOT NULL, headline TEXT NOT NULL
);
CREATE TABLE island (
  id INTEGER PRIMARY KEY, session_guid INTEGER NOT NULL, island_id INTEGER NOT NULL,
  name TEXT NOT NULL, first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL,
  UNIQUE (session_guid, island_id)
);
CREATE TABLE sample (             -- volle Auflösung, eine Zeile pro Insel × Ware × Messzeitpunkt
  island INTEGER NOT NULL REFERENCES island(id), product_guid INTEGER NOT NULL,
  ts INTEGER NOT NULL,                                  -- ts = Empfangszeit
  generation REAL NOT NULL, consumption REAL NOT NULL, delta REAL NOT NULL,
  perfect_generation REAL NOT NULL, perfect_consumption REAL NOT NULL,
  buildings INTEGER NOT NULL, avg_productivity REAL NOT NULL,
  PRIMARY KEY (island, product_guid, ts)
) WITHOUT ROWID;
CREATE TABLE sample_minute (      -- 1-Minuten-Mittel, von der Verdichtung erzeugt
  island INTEGER NOT NULL REFERENCES island(id), product_guid INTEGER NOT NULL,
  ts INTEGER NOT NULL,                                  -- ts = Minutenanfang
  generation REAL NOT NULL, consumption REAL NOT NULL, delta REAL NOT NULL,
  perfect_generation REAL NOT NULL, perfect_consumption REAL NOT NULL,
  buildings INTEGER NOT NULL,                           -- Maximum, nicht Mittel
  avg_productivity REAL NOT NULL, sample_count INTEGER NOT NULL,
  PRIMARY KEY (island, product_guid, ts)
) WITHOUT ROWID;
```

Zusätzliche Indizes gibt es in Migration 1 bewusst nicht: die Verlaufsabfrage (eine Insel, eine
Ware, ein Zeitraum) liegt auf dem führenden Teil beider Primärschlüssel.

### SQLite-Schema (Migration 2)

Die Tabelle `alert` kam bewusst erst dazu, als ihre Spalten aus der Regel-Engine
feststanden statt geraten zu sein.

```sql
CREATE TABLE alert (
  id INTEGER PRIMARY KEY,
  island INTEGER NOT NULL REFERENCES island(id), product_guid INTEGER NOT NULL,
  rule TEXT NOT NULL,                                   -- "deficit" | "no_local_production" | "productivity_drop"
  severity TEXT NOT NULL,                               -- "warning", bei "no_local_production" "info"
  raised_at INTEGER NOT NULL, cleared_at INTEGER,       -- cleared_at NULL = offen
  detail TEXT NOT NULL                                  -- kurzer englischer Text mit den Zahlen
);
CREATE INDEX alert_open ON alert (island, cleared_at);
```

Eine Zeile je ausgelöster Warnung. Der Zahlenwert, der die Regel ausgelöst hat, ist **keine
Spalte**: er ändert sich mit jeder Messung und gehört in die laufende Engine; der Verlauf
beantwortet „wann fing es an, wann war es vorbei“. Der Zustand der Engine liegt nur im Speicher,
deshalb schließt `Open` beim Start alle Warnungen, die ein früherer Lauf offen gelassen hat –
„offen“ heißt damit immer „dieser Prozess beobachtet sie“.

### SQLite-Schema (Migration 3)

Die Live-Capture 2026-09-22 (§12) hat den Takt geklärt: ein Statistik-Tick alle zwei Minuten. Ein
„1-Minuten-Mittel“ war damit das Mittel aus einer einzigen oder gar keiner Messung. Migration 3
zieht die Konsequenz und ergänzt die Tick-Id:

```sql
ALTER TABLE sample ADD COLUMN game_ts INTEGER NOT NULL DEFAULT 0;  -- Pipe-timeStamp, wie geliefert

ALTER TABLE sample_minute RENAME TO sample_bucket;
ALTER TABLE sample_bucket ADD COLUMN bucket_ms   INTEGER NOT NULL DEFAULT 600000; -- Breite dieser Zeile
UPDATE sample_bucket SET bucket_ms = 60000;                        -- Altbestand sind Minutenmittel
ALTER TABLE sample_bucket ADD COLUMN game_ts_max INTEGER NOT NULL DEFAULT 0;
```

`bucket_ms` steht in der Zeile, nicht in einer Konstante: die Breite ist Politik
(`CompactOptions.BucketWidth`), und eine Datenbank kann Zeilen aus mehreren Läufen enthalten –
der Verlauf zeichnet jede Zeile mit ihrer eigenen Breite. `game_ts` ist die Spielzeit in
Millisekunden, für alle Inseln eines Ticks identisch und damit die **Tick-Id**; sie steht still,
solange das Spiel pausiert, ist also Gruppierungsschlüssel, nie Zeitachse. Die Zeitachse bleibt
`ts` (Empfangszeit). Der Insel-Schlüssel ändert sich nicht.

**Datensparsamkeit:** Rohdaten 7 Tage in voller Auflösung, danach auf 10-Minuten-Mittelwerte
verdichten, nach 90 Tagen löschen (konfigurierbar über `CompactOptions`). Die Zahlen folgen dem
2-Minuten-Takt: eine Insel und eine Ware erzeugen rund 720 Zeilen pro Tag, nicht 8640 – eine
Woche Rohdaten sind wenige Megabyte und machen die Bereiche 24 h und 7 d überhaupt erst sinnvoll.
Workforce/Building-Maps nur im aktuellen Zustand halten, nicht im Verlauf (spart viel Platz).

## 5. Schnittstellen (HTTP)

Alle Endpunkte unter `/api/v1`, JSON mit
`Content-Type: application/json; charset=utf-8`. Umgesetzt in `internal/server`;
dieser Abschnitt ist die Referenz dafür.

| Methode | Pfad | Inhalt |
|---|---|---|
| GET | `/status` | Version, Verbindung (Modus, Zustand, Fehlertext, seit, Protokollversion, letzter Frame), Session (Headline, Start), `lan` (`enabled` = zweiter Listener offen, `local` = Anfrage kam über den Loopback-Listener), Anzahl Inseln, Verlauf an/aus mit letztem Messzeitpunkt, `alerts.active` (Anzahl offener Warnungen; Einträge mit Stufe `info` zählen nicht mit), `tick` (Spielzeit-Zeitstempel des jüngsten Snapshots, `null` wenn noch keiner da ist) und `warmingUp` |
| GET | `/islands` | Bekannte Inseln, sortiert nach (SessionGUID, IslandID), je mit `products`, `deficits` (Delta < 0) und `tick` (Spielzeit-Zeitstempel des Ticks, aus dem dieser Snapshot stammt) |
| GET | `/islands/{id}/products` | Insel plus alle Waren mit Namen, Kategorie, Rohwerten sowie `workforce`/`buildingsByGuid` (GUID → Name + Anzahl); Defizite zuerst, dann nach Name |
| GET | `/islands/{id}/products/{guid}/history?range=1h\|4h\|24h\|7d\|session` | Zeitreihe mit `from`, `to` und `points` (`aggregated` markiert verdichtete Punkte, `bucketMs` ihre Breite in ms, `tick` die Spielzeit-Id); Standard `1h`, unbekannter Bereich → 400 |
| GET | `/islands/{id}/efficiency` | Waren mit `efficiency` (Generation / PerfectGeneration, `null` wenn Perfekt = 0), `wasted` (Perfekt − Ist), `avgProductivity` (mittlere Gebäude-Produktivität in Prozent, §2.3) und `buildings` (Anzahl Gebäude – trennt „keine Gebäude“ von „Gebäude, aber kein Potenzial“), sortiert nach `wasted` absteigend |
| GET | `/alerts?active=true\|false&limit=&info=true\|false` | Warnungen und Hinweise (`severity` `warning` bzw. `info`), neueste zuerst; `info=false` lässt die Hinweise weg (der Warnungsverlauf der UI nutzt das). `active=true` (Standard) kommt aus der Regel-Engine, `active=false` aus der Datenbank (offene **und** beendete); ohne Datenbank 503 wie beim Verlauf |
| GET | `/events` | SSE-Stream: `status`, `snapshot` (Inselübersicht wie in `/islands`) und `alert`; Heartbeat `: ping` alle 15 s |
| GET | `/lan` | LAN-Zustand: `enabled`, `available` (eine private Adresse existiert), `ip`, `interface`, `port`, `reason` (wenn nicht möglich) und `url` – die Adresse **mit Token**, ausschließlich in Antworten an den Loopback-Listener |
| POST | `/lan` | Der einzige schreibende Endpunkt: `{"enabled": true\|false}` schaltet den LAN-Listener an bzw. aus. Nur über den Loopback-Listener (ein LAN-Client bekommt 403, auch mit gültigem Token), nur mit `Content-Type: application/json`, nur von der eigenen Seite (fremder `Origin` → 403); belegter Port oder keine private Adresse → 409 mit Begründung |
| GET | `/lan/qr.png` | QR-Code (256 px, PNG) der LAN-Adresse inklusive Token; nur über den Loopback-Listener **und** nur bei eingeschaltetem LAN-Modus, sonst 404 |

Konventionen:

- **Insel-Id** ist `"<sessionGUID>-<islandID>"` – in der URL und im JSON –, weil
  keine Hälfte allein eindeutig ist (Abschnitt 4). Unbekannte Insel → 404.
- **Sprache** aus `?lang=` (Katalogcode, z. B. `german`, `english`), sonst aus
  `Accept-Language` (`de` → Deutsch, sonst Englisch). Namen kommen aus dem
  Katalog, unbekannte GUIDs erscheinen als `#123`.
- **Fehler** sind `{"error":"…"}` mit passendem Status. Ohne Verlaufsdatenbank
  (`--no-db`) antwortet die Verlaufsabfrage mit 503.
- **Nur lesend, mit genau einer Ausnahme:** andere Methoden als GET/HEAD → 405;
  erlaubt ist allein `POST /lan` (der LAN-Schalter, Abschnitt 6). Jede Antwort
  trägt `Cache-Control: no-store` und `Referrer-Policy: no-referrer` – damit
  eine URL mit Token nicht über den Referer abfließt –, CORS-Header gibt es
  nicht (gleiche Origin).
- Beim Verbindungsaufbau schickt der SSE-Stream sofort ein `status`-Ereignis,
  je ein `snapshot` pro bekannter Insel und danach je ein `alert` (`kind`
  `"raised"`) pro offener Warnung, damit ein spät geöffneter Browser den
  vollständigen Stand hat. Langsame Clients verlieren Ereignisse, statt den
  Datenfluss aufzuhalten.
- Eine **Warnung** ist
  `{id?, islandId, islandName, sessionName, productGuid, productName, rule,
  severity, raisedAt, clearedAt|null, detail, value}`. `id` gibt es nur für eine
  gespeicherte Warnung, `value` (aktuelles Delta bzw. aktuelle Produktivität) nur für
  eine aus der Engine – der Verlauf speichert ihn nicht (§4). Das `alert`-Ereignis
  im Stream ist `{kind: "raised"|"cleared", alert: {…}}`. `rule` und `severity`
  sind Bezeichner, keine Anzeigetexte: die UI übersetzt sie.
- **Zwei Listener, eine API:** Loopback und LAN liefern dieselben Endpunkte
  aus. Unterschiede gibt es nur dort, wo etwas Geheimes oder Schaltendes im
  Spiel ist: `lan.url`, `qr.png` und `POST /lan` gehören dem Loopback-Listener,
  und `status.lan.local` sagt der UI, auf welcher Seite sie läuft. `/metrics`
  (außerhalb von `/api/v1`, siehe unten) gibt es nur auf dem Loopback-Listener. Woher eine
  Anfrage kommt, entscheidet der annehmende Listener (`ConnContext`), nie ein
  Header wie `Host` oder `X-Forwarded-For`.
- **Aufwärmphase:** Nach einem `SessionStart` liefert der erste Tick jede Insel mit null Waren
  (§12). `/status.warmingUp` ist genau dann `true`, wenn mindestens eine Insel bekannt ist und
  *keine* davon Waren meldet. Die UI zeigt dann „warte auf den ersten Statistik-Tick (bis zu
  2 min)“ statt zehn leerer Inseln. Der Wert wird im Server aus dem Live-Zustand berechnet und
  liegt auch im `status`-Ereignis des SSE-Streams.
- **`tick`** ist der Spielzeit-Zeitstempel der Pipe (§4). Er identifiziert den Tick und wird nicht
  angezeigt – rohe Spiel-Millisekunden sagen Spielenden nichts; die Statusleiste zeigt weiter
  „letzte Meldung vor X“.
- Die statische UI liegt unter `/` und wird per `embed` aus `web/` ausgeliefert.

### Prometheus-Metriken (`GET /metrics`, experimentell)

Außerhalb von `/api/v1`: `GET /metrics` liefert den Live-Zustand im
Prometheus-Textformat (`text/plain; version=0.0.4; charset=utf-8`). Umgesetzt in
`internal/server/metrics.go`. Optional – die UI braucht den Endpunkt nicht, und
wer kein Prometheus betreibt, merkt nichts davon.

- **Nur Loopback:** Über den LAN-Listener gibt es den Endpunkt nicht – mit
  gültigem Token 404, ohne Token wie überall 401. Das Sicherheitsmodell aus
  Abschnitt 6 bleibt unverändert. Prometheus muss daher auf demselben
  Windows-PC laufen; ein entfernter Server oder ein Container erreicht
  `127.0.0.1:53118` nicht. Metriken über das LAN sind bewusst nicht Teil
  dieses Stands.
- **Kein eigener Zustand:** Jeder Wert wird beim Abruf aus `state.State`
  gelesen. Keine zusätzliche Goroutine, kein Cache, keine Datenbank; die
  Zeitreihen speichert Prometheus.
- **Alle Metriken sind Gauges.** Werte, die nie gesetzt wurden, fehlen, statt
  als 0 zu erscheinen.

| Metrik | Labels | Bedeutung |
|---|---|---|
| `tabularium117_connection_up` | `mode` | 1, wenn die Pipe verbunden ist oder eine Aufnahme noch läuft, sonst 0 (auch nach dem Ende einer Aufnahme mit `--serve-after-replay`). Sagt nur, dass die Verbindung offen ist, nicht, dass die Daten brauchbar sind: Bei einer nicht unterstützten Protokollversion bleibt die Pipe verbunden, die Statistiken werden aber verworfen |
| `tabularium117_last_frame_timestamp_seconds` | – | Unix-Zeit des letzten Frames; fehlt, solange keiner kam. Alter: `time() - …` |
| `tabularium117_protocol_version` | – | angekündigte Protokollversion; fehlt, solange unbekannt |
| `tabularium117_warming_up` | – | 1 in der Aufwärmphase (siehe oben); die Warenreihen fehlen dann, statt auf 0 zu fallen |
| `tabularium117_islands` | – | Anzahl bekannter Inseln |
| `tabularium117_island_info` | `session_guid`, `island_id`, `island_name`, `session_name` | immer 1; liefert die Namen |
| `tabularium117_product_info` | `product_guid`, `product_name` | immer 1; unbekannte GUID → `#123` |
| `tabularium117_product_generation_per_minute` | `session_guid`, `island_id`, `product_guid` | Produktion/min |
| `tabularium117_product_consumption_per_minute` | wie oben | Verbrauch/min |
| `tabularium117_product_balance_per_minute` | wie oben | Delta/min, wie vom Spiel gemeldet |
| `tabularium117_product_perfect_generation_per_minute` | wie oben | Potenzial/min |
| `tabularium117_product_buildings` | wie oben | Anzahl Gebäude |

Entscheidungen:

- **Namen nur in den Info-Metriken**, damit ein umbenannter Ort nicht jede
  Warenreihe abreißen lässt. Verknüpft wird in PromQL über die GUID-Labels.
  Nach einer Umbenennung endet die alte `island_info`-Reihe mit dem nächsten
  Abruf (Prometheus markiert sie als veraltet), ein `group_left`-Join bleibt
  also eindeutig. Umlaute kommen vom Spiel als `_` an (§12) und stehen so im
  Label.
- **Namen immer englisch:** Ein Label, das der Sprache des Aufrufers folgt,
  würde eine Ware in zwei Zeitreihen spalten.
- **Zeitstempel statt Alter:** Ein Alter ändert sich bei jedem Abruf, auch wenn
  nichts passiert; das Alter rechnet PromQL.
- **Stabile Ausgabe:** Inseln nach (SessionGUID, IslandID), Waren nach GUID.
  Eine doppelte Reihe würde Prometheus den ganzen Abruf verwerfen lassen; eine
  Ware, die eine Nachricht zweimal nennt, gibt es aber ohnehin nur einmal
  (siehe §8).
- **Produktivität bewusst noch nicht enthalten:** Ihre Bedeutung ist geklärt
  (`docs/protocol.md`, „Productivity fields, resolved“), aber jeder
  Metrik-Name ist eine Zusage. Sie kommt dazu, wenn jemand sie braucht.
- **Keine Spielstand-Identität:** Die Pipe liefert keine Kennung für den
  Spielstand. `session_guid` ist die Region (Latium ist in jedem Spielstand
  3245), `island_id` ein `uint8` (§4). Zwei Spielstände können deshalb
  dieselben Labels erzeugen, und ihre Reihen gehen nahtlos ineinander über.
  Auseinanderhalten lassen sie sich nur über die Zeit. Dieselbe Grenze hat der
  Verlauf, der Inseln ebenfalls über (SessionGUID, IslandID) führt.
- **Dashboard:** `docs/grafana/tabularium117.json` baut nur auf diesen
  Metriken auf. Die Session ist dort eine Einzelauswahl, weil `island_id`
  nur innerhalb einer Session eindeutig ist (§4). Wer eine Metrik umbenennt,
  muss das Dashboard mitziehen.
- **Experimentell:** Namen und Labels können sich bis 1.0 noch ändern.

## 6. Sicherheit & Netzwerk

Umgesetzt in `internal/lan` (Adresswahl und Token) und `internal/server`
(Listener, Filter und Endpunkte). Dieser Abschnitt
beschreibt, was der Code tut.

**Listener**

- Der Loopback-Listener auf `127.0.0.1:<port>` existiert immer und ist die
  vertrauenswürdige Seite: kein Token, keine Filterung, und nur von dort lässt
  sich etwas umschalten.
- Der LAN-Modus fügt einen **zweiten** Listener auf **einer** privaten
  IPv4-Adresse hinzu – nie `0.0.0.0`, nie eine öffentliche und nie eine
  Link-Local-Adresse (169.254/16). Er ersetzt den Loopback-Listener nicht.
- **Adresswahl:** Schnittstellen, die oben und keine Loopback sind; davon die
  IPv4-Adressen in 10/8, 172.16/12 und 192.168/16. Reihenfolge:
  192.168/16 vor 10/8 vor 172.16/12 (Heimnetze zuerst), bei Gleichstand nach
  Schnittstellenname – damit dieselbe Maschine bei jedem Start dieselbe
  Adresse wählt. `--lan-ip <ip>` überschreibt die Wahl; die Adresse muss
  RFC 1918 sein und auf einer aktiven Schnittstelle dieses PCs liegen. Ist sie
  schon syntaktisch keine taugliche Adresse (kein IPv4, öffentlich, Loopback
  ohne Testhilfe), wird die Kommandozeile abgewiesen und der Start bricht ab.
  Liegt sie nur auf diesem PC nicht vor – oder gibt es ohne `--lan-ip` gar
  keinen Kandidaten –, bleibt der LAN-Modus aus: die Konsole meldet es, der
  Loopback-Listener läuft weiter, und `GET /lan` nennt in `reason`, warum der
  Schalter nicht benutzbar ist.
- `--lan-allow-loopback` ist eine dokumentierte **Testhilfe**: nur zusammen mit
  einer `--lan-ip` aus 127.0.0.0/8 gültig und nur dann auch als Client erlaubt.
  Loopback ist vom eigenen PC aus erreichbar und sonst von nirgends, öffnet
  also nichts – sie macht den LAN-Pfad ohne zweites Gerät testbar.
- Der LAN-Modus lässt sich beim Start (`--lan`) oder zur Laufzeit
  (`POST /api/v1/lan`) einschalten. Ausschalten schließt den Listener **und
  seine offenen Verbindungen**, einschließlich der Ereignis-Streams. Das
  Herunterfahren schließt beide Listener.

**Wer darf herein**

- Auf dem LAN-Listener werden nur Clients aus RFC-1918-Adressen bedient. Die
  Adresse stammt aus der angenommenen Verbindung (`RemoteAddr`), nie aus einem
  Header wie `X-Forwarded-For` – ein Header ist eine Behauptung. Alles andere
  bekommt 403 (JSON).
- Auf welchem Listener eine Anfrage ankam, wird beim Verbindungsaufbau
  festgehalten (`http.Server.ConnContext`), nicht aus dem `Host`-Header
  abgeleitet.

**Host-Prüfung (DNS-Rebinding)**

- Jede Anfrage – auf **beiden** Listenern, vor allem anderen, auch vor der
  Token-Prüfung und vor den statischen Dateien – muss im `Host`-Header den
  Namen tragen, den dieser Listener wirklich bedient: auf dem Loopback
  `127.0.0.1:<port>`, `localhost:<port>` oder `[::1]:<port>`, auf dem
  LAN-Listener nur `<lan-ip>:<port>`. Alles andere → 421 (JSON unter
  `/api/*`, sonst eine Zeile Text), ohne Cookie und ohne Weiterleitung.
- Das ist die Abwehr gegen **DNS-Rebinding**, die stehende Gefahr für einen
  Dienst auf localhost: Eine Seite aus dem Internet, deren Name nach Ablauf
  der TTL auf 127.0.0.1 zeigt, ist für den Browser plötzlich same-origin. Der
  `Origin`-Test verhindert, dass sie etwas schaltet; lesen könnte sie sonst
  alles – auch `GET /lan` mit der Adresse **samt Token**. Der Name im
  `Host`-Header unterscheidet beide Fälle: Eine umgebogene Seite fragt nach
  „evil.example“, nie nach „127.0.0.1“.

**Token**

- 128 Bit aus `crypto/rand`, base64url ohne Padding, **einmal pro
  Programmstart**. Ein Neustart entwertet damit jedes Gerät, das vorher
  hereingelassen wurde.
- Auf dem LAN-Listener muss jede Anfrage das Token tragen: entweder als
  `?token=…` an `/` – dann wird ein Cookie `tabularium_token` gesetzt (HttpOnly,
  SameSite=Lax, Path=/, ohne Ablauf, also Sitzungs-Cookie) und mit 303 auf `/`
  umgeleitet, damit das Token aus Adresszeile und Verlauf verschwindet – oder
  als dieses Cookie. Verglichen wird in konstanter Zeit
  (`crypto/subtle.ConstantTimeCompare`).
- Fehlt es oder ist es falsch: `/api/*` → 401 JSON, alles andere → eine
  minimale 401-Seite in Deutsch und Englisch („QR-Code am PC erneut scannen“)
  ohne Token und ohne Details. Der SSE-Stream ist eingeschlossen; `EventSource`
  schickt das Cookie mit.
- Das Token steht **nie** im Log: die Meldung lautet
  „LAN mode enabled on http://<ip>:<port>/ (token in the UI)“. Die UI ist der
  einzige Ort, der es herausgibt, und `Referrer-Policy: no-referrer` auf jeder
  Antwort verhindert, dass eine URL mit Token über den Referer abfließt.

**Schreiben**

- `POST /api/v1/lan` ist der einzige schreibende Endpunkt des Projekts und
  ändert nichts als diesen Schalter (Abschnitt 5). Er wird ausschließlich auf
  dem Loopback-Listener angenommen – ein LAN-Client mit gültigem Token bekommt
  403 –, verlangt `Content-Type: application/json` und weist eine Anfrage mit
  fremdem `Origin` ab (CSRF-Schutz für einen Dienst auf localhost).

**UI**

- Das Handy-Panel (Schalter, QR-Code, Adresse zum Kopieren, Begründung, wenn
  es nicht geht) erscheint nur, wenn `status.lan.local` wahr ist, also im
  Browser auf dem PC. Auf einem LAN-Client gibt es stattdessen nur eine kleine
  Anzeige in der Statusleiste.
- Hinweis im Panel: Die Windows-Firewall fragt beim ersten Mal nach – dort
  „Private Netzwerke“ wählen, nicht „Öffentliche Netzwerke“.

**Sonst**

- Keine Telemetrie, keine Update-Checks ohne Zustimmung, keine Konten.

## 7. Spieldaten, Namen & Lizenzen

- **GUID-Mapping** aus `anno-mods/anno-117-calculator` (MIT) generieren: ein Skript unter
  `tools/gen-catalog` erzeugt `internal/catalog/catalog.json` aus den Calculator-Parametern
  (Produkte, Gebäude, Arbeitskräfte, Regionen, Übersetzungen DE/EN).
- Revision des Calculators **pinnen** und in `THIRD_PARTY_NOTICES.md` dokumentieren.
- **Icons** sind © Ubisoft → im MVP **keine Icons ausliefern**, nur Namen + eigene, neutrale
  Kategorie-Symbole. Später klären (z. B. im anno-mods-Discord), wie andere Tools das handhaben.
- Unbekannte GUIDs nie verschlucken: als `#123456` anzeigen und loggen.

## 8. Robustheit

- Pipe nicht vorhanden → UI zeigt „Warte auf Anno 117 … (mit `/pipe` gestartet?)“ + Anleitung.
- Verbindung bricht ab (Spiel beendet, Savegame geladen) → automatischer Reconnect mit Backoff.
- Unbekannte Protokollversion (≠ 2) → klare Meldung in UI, Dekodieren stoppen (die Ubisoft-Referenz
  macht weiter; wir nicht), Rohdaten optional in Debug-Log schreiben.
- Kurze Frames sind Dekodierfehler, nie Nullwerte (die Referenz liefert stillschweigend 0).
- Nennt eine Nachricht dieselbe Ware zweimal, gilt der spätere Eintrag – wie im Verlauf
  (`INSERT OR REPLACE`). Entschieden wird das einmal in `internal/ingest`, damit Tabelle,
  Warnungen, Verlauf und `/metrics` dasselbe sehen; der erste Fall pro Lauf wird geloggt.
  Beobachtet wurde das bisher nie – der Log-Eintrag ist der Weg, es zu erfahren.
- **Replay-Modus** (`--replay datei.jsonl`) für Entwicklung und Tests ohne Spiel;
  **Record-Modus** (`--record datei.jsonl`) zum Aufzeichnen echter Daten. Aufgezeichnet werden
  **rohe Frames** (base64) mit Empfangszeit, nicht dekodiertes JSON – Format in `docs/protocol.md`.

## 9. Verteilung

- GitHub Actions baut bei Tag `v*` die `tabularium117.exe` (windows/amd64) und hängt sie ans Release.
- SHA-256-Prüfsumme im Release. SmartScreen-Warnung im README erklären.
- README auf Deutsch und Englisch, mit Screenshots und GIF.
- Ankündigung: anno-mods-Discord, r/anno, Anno-Union-Community.

## 10. Risiken

| Risiko | Auswirkung | Gegenmaßnahme |
|---|---|---|
| Ubisoft ändert/entfernt die Pipe per Patch | Tool funktioniert nicht | Protokoll gekapselt in `internal/protocol`, Versionserkennung, klare Fehlermeldung |
| Pipe liefert weniger als erhofft (z. B. nur aktive Insel) | Features eingeschränkt | Capture zeigt alle Inseln zweier Sessions in einem Tick – live bestätigt am 2026-09-22 (Record-Modus) |
| Spiel erlaubt nur einen Pipe-Client (Konflikt mit Connector) | Tools nicht parallel nutzbar | Noch ungetestet (`docs/protocol.md`, „Open questions“); ggf. im README dokumentieren |
| Bedeutung von Workforce-GUID 0 und Produkt-GUID 0 unklar (`timeStamp` und `AverageProductivity` sind seit dem Live-Mitschnitt geklärt) | Fehlinterpretation in UI | Werte nur durchreichen, Effizienz aus `Generation/PerfectGeneration`; offene Fragen in `docs/protocol.md` |
| Virenscanner-Fehlalarm | Nutzer vertrauen nicht | Open Source, reproduzierbarer Build, Prüfsummen |
| Überschneidung mit Connector-Projekt | Doppelarbeit | Im anno-mods-Discord abstimmen |

## 11. Definition „MVP fertig“

Erreicht mit v0.1.0-beta.1 (2026-09-22).

- [x] `tabularium117.exe` startet ohne Installation, öffnet Browser automatisch
- [x] Erkennt laufendes Anno 117 mit `/pipe`, verbindet automatisch neu
- [x] Zeigt alle Inseln und Waren mit Namen (DE/EN) – alle Inseln, die die Pipe
  liefert; im getesteten Spielstand waren das die eigenen, nicht die der KI
  (`docs/protocol.md`, offene Fragen)
- [x] Verlauf für jede Ware (mindestens 4 h; gehalten werden 7 Tage voll aufgelöst)
- [x] Effizienz-Ansicht
- [x] Defizit-Warnungen
- [x] LAN-Modus mit QR-Code, am Handy bedienbar
- [x] Release auf GitHub mit README

## 12. Entscheidungen aus der Protokollanalyse

Grundlage: `docs/protocol.md`. Der Praxistest am echten Spiel lief bewusst erst mit dem eigenen
Record-Modus, damit echte Rohdaten mitgeschnitten werden konnten statt nur ImGui-Fenster zu
beobachten.

- **Sprache: Go** (siehe §3). Nur `internal/pipe` darf Windows-abhängig sein.
- **MVP-Umfang bestätigt.** Die Capture enthält 14 Inseln aus zwei Sessions in einem Tick, also
  liefert die Pipe alle Inseln – Live-Übersicht, Verlauf, Warnungen und LAN-Modus bleiben wie
  geplant.
- **Effizienz** wird aus `Generation / PerfectGeneration` berechnet, nicht aus
  `AverageProductivity` (§2.3).
- **Zeitachse** ist die Empfangszeit; der Pipe-`timeStamp` wird opak mitgespeichert (§4).
- **Aufzeichnung** speichert rohe Frames (§8), damit Fixtures den Decoder beweisen und
  Protokolländerungen überstehen. Die Connector-Fixture `testdata/connector/example_responses.txt`
  ist dekodiertes JSON; sie dient dem Katalog-Abgleich und ist die Quelle von
  `testdata/connector-reencoded.jsonl`.
- **Port 53118** bleibt; der Connector nutzt 53117.

### Live-Befunde 2026-09-22

Der Praxistest am echten Spiel (eine Sitzung, ein Spielstand, 45 aufgezeichnete Frames; Details in
`docs/protocol.md`, „Live capture 2026-09-22“) hat die offenen Punkte beantwortet, die die
Voreinstellungen bestimmen:

- **Takt:** Die Pipe liefert genau **einen Frame pro Sekunde**, und ein vollständiger
  Statistik-Tick entsteht etwa **alle zwei Minuten Echtzeit** (gemessen 115 s, 121 s, 138 s).
  Ein Tick mit zehn Inseln braucht also zehn Sekunden, bis er ganz da ist. Eine „Messung“ in
  allen Regeln ist ein Tick, kein Frame und keine Sekunde.
- **`timeStamp` = Spielzeit in Millisekunden**, für alle Frames eines Ticks identisch und damit
  die **Tick-Id**. Sie steht still, während das Spiel pausiert (79 s Spielzeit in 121 s Echtzeit),
  ist also kein Zeitmaß. Zeitachse bleibt die Empfangszeit.
- **Leerer Tick nach `SessionStart`:** Nach dem Laden eines Spielstands kommt ein Tick, in dem
  *jede* Insel `numEntries = 0` meldet; die echten Zahlen folgen erst mit dem nächsten Tick, bis
  zu zwei Minuten später. Das ist „noch keine Statistik“, nicht „alles null“ – die UI sagt das
  auch so (`warmingUp`, §5).
- **Produktivität geklärt:** In allen 462 Einträgen mit Gebäuden gilt exakt
  `AverageProductivity = SummedProductivity / AmountOfBuildings × 100`. `SummedProductivity` ist
  die Summe der Produktivitätsfaktoren, `AverageProductivity` das Mittel in Prozent (live 0–270 %,
  Boosts eingerechnet). Damit ist der Wert anzeigbar (§2.3). Die Warnregel `productivity_drop`
  lief zunächst weiter auf der Effizienz und nutzt seit dem Mitschnitt vom 2026-09-23 die
  Produktivität (§2.4).
- **Umlaute:** Das Spiel ersetzt Nicht-ASCII-Zeichen in Inselnamen durch `_`
  („Römische Küste“ → `R_mische K_ste`). Nichts auf unserer Seite kann das zurückholen; Identität
  ist ohnehin der Schlüssel, nie der Name.
- **GUID 0** kommt als Produkt-GUID vor (ein Gebäude, keine Ausgabe). Sie ist jetzt ein
  synthetischer Katalogeintrag „(kein Produkt)“/„(no product)“ statt `#0`; Gebäude-GUID `153793`
  bleibt unbekannt und wird weiter als `#153793` angezeigt.

Daraus folgen die Voreinstellungen:

| Einstellung | Alt | Neu | Grund |
|---|---|---|---|
| `alerts.DropWindow` | 5 min | **15 min** | 5 min fassten höchstens zwei Ticks, die Regel konnte nie auslösen |
| `alerts.MinSamplesForDrop` | 3 | 3 (unverändert) | drei Ticks ≈ 6 min sind das kürzeste sinnvolle Mittel |
| `alerts.DeficitSamples` | 3 | 3 (unverändert) | drei Ticks ≈ 6 min anhaltendes Defizit |
| Rohdaten-Aufbewahrung | 24 h | **7 Tage** | 720 statt 8640 Zeilen pro Tag, Insel und Ware |
| Verdichtung | 1 min | **10 min** | ein Minutenmittel wäre das Mittel aus einer Messung |
| Verdichtete Aufbewahrung | 30 Tage | **90 Tage** | die verdichteten Zeilen sind winzig |
| Verlaufsbereiche | 1 h / 4 h / Session | **+ 24 h / 7 d** | erst mit dem 2-Minuten-Takt darstellbar |
