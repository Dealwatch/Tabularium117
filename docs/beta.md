# Tabularium 117 – Beta-Test-Anleitung / Beta Testing Guide

[Deutsch](#deutsch) · [English](#english)

---

## Deutsch

Danke, dass du beim Beta-Test von Tabularium 117 hilfst. Tabularium 117 ist
ein kostenloses, quelloffenes zweites Fenster für Anno 117: Es liest die
inoffizielle Pipe-Schnittstelle des Spiels und zeigt die Wirtschaftsdaten
live im Browser, auf dem PC oder auf dem Handy im selben Netzwerk.

### Installation

1. `tabularium117.exe` und `SHA256SUMS` von der
   [Release-Seite](https://github.com/Dealwatch/Tabularium117/releases)
   herunterladen.
2. Prüfsumme kontrollieren (PowerShell):
   ```powershell
   Get-FileHash .\tabularium117.exe -Algorithm SHA256
   ```
   Der Wert muss mit der Zeile zu `tabularium117.exe` in `SHA256SUMS`
   übereinstimmen.
3. `tabularium117.exe` starten. Windows zeigt beim ersten Start
   „Windows hat den Start dieser App verhindert" (SmartScreen) – auf
   **„Weitere Informationen"** und dann **„Trotzdem ausführen"** klicken.
   Diese Warnung ist bei einem unsignierten Build zu erwarten und bedeutet
   für sich genommen nicht, dass die Datei Schadsoftware enthält – prüfbar
   ist sie über die Prüfsumme aus Schritt 2 und den offenen Quellcode; siehe
   README.
4. Anno 117 braucht den Startparameter `/pipe`, sonst gibt es nichts zu
   lesen. **Steam:** Bibliothek → Rechtsklick auf Anno 117 → Eigenschaften →
   Allgemein → Startoptionen → `/pipe`. **Ubisoft Connect:** Rechtsklick auf
   das Spiel → Eigenschaften → Startargumente hinzufügen → `/pipe`.

### Test-Checkliste

Bitte so viele Punkte wie möglich durchgehen und auffälliges Verhalten
notieren, auch wenn es nicht wie ein Fehler wirkt:

- [ ] Tabularium 117 verbindet sich und zeigt die eigenen Inseln innerhalb
      von etwa 1 Sekunde nach Spielstart mit geladenem Savegame.
- [ ] Nach etwa 2 Minuten kommt ein zweiter Statistik-Tick (Zahlen
      aktualisieren sich).
- [ ] Eine Insel im Spiel umbenennen – ändert sich der Name auch in
      Tabularium 117?
- [ ] Verhalten bei Pause, Minimieren, Hauptmenü und beim Neuladen der
      Browserseite beobachten (kurze „warte auf Daten"-Anzeige ist
      erwartet, dauerhaftes Hängenbleiben nicht).
- [ ] Nach 10+ Minuten Spielzeit den Verlauf (History) einer Ware ansehen –
      zeigt das Diagramm sinnvolle Werte?
- [ ] Die Effizienz-Ansicht öffnen und mit der Live-Übersicht vergleichen.
- [ ] Eine Ware bewusst ins Defizit laufen lassen (z. B. Produktion
      abschalten) und nach etwa 6 Minuten anhaltendem Defizit prüfen, ob
      eine Warnung erscheint.
- [ ] Bei einer Ware mit „Importbedarf“ die möglichen Produktionsquellen
      aufklappen (in der Warentabelle und auf der Warnungsseite) – passen
      die genannten Inseln zu dem, was im Spiel die Ware herstellt?
- [ ] Handy-Modus auf einem echten Handy im selben WLAN testen (siehe
      README, Abschnitt „Handy-Modus"), inklusive der
      Windows-Firewall-Abfrage beim ersten Mal – bitte **„Private
      Netzwerke"** erlauben.
- [ ] Den **anno-mods Game-Connector gleichzeitig mit Tabularium 117**
      laufen lassen (beide gegen dieselbe laufende Spielinstanz) – empfängt
      Tabularium 117 weiterhin Daten, oder bricht die Verbindung ab? Das ist
      eine besonders wichtige offene Protokollfrage in `docs/protocol.md`.
- [ ] Hell/Dunkel-Design und Deutsch/Englisch umschalten.

### Fehler melden

Bitte ein [GitHub-Issue](https://github.com/Dealwatch/Tabularium117/issues)
mit der Bug-Vorlage öffnen. Hilfreich sind:

- die Log-Ausgabe mit `--verbose` (Tabularium 117 mit diesem Flag neu
  starten und den Fehler reproduzieren). Vor dem Posten bitte durchlesen: Das
  Log enthält den Pfad zum Datenverzeichnis und damit den Windows-Benutzernamen
  (`C:\Users\<name>\...`), im Handy-Modus auch die private IP-Adresse des PCs;
  was nicht öffentlich sein soll, einfach ersetzen;
- bei Protokollproblemen (falsche/fehlende Zahlen, unbekannte
  Protokollversion) ein Mitschnitt mit `--record capture.jsonl`.

**Datenschutzhinweis zum Mitschnitt:** Eine `--record`-Datei enthält die
Namen der eigenen Inseln sowie den Profil- bzw. Spielstand-Namen aus der
`SessionStart`-Nachricht des Spiels – sonst nichts, keine Konto- oder
Ubisoft-Connect-Daten.

Diese Namen stehen aber nicht im Klartext in der Datei: jede Zeile ist zwar
JSON, die eigentliche Nachricht darin ist jedoch Base64-codiert. Suchen und
Ersetzen im Texteditor findet sie deshalb nicht. Wer den Profilnamen nicht
mitschicken möchte, lässt Tabularium 117 den Mitschnitt anonymisieren:

```powershell
.\tabularium117.exe --anonymize-recording mitschnitt.jsonl
```

Das schreibt `mitschnitt.jsonl.anon.jsonl` (ein anderer Pfad geht mit
`--out <datei>`), ersetzt den Profilnamen durch `Player` und lässt alles
andere unverändert – die Zeitstempel und alle Zahlen bleiben, wie sie
aufgezeichnet wurden. `--anonymize-islands` ersetzt zusätzlich jeden
Inselnamen durch `Island <sessionGUID>-<islandID>`. Am Ende steht eine
Übersicht, wie viele Frames gelesen und wie viele Namen ersetzt wurden;
gestartet wird dabei nichts, weder der Server noch die Pipe. Bitte die
`.anon.jsonl`-Datei anhängen, nicht das Original.

Alles bleibt lokal auf dem eigenen PC bzw. im eigenen Netzwerk – Tabularium
117 sendet nichts an einen Server, das gilt auch während des Betas.

---

## English

Thank you for helping beta-test Tabularium 117. Tabularium 117 is a free,
open-source second screen for Anno 117: it reads the game's unofficial pipe
interface and shows the economy data live in a browser, on your PC or on
your phone in the same network.

### Install

1. Download `tabularium117.exe` and `SHA256SUMS` from the
   [release page](https://github.com/Dealwatch/Tabularium117/releases).
2. Verify the checksum (PowerShell):
   ```powershell
   Get-FileHash .\tabularium117.exe -Algorithm SHA256
   ```
   It must match the `tabularium117.exe` line in `SHA256SUMS`.
3. Start `tabularium117.exe`. Windows shows "Windows protected your PC"
   (SmartScreen) on first run – click **"More info"**, then **"Run
   anyway"**. This warning is expected for an unsigned build and does not by
   itself mean the file contains malware - what you can check is the checksum
   from step 2 and the source, which is public; see the README.
4. Anno 117 needs the `/pipe` launch argument, or there is nothing to read.
   **Steam:** Library → right-click Anno 117 → Properties → General → Launch
   Options → `/pipe`. **Ubisoft Connect:** right-click the game → Properties →
   Add launch arguments → `/pipe`.

### Test checklist

Please go through as many points as you can and note anything unusual, even
if it does not look like a bug:

- [ ] Tabularium 117 connects and shows your own islands within about 1
      second after the game starts with a savegame loaded.
- [ ] A second statistics tick arrives after about 2 minutes (the numbers
      refresh).
- [ ] Rename an island in the game – does the name change in Tabularium 117
      too?
- [ ] Watch the behaviour on pause, minimise, main menu, and a browser page
      reload (a brief "waiting for data" state is expected; getting stuck
      permanently is not).
- [ ] After 10+ minutes of play, look at a good's history chart – does it
      show sensible values?
- [ ] Open the efficiency view and compare it with the live overview.
- [ ] Deliberately run a good into deficit (for example, turn off its
      production) and check whether a warning appears after about 6 minutes
      of sustained deficit.
- [ ] For a good marked "import needed", open the possible production
      sources (in the goods table and on the warnings page) – do the islands
      listed match what produces the good in the game?
- [ ] Test phone mode on a real phone on the same Wi-Fi (see the README,
      "Phone mode" section), including the Windows firewall prompt on first
      use – please allow **"Private networks"**.
- [ ] Run the **anno-mods game connector at the same time** as Tabularium
      117 (both against the same running game) – does Tabularium 117 keep
      receiving frames, or does the connection break? This is an especially
      important open protocol question in `docs/protocol.md`.
- [ ] Switch light/dark theme and German/English.

### Reporting a problem

Please open a [GitHub issue](https://github.com/Dealwatch/Tabularium117/issues)
using the bug report template. It helps to attach:

- the log output from `--verbose` (restart Tabularium 117 with that flag
  and reproduce the issue). Please read it before posting: it contains the
  path to the data directory and with it your Windows user name
  (`C:\Users\<name>\...`), and in phone mode your PC's private IP address;
  replace whatever you would rather not publish;
- for protocol problems (wrong/missing numbers, unknown protocol version),
  a recording made with `--record capture.jsonl`.

**Privacy note on recordings:** a `--record` file contains your own island
names and the profile/save name from the game's `SessionStart` message – and
nothing else, no account or Ubisoft Connect data.

Those names are not in the file as plain text, though: every line is JSON,
but the message inside it is base64. Search-and-replace in a text editor will
not find them. If you would rather not share the profile name, let Tabularium
117 anonymise the recording for you:

```powershell
.\tabularium117.exe --anonymize-recording recording.jsonl
```

That writes `recording.jsonl.anon.jsonl` (use `--out <file>` for a different
path), replaces the profile name with `Player` and leaves everything else
alone – the timestamps and all the numbers stay exactly as recorded.
`--anonymize-islands` additionally replaces every island name with
`Island <sessionGUID>-<islandID>`. It prints a summary of how many frames
were read and how many names were replaced, and starts nothing at all,
neither the server nor the pipe. Please attach the `.anon.jsonl` file rather
than the original.

Everything stays local, on your own PC or your own network – Tabularium 117
never sends anything to a server, during the beta or otherwise.
