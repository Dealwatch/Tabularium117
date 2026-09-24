package state_test

import (
	"fmt"
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

// ticked is one island's snapshot of the tick with game timestamp stamp.
// name tells the tests' snapshots of one island apart.
func ticked(sessionGUID, islandID int32, stamp int64, name string) model.IslandSnapshot {
	s := snap(sessionGUID, islandID, name, 1)
	s.GameTimestamp = stamp
	return s
}

// completeTick is CompleteTick as "stamp: name name ...", or "none".
func completeTick(st *state.State) string {
	stamp, islands, ok := st.CompleteTick()
	if !ok {
		return "none"
	}
	out := fmt.Sprintf("%d:", stamp)
	for _, s := range islands {
		if s.GameTimestamp != stamp {
			return fmt.Sprintf("%s mixed %s@%d", out, s.Name, s.GameTimestamp)
		}
		out += " " + s.Name
	}
	return out
}

func TestCompleteTickNeverMixesTicks(t *testing.T) {
	st := state.New()
	if got := completeTick(st); got != "none" {
		t.Fatalf("fresh state: %s, want none", got)
	}

	// The first tick after a start has no earlier tick to compare with, so
	// only the next timestamp can close it.
	st.Put(ticked(3245, 1, 100, "a1"))
	st.Put(ticked(3245, 2, 100, "b1"))
	st.Put(ticked(6627, 1, 100, "c1"))
	if got := completeTick(st); got != "none" {
		t.Fatalf("while the first tick arrives: %s, want none - nothing says it is over", got)
	}
	st.Put(ticked(3245, 1, 200, "a2"))
	if got, want := completeTick(st), "100: a1 b1 c1"; got != want {
		t.Fatalf("after the next tick began: %s, want %s", got, want)
	}

	// Tick 200 arrives. Islands mixes 200 and 100 meanwhile; CompleteTick
	// stays on 100 until every island of it has reported again.
	st.Put(ticked(3245, 2, 200, "b2"))
	if got, want := completeTick(st), "100: a1 b1 c1"; got != want {
		t.Fatalf("with one island of tick 200 missing: %s, want %s", got, want)
	}
	mixed := map[int64]bool{}
	for _, s := range st.Islands() {
		mixed[s.GameTimestamp] = true
	}
	if len(mixed) != 2 {
		t.Fatalf("Islands carries ticks %v; the test expects it to mix two here", mixed)
	}
	st.Put(ticked(6627, 1, 200, "c2"))
	if got, want := completeTick(st), "200: a2 b2 c2"; got != want {
		t.Fatalf("once every island of tick 100 reported again: %s, want %s - no need to wait two minutes", got, want)
	}

	// Same island ID, other session: a different island. Keys, not IDs.
	st.Put(ticked(3245, 1, 300, "a3"))
	st.Put(ticked(3245, 2, 300, "b3"))
	st.Put(ticked(6627, 2, 300, "d3"))
	if got, want := completeTick(st), "200: a2 b2 c2"; got != want {
		t.Fatalf("(6627,2) is not (6627,1): %s, want %s", got, want)
	}
}

func TestCompleteTickWithAnIslandMissing(t *testing.T) {
	st := state.New()
	for _, stamp := range []int64{100, 200} {
		st.Put(ticked(1, 1, stamp, fmt.Sprint("a", stamp)))
		st.Put(ticked(1, 2, stamp, fmt.Sprint("b", stamp)))
		st.Put(ticked(1, 3, stamp, fmt.Sprint("c", stamp)))
	}
	// Tick 300 has no island 2: it cannot be complete by count, and the
	// boundary to tick 400 closes it instead - without island 2, whose tick
	// 200 numbers are not passed off as tick 300.
	st.Put(ticked(1, 1, 300, "a300"))
	st.Put(ticked(1, 3, 300, "c300"))
	if got, want := completeTick(st), "200: a200 b200 c200"; got != want {
		t.Fatalf("while tick 300 arrives: %s, want %s", got, want)
	}
	st.Put(ticked(1, 1, 400, "a400"))
	if got, want := completeTick(st), "300: a300 c300"; got != want {
		t.Fatalf("after tick 400 began: %s, want %s", got, want)
	}
	// Islands 1 and 3 are in tick 400 - all of tick 300. But island 2 is
	// still known and may yet come: tick 400 is not complete without it.
	st.Put(ticked(1, 3, 400, "c400"))
	if got, want := completeTick(st), "300: a300 c300"; got != want {
		t.Fatalf("tick 400 without island 2: %s, want %s - it may still arrive", got, want)
	}
	st.Put(ticked(1, 2, 400, "b400"))
	if got, want := completeTick(st), "400: a400 b400 c400"; got != want {
		t.Fatalf("once island 2 arrived: %s, want %s", got, want)
	}

	// An island that is not reported again leaves every later tick to the
	// boundary: late, never short.
	st.Put(ticked(1, 1, 500, "a500"))
	st.Put(ticked(1, 3, 500, "c500"))
	if got, want := completeTick(st), "400: a400 b400 c400"; got != want {
		t.Fatalf("tick 500 without island 2: %s, want %s", got, want)
	}
	st.Put(ticked(1, 1, 600, "a600"))
	if got, want := completeTick(st), "500: a500 c500"; got != want {
		t.Fatalf("after tick 600 began: %s, want %s", got, want)
	}

	// A new island - one the session has not seen - arriving after the
	// others still joins the tick they completed.
	st.Put(ticked(1, 2, 600, "b600"))
	st.Put(ticked(1, 3, 600, "c600"))
	if got, want := completeTick(st), "600: a600 b600 c600"; got != want {
		t.Fatalf("tick 600: %s, want %s", got, want)
	}
	st.Put(ticked(1, 4, 600, "d600"))
	if got, want := completeTick(st), "600: a600 b600 c600 d600"; got != want {
		t.Fatalf("a new island arriving after its tick was found complete: %s, want %s", got, want)
	}
}

func TestCompleteTickTakesTheLatestOfOneIsland(t *testing.T) {
	st := state.New()
	st.Put(ticked(1, 1, 100, "a-old"))
	st.Put(ticked(1, 2, 100, "b"))
	// While the game is paused its time stands still, and two ticks can
	// carry one timestamp. They are one tick then; the later values win.
	st.Put(ticked(1, 1, 100, "a-new"))
	st.Put(ticked(1, 1, 200, "a-next"))
	if got, want := completeTick(st), "100: a-new b"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	// The complete tick is a copy: a later Put does not change what an
	// earlier caller holds.
	_, islands, _ := st.CompleteTick()
	st.Put(ticked(1, 2, 200, "b-next"))
	if islands[0].Name != "a-new" || islands[1].Name != "b" {
		t.Errorf("a returned slice changed under its reader: %q %q", islands[0].Name, islands[1].Name)
	}
	if got, want := completeTick(st), "200: a-next b-next"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestResetForgetsTheTicks(t *testing.T) {
	st := state.New()
	for _, stamp := range []int64{100, 200} {
		st.Put(ticked(1, 1, stamp, "a"))
		st.Put(ticked(1, 2, stamp, "b"))
	}
	if got := completeTick(st); got != "200: a b" {
		t.Fatalf("before Reset: %s", got)
	}
	st.Reset()
	if got := completeTick(st); got != "none" {
		t.Fatalf("after Reset: %s, want none - a tick of the old session is none of the new one", got)
	}
	// The next session starts over: with no complete tick to compare with,
	// its first tick waits for the boundary even once every island is in.
	st.Put(ticked(1, 1, 300, "c"))
	st.Put(ticked(1, 2, 300, "d"))
	if got := completeTick(st); got != "none" {
		t.Fatalf("first tick after Reset: %s, want none", got)
	}
	st.Put(ticked(1, 1, 400, "e"))
	if got, want := completeTick(st), "300: c d"; got != want {
		t.Fatalf("got %s, want %s", got, want)
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
				island := snap(int32(w), int32(i%8), "island", 3)
				island.GameTimestamp = int64(i / 8)
				st.Put(island)
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
				_, _, _ = st.CompleteTick()
			}
		}()
	}
	wg.Wait()
}

// The connection words are an API: /api/v1/status sends them as they are,
// and web/js/app.js and every other client compare against the literal
// strings. A rename must fail here rather than silently in the UI or in the
// tabularium117_connection_up metric.
func TestConnectionWordsAreTheAPI(t *testing.T) {
	pinned := map[string]string{
		state.ModePipe:          "pipe",
		state.ModeReplay:        "replay",
		state.StateWaiting:      "waiting",
		state.StateConnected:    "connected",
		state.StateDisconnected: "disconnected",
		state.StateReplaying:    "replaying",
		state.StateEnded:        "ended",
	}
	for got, want := range pinned {
		if got != want {
			t.Errorf("a connection word changed: %q, want %q", got, want)
		}
	}
	if len(pinned) != 7 {
		t.Error("two connection words share one spelling")
	}

	for _, tc := range []struct {
		state string
		want  bool
	}{
		{state.StateWaiting, false},
		{state.StateConnected, true},
		{state.StateDisconnected, false},
		{state.StateReplaying, true},
		{state.StateEnded, false},
		{"", false},
	} {
		if got := (state.Connection{State: tc.state}).Delivering(); got != tc.want {
			t.Errorf("Delivering() in state %q = %v, want %v", tc.state, got, tc.want)
		}
	}
}
