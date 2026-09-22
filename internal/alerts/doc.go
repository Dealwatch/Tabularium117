// Package alerts evaluates rules against incoming snapshots and raises or
// clears warnings (KONZEPT.md section 2, feature 4; task T6.1).
//
// The engine is pure: it does no I/O, imports nothing beyond internal/model
// and the standard library, and never blocks. Apply is called from the ingest
// goroutine for every stored snapshot and returns the events that snapshot
// caused; the caller decides whether they are persisted, published or both.
//
// Two rules exist.
//
//   - "deficit": the product's Delta has been negative in DeficitSamples
//     consecutive samples. It clears after DeficitClearSamples consecutive
//     samples with a Delta of zero or more.
//   - "productivity_drop": the product's efficiency has fallen more than
//     DropPercentagePoints below its trailing mean over DropWindow. It clears
//     once it is back within DropClearPercentagePoints of that mean.
//
// Efficiency is Generation / PerfectGeneration in percent, the measure
// KONZEPT.md section 12 settled on in T0.4. AverageProductivity is
// deliberately not used. Its meaning is no longer open - the live capture of
// 2026-09-22 showed it is SummedProductivity / AmountOfBuildings x 100 - but
// it measures how hard the buildings run, not how the output compares with
// its optimum, which is the question these rules ask.
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
