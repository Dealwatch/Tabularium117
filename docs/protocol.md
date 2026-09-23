# Anno 117 pipe protocol

Everything Tabularium 117 knows about the game's named-pipe interface. This document
is the source of truth for `internal/protocol`; when the game changes the
format, update this file and that package together.

The pipe is an unsupported "easter egg" (Ubisoft's words); compatibility across
game versions is not guaranteed. Activate it by launching the game with the
`/pipe` argument: on Steam under Properties → General → "Launch Options",
which is the maintainer's setup and passes the argument through to the game
**[live]**; in Ubisoft Connect under Properties → "Add launch arguments".

## Sources

| Source | Revision | License | What it contributed |
|---|---|---|---|
| `UbisoftMainzAnno/anno117_pipe_example` (`src/pipe.h`, `src/pipe.cpp`) | `ae90bf2a`, 2026-09-17 | Unlicense | Wire format, connection flow, message types (the reference reader) |
| `anno-mods/anno117-game-connector` (`src/pipe.cpp`, `src/statistics_server.cpp`, `docs/`) | `ed6548f1`, 2026-09-20 | Unlicense | Identity gotcha, session-lifecycle notes, 14 captured real messages (`testdata/connector/`) |
| Live capture by the maintainer, 2026-09-22 (recorded with `--record`; reduced, anonymised excerpt in `testdata/live-2026-09-22.jsonl`) | game build of 2026-09-22 | — | 45 raw frames over 6.5 min: connect, three ticks, rename, pause, minimise, main menu, reload |

Facts below are marked **[code]** when they come from reading the reference
reader, **[capture]** when they come from the connector's decoded capture,
**[live]** when they were measured on the maintainer's raw live capture of
2026-09-22 (one save, one game build — a single run, not a guarantee),
**[inferred]** when they are the maintainer's reading of that evidence, and **[open]**
when nobody has verified them yet. Do not promote an [open] item without
evidence from the real game (record mode).

## Transport

- Pipe name: `\\.\pipe\anno117`. **[code]**
- The **game is the pipe server**, the tool is the client. The client opens the
  pipe with `CreateFile(GENERIC_READ, OPEN_EXISTING)` and only ever reads.
  Nothing is written to the game. **[code]**
- Before connecting, the reference reader polls `WaitNamedPipe` once per second
  until the pipe exists; `ERROR_FILE_NOT_FOUND` means the game is not running
  or was started without `/pipe`. **[code]**
- The reference reader sleeps 1 s between reads. That is a client-side choice,
  not the game's send interval. **[code]**
- End of stream: `ReadFile` fails with `ERROR_BROKEN_PIPE` when the game exits
  or closes the pipe. The reference reader then goes back to waiting for the
  pipe. **[code]**
- Whether the game accepts more than one client at a time (e.g. Tabularium 117 and
  the connector side by side) is unknown; the live run had one client. **[open]**
- **Pacing:** the game delivers exactly one frame per second, whatever the
  type — every one of the 45 live frames arrived 1.000 s ± 1 ms after the
  previous one, Version → SessionEnd → statistics alike. A tick with 10
  islands therefore takes 10 s to arrive. **[live]** Whether this is a 1 Hz
  sender loop draining a queue or a write-per-second is **[open]**; the
  consequence is the same.
- **Tick interval:** statistics ticks started 115 s, 121 s and (across a
  reload) 138 s apart in wall-clock time; the game-time distance between
  ticks was 121.0 s, 79.1 s (a pause inside that window) and 120.9 s. So a
  tick is produced about every **2 minutes of real time**, and the first one
  arrives 1 s after connecting. **[live]** **[inferred: 2 min schedule]**

## Framing

Every message is a length-prefixed frame:

```
int32 LE  length      -- byte count of the payload that follows (prefix excluded)
uint8     type        -- Message enum, see below
...       body        -- type-specific, exactly length-1 bytes
```

- The reference reader rejects frames longer than 1 MiB and skips a frame whose
  body reads short. Captured islands with 56–68 products are a few KB each, far
  below that. **[code] [capture]**
- Pipe mode (byte vs. message) is not set by the client; the reference reader
  reads exactly 4 bytes, then exactly `length` bytes. A robust client must not
  assume one `ReadFile` returns one frame. **[code]**

## Primitive encodings

All multi-byte values are little-endian (`static_assert(std::endian::native ==
std::endian::little)` in the reference). **[code]**

| Type | Encoding |
|---|---|
| `int32` | 4 bytes LE, two's complement |
| `int64` | 8 bytes LE, two's complement |
| `float` | 4 bytes LE, IEEE 754 binary32 |
| `uint8` | 1 byte |
| `string` | `uint8` length (0–255) followed by that many bytes; no terminator |

- String encoding is not specified; the captured names are plain ASCII.
  Treat as UTF-8 and never assume validity. A captured name has a trailing
  space (`"Zycada "`), so do not trim silently — store as delivered, trim only
  for display. **[capture]**
- The reference `Read*` helpers return `0` on truncation instead of failing.
  Tabularium 117's decoder must be strict: a short frame is a decode error, never a
  zero value. **[code]**

## Message types

`Message` is a `uint8` enum: **[code]**

| Value | Name | Body |
|---|---|---|
| 0 | `Version` | `int32 version` |
| 1 | `SessionStart` | `string headline` |
| 2 | `SessionEnd` | (empty) |
| 3 | `AreaProductionStatistics` | see below |

### `Version` (0)

Sent once as the first frame after a client connects (the reference reads it as
a "preamble"); confirmed live: frame 1 is `Version 2`. **[live]** The reference reader expects `PROTOCOL_VERSION = 2`, logs a
mismatch and **continues anyway**. Tabularium 117 should instead surface an unknown
version clearly and, unless configured otherwise, stop decoding (`KONZEPT.md`
§8). **[code]**

### `SessionStart` (1)

`string headline`. Live: the headline is the **player profile / save name**
(the player's profile name in the live run; `Captain` in the anonymised fixture in
`testdata/`, `Player` in what `--anonymize-recording` writes). **[live]**

- `SessionStart` is **not** sent when the client connects to a game that
  already has a save loaded: the live connect sequence was `Version`,
  `SessionEnd`, then a full statistics tick 1 s later. The headline is only
  known once a save is (re)loaded while connected. **[live]**
- Loading a save sends `SessionEnd`, then `SessionStart`, then one tick in
  which **every island has `numEntries = 0`** (see below). **[live]**
- Session-scoped state must reset on `SessionStart` *and* on `SessionEnd`;
  the connector's note that a start need not follow an end still stands.
  **[code] [live]**

### `SessionEnd` (2)

Empty. The reference clears all island data on it, and treats a broken pipe the
same way. **[code]** Live: sent when the client connects to a running game
(frame 2), when the player goes to the **main menu**, and again when a save
starts loading — it can arrive twice in a row and must be idempotent. **[live]**

### `AreaProductionStatistics` (3)

One frame per island (area) per tick, all frames of a tick carrying the same
`timeStamp`; a tick is complete when the next frame carries a different
`timeStamp` (there is no end-of-tick marker and no island count). **[code] [live]**

```
uint8   sessionID
uint8   islandID
uint8   areaIndex
int32   sessionGUID
string  areaName
int64   timeStamp
int32   numEntries
entry × numEntries
```

Each `entry` (`ProductionEntryData`):

```
int32   ProductGuid
float   ProductGeneration
float   ProductConsumption
float   ProductDelta
float   PerfectProductGeneration
float   PerfectProductConsumption
int32   AmountOfBuildings
int32   TotalMaintenance
float   TotalIncome
int32   TotalProfit
float   SummedProductivity
float   AverageProductivity
int32   numWorkforces
        (int32 workforceGUID, int32 amount) × numWorkforces
int32   numBuildings
        (int32 buildingGUID, int32 amount) × numBuildings
```

The fixed part of an entry is 48 bytes, followed by the two variable-length
pair lists. The reference reader **accumulates** duplicate keys inside a pair
list (`map[guid] += amount`); Tabularium 117 must do the same rather than overwrite.
**[code]**

## Identity

- `islandID` and `areaIndex` are single bytes and are **not unique** across
  sessions: the connector capture has `islandID` 5 and 11 in both sessions,
  the live capture has `islandID` 3 and 4 in both. `areaIndex` is `1` in
  every message of both captures (59 statistics frames). **[capture] [live]**
- `sessionID` is a small number (`1` for Latium and `3` for Albion in both
  captures); `sessionGUID` is an `int32` (`3245`, `6627`). The calculator's `sessions` list (pinned
  revision, see `THIRD_PARTY_NOTICES.md`) maps `3245` → "Latium" (region
  Roman, guid `3225`) and `6627` → "Albion" (region Celtic, guid `6626`), i.e.
  `sessionGUID` is the game session (world/region) the island lies in, not a
  play-through. **[capture] [code (calculator data)]**
