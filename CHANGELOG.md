# Changelog

All notable changes to Tabularium 117 are documented here, in the
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) style.

## [Unreleased]

### Added

- A ready-made Grafana dashboard for the Prometheus metrics,
  `docs/grafana/tabularium117.json`: connection, current deficits, balance
  and efficiency over time, and every good at a glance, filterable by
  session and island, with a separate good selector for the charts.

### Changed

- The status bar keeps the connection and age of the last frame visible;
  source, session, protocol and connection errors are in expandable details.
  A replay is shown neutrally rather than like a lost connection.
- The efficiency table says directly when buildings are idle or missing.
  Its longer explanations are optional, next to a short introduction. The
  efficiency API now includes the building count, so zero potential is not
  mistaken for zero buildings.
- The Grafana deficit and all-goods tables always cover all goods of the
  selected islands. Its charts initially focus on one available good, with
  a separate selector for comparing a few goods.

## [0.1.0-beta.2] - 2026-09-23

Second beta: the Steam launch option explained in the app, and optional
metrics for Prometheus and Grafana.

### Added

- The README states the measured resource usage of `tabularium117.exe`.
- **Prometheus metrics (optional, experimental):** `GET /metrics` serves the
  live state in the Prometheus text format - connection, islands, and
  production, consumption, balance, potential and building count per good and
  island, with names in separate info metrics. Loopback only, so Prometheus
  has to run on the same PC; the phone-mode listener answers 404. Nothing runs
  unless something scrapes it, and normal use needs neither Prometheus nor
  Grafana. The pipe carries no save identifier, so two saves can share the
  same series. Names and labels may still change before 1.0.

### Changed

- The in-app help, the README and the beta guide explain how to add the
  `/pipe` launch option on **Steam** as well as in Ubisoft Connect. Until now
  they only described Ubisoft Connect.
- Should a message ever list the same good twice, every view now keeps the
  later entry, as the history already did, and the log says so once per run.
  This has never been observed; until now the goods table would have shown
  both rows.

### Fixed

- With `--serve-after-replay`, the status bar kept saying "replaying" after
  the recording had ended. It now says "recording ended".

## [0.1.0-beta.1] - 2026-09-22

First beta release, for testing by a small group of real players before a
1.0 announcement.

### Added

- **Live island overview:** every good with production, consumption and
  balance per minute; deficits sort to the top and are marked in red. Filters
  for deficits and for goods with an open warning, and a search.
- **Island navigation:** islands grouped by session in the sidebar, a
  dropdown to switch island, and tabs between the goods table and the
  efficiency view.
- **History per good:** 1 h, 4 h, 24 h, 7 days or the whole session. The
  history keeps 7 days at full resolution, then 10-minute means, and deletes
  data after 90 days.
- **Efficiency view:** actual production against the potential per good,
  plus the average productivity of its buildings. Rows that produce nothing
  say whether the buildings are standing still or there are none.
- **Warnings:** a sustained deficit (default: 3 consecutive measurements) or
  an efficiency drop (default: 20 percentage points below its 15-minute
  mean), optionally with a browser notification or a sound.
- **Phone mode:** a QR code and a per-start access token open the UI to
  devices on the same private network (RFC 1918 only). Off by default.
- German and English, light and dark theme.
- **Record, replay and anonymise:** `--record` writes every raw frame to a
  file, `--replay` plays one back instead of the game, and
  `--anonymize-recording` removes the profile name (and with
  `--anonymize-islands` the island names) before a recording is shared.

### Known limitations

- Whether the game accepts two simultaneous pipe clients is untested (see
  `docs/protocol.md`, "Open questions").
- In the save tested on 2026-09-22, the pipe reported the player's own
  islands but not the AI islands. Co-op has not been tested; behaviour in
  other saves is not yet verified.
- Umlauts and other non-ASCII characters in island and other names arrive
  from the pipe as `_`.
- Loading or reloading a save sends one tick in which every island reports
  zero products; the UI shows a warm-up state until the real numbers arrive,
  up to two minutes later. Starting Tabularium 117 while a save is already
  loaded gets a full tick straight away.
- In phone mode, browser notifications and "keep the screen on" are not
  available: browsers allow both only over a secure connection, and phone
  mode is plain HTTP inside your own network. Both work on the PC.
- `tabularium117.exe` is not code-signed, so Windows SmartScreen warns on the
  first start (see README).
