package pipe_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/pipe"
	"github.com/Dealwatch/Tabularium117/internal/protocol"
	"github.com/Dealwatch/Tabularium117/internal/source"
)

// The tests drive the reconnect loop through a fake Dialer, so they run on
// Linux: the real pipe exists only on Windows and cannot be exercised here.

// dialFunc adapts a function to the Dialer interface.
type dialFunc func(ctx context.Context) (io.ReadCloser, error)

func (f dialFunc) Dial(ctx context.Context) (io.ReadCloser, error) { return f(ctx) }

// step is one scripted dial outcome.
type step func() (io.ReadCloser, error)

// notFound is a dial that reports a missing pipe.
func notFound() step {
	return func() (io.ReadCloser, error) {
		return nil, fmt.Errorf("dial: %w", pipe.ErrPipeNotFound)
	}
}

// dialErr is a dial that fails for some other reason.
func dialErr(msg string) step {
	return func() (io.ReadCloser, error) { return nil, errors.New(msg) }
}

// stream is a dial that succeeds and serves the given bytes, then io.EOF.
func stream(b []byte) step {
	return func() (io.ReadCloser, error) { return &byteConn{Reader: bytes.NewReader(b)}, nil }
}

// byteConn serves bytes from memory and records that it was closed.
type byteConn struct {
	*bytes.Reader
	mu     sync.Mutex
	closed bool
}

func (c *byteConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// scriptedDialer hands out the scripted outcomes in order and cancels the run
// once they are used up, so a test always terminates.
type scriptedDialer struct {
	mu        sync.Mutex
	steps     []step
	n         int
	exhausted func()
}

func (d *scriptedDialer) Dial(ctx context.Context) (io.ReadCloser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.n >= len(d.steps) {
		d.exhausted()
		return nil, ctx.Err()
	}
	s := d.steps[d.n]
	d.n++
	return s()
}

// result is what a scripted run observed.
type result struct {
	frames   []source.Frame
	statuses []pipe.Status
	sleeps   []time.Duration
	err      error
}

// runScript runs a client over the scripted dials with a fake sleep, so
// backoff is asserted without any real waiting.
func runScript(t *testing.T, steps ...step) *result {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu  sync.Mutex
		res result
	)
	client := pipe.New(pipe.Options{
		Dialer: &scriptedDialer{steps: steps, exhausted: cancel},
		OnStatus: func(s pipe.Status) {
			mu.Lock()
			res.statuses = append(res.statuses, s)
			mu.Unlock()
		},
		Sleep: func(ctx context.Context, d time.Duration) error {
			mu.Lock()
			res.sleeps = append(res.sleeps, d)
			mu.Unlock()
			return ctx.Err()
		},
	})

	out := make(chan source.Frame)
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx, out) }()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case f := <-out:
			res.frames = append(res.frames, f)
		case err := <-done:
			res.err = err
			return &res
		case <-deadline:
			t.Fatal("client did not stop within 5s")
		}
	}
}

// encodeFrames builds a byte stream of length-prefixed frames.
func encodeFrames(t *testing.T, msgs ...protocol.Message) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, m := range msgs {
		payload, err := protocol.Encode(m)
		if err != nil {
			t.Fatalf("encode %T: %v", m, err)
		}
		if err := protocol.WriteFrame(&buf, payload); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}
	return buf.Bytes()
}

// states extracts the emitted states in order.
func states(statuses []pipe.Status) []pipe.State {
	out := make([]pipe.State, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, s.State)
	}
	return out
}

func wantStates(t *testing.T, got []pipe.State, want ...pipe.State) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("states = %v, want %v", got, want)
		}
	}
}

func TestWaitsForPipeThenConnects(t *testing.T) {
	data := encodeFrames(t,
		protocol.Version{Version: protocol.SupportedVersion},
		protocol.SessionStart{Headline: "save"},
	)
	res := runScript(t, notFound(), notFound(), stream(data))

	if !errors.Is(res.err, context.Canceled) {
		t.Fatalf("Run err = %v, want context.Canceled", res.err)
	}
	// Waiting is emitted once for the two polls, not twice.
	wantStates(t, states(res.statuses), pipe.StateWaiting, pipe.StateConnected, pipe.StateDisconnected)
	if len(res.frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(res.frames))
	}
	if res.frames[0].ReceivedAt.IsZero() {
		t.Error("frame is not stamped with a receive time")
	}
	if res.statuses[2].Err == nil {
		t.Error("disconnect status carries no error")
	}
	want := []time.Duration{time.Second, time.Second, 500 * time.Millisecond}
	if !equalDurations(res.sleeps, want) {
		t.Errorf("sleeps = %v, want %v", res.sleeps, want)
	}
}

