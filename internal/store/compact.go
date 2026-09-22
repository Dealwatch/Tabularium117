package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// CompactOptions configures how long each resolution is kept
// (KONZEPT.md section 4, "Datensparsamkeit").
//
// The numbers follow the tick cadence the live capture of 2026-09-22 measured:
// one statistics tick every two minutes, so one island and one product produce
// about 720 rows a day, not 8640. Keeping a week of that is a few megabytes,
// which is what makes the long history ranges (24 h, 7 d) worth offering at
// full resolution.
type CompactOptions struct {
	// RawRetention is how long full-resolution samples are kept. Older ones
	// are folded into buckets of BucketWidth.
	RawRetention time.Duration
	// BucketWidth is the width of one aggregate row. It must be a multiple
	// of the tick interval to be a mean of anything: at ten minutes a bucket
	// holds about five ticks.
	BucketWidth time.Duration
	// BucketRetention is how long the buckets are kept. Older ones are
	// deleted.
	BucketRetention time.Duration
}

// DefaultCompactOptions is the policy for a two-minute tick: 7 days at full
// resolution, then 10-minute means, deleted after 90 days.
func DefaultCompactOptions() CompactOptions {
	return CompactOptions{
		RawRetention:    7 * 24 * time.Hour,
		BucketWidth:     10 * time.Minute,
		BucketRetention: 90 * 24 * time.Hour,
	}
}

// CompactStats reports what one Compact run changed. All zero means there was
// nothing to do.
type CompactStats struct {
	// BucketRows is the number of sample_bucket rows written or merged.
	BucketRows int64
	// RawDeleted is the number of raw sample rows folded away.
	RawDeleted int64
	// BucketDeleted is the number of expired sample_bucket rows removed.
	BucketDeleted int64
}

// Empty reports whether the run changed nothing.
func (c CompactStats) Empty() bool {
	return c.BucketRows == 0 && c.RawDeleted == 0 && c.BucketDeleted == 0
}

// SQL used when compacting.
const (
	// aggregateSQL folds every raw sample older than the cutoff into its
	// bucket. ?1 is the bucket width in milliseconds and ?2 the cutoff.
	//
	// Means for the rates, MAX for the building count: averaging a count of
	// buildings over a window in which one was finished would report a
	// fraction of a building, while the maximum answers the question the
	// column is there for ("how many stood on this island?"). game_ts is the
	// game's tick id, so the bucket keeps the newest one it covers rather
	// than a meaningless mean of clock readings.
	//
	// A conflict means the same bucket was compacted before - possible when a
	// sample for an already compacted window arrives late. Merging by sample
	// count keeps the stored value the mean of everything that ever went into
	// it, instead of letting the last run overwrite the earlier one. bucket_ms
	// takes the wider of the two, which only differs for a row written by a
	// build with another width (schema 2 wrote minute means).
	aggregateSQL = `INSERT INTO sample_bucket (
			island, product_guid, ts,
			generation, consumption, delta,
			perfect_generation, perfect_consumption,
			buildings, avg_productivity, sample_count,
			bucket_ms, game_ts_max)
		SELECT island, product_guid, (ts / ?1) * ?1,
			avg(generation), avg(consumption), avg(delta),
			avg(perfect_generation), avg(perfect_consumption),
			max(buildings), avg(avg_productivity), count(*),
			?1, max(game_ts)
		FROM sample
		WHERE ts < ?2
		GROUP BY island, product_guid, (ts / ?1) * ?1
		ON CONFLICT (island, product_guid, ts) DO UPDATE SET
			generation = (sample_bucket.generation * sample_bucket.sample_count
				+ excluded.generation * excluded.sample_count)
				/ (sample_bucket.sample_count + excluded.sample_count),
			consumption = (sample_bucket.consumption * sample_bucket.sample_count
				+ excluded.consumption * excluded.sample_count)
				/ (sample_bucket.sample_count + excluded.sample_count),
			delta = (sample_bucket.delta * sample_bucket.sample_count
				+ excluded.delta * excluded.sample_count)
				/ (sample_bucket.sample_count + excluded.sample_count),
			perfect_generation = (sample_bucket.perfect_generation * sample_bucket.sample_count
				+ excluded.perfect_generation * excluded.sample_count)
				/ (sample_bucket.sample_count + excluded.sample_count),
			perfect_consumption = (sample_bucket.perfect_consumption * sample_bucket.sample_count
				+ excluded.perfect_consumption * excluded.sample_count)
				/ (sample_bucket.sample_count + excluded.sample_count),
			buildings = max(sample_bucket.buildings, excluded.buildings),
			avg_productivity = (sample_bucket.avg_productivity * sample_bucket.sample_count
				+ excluded.avg_productivity * excluded.sample_count)
				/ (sample_bucket.sample_count + excluded.sample_count),
			sample_count = sample_bucket.sample_count + excluded.sample_count,
			bucket_ms = max(sample_bucket.bucket_ms, excluded.bucket_ms),
			game_ts_max = max(sample_bucket.game_ts_max, excluded.game_ts_max)`

	deleteRawSQL = `DELETE FROM sample WHERE ts < ?`

	deleteBucketSQL = `DELETE FROM sample_bucket WHERE ts < ?`
)

