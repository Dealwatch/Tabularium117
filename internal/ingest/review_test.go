package ingest_test

import (
	"bytes"
	"context"
	"errors"
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

// scriptedDialer hands out one canned connection per Dial call; once the
// script is exhausted it blocks until ctx is done, like a pipe that never
// reappears.
type scriptedDialer struct {
	conns [][]byte
}

func (d *scriptedDialer) Dial(ctx context.Context) (io.ReadCloser, error) {
	if len(d.conns) == 0 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c := d.conns[0]
	d.conns = d.conns[1:]
	return io.NopCloser(bytes.NewReader(c)), nil
}

func wire(t *testing.T, msgs ...protocol.Message) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, m := range msgs {
		payload, err := protocol.Encode(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := protocol.WriteFrame(&buf, payload); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func island(id int32, guid int32) protocol.AreaStatistics {
	return protocol.AreaStatistics{Snapshot: model.IslandSnapshot{
		Key: model.IslandKey{SessionGUID: guid, IslandID: id}, Name: "x",
	}}
}

// A reconnect must re-open the version gate: connection 1 announces an
// unsupported version (its island is dropped), connection 2 announces the
// supported one (its island is stored). This crosses pipe, ingest and state.
func TestReconnectReopensVersionGate(t *testing.T) {
	dialer := &scriptedDialer{conns: [][]byte{
		wire(t, protocol.Version{Version: 99}, island(1, 10)),
		wire(t, protocol.Version{Version: protocol.SupportedVersion}, island(2, 10)),
	}}
	st := state.New()
	p := &ingest.Pipeline{State: st}
	client := pipe.New(pipe.Options{
		Dialer:   dialer,
		OnStatus: p.PipeStatus(),
		Sleep:    func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx, client) }()

	deadline := time.Now().Add(time.Second)
	for {
		if p.Stats().Snapshots == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}

	stats := p.Stats()
	if stats.VersionDropped != 1 || stats.Snapshots != 1 {
		t.Fatalf("stats = %+v, want 1 dropped and 1 stored", stats)
	}
	if _, ok := st.Snapshot(model.IslandKey{SessionGUID: 10, IslandID: 1}); ok {
		t.Fatal("island from the unsupported-version connection was stored")
	}
	if _, ok := st.Snapshot(model.IslandKey{SessionGUID: 10, IslandID: 2}); !ok {
		t.Fatal("island from the supported-version connection is missing")
	}
	// The canned connection ends in EOF, so the state is "disconnected" by
	// now; the announced version of the last connection must still be there.
	if c := st.Connection(); c.ProtocolVersion != protocol.SupportedVersion {
		t.Fatalf("connection = %+v, want protocol version %d", c, protocol.SupportedVersion)
	}
}

// SessionEnd clears the islands; statistics after it repopulate the state.
func TestSessionEndResetsIslands(t *testing.T) {
	now := time.Now()
	src := &sliceSource{frames: []source.Frame{
		frame(t, protocol.Version{Version: protocol.SupportedVersion}, now),
		frame(t, island(1, 10), now),
		frame(t, island(2, 10), now),
		frame(t, protocol.SessionEnd{}, now),
		frame(t, island(3, 10), now),
	}}
	st := state.New()
	p := &ingest.Pipeline{State: st}
	if err := p.Run(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	islands := st.Islands()
	if len(islands) != 1 || islands[0].Key.IslandID != 3 {
		t.Fatalf("islands after SessionEnd = %v, want only island 3", islands)
	}
}
