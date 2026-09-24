package server_test

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/server"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

type sourcesJSON struct {
	Island  islandJSON `json:"island"`
	Product struct {
		GUID int32  `json:"guid"`
		Name string `json:"name"`
	} `json:"product"`
	Ready      bool       `json:"ready"`
	Tick       *int64     `json:"tick"`
	ReceivedAt *time.Time `json:"receivedAt"`
	Groups     []struct {
		SessionGUID int32  `json:"sessionGuid"`
		SessionName string `json:"sessionName"`
		Own         bool   `json:"own"`
		Sources     []struct {
			ID       string  `json:"id"`
			IslandID int32   `json:"islandId"`
			Name     string  `json:"name"`
			Delta    float32 `json:"delta"`
		} `json:"sources"`
	} `json:"groups"`
}

// render is the answer as one line - "Session*: Name +2.0, ... | ..." with *
// on the own session - so a test states the whole expected answer at once.
func (s sourcesJSON) render() string {
	if !s.Ready {
		return "not ready"
	}
	var groups []string
	for _, g := range s.Groups {
		var names []string
		for _, src := range g.Sources {
			names = append(names, fmt.Sprintf("%s %+.1f", src.Name, src.Delta))
		}
		own := ""
		if g.Own {
			own = "*"
		}
		groups = append(groups, fmt.Sprintf("%s%s: %s", g.SessionName, own, strings.Join(names, ", ")))
	}
	if len(groups) == 0 {
		return "none"
	}
	return strings.Join(groups, " | ")
}

const (
	latium = 3245
	albion = 6627
	oats   = 2068 // "Oats", "Hafer"
)

// isle is one island's snapshot; goods are (guid, buildings, delta) triples.
func isle(session, id int32, name string, goods ...[3]float32) model.IslandSnapshot {
	snap := model.IslandSnapshot{
		Key:  model.IslandKey{SessionGUID: session, IslandID: id},
		Name: name,
	}
	for _, g := range goods {
		snap.Products = append(snap.Products, model.ProductStat{
			ProductGUID: int32(g[0]),
			Buildings:   int32(g[1]),
			Delta:       g[2],
		})
	}
	return snap
}

func good(guid int32, buildings int32, delta float32) [3]float32 {
	return [3]float32{float32(guid), float32(buildings), delta}
}

// putTick stores the islands as one tick with game timestamp stamp.
func putTick(st *state.State, stamp int64, islands ...model.IslandSnapshot) {
	for _, snap := range islands {
		snap.GameTimestamp = stamp
		snap.ReceivedAt = time.Unix(stamp, 0)
		st.Put(snap)
	}
}

func sourcesServer(t *testing.T, st *state.State) string {
	t.Helper()
	srv := server.New(server.Options{State: st, Version: "test"})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts.URL
}

func sources(t *testing.T, url, island string, guid int32) sourcesJSON {
	t.Helper()
	var got sourcesJSON
	getJSON(t, fmt.Sprintf("%s/api/v1/islands/%s/products/%d/sources?lang=en", url, island, guid), http.StatusOK, &got)
	return got
}

// world is the example from the feature request, plus one island for every
// way of not being a possible source.
func world() []model.IslandSnapshot {
	return []model.IslandSnapshot{
		// The island that needs oats: consumes them, no building.
		isle(latium, 1, "Cinis", good(oats, 0, -3)),
		isle(latium, 2, "Agathea", good(oats, 1, 2)),
		isle(latium, 3, " Megaron ", good(oats, 2, 3.5)), // names are trimmed
		isle(latium, 4, "Taurus", good(oats, 0, 1)),      // no building
		isle(latium, 5, "Dolya", good(oats, 2, 0)),       // balance zero
		isle(latium, 6, "Iskaleith", good(oats, 3, -1)),  // balance negative
		isle(latium, 7, "Margum"),                        // has no oats at all
		// The same island ID as Cinis, in another session: another island.
		isle(albion, 1, "Argantum", good(oats, 3, 4.3)),
		isle(albion, 2, "Eboracum", good(oats, 1, 1.5)),
		// Listed twice: the later entry counts, as everywhere else.
		isle(albion, 3, "Gwynford", good(oats, 2, 5), good(oats, 2, -1)),
		// Not a number, and not a finite one: no balance, and JSON cannot
		// carry either.
		isle(albion, 4, "Caperby", good(oats, 1, float32(math.NaN()))),
		isle(albion, 5, "Tor Dolya", good(oats, 1, float32(math.Inf(1)))),
	}
}

