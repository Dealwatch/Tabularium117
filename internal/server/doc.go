// Package server serves the embedded web UI, the read-only REST API and the
// SSE live feed. It is the "HTTP-Server" box of KONZEPT.md section 3; the
// endpoints themselves are specified in KONZEPT.md section 5, which stays the
// single reference for them.
//
// The package reads internal/state for the live picture, internal/store for
// the history and the rule engine of internal/alerts for the warnings that
// are open right now, and resolves every GUID through internal/catalog. It
// knows nothing about the pipe or the wire format, and it changes nothing
// about the game or the history: the single write endpoint it has,
// POST /api/v1/lan, switches Tabularium 117's own LAN listener and nothing else,
// which keeps "strictly read-only towards the game" observable from the
// outside.
//
// Conventions that hold for every endpoint below /api/v1:
//
//   - An island is addressed as "<sessionGUID>-<islandID>", because neither
//     half is unique on its own (KONZEPT.md section 4).
//   - The answer's language comes from ?lang= or Accept-Language, and names
//     are resolved through the catalog, so an unknown GUID appears as "#123"
//     and is logged once instead of being dropped.
//   - Failures are {"error": "..."} with a fitting status code, responses
//     carry Cache-Control: no-store, and nothing sends a CORS header: the UI
//     is served from the same origin.
//   - Anything but GET or HEAD is refused with 405 before it reaches a
//     handler, except POST /api/v1/lan, the LAN switch.
//   - Live answers come from the rule engine and the live state; anything
//     historical needs the database and answers 503 without one, rather than
//     an empty result the UI would read as "nothing happened".
//
// Every request on both listeners passes a Host check first (server.go): the
// name asked for has to be one this listener serves, or the answer is 421.
// That is what keeps a page whose name was rebound to 127.0.0.1 from reading
// this API as if it were the browser on the PC.
//
// Binding is the caller's decision. The loopback listener always exists and is
// the trusted side. LAN mode (lan.go, KONZEPT.md section 6) adds a second
// listener on one private IPv4 address; everything that arrives there passes
// the RFC 1918 client filter and the access token first, and the side a
// request came in on is decided by the accepting listener, never by a header.
package server
