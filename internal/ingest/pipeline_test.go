package ingest_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/ingest"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/pipe"
	"github.com/Dealwatch/Tabularium117/internal/protocol"
	"github.com/Dealwatch/Tabularium117/internal/replay"
	"github.com/Dealwatch/Tabularium117/internal/source"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

// fixturePath is the re-encoded connector capture (see testdata/README.md).
// The numbers asserted here are the ones docs/protocol.md reports for it.
const fixturePath = "../../testdata/connector-reencoded.jsonl"

const (
	fixtureFrames   = 16
	fixtureIslands  = 14
	fixtureProducts = 423
	fixtureHeadline = "reencoded from connector capture"
)

// sliceSource emits a fixed list of frames and stops.
type sliceSource struct {
	frames []source.Frame
}

func (s *sliceSource) Run(ctx context.Context, out chan<- source.Frame) error {
	for _, f := range s.frames {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- f:
		}
	}
	return nil
}

// failingSource reports an error after emitting nothing.
type failingSource struct{ err error }

func (s *failingSource) Run(context.Context, chan<- source.Frame) error { return s.err }

// frame encodes a message into a frame payload with the given receive time.
func frame(t *testing.T, m protocol.Message, at time.Time) source.Frame {
	t.Helper()
	payload, err := protocol.Encode(m)
	if err != nil {
		t.Fatalf("encode %T: %v", m, err)
	}
	return source.Frame{ReceivedAt: at, Payload: payload}
}