func TestReconnectsAfterConnectionDrops(t *testing.T) {
	first := encodeFrames(t, protocol.Version{Version: protocol.SupportedVersion})
	second := encodeFrames(t, protocol.SessionEnd{})
	res := runScript(t, stream(first), stream(second))

	wantStates(t, states(res.statuses),
		pipe.StateConnected, pipe.StateDisconnected,
		pipe.StateConnected, pipe.StateDisconnected)
	if len(res.frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(res.frames))
	}
	if !errors.Is(res.statuses[1].Err, io.EOF) {
		t.Errorf("first disconnect err = %v, want io.EOF", res.statuses[1].Err)
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	res := runScript(t,
		dialErr("1"), dialErr("2"), dialErr("3"), dialErr("4"),
		dialErr("5"), dialErr("6"), dialErr("7"),
	)
	want := []time.Duration{
		500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 10 * time.Second, 10 * time.Second,
	}
	if !equalDurations(res.sleeps, want) {
		t.Fatalf("sleeps = %v, want %v", res.sleeps, want)
	}
}

func TestBackoffResetsAfterSuccess(t *testing.T) {
	data := encodeFrames(t, protocol.SessionEnd{})
	res := runScript(t, dialErr("boom"), dialErr("boom"), stream(data), dialErr("boom"))

	want := []time.Duration{
		500 * time.Millisecond, // first failure
		time.Second,            // second failure
		500 * time.Millisecond, // after the connection was lost: reset
		time.Second,
	}
	if !equalDurations(res.sleeps, want) {
		t.Fatalf("sleeps = %v, want %v", res.sleeps, want)
	}
}

func TestFramingErrorTriggersReconnect(t *testing.T) {
	// A zero length prefix: the stream is out of sync, the connection has to
	// be dropped rather than resynchronised.
	garbage := []byte{0, 0, 0, 0, 0xff}
	good := encodeFrames(t, protocol.SessionEnd{})
	res := runScript(t, stream(garbage), stream(good))

	wantStates(t, states(res.statuses),
		pipe.StateConnected, pipe.StateDisconnected,
		pipe.StateConnected, pipe.StateDisconnected)
	if !errors.Is(res.statuses[1].Err, protocol.ErrShortFrame) {
		t.Errorf("disconnect err = %v, want ErrShortFrame", res.statuses[1].Err)
	}
	if len(res.frames) != 1 {
		t.Fatalf("got %d frames, want 1 (from the second connection)", len(res.frames))
	}
}

func TestCancelWhileWaitingReturnsPromptly(t *testing.T) {
	// No Sleep override: the real one-second poll wait must still be
	// interrupted by the context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := pipe.New(pipe.Options{
		Dialer: dialFunc(func(context.Context) (io.ReadCloser, error) {
			return nil, fmt.Errorf("dial: %w", pipe.ErrPipeNotFound)
		}),
		OnStatus: func(s pipe.Status) {
			if s.State == pipe.StateWaiting {
				cancel()
			}
		},
	})

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- client.Run(ctx, make(chan source.Frame)) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run err = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Errorf("Run took %v to stop, want well under the 1s poll interval", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestCancelWhileConnectedClosesConnection(t *testing.T) {
	data := encodeFrames(t, protocol.SessionEnd{})
	conn := &blockingConn{data: data, release: make(chan struct{})}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := pipe.New(pipe.Options{
		Dialer: dialFunc(func(context.Context) (io.ReadCloser, error) { return conn, nil }),
	})

	out := make(chan source.Frame)
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx, out) }()

	select {
	case <-out:
	case <-time.After(3 * time.Second):
		t.Fatal("no frame delivered")
	}
	// The connection now blocks in Read, like a pipe with no data pending.
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run err = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation; the blocked read was not unblocked")
	}
	if !conn.isClosed() {
		t.Error("connection was not closed on cancellation")
	}
}

func TestUnsupportedPlatformIsFatal(t *testing.T) {
	// No Sleep override: if the client retried, this test would hang rather
	// than return.
	client := pipe.New(pipe.Options{
		Dialer: dialFunc(func(context.Context) (io.ReadCloser, error) {
			return nil, fmt.Errorf("dial: %w", pipe.ErrUnsupportedPlatform)
		}),
	})
	done := make(chan error, 1)
	go func() { done <- client.Run(context.Background(), make(chan source.Frame)) }()

	select {
	case err := <-done:
		if !errors.Is(err, pipe.ErrUnsupportedPlatform) {
			t.Fatalf("Run err = %v, want ErrUnsupportedPlatform", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run retried an unsupported platform instead of giving up")
	}
}

func TestStateString(t *testing.T) {
	cases := map[pipe.State]string{
		pipe.StateWaiting:      "waiting",
		pipe.StateConnected:    "connected",
		pipe.StateDisconnected: "disconnected",
		pipe.State(99):         "unknown",
	}
	for state, want := range cases {
		if got := state.String(); got != want {
			t.Errorf("State(%d).String() = %q, want %q", int(state), got, want)
		}
	}
}

// blockingConn serves its bytes and then blocks in Read until it is closed,
// which is how a real pipe behaves between ticks.
type blockingConn struct {
	mu      sync.Mutex
	data    []byte
	off     int
	closed  bool
	release chan struct{}
}

func (c *blockingConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	if c.off < len(c.data) {
		n := copy(p, c.data[c.off:])
		c.off += n
		c.mu.Unlock()
		return n, nil
	}
	c.mu.Unlock()
	<-c.release
	return 0, io.ErrClosedPipe
}

func (c *blockingConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.release)
	}
	return nil
}

func (c *blockingConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func equalDurations(got, want []time.Duration) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