func TestPossibleSourcesAreSelectedGroupedAndSorted(t *testing.T) {
	st := state.New()
	putTick(st, 100, world()...)
	putTick(st, 200, world()...) // the boundary: tick 100 is complete
	putTick(st, 300, world()...) // tick 200 complete by count
	url := sourcesServer(t, st)

	got := sources(t, url, "3245-1", oats)
	if want := "Latium*: Megaron +3.5, Agathea +2.0 | Albion: Argantum +4.3, Eboracum +1.5"; got.render() != want {
		t.Errorf("sources for Cinis:\n got %s\nwant %s", got.render(), want)
	}
	if got.Tick == nil || *got.Tick != 300 {
		t.Errorf("tick = %v, want 300", got.Tick)
	}
	if got.Island.ID != "3245-1" || got.Product.GUID != oats || got.Product.Name != "Oats" {
		t.Errorf("island %q, product %d %q", got.Island.ID, got.Product.GUID, got.Product.Name)
	}
	src := got.Groups[1].Sources[0]
	if src.ID != "6627-1" || src.IslandID != 1 || got.Groups[1].SessionGUID != albion {
		t.Errorf("Argantum is %+v in session %d", src, got.Groups[1].SessionGUID)
	}

	// From Albion the other way round: Albion first, because it is the own
	// session, and Latium still listed. Eboracum itself is left out.
	got = sources(t, url, "6627-2", oats)
	if want := "Albion*: Argantum +4.3 | Latium: Megaron +3.5, Agathea +2.0"; got.render() != want {
		t.Errorf("sources for Eboracum:\n got %s\nwant %s", got.render(), want)
	}
}

func TestPossibleSourcesEmptyStates(t *testing.T) {
	st := state.New()
	islands := []model.IslandSnapshot{
		isle(latium, 1, "Cinis", good(oats, 0, -3), good(2141, 0, -1)),
		isle(latium, 2, "Agathea", good(2141, 2, -1)),
		isle(albion, 1, "Argantum", good(oats, 3, 4.3)),
		isle(albion, 2, "Eboracum", good(oats, 0, -2)),
	}
	putTick(st, 100, islands...)
	putTick(st, 200, islands...)
	url := sourcesServer(t, st)

	// Only another session has one: that session is shown, and not as the
	// own one.
	got := sources(t, url, "3245-1", oats)
	if want := "Albion: Argantum +4.3"; got.render() != want {
		t.Errorf("only Albion: got %s, want %s", got.render(), want)
	}
	// Nobody has a positive balance: ready, and no groups. That is "none
	// found", which the UI words differently from "not known yet".
	got = sources(t, url, "3245-1", 2141)
	if got.render() != "none" || got.Groups == nil {
		t.Errorf("no source anywhere: got %s (groups %v), want none with an empty list", got.render(), got.Groups)
	}
	// An island that produces the good itself is not its own source: the
	// endpoint answers for any good, and Argantum has only itself.
	got = sources(t, url, "6627-1", oats)
	if got.render() != "none" {
		t.Errorf("the island itself: got %s, want none", got.render())
	}
	// A GUID the catalog does not know still gets an answer and a name.
	got = sources(t, url, "3245-1", 999999)
	if got.render() != "none" || got.Product.Name != "#999999" {
		t.Errorf("unknown good: %s, %q", got.render(), got.Product.Name)
	}
}