// Compact enforces the retention policy as of now: raw samples older than
// RawRetention become means of BucketWidth, and bucket rows older than
// BucketRetention are deleted.
//
// Aggregation and deletion share one transaction, so a failure cannot leave
// samples counted twice or dropped without being aggregated first.
func (s *Store) Compact(ctx context.Context, now time.Time, opts CompactOptions) (CompactStats, error) {
	if opts.RawRetention <= 0 {
		opts.RawRetention = DefaultCompactOptions().RawRetention
	}
	if opts.BucketWidth <= 0 {
		opts.BucketWidth = DefaultCompactOptions().BucketWidth
	}
	if opts.BucketRetention <= 0 {
		opts.BucketRetention = DefaultCompactOptions().BucketRetention
	}

	rawCutoff := millis(now.Add(-opts.RawRetention))
	bucketCutoff := millis(now.Add(-opts.BucketRetention))
	bucketMillis := opts.BucketWidth.Milliseconds()

	var stats CompactStats
	err := s.write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, aggregateSQL, bucketMillis, rawCutoff)
		if err != nil {
			return fmt.Errorf("store: compact: aggregate: %w", err)
		}
		if stats.BucketRows, err = res.RowsAffected(); err != nil {
			return fmt.Errorf("store: compact: aggregate: %w", err)
		}

		if res, err = tx.ExecContext(ctx, deleteRawSQL, rawCutoff); err != nil {
			return fmt.Errorf("store: compact: delete raw samples: %w", err)
		}
		if stats.RawDeleted, err = res.RowsAffected(); err != nil {
			return fmt.Errorf("store: compact: delete raw samples: %w", err)
		}

		if res, err = tx.ExecContext(ctx, deleteBucketSQL, bucketCutoff); err != nil {
			return fmt.Errorf("store: compact: delete aggregated samples: %w", err)
		}
		if stats.BucketDeleted, err = res.RowsAffected(); err != nil {
			return fmt.Errorf("store: compact: delete aggregated samples: %w", err)
		}
		return nil
	})
	if err != nil {
		return CompactStats{}, err
	}

	// Deleting a day of samples leaves that much in the write-ahead log.
	// Checkpointing gives the space back to the file instead of letting the
	// WAL grow without bound. Best effort: the data is committed either way.
	if !stats.Empty() && !isMemory(s.path) {
		s.checkpoint(ctx)
	}
	return stats, nil
}

// checkpoint truncates the write-ahead log. Failures are ignored on purpose;
// a checkpoint is housekeeping, not correctness.
func (s *Store) checkpoint(ctx context.Context) {
	if err := s.acquireWrite(ctx); err != nil {
		return
	}
	defer s.releaseWrite()
	row := s.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	var busy, log, checkpointed int
	_ = row.Scan(&busy, &log, &checkpointed)
}

// DefaultCompactInterval is how often RunCompactor enforces the retention
// policy. The thresholds are days, so ten minutes is ample.
const DefaultCompactInterval = 10 * time.Minute

// RunCompactor compacts once immediately and then every interval, until ctx is
// done. It never returns an error: a failed run is logged and retried at the
// next tick, because losing the retention policy for one interval is not worth
// stopping the program for.
//
// Retention is measured against the newest sample the store has written, not
// against the wall clock. Replaying a recording from last month must produce a
// history of last month, not an empty database, and "seven days of full
// resolution" means seven days of the data's own time either way. A store that
// has not been written to yet is left alone entirely: there is no anchor, and
// nothing that could have aged.
func (s *Store) RunCompactor(ctx context.Context, interval time.Duration, opts CompactOptions, log *slog.Logger) {
	log = logOrDiscard(log)
	if interval <= 0 {
		interval = DefaultCompactInterval
	}

	run := func() {
		anchor := s.LastSampleTime()
		if anchor.IsZero() {
			return
		}
		stats, err := s.Compact(ctx, anchor, opts)
		switch {
		case err != nil && ctx.Err() != nil:
			// Shutdown cancelled the run; that is not a failure.
		case err != nil:
			log.Error("cannot compact the history database", "err", err)
		case !stats.Empty():
			log.Info("compacted the history database",
				"anchor", anchor,
				"bucket_rows", stats.BucketRows,
				"raw_deleted", stats.RawDeleted,
				"bucket_deleted", stats.BucketDeleted)
		}
	}

	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