- Therefore the island key is `(sessionGUID, islandID)`, as `CONTRIBUTING.md` and
  `KONZEPT.md` §4 state. Whether `sessionID` is ever needed in addition is
  open; keep it in the data but not in the key until evidence says otherwise.
  **[open]**
- A play-through (one save) currently has no identifier in the protocol other
  than the `SessionStart` headline. **[open]**

## Semantics observed in the captures

### Connector capture (`testdata/connector/example_responses.txt`, 14 messages)

- Both sessions' islands (7 + 7) arrive in one tick with one `timeStamp`.
  **[capture]**
- Islands with `numEntries = 0` exist (`"Zycada "`). **[capture]**
- Workforce GUID `0` occurs as a key there (never in the live capture, where
  product GUID `0` occurs instead). Meaning unknown; never drop it. Tabularium 117
  now names it "(no product)" through a synthetic catalog entry rather than
  showing `#0` - see "Consequences for Tabularium 117". **[capture] [open]**
- `TotalProfit` is the integer truncation of `TotalIncome - TotalMaintenance`
  in every entry of both captures (423 + 708). **[capture] [live]**
- `ProductDelta = ProductGeneration - ProductConsumption` in every entry of
  both captures. **[capture] [live]**

### Live capture 2026-09-22 (45 frames, protocol v2)

