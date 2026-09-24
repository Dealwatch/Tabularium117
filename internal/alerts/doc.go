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
//   - "no_local_production": the same streak on an island without any
//     building for the product. The island needs it from elsewhere, which is
//     how most goods reach most islands - not a fault in itself, so the
//     severity is info: kept and listed, but not counted or announced as a
//     warning. The pipe says nothing about trade: whether a ship actually
//     brings the product, or the storage is running empty, is not in the
//     data, and the rule claims neither.
//   - "productivity_drop": the productivity of the product's buildings has
//     fallen more than DropPercentagePoints below its trailing mean over
//     DropWindow, while the island has been short of the product for
//     DropShortfallSamples consecutive samples. It clears once the
//     productivity is back within DropClearPercentagePoints of that mean,
//     or the shortfall has been over for DeficitClearSamples samples, or
//     the buildings are gone. Severity warning.
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
// that looks exactly like one that lacks workers or input goods - an island
// with far more capacity than it consumes spends most of its time like that.
// The difference that is in the data is the delta: a full storage keeps it
// at zero or above, a stalled chain drives it below. Hence the shortfall
// condition. On the capture every productivity drop happened with a delta of
// zero or more, and the rule raises none; what it keeps is a drop that
// explains a shortage, not every building that pauses.
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
// island's snapshots keeps its deficit state and its alert: the engine
// cannot tell "the chain was demolished" from "the game left it out of this
// tick", and silently clearing a warning is the worse of the two mistakes.
// A productivity drop is the exception: a product without buildings, or
// missing from a snapshot that has goods in it, has nothing left that could
// have stalled, so its drop alert ends. An empty snapshot - the warm-up
// after a save loads - changes nothing. A session boundary is the one place
// where all state is dropped, through Reset.
package alerts
