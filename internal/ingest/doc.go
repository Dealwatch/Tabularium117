// Package ingest is the normalizer of KONZEPT.md section 3: it consumes raw
// frames from a source, decodes them and keeps internal/state current.
//
// It is the one place where the pieces meet, and it owns the policy decisions
// that neither the source nor the decoder may take:
//
//   - Recording comes first. When a recording is configured, the raw frame is
//     written before it is decoded, so that a frame Tabularium 117 cannot decode is
//     still captured and can be analysed later (task T0.3).
//   - An unsupported protocol version stops decoding, as KONZEPT.md section 8
//     requires and unlike the Ubisoft reference reader, which carries on.
//     Statistics frames are dropped until the game announces a version again,
//     which every pipe connection does as its first frame. Before any Version
//     frame is seen the pipeline accepts frames: a recording may begin
//     anywhere, and nothing has contradicted the expected format yet.
//   - A frame that fails to decode is logged and skipped, never fatal. One
//     malformed frame must not take the whole pipeline down; the connection is
//     the pipe client's problem, not the decoder's.
//   - Losing the connection does not clear the islands. The last known picture
//     stays visible (the UI greys it out) because it is still the best
//     information there is. Only a session boundary - SessionStart or
//     SessionEnd - resets the islands, because island identity stops being
//     meaningful there.
//
// Ordering: a Pipeline is driven by a single goroutine, while the source's
// status callback runs in the source's own goroutine. Both are safe: the
// per-connection version gate is guarded by a mutex, and the pipe client emits
// its connected status before it sends the first frame of that connection.
package ingest
