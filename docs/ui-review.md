# UI release review

## Evidence from this change

- Eight dependency-free Node DOM-logic tests pass: combined search/filters,
  warning scope, sort direction and focus, German number formatting and warm-up,
  native island switching, late/out-of-order responses, and efficiency edge cases.
- JavaScript syntax, local module references and diff whitespace checked.
- No real-game, LAN, rendered-layout or chart-interaction verification claimed.
- Owner screenshots establish the German desktop dark home/efficiency layout
  after region grouping and meter alignment. They exposed a zero-deficit grid
  placement conflict: the muted-link style also matched the product-count rule.
  The selector now targets the product-count span only. This CSS correction
  still needs a rendered check; mobile, light theme and charts remain open.
- Browser preview is blocked by URL security policy (after the earlier usage
  limit). The follow-up CSS has not been rendered here.
- `./scripts/check.sh` cannot run here because Go is not installed.

## Before merging / Vor dem Merge

Run `./scripts/check.sh`, then `node --test scripts/ui.test.mjs` (Node 22+).
Start a replay with:

```sh
go run ./cmd/tabularium117 --replay testdata/connector-reencoded.jsonl --replay-speed 0 --serve-after-replay
```

1. Verify Latium/Albion headings in the sidebar, overview and island picker;
   deficit links open the correct island with the deficit filter selected.
   Replay says Recorded data/Aufzeichnungsdaten with an absolute recorded-data timestamp.
   The API does not distinguish replay in progress from replay completed.
2. At desktop width and 390/320 px, inspect home, goods, efficiency, history,
   warnings, help and the local phone panel. No page-wide horizontal overflow;
   wide tables scroll within their own region.
3. On mobile, choose an island near the end of the list: details begin at the
   top. Return with “Islands”, switch islands, and use browser Back/Forward.
4. Combine each filter with search, test no matches, sort by keyboard, and open
   a product using its link. Inspect focus outlines and sticky product names.
5. Toggle German/English and dark/light on all views, especially on a chart.
   Per-minute units remain explicit; chart colors update immediately.
6. Verify zero active warnings still allows entry to the warning history.
   Check no history, one point, multiple points and disabled history (`--no-db`).
7. Check waiting-for-game, warm-up, connection loss, session change, long island
   names and unknown GUIDs. Exercise rapid island and history-range switching.
8. On a real phone, confirm LAN access, QR scanning and the existing optional
   screen-awake behavior. These cannot be established by DOM tests.
9. Replace the previous screenshots under `docs/screenshots/` with verified
   current views and restore their README embeds; label replay screenshots.

The code is prepared on a review branch. Visual acceptance is intentionally open.
