# Contributing to Tabularium 117

Thanks for helping. This file holds the project rules that every change is
measured against; `KONZEPT.md` (concept, architecture, data model, API) is
the source of truth for *what* is built, `docs/protocol.md` for everything
known about the game's pipe.

## Ground rules

- **Read-only towards the game.** Tabularium 117 never writes to the pipe and
  never touches game files. No telemetry, no cloud, no accounts.
- **The pipe format lives in one place:** `internal/protocol`. No other
  package parses raw frames. When the protocol is unclear, do not guess —
  record raw data (`--record`) and look at it; `docs/protocol.md` labels every
  fact as measured, inferred or open.
- **Island identity is `(SessionGUID, IslandID)`**, never `IslandID` alone.
- **Default binding is `127.0.0.1:53118`.** Nothing opens the LAN without the
  access token check, the RFC 1918 client filter and the Host check
  (`KONZEPT.md` §6).
- **Unknown GUIDs are never swallowed:** display as `#123456`, log once.
- **Frontend without a build step:** HTML, CSS and JavaScript ES modules under
  `web/`, embedded into the executable; uPlot is vendored. Its logic is
  tested in `scripts/ui.test.mjs` against a small DOM double
  (`node --test scripts/ui.test.mjs`, part of `check.sh`): no browser, no
  dependency, and no claim about how the page looks - that still needs eyes.
- **Dependencies:** standard library plus `go-winio`, `modernc.org/sqlite` and
  `go-qrcode`. Adding one is a discussion, not a commit.
- **Game names and GUIDs** come from a pinned revision of
  `anno-mods/anno-117-calculator`, recorded in `THIRD_PARTY_NOTICES.md`; no
  Ubisoft icons are shipped.
- **Language:** code, comments and commit messages in English; user-facing
  documentation in German and English. `KONZEPT.md` is German.

## Layout

`cmd/tabularium117` (CLI), `internal/{pipe, protocol, model, source, replay,
ingest, state, catalog, store, alerts, server, lan}`, `web/` (UI),
`tools/` (generators), `testdata/` (recordings and fixtures), `docs/`.

## Environments

- Target: Windows/amd64, one static `tabularium117.exe`.
- Development works on Linux and macOS too: only `internal/pipe` depends on
  Windows; everything else builds and tests everywhere using the recordings
  in `testdata/`. A test against the real game needs Windows and Anno 117.

## Verifying a change

```sh
./scripts/check.sh                    # gofmt, go vet, go test, the UI tests — the definition of "verified"
git config core.hooksPath .githooks   # once per clone: runs check.sh before every push
make build                            # dist/tabularium117.exe (windows/amd64); build.ps1 on Windows
go run ./cmd/tabularium117 --replay testdata/live-2026-09-22.jsonl --serve-after-replay
```

Any Go from 1.21 on will do: `go.mod` pins the toolchain (`toolchain
go1.25.14`), and the `go` command fetches it by itself (`GOTOOLCHAIN=auto`).
Keep that pin on the newest 1.25 patch release - it is what the release is
built with, and nearly every patch release closes security holes in the
standard library. CI's `govulncheck` job fails when it falls behind.

`.github/workflows/ci.yml` runs the same `check.sh` on Linux **and** Windows
for every push and pull request, plus a `go mod tidy` diff and the
cross-compiled release build; `.github/workflows/release.yml` builds the
release from a tag. Windows is not optional there: `internal/pipe` builds
nowhere else, and two classes of bug only show on it - CRLF checkouts that
make `gofmt` fail (see `.gitattributes`) and content types taken from the
registry (see `internal/server/server.go`).

Run `./scripts/check.sh` locally anyway: it is the faster loop, and the
pre-push hook is what keeps a red commit from being pushed at all.

## Conventions

- Keep unrelated cleanup out of a scoped change.
- Verification claims describe what actually ran: unit tests, a headless
  browser run, a replay, or the real game are different kinds of evidence.
- A new guard comes with a failure-path test, not only the happy path.
- When behaviour, interfaces or the workflow change, update the
  documentation in the same change (`KONZEPT.md` §5 for the API, `README.md`
  for flags, `docs/protocol.md` for protocol facts).