// The islands of one tick arrive over about ten seconds, the first island
// first. The sources must not mix the new tick of the islands that already
// reported with the old one of the rest.
func TestPossibleSourcesComeFromOneCompleteTick(t *testing.T) {
	st := state.New()
	tick := func(agathea, argantum float32) []model.IslandSnapshot {
		return []model.IslandSnapshot{
			isle(latium, 1, "Cinis", good(oats, 0, -3)),
			isle(latium, 2, "Agathea", good(oats, 1, agathea)),
			isle(albion, 1, "Argantum", good(oats, 3, argantum)),
		}
	}
	url := sourcesServer(t, st)

	// Right after connecting nothing is complete yet: not ready, which is not
	// the same as "none found".
	putTick(st, 100, tick(1, 1)...)
	got := sources(t, url, "3245-1", oats)
	if got.render() != "not ready" || got.Tick != nil || got.ReceivedAt != nil || got.Groups == nil {
		t.Fatalf("first tick: %s (tick %v, received %v, groups %v), want not ready with an empty list",
			got.render(), got.Tick, got.ReceivedAt, got.Groups)
	}

	putTick(st, 200, tick(2, 2)...)
	next := tick(3, 3)
	// Tick 300 arrives: Cinis first, then Agathea. Argantum is still on 200.
	putTick(st, 300, next[0], next[1])
	got = sources(t, url, "3245-1", oats)
	if want := "Latium*: Agathea +2.0 | Albion: Argantum +2.0"; got.render() != want || *got.Tick != 200 {
		t.Errorf("while tick 300 arrives: %s at tick %v, want %s at 200 - Agathea's 300 must not sit next to Argantum's 200",
			got.render(), *got.Tick, want)
	}
	// The age is tick 200's: when its last island arrived, not when tick
	// 300 began (putTick stamps each island with its tick as Unix time).
	if got.ReceivedAt == nil || !got.ReceivedAt.Equal(time.Unix(200, 0)) {
		t.Errorf("receivedAt = %v, want tick 200's %v", got.ReceivedAt, time.Unix(200, 0))
	}
	putTick(st, 300, next[2])
	got = sources(t, url, "3245-1", oats)
	if want := "Latium*: Agathea +3.0 | Albion: Argantum +3.0"; got.render() != want || *got.Tick != 300 {
		t.Errorf("once tick 300 is in: %s at tick %v, want %s at 300", got.render(), *got.Tick, want)
	}
}

func TestPossibleSourcesAfterLoadingASave(t *testing.T) {
	st := state.New()
	url := sourcesServer(t, st)
	// Loading a save: one tick in which every island is empty, then the real
	// numbers. The empty tick is complete and still says nothing.
	empty := []model.IslandSnapshot{isle(latium, 1, "Cinis"), isle(albion, 1, "Argantum")}
	putTick(st, 100, empty...)
	putTick(st, 200, isle(latium, 1, "Cinis", good(oats, 0, -3)))
	got := sources(t, url, "3245-1", oats)
	if got.render() != "not ready" {
		t.Errorf("after the empty tick: %s, want not ready rather than none", got.render())
	}
	putTick(st, 200, isle(albion, 1, "Argantum", good(oats, 3, 4.3)))
	got = sources(t, url, "3245-1", oats)
	if want := "Albion: Argantum +4.3"; got.render() != want {
		t.Errorf("after the first real tick: %s, want %s", got.render(), want)
	}
}

func TestPossibleSourcesFollowTheIslands(t *testing.T) {
	st := state.New()
	url := sourcesServer(t, st)
	putTick(st, 100, isle(latium, 1, "Cinis", good(oats, 0, -3)), isle(latium, 2, "Agathea", good(oats, 1, 2)))
	putTick(st, 200, isle(latium, 1, "Cinis", good(oats, 0, -3)))
	if got := sources(t, url, "3245-1", oats); got.render() != "Latium*: Agathea +2.0" {
		t.Fatalf("tick 100: %s", got.render())
	}
	// Agathea's oats are missing from tick 200 (and back in 300).
	putTick(st, 200, isle(latium, 2, "Agathea"))
	if got := sources(t, url, "3245-1", oats); got.render() != "none" {
		t.Fatalf("tick 200, oats gone: %s, want none", got.render())
	}
	// Tick 300 begins with the renamed island; Cinis has not reported yet,
	// so tick 200 is still the complete one - and it has no oats on Agathea.
	putTick(st, 300, isle(latium, 2, "Nova Agathea", good(oats, 1, 2.5)))
	if got := sources(t, url, "3245-1", oats); got.render() != "none" {
		t.Fatalf("while tick 300 arrives: %s, want none", got.render())
	}
	putTick(st, 300, isle(latium, 1, "Cinis", good(oats, 0, -3)))
	if got := sources(t, url, "3245-1", oats); got.render() != "Latium*: Nova Agathea +2.5" {
		t.Fatalf("tick 300: %s", got.render())
	}
	// A rename lands in the name right away, even while the balance is
	// still the complete tick's.
	putTick(st, 400, isle(latium, 2, "Agathea Magna", good(oats, 1, 9)))
	if got := sources(t, url, "3245-1", oats); got.render() != "Latium*: Agathea Magna +2.5" {
		t.Errorf("after a rename: %s, want the new name with tick 300's balance", got.render())
	}
}