The recorded run: save already loaded at connect; Latium shown; an island renamed
to "Römische Küste"; Albion opened; an island renamed to "Englische Küste";
game paused, minimised, maximised; main menu; save reloaded; stop.

- **Which islands:** 4 Latium + 6 Albion islands, all owned by the player,
  in every tick — including Albion in the first tick, before the player had
  opened that region in this session. AI-owned islands in Latium were **not**
  reported. So the pipe reports **all player-owned islands of all regions**,
  regardless of the current view. **[live]**
- **timeStamp** is **game time in milliseconds**: it advanced 121.0 s between
  ticks 1 and 2 (wall 115 s) and only 79.1 s across the paused window (wall
  121 s), i.e. it stops while the game is paused, while ticks keep coming on
  the real-time schedule. Its epoch is the session's game clock (values
  ≈ 1.5 × 10⁸ ms ≈ 42 h of play). It is identical for all frames of a tick
  and therefore a usable **tick id**. **[live] [inferred: unit]**
- **Pause:** a tick was still delivered during the paused window, with the
  game clock nearly frozen. Pausing does not stop or end the stream. **[live]**
- **Minimise:** the third tick arrived while the game was minimised. No
  `SessionEnd`, no gap. **[live]**
- **Main menu:** `SessionEnd`, no further frames. **[live]**
- **Reload:** `SessionEnd` again, `SessionStart <profile name>` 11 s later,
  then 1 s later a tick of 10 frames with `numEntries = 0` for every island
  (names as in the save, i.e. the unsaved renames were gone). The next real
  tick was outside the capture window; **[inferred]** it follows on the
  normal ~2 min schedule. **Any consumer must treat an all-empty tick right
  after `SessionStart` as "no statistics yet", not as "everything is zero".**
  **[live]**
- **Renamed islands:** "Römische Küste" arrived as `R_mische K_ste`, byte
  `0x5F` per umlaut — the **game replaces non-ASCII characters with `_`**
  before writing to the pipe; the length prefix counts the replaced bytes.
  Nothing can restore the original spelling on our side. **[live]**
- **Productivity fields, resolved:** in all 462 live entries with buildings,
  `AverageProductivity = SummedProductivity / AmountOfBuildings × 100`
  exactly. `SummedProductivity` is the sum of the per-building productivity
  factors (16 buildings at 100 % → 16.0), `AverageProductivity` the mean in
  percent (range 0–270 % live, boosts included). **[live]** The connector
  capture's odd values (e.g. Summed 27.87 with 6 buildings, Average 464.6)
  fit the same formula (27.87 / 6 × 100 = 464.5). **[capture]**
- **Units:** `ProductGeneration`, `ProductConsumption`, `ProductDelta` and
  their perfect counterparts are rates per minute, which is the unit the
  game's own production figures use. The UI labels those columns `/min`.
  **[inferred]**
- **Efficiency** as `Generation / PerfectGeneration` ranges 0–1.5 live
  (boosts push it above 1). **[live]**
