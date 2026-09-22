package store

import (
	"context"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// The compaction fixture: one island, two products, one sample every two
// minutes - the game's real tick cadence (docs/protocol.md, "Live capture
// 2026-09-22") - over eight days ending at now, so that a day of it is past
// the seven-day raw retention. Values are chosen so that every ten-minute
// bucket has a mean that can be written down exactly:
//
//	generation       = i mod 5            -> bucket mean 2.0
//	consumption      = bucket index       -> bucket mean = the bucket index
//	buildings        = i mod 5            -> bucket max 4
//	avg_productivity = 0.5                -> bucket mean 0.5
//	game_ts          = i * 120000         -> bucket max = the last tick's
const (
	fixtureSpan      = 8 * 24 * time.Hour
	fixtureStep      = 2 * time.Minute
	fixtureBucket    = 10 * time.Minute
	samplesPerBucket = int(fixtureBucket / fixtureStep) // 5
	fixtureProductA  = int32(1010017)
	fixtureProductB  = int32(1010196)
)

// writeFixture fills s and returns the island key. now is bucket-aligned, so
// the buckets line up with the samples.
func writeFixture(t *testing.T, s *Store, now time.Time) model.IslandKey {
	t.Helper()
	k := model.IslandKey{SessionGUID: 3245, IslandID: 5}
	start := now.Add(-fixtureSpan)

	var batch []model.IslandSnapshot
	for i := 0; ; i++ {
		ts := start.Add(time.Duration(i) * fixtureStep)
		if ts.After(now) {
			break
		}
		bucket := float32(i / samplesPerBucket)
		value := float32(i % samplesPerBucket)
		products := make([]model.ProductStat, 0, 2)
		for _, guid := range []int32{fixtureProductA, fixtureProductB} {
			products = append(products, model.ProductStat{
				ProductGUID:        guid,
				Generation:         value,
				Consumption:        bucket,
				Delta:              value - bucket,
				PerfectGeneration:  10,
				PerfectConsumption: 20,
				Buildings:          int32(i % samplesPerBucket),
				AvgProductivity:    0.5,
			})
		}
		batch = append(batch, model.IslandSnapshot{
			Key: k, Name: "Juliana", ReceivedAt: ts, Products: products,
			// The tick id advances with the tick, like the game clock does.
			GameTimestamp: int64(i) * fixtureStep.Milliseconds(),
		})
	}
	if err := s.WriteSnapshots(context.Background(), batch); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return k
}

// count runs a scalar query.
func count(t *testing.T, s *Store, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := s.db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestCompact(t *testing.T) {
	ctx := context.Background()
	// A ten-minute-aligned "now" keeps the bucket boundaries predictable.
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	s, err := Open(ctx, MemoryPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	k := writeFixture(t, s, now)
	opts := DefaultCompactOptions()
	rawCutoff := now.Add(-opts.RawRetention)

	const products = 2
	// Eight days of samples, the last one exactly at now.
	totalPerProduct := int64(fixtureSpan/fixtureStep) + 1
	// One day is older than the seven-day raw retention.
	agedPerProduct := int64(24 * time.Hour / fixtureStep)
	bucketsPerProduct := agedPerProduct / int64(samplesPerBucket)

	if got := count(t, s, `SELECT count(*) FROM sample`); got != totalPerProduct*products {
		t.Fatalf("fixture wrote %d raw rows, want %d", got, totalPerProduct*products)
	}

	stats, err := s.Compact(ctx, now, opts)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if stats.BucketRows != bucketsPerProduct*products {
		t.Errorf("BucketRows = %d, want %d", stats.BucketRows, bucketsPerProduct*products)
	}
	if stats.RawDeleted != agedPerProduct*products {
		t.Errorf("RawDeleted = %d, want %d", stats.RawDeleted, agedPerProduct*products)
	}
	if stats.BucketDeleted != 0 {
		t.Errorf("BucketDeleted = %d, want 0; nothing is 90 days old yet", stats.BucketDeleted)
	}

	// Raw rows survive only inside the retention window.
	if got := count(t, s, `SELECT count(*) FROM sample WHERE ts < ?`, millis(rawCutoff)); got != 0 {
		t.Errorf("%d raw rows older than the retention window survived", got)
	}
	if got, want := count(t, s, `SELECT count(*) FROM sample`), (totalPerProduct-agedPerProduct)*products; got != want {
		t.Errorf("raw rows left = %d, want %d", got, want)
	}
	// Every bucket holds exactly the five ticks of its ten minutes, and says
	// how wide it is.
	if got := count(t, s, `SELECT count(*) FROM sample_bucket WHERE sample_count <> ?`, samplesPerBucket); got != 0 {
		t.Errorf("%d bucket rows do not have sample_count %d", got, samplesPerBucket)
	}
	if got := count(t, s, `SELECT count(*) FROM sample_bucket WHERE bucket_ms <> ?`,
		fixtureBucket.Milliseconds()); got != 0 {
		t.Errorf("%d bucket rows do not record the 10-minute width", got)
	}

	// The stored means are the means of the five ticks, and the tick id is
	// the newest one the bucket covers (i = 4 -> 4 * 120000 ms).
	var gen, cons, delta, prod float64
	var buildings, gameTS int64
	row := s.db.QueryRowContext(ctx,
		`SELECT generation, consumption, delta, avg_productivity, buildings, game_ts_max
		 FROM sample_bucket WHERE product_guid = ? ORDER BY ts LIMIT 1`, fixtureProductA)
	if err := row.Scan(&gen, &cons, &delta, &prod, &buildings, &gameTS); err != nil {
		t.Fatalf("read the first bucket row: %v", err)
	}
	if gen != 2 || cons != 0 || delta != 2 || prod != 0.5 || buildings != 4 {
		t.Errorf("first bucket row = gen %v, cons %v, delta %v, productivity %v, buildings %d; "+
			"want 2, 0, 2, 0.5 and the maximum building count 4", gen, cons, delta, prod, buildings)
	}
	if want := int64(samplesPerBucket-1) * fixtureStep.Milliseconds(); gameTS != want {
		t.Errorf("first bucket game_ts_max = %d, want %d", gameTS, want)
	}

	// History mixes both resolutions and marks the aggregated points.
	points, err := s.History(ctx, k, fixtureProductA, now.Add(-fixtureSpan), now)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	wantPoints := int(bucketsPerProduct + totalPerProduct - agedPerProduct)
	if len(points) != wantPoints {
		t.Fatalf("History returned %d points, want %d", len(points), wantPoints)
	}
	for i, p := range points {
		if i > 0 && p.TS.Before(points[i-1].TS) {
			t.Fatalf("history is not ordered by time at index %d", i)
		}
		if want := p.TS.Before(rawCutoff); p.Aggregated != want {
			t.Fatalf("point %d at %v: Aggregated = %v, want %v", i, p.TS, p.Aggregated, want)
		}
		// Every point says how wide it is, so the chart can mix the two.
		wantWidth := int64(0)
		if p.Aggregated {
			wantWidth = fixtureBucket.Milliseconds()
		}
		if p.BucketMs != wantWidth {
			t.Fatalf("point %d at %v: BucketMs = %d, want %d", i, p.TS, p.BucketMs, wantWidth)
		}
		if p.GameTS == 0 {
			t.Fatalf("point %d at %v lost the tick id", i, p.TS)
		}
	}

	// Running again changes nothing: compaction is idempotent.
	if again, err := s.Compact(ctx, now, opts); err != nil {
		t.Fatalf("second Compact: %v", err)
	} else if !again.Empty() {
		t.Errorf("second Compact = %+v, want nothing to do", again)
	}

	// Past the bucket retention everything goes.
	if _, err := s.Compact(ctx, now.Add(91*24*time.Hour), opts); err != nil {
		t.Fatalf("third Compact: %v", err)
	}
	if got := count(t, s, `SELECT count(*) FROM sample_bucket`); got != 0 {
		t.Errorf("%d bucket rows survived the 90-day retention", got)
	}
	if got := count(t, s, `SELECT count(*) FROM sample`); got != 0 {
		t.Errorf("%d raw rows survived, all of them older than the raw retention", got)
	}
	// The island itself stays: it is identity, not history.
	if got := count(t, s, `SELECT count(*) FROM island`); got != 1 {
		t.Errorf("island rows = %d, want the island to survive cleanup", got)
	}
}

// A late sample for an already compacted bucket must be folded into the
// existing row, not thrown away and not allowed to replace it.
func TestCompactMergesIntoAnExistingBucket(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	bucket := now.Add(-8 * 24 * time.Hour).Truncate(10 * time.Minute)

	s, err := Open(ctx, MemoryPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	k := model.IslandKey{SessionGUID: 1, IslandID: 1}
	write := func(at time.Time, generation float32, buildings int32) {
		t.Helper()
		err := s.WriteSnapshots(ctx, []model.IslandSnapshot{{
			Key: k, Name: "Isle", ReceivedAt: at,
			Products: []model.ProductStat{{ProductGUID: 7, Generation: generation, Buildings: buildings}},
		}})
		if err != nil {
			t.Fatalf("WriteSnapshots: %v", err)
		}
	}

	write(bucket, 10, 3)
	if _, err := s.Compact(ctx, now, DefaultCompactOptions()); err != nil {
		t.Fatalf("first Compact: %v", err)
	}
	// A second reading lands in the same bucket after it was compacted.
	write(bucket.Add(2*time.Minute), 20, 1)
	if _, err := s.Compact(ctx, now, DefaultCompactOptions()); err != nil {
		t.Fatalf("second Compact: %v", err)
	}

	var gen float64
	var buildings, samples int64
	err = s.db.QueryRowContext(ctx,
		`SELECT generation, buildings, sample_count FROM sample_bucket`).Scan(&gen, &buildings, &samples)
	if err != nil {
		t.Fatalf("read the merged bucket: %v", err)
	}
	if samples != 2 || gen != 15 || buildings != 3 {
		t.Errorf("merged bucket = gen %v, buildings %d, count %d; want 15, 3 and 2", gen, buildings, samples)
	}
}

// RunCompactor has to do one pass immediately, not only after the first tick,
// and stop when the context ends.
func TestRunCompactorCompactsAtStartAndStops(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	s, err := Open(ctx, MemoryPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	k := model.IslandKey{SessionGUID: 1, IslandID: 1}
	// One reading older than the raw retention and one at the end of the
	// series, which is what anchors the retention window.
	for _, at := range []time.Time{now.Add(-8 * 24 * time.Hour), now} {
		err := s.WriteSnapshots(ctx, []model.IslandSnapshot{{
			Key: k, Name: "Isle", ReceivedAt: at,
			Products: []model.ProductStat{{ProductGUID: 7, Generation: 1}},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		s.RunCompactor(runCtx, time.Hour, DefaultCompactOptions(), nil)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for count(t, s, `SELECT count(*) FROM sample_bucket`) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("RunCompactor did not compact on start")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunCompactor did not return after its context was cancelled")
	}
}

// Retention is measured against the data, not against the wall clock.
//
// A recording replayed long after it was made carries its original receive
// times. Measuring those against time.Now() would fold the whole recording
// into buckets at the first pass and delete it outright once it was nominally
// 90 days old - the replay would write a history and then erase it.
func TestRunCompactorDoesNotAgeReplayedData(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, MemoryPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// A recording made 100 days ago, spanning three ticks, replayed today.
	recorded := time.Now().Add(-100 * 24 * time.Hour)
	k := model.IslandKey{SessionGUID: 3245, IslandID: 5}
	var batch []model.IslandSnapshot
	for i := range 3 {
		batch = append(batch, model.IslandSnapshot{
			Key: k, Name: "Juliana", ReceivedAt: recorded.Add(time.Duration(i) * 2 * time.Minute),
			Products: []model.ProductStat{{ProductGUID: 7, Generation: float32(i)}},
		})
	}
	if err := s.WriteSnapshots(ctx, batch); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		s.RunCompactor(runCtx, time.Hour, DefaultCompactOptions(), nil)
		close(done)
	}()
	// The pass at start is synchronous, but give it room on a loaded machine.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunCompactor did not return")
	}

	if got := count(t, s, `SELECT count(*) FROM sample`); got != 3 {
		t.Errorf("%d raw rows left, want all 3: a replayed recording must not be aged by the wall clock", got)
	}
	if got := count(t, s, `SELECT count(*) FROM sample_bucket`); got != 0 {
		t.Errorf("%d bucket rows were produced, want none", got)
	}
}

// With nothing written there is no anchor, so there is nothing that could have
// aged - and an existing database must not be compacted against a guess.
func TestRunCompactorSkipsAnEmptyStore(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, MemoryPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if !s.LastSampleTime().IsZero() {
		t.Fatalf("LastSampleTime = %v on a fresh store, want the zero time", s.LastSampleTime())
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		s.RunCompactor(runCtx, time.Hour, DefaultCompactOptions(), nil)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunCompactor did not return")
	}
}

// LastSampleTime is the retention anchor, so it has to track the newest
// receive time written and never move backwards.
func TestLastSampleTimeIsTheNewestWrite(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, MemoryPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	k := model.IslandKey{SessionGUID: 1, IslandID: 1}
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	write := func(when time.Time) {
		t.Helper()
		err := s.WriteSnapshots(ctx, []model.IslandSnapshot{{
			Key: k, Name: "Isle", ReceivedAt: when,
			Products: []model.ProductStat{{ProductGUID: 7}},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}

	write(at)
	if got := s.LastSampleTime(); !got.Equal(at) {
		t.Fatalf("LastSampleTime = %v, want %v", got, at)
	}
	// An out-of-order snapshot must not pull the anchor back.
	write(at.Add(-time.Hour))
	if got := s.LastSampleTime(); !got.Equal(at) {
		t.Errorf("LastSampleTime = %v after an older write, want %v", got, at)
	}
	write(at.Add(time.Hour))
	if got := s.LastSampleTime(); !got.Equal(at.Add(time.Hour)) {
		t.Errorf("LastSampleTime = %v, want it to follow the newest write", got)
	}
}
