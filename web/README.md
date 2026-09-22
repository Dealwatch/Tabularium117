# Tabularium 117 web UI

Static frontend embedded into `tabularium117.exe` via `embed` (see `embed.go`) and
served at `/`. No build step: HTML, CSS, and JavaScript ES modules only; uPlot
is vendored under `vendor/uplot/`.

Layout: `index.html` (shell), `css/app.css`, `js/app.js` (entry, router,
status bar), `js/api.js`, `js/live.js` (SSE), `js/i18n.js` (DE/EN),
`js/alerts.js`, `js/wakelock.js` (the "keep the screen on" opt-in),
`js/views/*.js` (islands, history, efficiency, alerts, help, phone). The phone
panel (`js/views/phone.js`, route `#/phone`) is shown only when
`/api/v1/status` reports `lan.local`, i.e. to the browser on the PC itself; a
LAN client gets a small indicator in the status bar instead.
New top-level directories must be added to the `//go:embed` pattern in
`embed.go` to reach the executable. To look at the UI without the game:
`go run ./cmd/tabularium117 --replay testdata/connector-reencoded.jsonl --serve-after-replay`.