func TestPossibleSourcesLanguageAndErrors(t *testing.T) {
	st := state.New()
	islands := []model.IslandSnapshot{
		isle(latium, 1, "Cinis", good(oats, 0, -3)),
		isle(9999, 1, "Terra incognita", good(oats, 1, 1)), // a session the catalog does not know
	}
	putTick(st, 100, islands...)
	putTick(st, 200, islands...)
	url := sourcesServer(t, st)

	var got sourcesJSON
	getJSON(t, url+"/api/v1/islands/3245-1/products/2068/sources?lang=de", http.StatusOK, &got)
	if got.Product.Name != "Hafer" {
		t.Errorf("product name in German = %q, want Hafer", got.Product.Name)
	}
	if want := "#9999: Terra incognita +1.0"; got.render() != want {
		t.Errorf("unknown session: %s, want %s", got.render(), want)
	}

	if msg := errorOf(t, url+"/api/v1/islands/1-99/products/2068/sources", http.StatusNotFound); !strings.Contains(msg, "unknown island") {
		t.Errorf("unknown island: %q", msg)
	}
	if msg := errorOf(t, url+"/api/v1/islands/3245-1/products/oats/sources", http.StatusBadRequest); !strings.Contains(msg, "numeric GUID") {
		t.Errorf("bad GUID: %q", msg)
	}
}

// The JSON never calls a possible source anything more than that.
func TestPossibleSourcesDoNotClaimDeliveries(t *testing.T) {
	st := state.New()
	putTick(st, 100, world()...)
	putTick(st, 200, world()...)
	url := sourcesServer(t, st)
	resp := get(t, url+"/api/v1/islands/3245-1/products/2068/sources", nil)
	body := strings.ToLower(readAll(t, resp))
	for _, word := range []string{"supplier", "surplus", "route", "deliver", "import"} {
		if strings.Contains(body, word) {
			t.Errorf("the answer says %q: %s", word, body)
		}
	}
}

// A tick that lacked an island is no measure of when the next one is
// complete: the missing island may still be on its way, and it may be the
// only source there is. The answer must not say "none" before it had the
// chance to arrive.
func TestPossibleSourcesWaitForEveryKnownIsland(t *testing.T) {
	st := state.New()
	url := sourcesServer(t, st)
	cinis := isle(latium, 1, "Cinis", good(oats, 0, -3))
	putTick(st, 100, cinis, isle(latium, 2, "Agathea"), isle(latium, 3, "Megaron", good(oats, 2, 3)))
	// Tick 200 lacks Megaron; the start of tick 300 closes it without.
	putTick(st, 200, cinis, isle(latium, 2, "Agathea", good(oats, 1, 1)))
	putTick(st, 300, cinis)
	if got := sources(t, url, "3245-1", oats); got.render() != "Latium*: Agathea +1.0" || *got.Tick != 200 {
		t.Fatalf("tick 200: %s at %v", got.render(), *got.Tick)
	}
	// Tick 300: Agathea no longer has a balance, and Megaron - the source -
	// has not reported yet. Tick 200's islands are all in, but tick 300 is
	// not complete: "none" here would be a statement about an island that
	// is still on its way.
	putTick(st, 300, isle(latium, 2, "Agathea"))
	if got := sources(t, url, "3245-1", oats); got.render() != "Latium*: Agathea +1.0" || *got.Tick != 200 {
		t.Errorf("while Megaron is still out: %s at %v, want tick 200's answer rather than none", got.render(), *got.Tick)
	}
	putTick(st, 300, isle(latium, 3, "Megaron", good(oats, 2, 4)))
	if got := sources(t, url, "3245-1", oats); got.render() != "Latium*: Megaron +4.0" || *got.Tick != 300 {
		t.Errorf("once Megaron is in: %s at %v, want Megaron at 300", got.render(), *got.Tick)
	}
}