// logTo returns a logger writing into buf, so tests can assert on the output.
func logTo(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func fixtureReader(t *testing.T) *replay.Reader {
	t.Helper()
	f, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return replay.NewReader(f, replay.Options{Speed: 0})
}

func TestFixtureFillsState(t *testing.T) {
	st := state.New()
	var seen []model.IslandSnapshot
	p := &ingest.Pipeline{
		State:      st,
		OnSnapshot: func(s model.IslandSnapshot) { seen = append(seen, s) },
	}

	if err := p.Run(context.Background(), fixtureReader(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	islands := st.Islands()
	if len(islands) != fixtureIslands {
		t.Fatalf("got %d islands, want %d", len(islands), fixtureIslands)
	}
	products := 0
	for _, island := range islands {
		products += len(island.Products)
	}
	if products != fixtureProducts {
		t.Errorf("got %d products, want %d", products, fixtureProducts)
	}
	if len(seen) != fixtureIslands {
		t.Errorf("OnSnapshot called %d times, want %d", len(seen), fixtureIslands)
	}

	headline, startedAt := st.Session()
	if headline != fixtureHeadline {
		t.Errorf("headline = %q, want %q", headline, fixtureHeadline)
	}
	if startedAt.IsZero() {
		t.Error("session start time was not set")
	}

	conn := st.Connection()
	if conn.ProtocolVersion != protocol.SupportedVersion {
		t.Errorf("Connection.ProtocolVersion = %d, want %d", conn.ProtocolVersion, protocol.SupportedVersion)
	}
	if conn.Err != "" {
		t.Errorf("Connection.Err = %q, want empty", conn.Err)
	}
	if conn.LastFrameAt.IsZero() {
		t.Error("Connection.LastFrameAt was not updated")
	}

	stats := p.Stats()
	if stats.Frames != fixtureFrames || stats.Snapshots != fixtureIslands || stats.DecodeErrors != 0 {
		t.Errorf("stats = %+v, want %d frames, %d snapshots, no errors", stats, fixtureFrames, fixtureIslands)
	}
}

func TestUnsupportedVersionDropsStatistics(t *testing.T) {
	st := state.New()
	var buf bytes.Buffer
	p := &ingest.Pipeline{State: st, Log: logTo(&buf)}

	now := time.Now()
	stat := protocol.AreaStatistics{Snapshot: model.IslandSnapshot{
		Key:  model.IslandKey{SessionGUID: 3245, IslandID: 5},
		Name: "Juliana",
	}}
	src := &sliceSource{frames: []source.Frame{
		frame(t, protocol.Version{Version: protocol.SupportedVersion + 99}, now),
		frame(t, stat, now),
		frame(t, stat, now),
	}}

	if err := p.Run(context.Background(), src); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := st.Islands(); len(got) != 0 {
		t.Fatalf("got %d islands, want 0: an unsupported version must stop decoding", len(got))
	}
	conn := st.Connection()
	if conn.Err == "" {
		t.Error("Connection.Err is empty, want an explanation of the version mismatch")
	}
	if !strings.Contains(conn.Err, "101") {
		t.Errorf("Connection.Err = %q, want it to name the announced version", conn.Err)
	}
	if stats := p.Stats(); stats.VersionDropped != 2 || stats.Snapshots != 0 {
		t.Errorf("stats = %+v, want 2 dropped and 0 stored", stats)
	}
	// One error line for the connection, not one per dropped frame.
	if n := strings.Count(buf.String(), "level=ERROR"); n != 1 {
		t.Errorf("logged %d error lines, want exactly 1\n%s", n, buf.String())
	}
}

func TestSupportedVersionReopensTheGate(t *testing.T) {
	st := state.New()
	p := &ingest.Pipeline{State: st}

	now := time.Now()
	stat := protocol.AreaStatistics{Snapshot: model.IslandSnapshot{
		Key: model.IslandKey{SessionGUID: 1, IslandID: 2},
	}}
	src := &sliceSource{frames: []source.Frame{
		frame(t, protocol.Version{Version: 999}, now),
		frame(t, stat, now),
		frame(t, protocol.Version{Version: protocol.SupportedVersion}, now),
		frame(t, stat, now),
	}}
	if err := p.Run(context.Background(), src); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := st.Islands(); len(got) != 1 {
		t.Fatalf("got %d islands, want 1", len(got))
	}
	if conn := st.Connection(); conn.Err != "" {
		t.Errorf("Connection.Err = %q, want it cleared by the supported version", conn.Err)
	}
}

func TestGarbageFrameIsSkipped(t *testing.T) {
	st := state.New()
	var buf bytes.Buffer
	p := &ingest.Pipeline{State: st, Log: logTo(&buf)}

	now := time.Now()
	good := protocol.AreaStatistics{Snapshot: model.IslandSnapshot{
		Key:  model.IslandKey{SessionGUID: 3245, IslandID: 5},
		Name: "Juliana",
	}}
	src := &sliceSource{frames: []source.Frame{
		{ReceivedAt: now, Payload: []byte{0x03, 0x01, 0x02}}, // statistics, truncated
		{ReceivedAt: now, Payload: []byte{0xfe}},             // unknown type
		frame(t, good, now),
	}}

	if err := p.Run(context.Background(), src); err != nil {
		t.Fatalf("Run: %v", err)
	}

	islands := st.Islands()
	if len(islands) != 1 || islands[0].Name != "Juliana" {
		t.Fatalf("got %d islands (%v), want the one good frame stored", len(islands), islands)
	}
	if stats := p.Stats(); stats.DecodeErrors != 2 || stats.Frames != 3 {
		t.Errorf("stats = %+v, want 3 frames and 2 decode errors", stats)
	}
	if n := strings.Count(buf.String(), "level=WARN"); n != 2 {
		t.Errorf("logged %d warnings, want 2\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), "bytes=3") {
		t.Errorf("decode warning does not report the frame length\n%s", buf.String())
	}
}

func TestRecordingReplaysToTheSameFrames(t *testing.T) {
	var recorded bytes.Buffer
	p := &ingest.Pipeline{State: state.New(), Record: replay.NewWriter(&recorded)}

	if err := p.Run(context.Background(), fixtureReader(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	original := collect(t, fixtureReader(t))
	again := collect(t, replay.NewReader(bytes.NewReader(recorded.Bytes()), replay.Options{Speed: 0}))
	if len(again) != len(original) {
		t.Fatalf("recording has %d frames, want %d", len(again), len(original))
	}
	for i := range original {
		if !bytes.Equal(again[i].Payload, original[i].Payload) {
			t.Fatalf("frame %d differs from the original payload", i)
		}
		if !again[i].ReceivedAt.Equal(original[i].ReceivedAt) {
			t.Fatalf("frame %d: recorded time %v, want %v", i, again[i].ReceivedAt, original[i].ReceivedAt)
		}
	}
}

func TestRecordingKeepsUndecodableFrames(t *testing.T) {
	var recorded bytes.Buffer
	p := &ingest.Pipeline{State: state.New(), Record: replay.NewWriter(&recorded)}

	garbage := source.Frame{ReceivedAt: time.Now(), Payload: []byte{0x03, 0x01}}
	if err := p.Run(context.Background(), &sliceSource{frames: []source.Frame{garbage}}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	frames := collect(t, replay.NewReader(bytes.NewReader(recorded.Bytes()), replay.Options{Speed: 0}))
	if len(frames) != 1 || !bytes.Equal(frames[0].Payload, garbage.Payload) {
		t.Fatalf("got %d recorded frames, want the undecodable one recorded verbatim", len(frames))
	}
}

func TestRunReturnsSourceError(t *testing.T) {
	want := errors.New("source broke")
	p := &ingest.Pipeline{State: state.New()}
	if err := p.Run(context.Background(), &failingSource{err: want}); !errors.Is(err, want) {
		t.Fatalf("Run err = %v, want %v", err, want)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &ingest.Pipeline{State: state.New()}
	err := p.Run(ctx, fixtureReader(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run err = %v, want context.Canceled", err)
	}
}

func TestPipeStatusMapping(t *testing.T) {
	st := state.New()
	p := &ingest.Pipeline{State: st}
	onStatus := p.PipeStatus()

	at := time.Now()
	onStatus(pipe.Status{State: pipe.StateWaiting, At: at})
	conn := st.Connection()
	if conn.Mode != "pipe" || conn.State != "waiting" || !conn.Since.Equal(at) {
		t.Fatalf("connection = %+v, want mode pipe, state waiting", conn)
	}

	// An unsupported version arrives, then the connection is lost and
	// re-established: the gate must be open again.
	src := &sliceSource{frames: []source.Frame{frame(t, protocol.Version{Version: 42}, at)}}
	if err := p.Run(context.Background(), src); err != nil {
		t.Fatalf("Run: %v", err)
	}
	st.Put(model.IslandSnapshot{Key: model.IslandKey{SessionGUID: 1, IslandID: 1}})

	onStatus(pipe.Status{State: pipe.StateDisconnected, Err: io.EOF, At: at.Add(time.Second)})
	conn = st.Connection()
	if conn.State != "disconnected" || conn.Err != io.EOF.Error() {
		t.Errorf("connection = %+v, want disconnected with the error text", conn)
	}
	if len(st.Islands()) != 1 {
		t.Error("islands were cleared on disconnect; they must stay visible")
	}

	onStatus(pipe.Status{State: pipe.StateConnected, At: at.Add(2 * time.Second)})
	conn = st.Connection()
	if conn.Err != "" || conn.ProtocolVersion != 0 {
		t.Errorf("connection = %+v, want the per-connection fields reset", conn)
	}

	stat := protocol.AreaStatistics{Snapshot: model.IslandSnapshot{Key: model.IslandKey{SessionGUID: 2, IslandID: 2}}}
	if err := p.Run(context.Background(), &sliceSource{frames: []source.Frame{frame(t, stat, at)}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(st.Islands()) != 2 {
		t.Error("the version gate stayed shut across a reconnect")
	}
}

// collect drains a source into a slice.
func collect(t *testing.T, src source.Source) []source.Frame {
	t.Helper()
	out := make(chan source.Frame)
	done := make(chan error, 1)
	go func() { done <- src.Run(context.Background(), out) }()

	var frames []source.Frame
	for {
		select {
		case f := <-out:
			frames = append(frames, f)
		case err := <-done:
			if err != nil {
				t.Fatalf("source: %v", err)
			}
			return frames
		case <-time.After(5 * time.Second):
			t.Fatal("source did not finish")
		}
	}
}

// The session callbacks exist so the store can open and close a game_session
// row. They fire after the state has been updated, with the frame's receive
// time, and a pipeline without them behaves exactly as before.
func TestSessionCallbacks(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)
	src := &sliceSource{frames: []source.Frame{
		frame(t, protocol.Version{Version: protocol.SupportedVersion}, now),
		frame(t, protocol.SessionStart{Headline: "a savegame"}, now),
		frame(t, protocol.SessionEnd{}, later),
	}}

	type event struct {
		kind     string
		headline string
		at       time.Time
	}
	var events []event
	st := state.New()
	p := &ingest.Pipeline{State: st}
	p.OnSessionStart = func(headline string, at time.Time) {
		// The state must already reflect the start when the callback runs.
		if got, _ := st.Session(); got != headline {
			t.Errorf("state headline = %q inside OnSessionStart, want %q", got, headline)
		}
		events = append(events, event{"start", headline, at})
	}
	p.OnSessionEnd = func(at time.Time) {
		events = append(events, event{kind: "end", at: at})
	}

	if err := p.Run(context.Background(), src); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []event{{"start", "a savegame", now}, {"end", "", later}}
	if len(events) != len(want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
	for i := range want {
		if events[i].kind != want[i].kind || events[i].headline != want[i].headline ||
			!events[i].at.Equal(want[i].at) {
			t.Errorf("event %d = %+v, want %+v", i, events[i], want[i])
		}
	}
}

// A pipeline with no callbacks must not notice their absence.
func TestSessionCallbacksAreOptional(t *testing.T) {
	now := time.Now()
	src := &sliceSource{frames: []source.Frame{
		frame(t, protocol.SessionStart{Headline: "x"}, now),
		frame(t, protocol.SessionEnd{}, now),
	}}
	p := &ingest.Pipeline{State: state.New()}
	if err := p.Run(context.Background(), src); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
