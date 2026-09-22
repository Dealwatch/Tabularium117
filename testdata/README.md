# Test fixtures

Replay fixtures for developing and testing Tabularium 117 without a running game.
`docs/protocol.md` describes the pipe format these fixtures were captured from.

## `connector/`

`example_responses.txt` is copied verbatim from
`anno-mods/anno117-game-connector`, commit
`ed6548f1e706fc8ceafbe1821f82a79292d506de` (2026-09-20), file
`docs/example_responses.txt`. That repository is released into the public
domain (Unlicense); the captured game data itself is © Ubisoft and used here
for interoperability testing only.

It holds 14 real `AreaProductionStatistics` messages (pipe protocol v2) as
the connector's SSE lines (`data: {...}`), i.e. the decoded JSON form, not
raw pipe bytes: two game sessions, seven islands each, all sharing one
`timeStamp`. The catalog coverage test reads it, and it is the source of
`connector-reencoded.jsonl` below.

## `connector-reencoded.jsonl`

A Tabularium 117 replay recording in the format defined in `docs/protocol.md`: one
JSON object per line, `frame` holding the base64 of a raw pipe payload (type
byte plus body, without the length prefix). It contains 16 frames – a `Version`
frame, a `SessionStart` frame, and the 14 `AreaProductionStatistics` messages of
`connector/example_responses.txt` – with synthetic timestamps one second apart
starting at `2026-09-21T12:00:00Z`.

**This file is re-encoded, not captured.** Its bytes were produced by encoding
the connector's *decoded* JSON back into the wire format, so it proves the
decoder, not the game: it can only contain what the connector's decoder already
understood, and a field the connector dropped is missing here too. The real
capture from a running game is `live-2026-09-22.jsonl` below.
Regenerate it deterministically with:

```
go run ./tools/reencode-connector
```

The JSON field names follow the connector's `BuildStatisticsPayload()`
(`version` there is the connector's SSE schema version 1, not the pipe protocol
version). Tabularium 117's own record/replay format is defined in `docs/protocol.md`.

## `live-2026-09-22.jsonl`

A **real recording** from the maintainer's PC (2026-09-22, recorded
with `tabularium117 --record`), reduced from 45 to 35 frames and anonymised:

- kept: frame 1 `Version 2`; frame 2 `SessionEnd` (sent on connect to a
  running game); the first full tick (10 islands, 236 entries); the third
  tick with two islands renamed to `R_mische K_ste` / `Englische K_ste` (the
  game replaces umlauts with `_`); the two `SessionEnd` frames around the
  reload; `SessionStart`; the all-empty tick that follows a reload
  (10 islands, 0 entries each);
- dropped: the second tick (identical structure to the first);
- changed: the `SessionStart` headline (the player's profile name) was
  replaced by `Captain`. Island names are game-generated and unchanged.

Receive times (`t`) are the original ones, so the 1 s pacing and the ~2 min
tick spacing are real. See `docs/protocol.md`, "Live capture 2026-09-22".
The full raw capture is not in the repository.
