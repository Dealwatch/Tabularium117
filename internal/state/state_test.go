package state_test

import (
	"sync"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

func snap(sessionGUID, islandID int32, name string, products int) model.IslandSnapshot {
	s := model.IslandSnapshot{
		Key:  model.IslandKey{SessionGUID: sessionGUID, IslandID: islandID},
		Name: name,
	}
	for i := 0; i < products; i++ {
		s.Products = append(s.Products, model.ProductStat{ProductGUID: int32(i)})
	}
	return s
}

func TestPutReplacesAndIslandsIsOrdered(t *testing.T) {
	st := state.New()
	st.Put(snap(6627, 5, "b", 1))
	st.Put(snap(3245, 11, "a", 2))
	st.Put(snap(3245, 5, "first", 3))
	// Same key again: the newer snapshot replaces the older one.
	st.Put(snap(3245, 5, "second", 4))

	islands := st.Islands()
	if len(islands) != 3 {
		t.Fatalf("got %d islands, want 3 (the island key is (SessionGUID, IslandID))", len(islands))
	}
	want := []model.IslandKey{
		{SessionGUID: 3245, IslandID: 5},
		{SessionGUID: 3245, IslandID: 11},
		{SessionGUID: 6627, IslandID: 5},
	}
	for i, key := range want {
		if islands[i].Key != key {
			t.Fatalf("islands[%d].Key = %v, want %v", i, islands[i].Key, key)
		}
	}
	if islands[0].Name != "second" || len(islands[0].Products) != 4 {
		t.Errorf("island (3245,5) = %q with %d products, want the replacement", islands[0].Name, len(islands[0].Products))
	}

	got, ok := st.Snapshot(model.IslandKey{SessionGUID: 6627, IslandID: 5})
	if !ok || got.Name != "b" {
		t.Errorf("Snapshot((6627,5)) = %q, %v; want %q, true", got.Name, ok, "b")
	}
	if _, ok := st.Snapshot(model.IslandKey{SessionGUID: 1, IslandID: 1}); ok {
		t.Error("Snapshot of an unknown island reported ok")
	}
}

func TestResetClearsIslandsOnly(t *testing.T) {
	st := state.New()
	st.Put(snap(1, 1, "x", 1))
	st.SetSession("save")
	st.SetConnection(state.Connection{Mode: "replay", State: "replaying"})

	st.Reset()

	if got := st.Islands(); len(got) != 0 {
		t.Fatalf("got %d islands after Reset, want 0", len(got))
	}
	if headline, _ := st.Session(); headline != "save" {
		t.Errorf("Reset cleared the session headline (%q)", headline)
	}
	if st.Connection().Mode != "replay" {
		t.Error("Reset cleared the connection status")
	}
	// The map must still be usable afterwards.
	st.Put(snap(1, 1, "y", 1))
	if got := st.Islands(); len(got) != 1 {
		t.Fatalf("got %d islands after Put following Reset, want 1", len(got))
	}
}

func TestSessionAndConnection(t *testing.T) {
	st := state.New()
	if headline, started := st.Session(); headline != "" || !started.IsZero() {
		t.Errorf("fresh state has session %q/%v, want empty", headline, started)
	}

	before := time.Now()
	st.SetSession("Anno 117")
	headline, started := st.Session()
	if headline != "Anno 117" {
		t.Errorf("headline = %q", headline)
	}
	if started.Before(before) {
		t.Errorf("session start %v is before SetSession was called (%v)", started, before)
	}

	st.SetConnection(state.Connection{Mode: "pipe", State: "waiting"})
	st.UpdateConnection(func(c *state.Connection) {
		c.LastFrameAt = before
		c.ProtocolVersion = 2
	})
	conn := st.Connection()
	if conn.Mode != "pipe" || conn.State != "waiting" || conn.ProtocolVersion != 2 || !conn.LastFrameAt.Equal(before) {
		t.Errorf("connection = %+v, want the merged values", conn)
	}
}

// TestConcurrentAccess is meaningful under -race, which scripts/check.sh does
// not run by default; run go test -race ./internal/state to exercise it.
func TestConcurrentAccess(t *testing.T) {
	st := state.New()
	const writers, readers, rounds = 4, 4, 200

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				st.Put(snap(int32(w), int32(i%8), "island", 3))
				st.UpdateConnection(func(c *state.Connection) { c.LastFrameAt = time.Now() })
				if i%50 == 0 {
					st.SetSession("save")
					st.Reset()
				}
			}
		}(w)
	}
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				for _, snap := range st.Islands() {
					_ = snap.Key
					_ = len(snap.Products)
				}
				_, _ = st.Session()
				_ = st.Connection()
				_, _ = st.Snapshot(model.IslandKey{SessionGUID: 1, IslandID: 1})
			}
		}()
	}
	wg.Wait()
}
