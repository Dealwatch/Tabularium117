# Changelog

All notable changes to Tabularium 117 are documented here, in the
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) style.

## [0.1.0-beta.1] - unreleased

First beta release, for testing by a small group of real players before a
1.0 announcement.

### Added

- Live island overview: every good with production, consumption, and delta;
  deficits sort to the top and are marked in red.
- History per good, with 1 h, 4 h, 24 h, 7 day, and whole-session ranges;
  the history database keeps 7 days at full resolution, then compresses to
  10-minute means, and deletes data after 90 days.
- Efficiency view: actual production against the theoretical perfect
  production per good, plus a productivity column showing the average
  productivity of that good's buildings.
- One UI shell for an island: the island's name, the session it belongs to,
  a dropdown to switch island without going back to the sidebar, and tabs
  between the goods table and the efficiency view. The sidebar groups its
  islands under their session instead of repeating the session's name on
  every row, and the deficit counts are tinted rather than solid, so the
  warnings stay the loudest thing on the page.
- The efficiency view explains the two ways a good can produce nothing: a
  productivity of 0 % next to a perfect generation means the buildings are
  standing still, a "-" means there are no producing buildings for that good
  on the island. Both carry the sentence as a tooltip, and the paragraph
  above the table says it too, because a phone has no hover.
- Rule-based alerts with hysteresis: a sustained deficit (default 3
  consecutive negative-delta measurements) or an efficiency drop (default 20
  percentage points below its 15-minute mean) raises a warning, optionally
  with a browser notification or sound.
- Phone/LAN mode: a QR code and a per-start access token expose the UI to
  other devices on the same private network (RFC 1918 only), off by
  default.
- German and English UI, switchable at runtime, in a light and a dark
  theme that share one accent colour.
- Replay and record: `--replay` plays back a JSONL capture instead of the
  real pipe, `--record` writes every raw frame to a JSONL file.
- Windows-only named-pipe client (`internal/pipe`); every other package
  builds and runs on Linux against replay fixtures.
- `--anonymize-recording <file>` rewrites a recording without the profile
  name from `SessionStart` in it and exits, without starting the server or
  the pipe. `--out <file>` picks the output (default: the input with
  `.anon.jsonl` appended) and `--anonymize-islands` also replaces every
  island name with `Island <sessionGUID>-<islandID>`. A recording carries its
  frames base64-encoded, so the search-and-replace this replaces could never
  have found those names.

The 2-minute statistics tick cadence and the productivity formula documented
in `docs/protocol.md` and `KONZEPT.md` both come from the live capture
recorded against the real game on 2026-09-22 (task T0.3).

### Fixed

- Islands of a finished session stayed in the sidebar: the browser only ever
  added and updated islands from the event stream, which cannot express a
  removal. The page now refetches the whole list whenever the status reports
  a different island count, a different session or a change of connection,
  and on every (re)connect of the stream, and leaves an island view whose
  island is gone.
- The status bar froze between session events: "last frame X ago" and the
  warm-up hint only travelled with a `status` event, which was published on a
  connection, session or version change and not on a statistics tick.
  Publishing an island snapshot now publishes the status with it.

### Known limitations

- Whether the game accepts two simultaneous pipe clients is untested; the
  live capture used one client at a time (see `docs/protocol.md`, "Open
  questions").
- In the save tested on 2026-09-22, the pipe reported the player's own
  islands but not the AI islands. Co-op has not been tested; behavior in
  other saves is not yet verified.
- Umlauts and other non-ASCII characters in island and other names arrive
  from the pipe as `_`.
- The first statistics tick after a reload of the page (or of Tabularium 117
  itself) is empty; the UI shows a short warm-up state until the next tick.
- `tabularium117.exe` is an unsigned binary without a commercial code-signing
  certificate, so Windows SmartScreen warns on first run (see README).
