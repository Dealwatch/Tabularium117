package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/store"
)

// base is a fixed instant, so that the assertions below do not depend on when
// the test runs.
var base = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func open(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), store.MemoryPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func key(session, island int32) model.IslandKey {
	return model.IslandKey{SessionGUID: session, IslandID: island}
}

// snap builds a snapshot with one product per guid, values derived from the
// guid so that a mix-up is visible.
func snap(k model.IslandKey, name string, at time.Time, guids ...int32) model.IslandSnapshot {
	s := model.IslandSnapshot{Key: k, Name: name, ReceivedAt: at}
	for _, g := range guids {
		s.Products = append(s.Products, model.ProductStat{
			ProductGUID:        g,
			Generation:         float32(g) + 0.5,
			Consumption:        float32(g) + 0.25,
			Delta:              0.25,
			PerfectGeneration:  float32(g) * 2,
			PerfectConsumption: float32(g) * 3,
			Buildings:          g % 7,
			AvgProductivity:    0.75,
			// Not persisted; present to prove they are ignored, not fatal.
			Workforce:       map[int32]int32{1: 2},
			BuildingsByGUID: map[int32]int32{3: 4},
		})
	}
	return s
}

func TestOpenCreatesAndMigratesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tabularium117.db")
	ctx := context.Background()

	s, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.WriteSnapshots(ctx, []model.IslandSnapshot{snap(key(1, 2), "Roma", base, 100)}); err != nil {
		t.Fatalf("WriteSnapshots: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening must be a no-op: the schema is already at the latest version
	// and the data is still there.
	again, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()

	islands, err := again.Islands(ctx)
	if err != nil {
		t.Fatalf("Islands: %v", err)
	}
	if len(islands) != 1 || islands[0].Name != "Roma" {
		t.Fatalf("islands after reopen = %+v, want the one written before", islands)
	}
}

func TestWriteAndReadBack(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	in := snap(key(3245, 5), "Juliana", base, 1010017, 1010196)
	if err := s.WriteSnapshots(ctx, []model.IslandSnapshot{in}); err != nil {
		t.Fatalf("WriteSnapshots: %v", err)
	}

	islands, err := s.Islands(ctx)
	if err != nil {
		t.Fatalf("Islands: %v", err)
	}
	if len(islands) != 1 {
		t.Fatalf("Islands = %+v, want one row", islands)
	}
	got := islands[0]
	if got.Key != in.Key || got.Name != "Juliana" {
		t.Errorf("island = %+v, want key %+v named Juliana", got, in.Key)
	}
	if !got.FirstSeen.Equal(base) || !got.LastSeen.Equal(base) {
		t.Errorf("first/last seen = %v/%v, want both %v", got.FirstSeen, got.LastSeen, base)
	}

	points, err := s.History(ctx, in.Key, 1010017, base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("History = %+v, want one point", points)
	}
	p, want := points[0], in.Products[0]
	if !p.TS.Equal(base) {
		t.Errorf("point ts = %v, want %v", p.TS, base)
	}
	if p.Generation != want.Generation || p.Consumption != want.Consumption ||
		p.Delta != want.Delta || p.PerfectGeneration != want.PerfectGeneration ||
		p.PerfectConsumption != want.PerfectConsumption || p.Buildings != want.Buildings ||
		p.AvgProductivity != want.AvgProductivity {
		t.Errorf("point = %+v, want the values of %+v", p, want)
	}
	if p.Aggregated {
		t.Error("a raw sample must not be marked aggregated")
	}

	// An unknown island is an empty answer, not an error.
	if pts, err := s.History(ctx, key(99, 99), 1010017, base, base); err != nil || len(pts) != 0 {
		t.Errorf("History of an unknown island = %v, %v; want no points and no error", pts, err)
	}
}

func TestIslandUpsertKeepsFirstSeenAndUpdatesName(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	k := key(7, 5)

	later := base.Add(90 * time.Second)
	if err := s.WriteSnapshots(ctx, []model.IslandSnapshot{snap(k, "Old name", base, 10)}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := s.WriteSnapshots(ctx, []model.IslandSnapshot{snap(k, "New name", later, 10)}); err != nil {
		t.Fatalf("second write: %v", err)
	}

	islands, err := s.Islands(ctx)
	if err != nil {
		t.Fatalf("Islands: %v", err)
	}
	if len(islands) != 1 {
		t.Fatalf("Islands = %+v, want one row; (session_guid, island_id) must be unique", islands)
	}
	got := islands[0]
	if got.Name != "New name" {
		t.Errorf("name = %q, want the latest one", got.Name)
	}
	if !got.FirstSeen.Equal(base) {
		t.Errorf("first_seen = %v, want it unchanged at %v", got.FirstSeen, base)
	}
	if !got.LastSeen.Equal(later) {
		t.Errorf("last_seen = %v, want %v", got.LastSeen, later)
	}

	// An out-of-order snapshot must not drag last_seen backwards.
	if err := s.WriteSnapshots(ctx, []model.IslandSnapshot{snap(k, "New name", base, 10)}); err != nil {
		t.Fatalf("third write: %v", err)
	}
	if islands, err = s.Islands(ctx); err != nil {
		t.Fatal(err)
	} else if !islands[0].LastSeen.Equal(later) {
		t.Errorf("last_seen = %v after an older snapshot, want %v", islands[0].LastSeen, later)
	}
}

func TestSameTimestampDoesNotDuplicate(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	k := key(1, 1)

	first := snap(k, "Isle", base, 42)
	second := snap(k, "Isle", base, 42)
	second.Products[0].Generation = 99
	for _, batch := range [][]model.IslandSnapshot{{first}, {second}} {
		if err := s.WriteSnapshots(ctx, batch); err != nil {
			t.Fatalf("WriteSnapshots: %v", err)
		}
	}

	points, err := s.History(ctx, k, 42, base.Add(-time.Minute), base.Add(time.Minute))
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("History = %+v, want a single point per (island, product, ts)", points)
	}
	if points[0].Generation != 99 {
		t.Errorf("generation = %v, want the later write (99) to win", points[0].Generation)
	}
}

func TestHistoryRangeAndOrder(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	k := key(1, 1)

	// Write in a deliberately jumbled order; the query has to sort.
	for _, offset := range []time.Duration{2 * time.Minute, 0, time.Minute, 3 * time.Minute} {
		if err := s.WriteSnapshots(ctx, []model.IslandSnapshot{snap(k, "Isle", base.Add(offset), 42)}); err != nil {
			t.Fatalf("WriteSnapshots: %v", err)
		}
	}

	points, err := s.History(ctx, k, 42, base.Add(time.Minute), base.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("History = %+v, want the two points inside [from, to]", points)
	}
	if !points[0].TS.Equal(base.Add(time.Minute)) || !points[1].TS.Equal(base.Add(2*time.Minute)) {
		t.Errorf("points = %v/%v, want them ordered by time", points[0].TS, points[1].TS)
	}

	// The bounds are inclusive on both ends.
	if pts, err := s.History(ctx, k, 42, base, base); err != nil {
		t.Fatal(err)
	} else if len(pts) != 1 {
		t.Errorf("History(base, base) = %+v, want the point exactly at base", pts)
	}
}

func TestWriteSnapshotsEmptyBatchAndIslandWithoutProducts(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	if err := s.WriteSnapshots(ctx, nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	// An island the game reports with no products still belongs in the list -
	// the fixture has one ("Zycada").
	if err := s.WriteSnapshots(ctx, []model.IslandSnapshot{snap(key(1, 13), "Zycada ", base)}); err != nil {
		t.Fatalf("WriteSnapshots: %v", err)
	}
	islands, err := s.Islands(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(islands) != 1 || islands[0].Name != "Zycada " {
		t.Fatalf("islands = %+v, want the product-less island, name untrimmed", islands)
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	first, err := s.StartSession(ctx, base, 2, "first")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.EndSession(ctx, first, base.Add(time.Hour)); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	second, err := s.StartSession(ctx, base.Add(2*time.Hour), 2, "second")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if second == first {
		t.Fatal("StartSession returned the same id twice")
	}

	sessions, err := s.Sessions(ctx, 0)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("Sessions = %+v, want two", sessions)
	}
	if sessions[0].Headline != "second" {
		t.Errorf("Sessions[0] = %+v, want the newest session first", sessions[0])
	}
	if !sessions[0].EndedAt.IsZero() {
		t.Errorf("an open session must have a zero EndedAt, got %v", sessions[0].EndedAt)
	}
	if sessions[1].ProtocolVersion != 2 || !sessions[1].EndedAt.Equal(base.Add(time.Hour)) {
		t.Errorf("Sessions[1] = %+v, want version 2 and the recorded end time", sessions[1])
	}

	if limited, err := s.Sessions(ctx, 1); err != nil {
		t.Fatal(err)
	} else if len(limited) != 1 || limited[0].Headline != "second" {
		t.Errorf("Sessions(1) = %+v, want only the newest", limited)
	}
}

func TestCloseOpenSessions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tabularium117.db")

	s, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.StartSession(ctx, base, 2, "crashed"); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	// Closing the handle without EndSession is what a crash looks like.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()

	sessions, err := again.Sessions(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("Sessions = %+v, want one", sessions)
	}
	if sessions[0].EndedAt.IsZero() {
		t.Error("opening the database must close a session left open by a crash")
	}

	// Ending an already closed session is not an error.
	if err := again.EndSession(ctx, sessions[0].ID, base.Add(time.Hour)); err != nil {
		t.Fatalf("EndSession on a closed session: %v", err)
	}
	if after, err := again.Sessions(ctx, 0); err != nil {
		t.Fatal(err)
	} else if !after[0].EndedAt.Equal(sessions[0].EndedAt) {
		t.Error("a late EndSession must not overwrite the recorded end time")
	}
}
