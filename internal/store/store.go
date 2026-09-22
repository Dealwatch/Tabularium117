package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	// The SQLite driver. Pure Go, so CGO_ENABLED=0 still produces one static
	// executable. internal/store is the only package allowed to import it.
	_ "modernc.org/sqlite"
)

// driverName is the database/sql driver modernc.org/sqlite registers.
const driverName = "sqlite"

// MemoryPath opens a private in-memory database. Tests use it; the real
// program always uses a file so that the history survives a restart.
const MemoryPath = ":memory:"

// Store is an open history database.
type Store struct {
	db   *sql.DB
	path string

	// writeLock serialises writers. SQLite permits exactly one at a time, and
	// waiting in Go produces a clear queue instead of busy-timeout errors
	// that callers would have to retry themselves.
	//
	// It is a channel rather than a sync.Mutex so that waiting for it can be
	// selected against a context. A caller that passes a five-second deadline
	// must not be able to wait longer than that because a minutes-long
	// compaction happens to hold the lock.
	writeLock chan struct{}

	// lastSample is the newest snapshot receive time ever written, in Unix
	// milliseconds, or 0 when this process has written nothing. See
	// LastSampleTime.
	lastSample atomic.Int64
}

// Open opens (and creates, if needed) the database at path and brings its
// schema up to date. Use MemoryPath for a private in-memory database.
//
// Sessions and alerts left open by an earlier crash are closed as part of
// opening, so a row with no end time always means "this process owns it".
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open(driverName, dsn(path))
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}

	if isMemory(path) {
		// Every connection to ":memory:" would get its own empty database,
		// so an in-memory store is exactly one connection.
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(maxOpenConns)
	}
	db.SetMaxIdleConns(maxOpenConns)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}

	s := &Store{db: db, path: path, writeLock: make(chan struct{}, 1)}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	now := time.Now()
	if err := s.CloseOpenSessions(ctx, now); err != nil {
		db.Close()
		return nil, err
	}
	// The rule engine keeps its state in memory only, so alerts left open by
	// an earlier run can never be cleared by anyone. Closing them here keeps
	// "open" meaning "this process is watching it".
	if err := s.CloseOpenAlerts(ctx, now); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// maxOpenConns caps the pool for a file database. One writer plus a handful of
// readers is all this program ever needs.
const maxOpenConns = 4

// Close closes the database. Pending writes must be finished first; Close does
// not flush a Batcher.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("store: close %s: %w", s.path, err)
	}
	return nil
}

// Path reports the file the store was opened from.
func (s *Store) Path() string { return s.path }

// dsn builds the connection string. The pragmas are part of the DSN rather
// than a one-off statement because database/sql opens connections lazily and
// every one of them has to be configured the same way.
//
// The driver strips the query from a DSN that does not start with "file:", so
// path is handed to SQLite verbatim - which keeps Windows paths such as
// C:\Users\x\Tabularium117\tabularium117.db working without URI escaping.
func dsn(path string) string {
	pragmas := []string{
		// Wait instead of failing when another connection holds the write
		// lock; the compactor and the batcher can overlap.
		"_pragma=busy_timeout(5000)",
		// Durable enough for a history that can be re-recorded, and much
		// cheaper than FULL on every commit.
		"_pragma=synchronous(NORMAL)",
		// SQLite disables foreign keys per connection by default.
		"_pragma=foreign_keys(ON)",
	}
	if !isMemory(path) {
		// WAL lets the UI read while the batcher writes. It is a property of
		// the file, so it is meaningless for an in-memory database and the
		// driver would report an error for it.
		pragmas = append(pragmas, "_pragma=journal_mode(WAL)")
	}
	return path + "?" + strings.Join(pragmas, "&")
}

// isMemory reports whether path names an in-memory database rather than a
// file. Open takes a plain filesystem path or MemoryPath, nothing else, so
// this is an exact comparison rather than a guess at SQLite's URI forms.
func isMemory(path string) bool { return path == MemoryPath }

// LastSampleTime reports the newest snapshot receive time this Store has
// written, or the zero time when it has written nothing yet.
//
// It is the time base for retention (see RunCompactor). The data's own clock
// is used rather than the wall clock because a replayed recording carries the
// timestamps it was recorded with: measuring a two-week-old recording against
// time.Now() would delete it the moment it was written.
func (s *Store) LastSampleTime() time.Time {
	ms := s.lastSample.Load()
	if ms == 0 {
		return time.Time{}
	}
	return fromMillis(ms)
}

// noteSampleTime raises LastSampleTime to ms if ms is newer.
func (s *Store) noteSampleTime(ms int64) {
	for {
		current := s.lastSample.Load()
		if ms <= current {
			return
		}
		if s.lastSample.CompareAndSwap(current, ms) {
			return
		}
	}
}

// acquireWrite takes the write lock, or gives up when ctx is done. Every
// caller that succeeds must call releaseWrite.
func (s *Store) acquireWrite(ctx context.Context) error {
	select {
	case s.writeLock <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("store: waiting for the write lock: %w", ctx.Err())
	}
}

// releaseWrite returns the write lock.
func (s *Store) releaseWrite() { <-s.writeLock }

// write runs fn inside a write transaction, serialised against other writers.
// The transaction is rolled back unless fn returns nil.
func (s *Store) write(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if err := s.acquireWrite(ctx); err != nil {
		return err
	}
	defer s.releaseWrite()
	return s.writeLocked(ctx, fn)
}

// writeLocked is write for callers that already hold the write lock.
func (s *Store) writeLocked(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// millis converts a time to the storage representation: Unix milliseconds.
func millis(t time.Time) int64 { return t.UnixMilli() }

// fromMillis converts a stored timestamp back, in the local time zone.
func fromMillis(ms int64) time.Time { return time.UnixMilli(ms) }

// logOrDiscard returns log, or a logger that throws everything away when log
// is nil, so that callers never have to check.
func logOrDiscard(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return log
}
