package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// The schema version is what tells a future build which migrations to run, so
// it has to be written on a fresh database and left alone on an existing one.
func TestUserVersionIsRecordedOnceAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tabularium117.db")

	for _, round := range []string{"create", "reopen"} {
		s, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("%s: Open: %v", round, err)
		}
		var version int
		if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
			t.Fatalf("%s: read user_version: %v", round, err)
		}
		if version != len(migrations) {
			t.Errorf("%s: user_version = %d, want %d", round, version, len(migrations))
		}
		s.Close()
	}
}

// A database from a newer Tabularium 117 must be refused, not silently used with
// tables this build does not understand.
func TestFutureSchemaVersionIsRefused(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tabularium117.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA user_version = 99`); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	s.Close()

	if _, err := Open(ctx, path); err == nil {
		t.Fatal("opening a database with a newer schema version must fail")
	}
}

// Every connection has to carry the pragmas, not just the first one, because
// database/sql opens them lazily.
func TestPragmasAreAppliedToEveryConnection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tabularium117.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Hold one connection open so the next query has to use a second one.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	for _, c := range []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
		{"synchronous", "1"}, // NORMAL
	} {
		var got string
		if err := s.db.QueryRowContext(ctx, `PRAGMA `+c.pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", c.pragma, err)
		}
		if got != c.want {
			t.Errorf("PRAGMA %s = %q, want %q", c.pragma, got, c.want)
		}
	}
}

// A database written by the phase 3 build has to be upgraded in place, with
// its data intact. The version 1 database is built by running migration 1
// alone, which is exactly what that build shipped.
func TestSchemaOneIsUpgradedToLatest(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tabularium117.db")

	// --- build a version 1 database ---
	old, err := openRaw(ctx, path)
	if err != nil {
		t.Fatalf("open the raw database: %v", err)
	}
	err = old.write(ctx, func(tx *sql.Tx) error {
		if err := migrate1(ctx, tx); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `PRAGMA user_version = 1;
			INSERT INTO island (session_guid, island_id, name, first_seen, last_seen)
			VALUES (3245, 5, 'Juliana', 1000, 2000);`)
		return err
	})
	if err != nil {
		t.Fatalf("create the version 1 database: %v", err)
	}
	if _, err := old.db.ExecContext(ctx, `SELECT 1 FROM alert`); err == nil {
		t.Fatal("the version 1 schema must not have an alert table")
	}
	old.Close()

	// --- open it with this build ---
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open the version 1 database: %v", err)
	}
	defer s.Close()

	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != len(migrations) {
		t.Errorf("user_version = %d, want %d", version, len(migrations))
	}

	// The island survived the upgrade and the new table is usable against it.
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM island WHERE id = 1`).Scan(&name); err != nil {
		t.Fatalf("the island did not survive the migration: %v", err)
	}
	if name != "Juliana" {
		t.Errorf("island name = %q, want Juliana", name)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO alert (island, product_guid, rule, severity, raised_at, detail)
		 VALUES (1, 2068, 'deficit', 'warning', 3000, 'delta -1.0 for 3 samples')`); err != nil {
		t.Fatalf("the upgraded database does not accept alerts: %v", err)
	}

	// The index migration 2 promises has to be there: without it, "the open
	// alert for this island" is a table scan on every clear.
	var index string
	if err := s.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'alert'`).Scan(&index); err != nil {
		t.Fatalf("the alert table has no index: %v", err)
	}

	// Migration 3 renamed the aggregate table and added the tick id, so a
	// version 1 database has to come out with the current shape.
	if _, err := s.db.ExecContext(ctx, `SELECT 1 FROM sample_minute`); err == nil {
		t.Error("sample_minute still exists; migration 3 renames it to sample_bucket")
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sample (island, product_guid, ts, generation, consumption, delta,
			perfect_generation, perfect_consumption, buildings, avg_productivity, game_ts)
		 VALUES (1, 2068, 4000, 1, 0, 1, 2, 0, 3, 100, 150000000)`); err != nil {
		t.Fatalf("the upgraded database does not accept a sample with a tick id: %v", err)
	}
}

// A database written by the phase 6 build (schema 2) holds real one-minute
// means. Migration 3 widens the policy to ten minutes, and those existing
// rows must keep saying they are one minute wide - they are means of a
// minute, and the history renders every row by its own width. Getting this
// wrong would stretch old points across ten times their real span.
func TestSchemaTwoKeepsItsMinuteMeans(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tabularium117.db")

	old, err := openRaw(ctx, path)
	if err != nil {
		t.Fatalf("open the raw database: %v", err)
	}
	err = old.write(ctx, func(tx *sql.Tx) error {
		if err := migrate1(ctx, tx); err != nil {
			return err
		}
		if err := migrate2(ctx, tx); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `PRAGMA user_version = 2;
			INSERT INTO island (session_guid, island_id, name, first_seen, last_seen)
			VALUES (3245, 5, 'Juliana', 1000, 2000);
			INSERT INTO sample_minute (island, product_guid, ts, generation, consumption,
				delta, perfect_generation, perfect_consumption, buildings,
				avg_productivity, sample_count)
			VALUES (1, 2068, 60000, 5, 4, 1, 10, 4, 2, 100, 6);`)
		return err
	})
	if err != nil {
		t.Fatalf("create the version 2 database: %v", err)
	}
	old.Close()

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open the version 2 database: %v", err)
	}
	defer s.Close()

	var bucketMS, gameTS, samples int64
	var generation float64
	err = s.db.QueryRowContext(ctx,
		`SELECT bucket_ms, game_ts_max, sample_count, generation FROM sample_bucket`).
		Scan(&bucketMS, &gameTS, &samples, &generation)
	if err != nil {
		t.Fatalf("the minute mean did not survive the rename: %v", err)
	}
	if bucketMS != 60_000 {
		t.Errorf("bucket_ms = %d for a row written as a minute mean, want 60000", bucketMS)
	}
	if gameTS != 0 || samples != 6 || generation != 5 {
		t.Errorf("migrated row = game_ts_max %d, sample_count %d, generation %v; want 0, 6, 5",
			gameTS, samples, generation)
	}

	// A fresh bucket written by this build records the current width, so the
	// two coexist in one table.
	points, err := s.History(ctx, model.IslandKey{SessionGUID: 3245, IslandID: 5}, 2068,
		fromMillis(0), fromMillis(120_000))
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(points) != 1 || !points[0].Aggregated || points[0].BucketMs != 60_000 {
		t.Errorf("History = %+v, want one aggregated point 60000 ms wide", points)
	}
}

// openRaw opens the database without migrating it, so that a test can build
// an older schema by hand.
func openRaw(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open(driverName, dsn(path))
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path, writeLock: make(chan struct{}, 1)}, nil
}
