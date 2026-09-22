package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// IslandRow is one row of the island table: the surrogate id, the real
// identity, and when the island was first and last reported.
type IslandRow struct {
	ID        int64
	Key       model.IslandKey
	Name      string
	FirstSeen time.Time
	LastSeen  time.Time
}

// SessionRow is one recorded game session. EndedAt is the zero time while the
// session is still open.
type SessionRow struct {
	ID              int64
	StartedAt       time.Time
	EndedAt         time.Time
	ProtocolVersion int32
	Headline        string
}

// Point is one measurement of one product on one island.
//
// Aggregated marks a point that came from sample_bucket, i.e. the mean of a
// time window rather than a single reading. The UI needs to know, because
// such a point smooths away short spikes. BucketMs says how wide that window
// was, in milliseconds, and is 0 for a raw point: the width is a policy
// (CompactOptions.BucketWidth) and a database may hold rows from several,
// including the one-minute means an older build wrote, so every row carries
// its own.
//
// GameTS is the game clock in milliseconds at that measurement - the pipe's
// timeStamp, which is the tick's id. For an aggregated point it is the
// largest one the bucket covers. It is 0 for a row written before schema 3
// and for a snapshot that carried none.
type Point struct {
	TS                 time.Time
	Generation         float32
	Consumption        float32
	Delta              float32
	PerfectGeneration  float32
	PerfectConsumption float32
	Buildings          int32
	AvgProductivity    float32
	Aggregated         bool
	BucketMs           int64
	GameTS             int64
}

// SQL used when reading.
const (
	islandsSQL = `SELECT id, session_guid, island_id, name, first_seen, last_seen
		FROM island ORDER BY session_guid, island_id`

	islandIDSQL = `SELECT id FROM island WHERE session_guid = ? AND island_id = ?`

	sessionsSQL = `SELECT id, started_at, ended_at, protocol_version, headline
		FROM game_session ORDER BY started_at DESC, id DESC`

	// historySQL reads both resolutions of one product's time series. The two
	// tables never cover the same instant - Compact moves a row from one to
	// the other - so the parts are simply concatenated and sorted.
	historySQL = `SELECT ts, generation, consumption, delta,
			perfect_generation, perfect_consumption, buildings, avg_productivity,
			0 AS aggregated, 0 AS bucket_ms, game_ts
		FROM sample
		WHERE island = ? AND product_guid = ? AND ts >= ? AND ts <= ?
	UNION ALL
		SELECT ts, generation, consumption, delta,
			perfect_generation, perfect_consumption, buildings, avg_productivity,
			1 AS aggregated, bucket_ms, game_ts_max
		FROM sample_bucket
		WHERE island = ? AND product_guid = ? AND ts >= ? AND ts <= ?
	ORDER BY ts`
)

// Islands returns every known island, ordered by (SessionGUID, IslandID) so
// that lists are stable.
func (s *Store) Islands(ctx context.Context) ([]IslandRow, error) {
	rows, err := s.db.QueryContext(ctx, islandsSQL)
	if err != nil {
		return nil, fmt.Errorf("store: list islands: %w", err)
	}
	defer rows.Close()

	var out []IslandRow
	for rows.Next() {
		var r IslandRow
		var first, last int64
		if err := rows.Scan(&r.ID, &r.Key.SessionGUID, &r.Key.IslandID, &r.Name, &first, &last); err != nil {
			return nil, fmt.Errorf("store: list islands: %w", err)
		}
		r.FirstSeen = fromMillis(first)
		r.LastSeen = fromMillis(last)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list islands: %w", err)
	}
	return out, nil
}

// Sessions returns the recorded game sessions, newest first. A limit of zero
// or less returns all of them.
func (s *Store) Sessions(ctx context.Context, limit int) ([]SessionRow, error) {
	query := sessionsSQL
	var args []any
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list sessions: %w", err)
	}
	defer rows.Close()

	var out []SessionRow
	for rows.Next() {
		var r SessionRow
		var started int64
		var ended sql.NullInt64
		if err := rows.Scan(&r.ID, &started, &ended, &r.ProtocolVersion, &r.Headline); err != nil {
			return nil, fmt.Errorf("store: list sessions: %w", err)
		}
		r.StartedAt = fromMillis(started)
		if ended.Valid {
			r.EndedAt = fromMillis(ended.Int64)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list sessions: %w", err)
	}
	return out, nil
}

// History returns one product's time series on one island within the closed
// interval [from, to], ordered by time. Raw and aggregated points are mixed;
// Point.Aggregated tells them apart and Point.BucketMs says how wide each
// aggregate is.
//
// An island that is not in the database yields no points and no error: asking
// for the history of something that was never seen is an empty answer, not a
// failure.
func (s *Store) History(ctx context.Context, key model.IslandKey, productGUID int32, from, to time.Time) ([]Point, error) {
	var islandID int64
	err := s.db.QueryRowContext(ctx, islandIDSQL, key.SessionGUID, key.IslandID).Scan(&islandID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: history: look up island (%d,%d): %w",
			key.SessionGUID, key.IslandID, err)
	}

	lo, hi := millis(from), millis(to)
	rows, err := s.db.QueryContext(ctx, historySQL,
		islandID, productGUID, lo, hi,
		islandID, productGUID, lo, hi)
	if err != nil {
		return nil, fmt.Errorf("store: history: %w", err)
	}
	defer rows.Close()

	var out []Point
	for rows.Next() {
		var p Point
		var ts int64
		var aggregated int
		if err := rows.Scan(&ts, &p.Generation, &p.Consumption, &p.Delta,
			&p.PerfectGeneration, &p.PerfectConsumption, &p.Buildings,
			&p.AvgProductivity, &aggregated, &p.BucketMs, &p.GameTS); err != nil {
			return nil, fmt.Errorf("store: history: %w", err)
		}
		p.TS = fromMillis(ts)
		p.Aggregated = aggregated != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: history: %w", err)
	}
	return out, nil
}
