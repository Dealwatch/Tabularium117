// Package store persists the sample history in SQLite.
//
// It owns the schema, migrations, batched writes, downsampling, and time-range
// queries. It is the only package that imports a database driver
// (modernc.org/sqlite, pure Go so that the release stays a single CGO-free
// executable); everything above it sees Go types from internal/model and the
// row types declared here.
//
// Design decisions worth knowing before changing anything here:
//
//   - Time is the receiver's clock, stored as Unix milliseconds in an INTEGER
//     column (docs/protocol.md: the pipe's own timeStamp is opaque). The game
//     clock is deliberately not a time axis.
//
//   - Island identity is (SessionGUID, IslandID), never IslandID alone
//     (KONZEPT.md section 4). The island table's integer id is a local surrogate
//     key that only exists to keep the sample rows narrow.
//
//   - History lives in two tables: sample at full resolution and sample_bucket
//     with means of CompactOptions.BucketWidth (ten minutes by default, and
//     one minute for rows an older build wrote - each row carries its own
//     bucket_ms). Compact moves rows from the first into the second once they
//     are older than the raw retention, so a query for a long range reads far
//     fewer rows. The two ranges are disjoint by construction, which is why
//     History can simply concatenate them.
//
//   - The alert table (migration 2, task T6.2) records when a warning started
//     and when it ended, not what it looked like in between. The value that
//     triggered a rule changes with every sample and belongs to the live
//     engine in internal/alerts; the row keeps the detail text it was raised
//     with. An alert with no cleared_at is one this process is watching, so
//     Open closes whatever an earlier run left behind.
//
//   - Workforce and building maps are not persisted (KONZEPT.md section 4).
//     They exist only in the live state; storing them would multiply the
//     history's size for data that is only ever looked at "right now".
//
//   - Retention is measured against the newest sample the store has been given,
//     not against the wall clock. A recording carries the timestamps it was
//     recorded with, so replaying last month's capture must produce last
//     month's history rather than an empty database. See RunCompactor.
//
// Concurrency: writes are serialised by a one-slot channel rather than a
// mutex, because SQLite allows one writer at a time and the wait has to be
// cancellable - a caller's deadline must bound the queueing too, or a long
// compaction would freeze the writers behind it. Reads go through the
// connection pool and run concurrently with a writer thanks to WAL mode. A
// *Store is safe for use from multiple goroutines.
//
// Batcher is the intended way in for live data: it takes snapshots, session
// boundaries and alert events without blocking and writes them from its own
// goroutine, so that ingest never waits for the database. They share one
// queue, which is what keeps them in the order ingest saw them - an alert is
// written after the snapshot that caused it, so its island row exists.
package store
