package ingest_test

import (
	"context"
	"fmt"
	"os"
	"strings"
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

// The live recording's ticks arrive island by island, ten frames one second
// apart. The complete tick the possible sources are read from must never mix
// two of them, must follow a new tick as soon as its last island is in rather
// than a whole tick later, and must forget the old session at the reload.
func TestLiveFixtureCompleteTickIsOneTick(t *testing.T) {
	f, err := os.Open("../../testdata/live-2026-09-22.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	st := state.New()
	var seen []string // the complete tick after every snapshot, as "stamp/islands"
	p := &ingest.Pipeline{
		State: st,
		OnSnapshot: func(snap model.IslandSnapshot) {
			stamp, islands, ok := st.CompleteTick()
			if !ok {
				seen = append(seen, "none")
				return
			}
			for _, is := range islands {
				if is.GameTimestamp != stamp {
					t.Fatalf("after %q: the complete tick %d holds %q from tick %d", snap.Name, stamp, is.Name, is.GameTimestamp)
				}
			}
			seen = append(seen, fmt.Sprintf("%d/%d", stamp, len(islands)))
		},
	}
	if err := p.Run(context.Background(), replay.NewReader(f, replay.Options{})); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 30 {
		t.Fatalf("got %d snapshots, want 30", len(seen))
	}

	// Tick 1 is the first after connecting: nothing to compare it with, so
	// it is complete only when tick 2 begins. Tick 2 is complete with its
	// tenth island. The reload resets; the empty tick after it is the first
	// of the new session again.
	first, second := seen[10], seen[19]
	for i, got := range seen {
		var want string
		switch {
		case i < 10:
			want = "none"
		case i < 19:
			want = first
		case i < 20:
			want = second
		default:
			want = "none"
		}
		if got != want {
			t.Errorf("after snapshot %d: complete tick %s, want %s", i+1, got, want)
		}
	}
	if !strings.HasSuffix(first, "/10") || !strings.HasSuffix(second, "/10") || first == second {
		t.Errorf("ticks %s and %s: want two different ticks of ten islands", first, second)
	}
}
