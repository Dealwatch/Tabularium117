package ingest_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/ingest"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/pipe"
	"github.com/Dealwatch/Tabularium117/internal/protocol"
	"github.com/Dealwatch/Tabularium117/internal/source"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

// OnStatus is what pushes the connection, the session and the protocol
// version to the browser. It fires for everything the status shows - and not
// for an ordinary statistics frame, which would turn the stream into one
// status event per island per tick.
func TestOnStatusFiresForStatusChanges(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	st := state.New()
	p := &ingest.Pipeline{State: st}

	var calls int
	var seen []string
	p.OnStatus = func() {
		calls++
		conn := st.Connection()
		headline, _ := st.Session()
		seen = append(seen, conn.State+"/"+headline)
	}

	onStatus := p.PipeStatus()
	onStatus(pipe.Status{State: pipe.StateConnected, At: now})
	if calls != 1 {
		t.Fatalf("a connection event produced %d notifications, want 1", calls)
	}
	if seen[0] != "connected/" {
		t.Errorf("the state was not updated before the notification: %q", seen[0])
	}

	stat := protocol.AreaStatistics{Snapshot: model.IslandSnapshot{
		Key: model.IslandKey{SessionGUID: 3245, IslandID: 5},
	}}
	src := &sliceSource{frames: []source.Frame{
		frame(t, protocol.Version{Version: protocol.SupportedVersion}, now),
		frame(t, protocol.SessionStart{Headline: "a savegame"}, now),
		frame(t, stat, now),
		frame(t, stat, now.Add(time.Second)),
		frame(t, protocol.SessionEnd{}, now.Add(time.Minute)),
	}}
	if err := p.Run(context.Background(), src); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Connected, version, session start, session end: four, with the two
	// statistics frames in between changing nothing.
	if calls != 4 {
		t.Errorf("%d notifications (%v), want one each for the connection, "+
			"the version and the two session boundaries", calls, seen)
	}
	if seen[2] != "connected/a savegame" {
		t.Errorf("the session was not applied before the notification: %q", seen[2])
	}

	// An unsupported version is a status change too: it is what the UI shows
	// instead of data.
	before := calls
	onStatus(pipe.Status{State: pipe.StateConnected, At: now.Add(2 * time.Minute)})
	bad := &sliceSource{frames: []source.Frame{frame(t, protocol.Version{Version: 42}, now)}}
	if err := p.Run(context.Background(), bad); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != before+2 {
		t.Errorf("%d notifications after an unsupported version, want %d", calls, before+2)
	}
	if st.Connection().Err == "" {
		t.Error("the unsupported version left no message for the UI")
	}

	// A frame that cannot be decoded is not a status change.
	before = calls
	if err := p.Run(context.Background(), &sliceSource{frames: []source.Frame{
		{Payload: []byte{0xff, 0xff}, ReceivedAt: now},
	}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != before {
		t.Errorf("a broken frame produced %d notifications, want none", calls-before)
	}
}

// A pipeline without OnStatus must behave exactly as before.
func TestOnStatusIsOptional(t *testing.T) {
	now := time.Now()
	st := state.New()
	p := &ingest.Pipeline{State: st}
	p.PipeStatus()(pipe.Status{State: pipe.StateDisconnected, Err: io.EOF, At: now})
	if err := p.Run(context.Background(), &sliceSource{frames: []source.Frame{
		frame(t, protocol.SessionStart{Headline: "x"}, now),
		frame(t, protocol.SessionEnd{}, now),
	}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
