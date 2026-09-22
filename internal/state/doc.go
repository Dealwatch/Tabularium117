// Package state holds the current picture of the game: the latest snapshot per
// island, the current session, and the connection status.
//
// It is the "state (in-memory, aktuell)" box of KONZEPT.md section 3 and the
// only place the HTTP layer (phase 4) reads the live view from. It depends on
// internal/model alone and knows nothing about the pipe, the wire format or
// SQLite; history belongs to internal/store, not here.
//
// Every method is safe for concurrent use. Snapshots are treated as immutable
// once they are decoded: Put takes ownership of the value it is given, and
// readers must not mutate what Snapshot or Islands return. That is what allows
// the state to hand out values without copying the product slice and the maps
// inside it.
package state