- **Entries:** 236 per tick, 0–75 per island; 103 product GUIDs (all but `0`
  in the catalog), 107 building GUIDs (all but `153793`), 9 workforce GUIDs
  (all known). Product GUID `0` appears with one building and zero output —
  probably a building without a product; named "(no product)". **[live]**
- Values change between ticks (19 of 30 consecutive island snapshots
  differ), so the data is live, not cached. No frame carried trailing bytes.
  **[live]**

## Consequences for Tabularium 117

- The format is a flat little-endian binary layout with length prefixes —
  trivial to decode with Go's `encoding/binary`. Nothing here argues for the
  C#/.NET alternative.
- `internal/protocol` decodes one frame at a time from an `io.Reader`
  (`4-byte length → body`), validates `length ≤ 1 MiB`, dispatches on `type`,
  and returns typed errors for unknown type, unknown version, and short body.
- Bytes left over after a message has been fully decoded are ignored, so that a
  future revision appending a field does not break decoding; the version gate
  (`CheckVersion`) is what catches a format we genuinely cannot read.
- `internal/pipe` handles only Windows I/O and reconnect; it hands byte frames
  to the decoder and never interprets them.
- Sample timestamps come from the receiver's clock; the pipe's `timeStamp` is
  stored as an opaque `int64`. Since the live capture shows it is a per-tick
  game-time id, it is also the right key for grouping the islands of one tick
  (all frames of a tick share it; the islands of one tick arrive over ~10 s
  of wall-clock time).
- Ticks every ~2 min mean: a "3 consecutive samples" rule needs ~6 min to
  fire; a 5-minute trailing window holds at most two ticks; 24 h of raw
  samples is only ~720 rows per product and island. Retention, windows and
  the drop rule's minimum sample count must be set for a 2-minute cadence,
  not for seconds (see `KONZEPT.md` §12 and the alert/compaction defaults).
- The all-empty tick after `SessionStart` must not produce deficit alerts
  (it cannot: it has no product entries, and `SessionStart` resets the alert
  engine — proven by a test on the live fixture) and should be shown as
  "waiting for the first statistics" rather than as ten empty islands.
- `SessionEnd` arrives on connect and twice around a reload; resetting state
  and ending an open session must be idempotent.
- Island names may contain `_` where the player typed umlauts; identity is
  the key, never the name, so renames are just name updates.
- GUID `0` is not a catalog gap but the game's way of saying "none", so it is
  a synthetic catalog entry ("(no product)" / "(kein Produkt)", written by
  `tools/gen-catalog`) instead of the `#0` rendering. The "unknown GUID,
  shown as `#123456` and logged once" rule is unchanged and still applies to
  real gaps such as building GUID `153793`.
- The connector listens on `127.0.0.1:53117`; Tabularium 117 uses 53118 so both can
  run at once (whether the *game* allows two pipe clients is a separate open
  question above).

## Tabularium 117 record/replay format

Record raw frames, not decoded JSON, so a fixture proves the decoder and
survives protocol changes:

```
{"t":"2026-09-21T13:00:00.123Z","frame":"<base64 of the payload without the length prefix>"}
```

One line per frame, in arrival order, `t` = wall-clock receive time. Replay
feeds the frames through the same decoder at the recorded pace (or faster).
`testdata/live-2026-09-22.jsonl` is a real recording in this format (reduced
and anonymised, see `testdata/README.md`); `testdata/connector-reencoded.jsonl`
is re-encoded from the connector's decoded JSON and labelled as such.

## Open questions

Answered by the live capture: send interval (~2 min, 1 frame/s), all islands
of all regions (player-owned only), `SessionStart` content and timing,
`timeStamp` unit and pause behaviour, productivity formula, `areaIndex`,
non-ASCII names, minimise/main-menu behaviour, empty tick after reload.

Still open:

1. Does the game accept multiple simultaneous pipe clients? (Not tested; the
   live run had one client.)
2. Meaning of workforce GUID `0` (connector capture only) and product GUID
   `0` (live: one building, no output); and building GUID `153793`, which the
   pinned calculator revision does not know.
3. Whether AI or co-op players' islands ever appear (the live save had AI
   islands in Latium and they did not).
4. Whether the tick schedule is exactly 2 min, and whether it is tied to the
   statistics screen's refresh or to a game-speed setting.
5. Whether `sessionID` is ever needed besides `sessionGUID` (both captures:
   1 ↔ 3245, 3 ↔ 6627).
