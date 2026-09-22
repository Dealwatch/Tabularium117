package ingest_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/ingest"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/replay"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

// The live recording of 2026-09-22 (docs/protocol.md, "Live capture") ends
// with a reload: SessionEnd, SessionStart, then a tick in which every island
// reports zero products. That tick must not raise deficit alerts, and the
// session boundary must reset the engine and the state.
func TestLiveFixtureEmptyTickRaisesNoAlerts(t *testing.T) {
	f, err := os.Open("../../testdata/live-2026-09-22.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	st := state.New()
	engine := alerts.New(alerts.DefaultConfig())
	var events []alerts.Event
	var headline string
	var emptyAfterStart int
	p := &ingest.Pipeline{
		State: st,
		OnSnapshot: func(snap model.IslandSnapshot) {
			events = append(events, engine.Apply(snap)...)
			if headline != "" && len(snap.Products) == 0 {
				emptyAfterStart++
			}
		},
	}
	p.OnSessionStart = func(h string, _ time.Time) { headline = h; events = append(events, engine.Reset()...) }
	p.OnSessionEnd = func(_ time.Time) { events = append(events, engine.Reset()...) }

	if err := p.Run(context.Background(), replay.NewReader(f, replay.Options{})); err != nil {
		t.Fatal(err)
	}

	if headline != "Captain" {
		t.Fatalf("headline = %q, want the anonymised fixture headline", headline)
	}
	if emptyAfterStart != 10 {
		t.Fatalf("empty islands after SessionStart = %d, want 10", emptyAfterStart)
	}
	islands := st.Islands()
	if len(islands) != 10 {
		t.Fatalf("islands after reload = %d, want 10", len(islands))
	}
	for _, is := range islands {
		if len(is.Products) != 0 {
			t.Fatalf("island %q still has %d products after the empty tick", is.Name, len(is.Products))
		}
	}
	// Two ticks of live data cannot satisfy the 3-sample deficit rule, and the
	// empty tick has nothing to evaluate: no alert may ever have been raised.
	for _, ev := range events {
		if ev.Kind == "raised" {
			t.Fatalf("unexpected alert raised: %+v", ev.Alert)
		}
	}
	if got := engine.Active(); len(got) != 0 {
		t.Fatalf("active alerts after the empty tick = %d, want 0", len(got))
	}
	if stats := p.Stats(); stats.Frames != 35 || stats.Snapshots != 30 || stats.DecodeErrors != 0 {
		t.Fatalf("stats = %+v, want 35 frames, 30 snapshots, 0 decode errors", stats)
	}
}
