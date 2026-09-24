// Package alerts evaluates rules against incoming snapshots and raises or
// clears warnings (KONZEPT.md section 2, feature 4).
//
// The engine is pure: it does no I/O, imports nothing beyond internal/model
// and the standard library, and never blocks. Apply is called from the ingest
// goroutine for every stored snapshot and returns the events that snapshot
// caused; the caller decides whether they are persisted, published or both.
//
// Three rules exist.
//
//   - "deficit": the product's Delta has been negative in DeficitSamples
//     consecutive samples, on an island with buildings of its own for it.
//     It clears after DeficitClearSamples consecutive samples with a Delta
//     of zero or more. Severity warning.
//   - "import": the same streak on an island without any building for the
//     product. The island lives on imports of it, which is how most goods
//     reach most islands - not a fault, so the severity is info: kept and
//     listed, but not counted or announced as a warning.
//   - "productivity_drop": the productivity of the product's buildings has
//     fallen more than DropPercentagePoints below its trailing mean over
//     DropWindow, and the product was consumed on the island within that
//     window. It clears once the productivity is back within
//     DropClearPercentagePoints of that mean. Severity warning.
//
// The drop rule reads AverageProductivity (SummedProductivity /
// AmountOfBuildings x 100, docs/protocol.md), not Generation /
// PerfectGeneration. It first did the latter, and a 35-minute live capture
// of 2026-09-23 showed why that cannot work: the pipe counts completed
// production cycles per tick, so the generation of a building that runs
// without pause still jumps between 0 and its full rate from tick to tick.
// Measured against a trailing mean, that is a drop every few ticks - 37
// alerts in that capture, most of them for buildings running at 86-100 %.
// The productivity is the game's own running average and does not jitter.
// It is capped at 100 %: items and effects push it far above that, and a
// boost wearing off (174 % down to 151 % in the capture) is not a stall.
//
// Storage is not in the pipe. A building whose storage is full stops, and
// that looks exactly like one that lacks workers or input goods. Gating the
// drop rule on consumption is how the common harmless case - a surplus
// product that nobody takes from the storage - stays quiet. It cannot catch
// every one: on the same capture the rule raises 4 alerts, two of them real
// slowdowns (sausages at 77 %, ornate wood at 55 %) and two for marble,
// whose storage was full while a construction site took some now and then.
//
// One sample is one statistics tick, and the game produces a tick roughly
// every two minutes (docs/protocol.md). Every count and window in Config is
// set for that cadence, not for seconds.
//
// Both rules use hysteresis: the raise and the clear thresholds differ, and a
// streak counter is reset the moment its condition breaks, so a value that
// alternates around a threshold never produces a stream of events.
//
// State is per (island, product). A product that stops appearing in an
// island's snapshots keeps its state and its alert: the engine cannot tell
// "the chain was demolished" from "the game left it out of this tick", and
// silently clearing a warning is the worse of the two mistakes. A session
// boundary is the one place where state is dropped, through Reset.
package alerts
