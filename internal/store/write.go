package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// SQL used when writing. Kept as constants so the statements are visible in
// one place and can be prepared once per transaction.
const (
	// insertSessionSQL opens a game session row.
	insertSessionSQL = `INSERT INTO game_session (started_at, ended_at, protocol_version, headline)
		VALUES (?, NULL, ?, ?)`

	// endSessionSQL closes one session, leaving an already closed one alone
	// so that a late EndSession cannot overwrite the real end time.
	endSessionSQL = `UPDATE game_session SET ended_at = ? WHERE id = ? AND ended_at IS NULL`

	// closeOpenSessionsSQL is crash recovery: a process that was killed never
	// wrote its ended_at.
	closeOpenSessionsSQL = `UPDATE game_session SET ended_at = ? WHERE ended_at IS NULL`

	// upsertIslandSQL resolves (session_guid, island_id) to the local surrogate
	// id, creating the row on first sight.
	//
	// last_seen takes the later of the two values so that a snapshot that
	// arrives out of order - a batch is not sorted - cannot move the island
	// backwards in time. first_seen is never touched again.
	upsertIslandSQL = `INSERT INTO island (session_guid, island_id, name, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (session_guid, island_id) DO UPDATE SET
			name      = excluded.name,
			last_seen = max(island.last_seen, excluded.last_seen)
		RETURNING id`

	// insertSampleSQL writes one product row. REPLACE, not IGNORE: the same
	// (island, product, ts) twice means the same measurement was delivered
	// twice, and the later copy is the one to keep.
	//
	// game_ts is the pipe's timeStamp as delivered - the game clock in
	// milliseconds, identical for every island of one tick and therefore the
	// tick's id (docs/protocol.md). It is not part of the key: ts, the
	// receive time, stays the time axis, and the game clock stands still
	// while the game is paused.
	insertSampleSQL = `INSERT OR REPLACE INTO sample (
			island, product_guid, ts,
			generation, consumption, delta,
			perfect_generation, perfect_consumption,
			buildings, avg_productivity, game_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
)

// StartSession records the beginning of a game session and returns its id.
func (s *Store) StartSession(ctx context.Context, startedAt time.Time, protocolVersion int32, headline string) (int64, error) {
	var id int64
	err := s.write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, insertSessionSQL, millis(startedAt), protocolVersion, headline)
		if err != nil {
			return fmt.Errorf("store: start session: %w", err)
		}
		id, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("store: start session: %w", err)
		}
		return nil
	})
	return id, err
}

// EndSession records the end of one session. Ending a session that is already
// closed, or one that does not exist, is not an error: shutdown must not fail
// because the crash recovery in Open got there first.
func (s *Store) EndSession(ctx context.Context, id int64, endedAt time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, endSessionSQL, millis(endedAt), id); err != nil {
			return fmt.Errorf("store: end session %d: %w", id, err)
		}
		return nil
	})
}

// CloseOpenSessions closes every session that has no end time, which is how a
// crash or a kill leaves them behind. Open calls it, so an open session always
// belongs to a running process.
func (s *Store) CloseOpenSessions(ctx context.Context, endedAt time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, closeOpenSessionsSQL, millis(endedAt)); err != nil {
			return fmt.Errorf("store: close open sessions: %w", err)
		}
		return nil
	})
}

// WriteSnapshots stores a batch of island snapshots in a single transaction.
//
// Each snapshot's ReceivedAt is the sample timestamp (docs/protocol.md: the
// receive time is the time base). An island with no products still updates the
// island row - the game reports islands that produce nothing, and dropping
// them would make them disappear from the history for no reason.
func (s *Store) WriteSnapshots(ctx context.Context, snaps []model.IslandSnapshot) error {
	if len(snaps) == 0 {
		return nil
	}
	var newest int64
	err := s.write(ctx, func(tx *sql.Tx) error {
		islandStmt, err := tx.PrepareContext(ctx, upsertIslandSQL)
		if err != nil {
			return fmt.Errorf("store: prepare island upsert: %w", err)
		}
		defer islandStmt.Close()
		sampleStmt, err := tx.PrepareContext(ctx, insertSampleSQL)
		if err != nil {
			return fmt.Errorf("store: prepare sample insert: %w", err)
		}
		defer sampleStmt.Close()

		for _, snap := range snaps {
			ts := millis(snap.ReceivedAt)
			newest = max(newest, ts)
			var islandID int64
			err := islandStmt.QueryRowContext(ctx,
				snap.Key.SessionGUID, snap.Key.IslandID, snap.Name, ts, ts).Scan(&islandID)
			if err != nil {
				return fmt.Errorf("store: upsert island (%d,%d): %w",
					snap.Key.SessionGUID, snap.Key.IslandID, err)
			}
			for _, p := range snap.Products {
				_, err := sampleStmt.ExecContext(ctx,
					islandID, p.ProductGUID, ts,
					p.Generation, p.Consumption, p.Delta,
					p.PerfectGeneration, p.PerfectConsumption,
					p.Buildings, p.AvgProductivity, snap.GameTimestamp)
				if err != nil {
					return fmt.Errorf("store: write sample (island %d, product %d): %w",
						islandID, p.ProductGUID, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Only a committed batch may move the retention anchor.
	s.noteSampleTime(newest)
	return nil
}
