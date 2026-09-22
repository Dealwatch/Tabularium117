package store

import (
	"context"
	"database/sql"
	"fmt"
)

// migrations are applied in order; the index plus one is the schema version
// recorded in PRAGMA user_version. Never edit a released migration - append a
// new one, because an existing database only ever runs the ones it is missing.
//
// Migration 2 adds the alert table that KONZEPT.md section 4 held back in
// its first version: its columns follow the rule engine of internal/alerts
// rather than a guess made before the engine existed.
//
// Migration 3 is the consequence of the live capture of 2026-09-22: the game
// delivers one statistics tick every two minutes, so one-minute means were
// never means of anything. It widens the aggregate to ten minutes (renaming
// the table with it) and records the game's own per-tick timestamp.
var migrations = []func(ctx context.Context, tx *sql.Tx) error{
	migrate1,
	migrate2,
	migrate3,
}

// migrate brings the schema up to len(migrations), applying each missing
// migration in its own transaction. Reopening an up-to-date database does
// nothing.
func (s *Store) migrate(ctx context.Context) error {
	if err := s.acquireWrite(ctx); err != nil {
		return err
	}
	defer s.releaseWrite()

	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("store: %s has schema version %d, this build knows %d; "+
			"it was written by a newer Tabularium 117", s.path, version, len(migrations))
	}

	for i := version; i < len(migrations); i++ {
		next := i + 1
		err := s.writeLocked(ctx, func(tx *sql.Tx) error {
			if err := migrations[i](ctx, tx); err != nil {
				return fmt.Errorf("store: migration %d: %w", next, err)
			}
			// PRAGMA user_version takes no placeholder, and next is an int
			// from this file, not from input.
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, next)); err != nil {
				return fmt.Errorf("store: migration %d: record version: %w", next, err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// schema1 is the initial schema, following KONZEPT.md section 4.
//
// Every timestamp is Unix milliseconds on the receiver's clock. sample and
// sample_minute are WITHOUT ROWID: their primary key is the whole row's
// identity, so a rowid would only add a second index over the same columns.
const schema1 = `
CREATE TABLE game_session (
    id               INTEGER PRIMARY KEY,
    started_at       INTEGER NOT NULL,
    ended_at         INTEGER,
    protocol_version INTEGER NOT NULL,
    headline         TEXT    NOT NULL
);

CREATE TABLE island (
    id           INTEGER PRIMARY KEY,
    session_guid INTEGER NOT NULL,
    island_id    INTEGER NOT NULL,
    name         TEXT    NOT NULL,
    first_seen   INTEGER NOT NULL,
    last_seen    INTEGER NOT NULL,
    UNIQUE (session_guid, island_id)
);

CREATE TABLE sample (
    island             INTEGER NOT NULL REFERENCES island(id),
    product_guid       INTEGER NOT NULL,
    ts                 INTEGER NOT NULL,
    generation         REAL    NOT NULL,
    consumption        REAL    NOT NULL,
    delta              REAL    NOT NULL,
    perfect_generation REAL    NOT NULL,
    perfect_consumption REAL   NOT NULL,
    buildings          INTEGER NOT NULL,
    avg_productivity   REAL    NOT NULL,
    PRIMARY KEY (island, product_guid, ts)
) WITHOUT ROWID;

CREATE TABLE sample_minute (
    island             INTEGER NOT NULL REFERENCES island(id),
    product_guid       INTEGER NOT NULL,
    ts                 INTEGER NOT NULL,
    generation         REAL    NOT NULL,
    consumption        REAL    NOT NULL,
    delta              REAL    NOT NULL,
    perfect_generation REAL    NOT NULL,
    perfect_consumption REAL   NOT NULL,
    buildings          INTEGER NOT NULL,
    avg_productivity   REAL    NOT NULL,
    sample_count       INTEGER NOT NULL,
    PRIMARY KEY (island, product_guid, ts)
) WITHOUT ROWID;
`

// migrate1 creates the version 1 schema.
//
// No extra index is created. The history query selects one island and one
// product over a time range, which is the leading edge of both primary keys,
// and compaction and cleanup scan by ts across the whole table, where an index
// would cost more to maintain than it saves.
func migrate1(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, schema1)
	return err
}

// schema2 adds the alert table.
//
// One row per raised alert. cleared_at is NULL while the alert is open,
// which makes "the open alert for this island, product and rule" a single
// row and gives the index below something selective to work with. The
// numeric value that triggered the rule is deliberately not a column: it
// changes with every sample, and what the history is for is when a warning
// started and when it ended. detail keeps the numbers it started with.
const schema2 = `
CREATE TABLE alert (
    id           INTEGER PRIMARY KEY,
    island       INTEGER NOT NULL REFERENCES island(id),
    product_guid INTEGER NOT NULL,
    rule         TEXT    NOT NULL,
    severity     TEXT    NOT NULL,
    raised_at    INTEGER NOT NULL,
    cleared_at   INTEGER,
    detail       TEXT    NOT NULL
);

CREATE INDEX alert_open ON alert (island, cleared_at);
`

// migrate2 creates the alert table on top of the version 1 schema.
func migrate2(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, schema2)
	return err
}

// schema3 adapts the history to the real tick cadence (docs/protocol.md,
// "Live capture 2026-09-22"; KONZEPT.md section 4).
//
// Three things change.
//
// The aggregate table is no longer a minute table. At one tick every two
// minutes a "one-minute mean" was the mean of one sample or of none, so the
// resolution bought nothing and the name was a lie. It becomes sample_bucket
// with an explicit bucket_ms, because the width is now a policy
// (CompactOptions.BucketWidth) rather than a constant, and a row has to say
// how wide it is: rows written by an older build really are minute means,
// and the history query renders every row by its own width instead of
// assuming today's. That is why the column is added with the new default and
// then set to 60000 for the rows that already exist - they are the old ones.
//
// game_ts is the pipe's timeStamp, stored as delivered. It is the game clock
// in milliseconds and identical for every island of one tick, which makes it
// the tick's id; it stops while the game is paused, so it is a grouping key,
// never a time axis. The aggregate keeps the largest one it folded in.
//
// Both columns are added with a default, so the rows already in the database
// stay valid and no table has to be rewritten.
const schema3 = `
ALTER TABLE sample ADD COLUMN game_ts INTEGER NOT NULL DEFAULT 0;

ALTER TABLE sample_minute RENAME TO sample_bucket;
ALTER TABLE sample_bucket ADD COLUMN bucket_ms INTEGER NOT NULL DEFAULT 600000;
UPDATE sample_bucket SET bucket_ms = 60000;
ALTER TABLE sample_bucket ADD COLUMN game_ts_max INTEGER NOT NULL DEFAULT 0;
`

// migrate3 widens the aggregate and records the tick id.
func migrate3(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, schema3)
	return err
}
