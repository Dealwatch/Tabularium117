package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/server"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

// listen starts a real server on a free loopback port and returns it with its
// base URL. Cancelling ctx shuts it down; the returned channel carries what
// serving ended with.
func listen(t *testing.T, ctx context.Context, fx fixture, heartbeat time.Duration) (*server.Server, string, chan error) {
	t.Helper()
	ready := make(chan net.Addr, 1)
	srv := server.New(server.Options{
		State:     fx.state,
		Store:     fx.store,
		Version:   "test",
		Heartbeat: heartbeat,
		Now:       func() time.Time { return fx.now },
		OnReady:   func(addr net.Addr) { ready <- addr },
	})
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, "127.0.0.1:0") }()

	select {
	case addr := <-ready:
		if srv.Addr() == nil || srv.Addr().String() != addr.String() {
			t.Fatalf("Addr() = %v, want the bound %v", srv.Addr(), addr)
		}
		return srv, "http://" + addr.String(), done
	case err := <-done:
		t.Fatalf("the server stopped instead of binding: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not bind")
	}
	return nil, "", nil
}

// stream is a connected event stream, read frame by frame.
type stream struct {
	resp   *http.Response
	reader *bufio.Reader
	cancel context.CancelFunc
}

// openStream connects to the event endpoint and checks the stream's headers.
func openStream(t *testing.T, ctx context.Context, url string) *stream {
	t.Helper()
	// A bounded request context so that a server that never answers fails
	// the test instead of hanging it.
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		t.Fatalf("build the stream request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("GET %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}
	for header, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-store",
		"X-Accel-Buffering": "no",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	s := &stream{resp: resp, reader: bufio.NewReader(resp.Body), cancel: cancel}
	t.Cleanup(s.close)
	return s
}

func (s *stream) close() {
	s.cancel()
	s.resp.Body.Close()
}

// frame reads one SSE frame. A comment line - the heartbeat - is reported as
// the event name ":".
func (s *stream) frame() (name, data string, err error) {
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			return "", "", err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if name == "" && data == "" {
				continue // the blank line after a comment
			}
			return name, data, nil
		case strings.HasPrefix(line, ":"):
			return ":", strings.TrimSpace(line[1:]), nil
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
}

// nextEvent returns the next real event, skipping heartbeats.
func (s *stream) nextEvent(t *testing.T) (string, string) {
	t.Helper()
	for {
		name, data, err := s.frame()
		if err != nil {
			t.Fatalf("read the next event: %v", err)
		}
		if name == ":" {
			continue
		}
		return name, data
	}
}

// waitHeartbeat reads until a comment line arrives.
func (s *stream) waitHeartbeat(t *testing.T) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		name, data, err := s.frame()
		if err != nil {
			t.Fatalf("read a heartbeat: %v", err)
		}
		if name == ":" {
			if data != "ping" {
				t.Errorf("heartbeat comment = %q, want ping", data)
			}
			return
		}
	}
	t.Fatal("no heartbeat arrived")
}

// island decodes a snapshot event's payload.
func islandEvent(t *testing.T, data string) islandJSON {
	t.Helper()
	var out islandJSON
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		t.Fatalf("decode the snapshot event %q: %v", data, err)
	}
	return out
}

// waitFor polls until cond holds or the test gives up.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Two clients at once: both are caught up on connect, both see later
// publications, both get heartbeats, and neither depends on the other.
func TestEventStreamTwoClients(t *testing.T) {
	fx := loadFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, base, done := listen(t, ctx, fx, 20*time.Millisecond)

	first := openStream(t, ctx, base+"/api/v1/events")
	second := openStream(t, ctx, base+"/api/v1/events?lang=german")

	for name, s := range map[string]*stream{"first": first, "second": second} {
		event, data := s.nextEvent(t)
		if event != "status" {
			t.Fatalf("%s client: first event = %q, want status", name, event)
		}
		var status statusJSON
		if err := json.Unmarshal([]byte(data), &status); err != nil {
			t.Fatalf("%s client: decode the status: %v", name, err)
		}
		if status.Islands != 14 {
			t.Errorf("%s client: status reports %d islands, want 14", name, status.Islands)
		}

		seen := map[string]bool{}
		for i := 0; i < 14; i++ {
			event, data := s.nextEvent(t)
			if event != "snapshot" {
				t.Fatalf("%s client: catch-up event %d = %q, want snapshot", name, i, event)
			}
			seen[islandEvent(t, data).ID] = true
		}
		if len(seen) != 14 {
			t.Errorf("%s client: caught up on %d islands, want all 14", name, len(seen))
		}
	}

	waitFor(t, "both clients to be registered", func() bool { return srv.StreamClients() == 2 })

	// A publication after the catch-up reaches both clients.
	updated := fx.snapshots[0]
	updated.Name = "Juliana Nova"
	srv.PublishSnapshot(updated)
	for name, s := range map[string]*stream{"first": first, "second": second} {
		event, data := s.nextEvent(t)
		if event != "snapshot" {
			t.Fatalf("%s client: event = %q, want snapshot", name, event)
		}
		if island := islandEvent(t, data); island.Name != "Juliana Nova" || island.ID != julianaID {
			t.Errorf("%s client: snapshot = %+v, want the published island", name, island)
		}
	}

	// A status change reaches both as well.
	srv.PublishStatus()
	for name, s := range map[string]*stream{"first": first, "second": second} {
		if event, _ := s.nextEvent(t); event != "status" {
			t.Errorf("%s client: event = %q, want status", name, event)
		}
	}

	// The heartbeat keeps an idle stream alive.
	first.waitHeartbeat(t)
	second.waitHeartbeat(t)

	// A client that goes away is forgotten.
	first.close()
	waitFor(t, "the disconnected client to be removed", func() bool { return srv.StreamClients() == 1 })

	// Shutting the server down ends the stream that is still open, and the
	// shutdown itself is clean.
	cancel()
	if _, _, err := second.frame(); err == nil {
		t.Error("the stream did not end when the server shut down")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a graceful shutdown must return nil, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not shut down")
	}
	if srv.StreamClients() != 0 {
		t.Errorf("%d clients left after the shutdown", srv.StreamClients())
	}
}

// Serving stops cleanly when the context is cancelled, even with nothing
// connected.
func TestGracefulShutdown(t *testing.T) {
	fx := loadFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, base, done := listen(t, ctx, fx, 0)

	var status statusJSON
	getJSON(t, base+"/api/v1/status", http.StatusOK, &status)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown returned %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not stop after the context was cancelled")
	}
}

// A port that is already taken has to fail loudly: the command turns this
// into its exit code instead of a UI that never appears.
func TestListenOnBusyPortFails(t *testing.T) {
	fx := loadFixture(t, false)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	srv := server.New(server.Options{State: fx.state})
	err = srv.ListenAndServe(context.Background(), busy.Addr().String())
	if err == nil {
		t.Fatal("binding a port that is in use must fail")
	}
	if !strings.Contains(err.Error(), busy.Addr().String()) {
		t.Errorf("error = %v, want it to name the address", err)
	}
	if srv.Addr() != nil {
		t.Errorf("Addr() = %v after a failed bind, want nil", srv.Addr())
	}
}

// A statistics tick moves more than the island it carries: the receive time
// behind "last frame X ago", the warm-up flag and the island count all live
// in the status, and the pipeline only republishes that on a connection, a
// session or a version change - two minutes apart at best. PublishSnapshot
// therefore publishes the status as well, so a browser between two of those
// events is not looking at a frozen status bar.
func TestPublishSnapshotAlsoPublishesStatus(t *testing.T) {
	st := state.New()
	first := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	st.SetConnection(state.Connection{
		Mode: "replay", State: "replaying", Since: first, ProtocolVersion: 2, LastFrameAt: first,
	})
	fx := fixture{state: st, now: first}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, base, _ := listen(t, ctx, fx, 0)
	s := openStream(t, ctx, base+"/api/v1/events")

	// The catch-up is the status alone: no island has arrived yet.
	event, data := s.nextEvent(t)
	if event != "status" {
		t.Fatalf("catch-up event = %q, want status", event)
	}
	var status statusJSON
	if err := json.Unmarshal([]byte(data), &status); err != nil {
		t.Fatalf("decode the catch-up status: %v", err)
	}
	if status.Islands != 0 || status.WarmingUp {
		t.Fatalf("catch-up status = %d islands, warmingUp=%v, want 0 and false", status.Islands, status.WarmingUp)
	}
	waitFor(t, "the client to be registered", func() bool { return srv.StreamClients() == 1 })

	// The first tick of a session reports every island with no products at
	// all (docs/protocol.md): that is the warm-up, and the status is the only
	// place it shows.
	key := model.IslandKey{SessionGUID: 3245, IslandID: 5}
	empty := model.IslandSnapshot{Key: key, Name: "Juliana", ReceivedAt: first.Add(2 * time.Minute)}
	publish(st, srv, empty)

	if event, _ := s.nextEvent(t); event != "snapshot" {
		t.Fatalf("event after the publication = %q, want snapshot first", event)
	}
	status = nextStatus(t, s)
	if !status.WarmingUp {
		t.Error("warmingUp = false after a tick with no products at all, want true")
	}
	if status.Islands != 1 {
		t.Errorf("status reports %d islands, want 1", status.Islands)
	}
	if status.Connection.LastFrameAt == nil || !status.Connection.LastFrameAt.Equal(empty.ReceivedAt) {
		t.Errorf("connection.lastFrameAt = %v, want the tick's receive time %v",
			status.Connection.LastFrameAt, empty.ReceivedAt)
	}

	// The next tick carries the real numbers. Nothing about the connection or
	// the session changed, so this status exists only because the snapshot
	// published it.
	full := model.IslandSnapshot{
		Key: key, Name: "Juliana", ReceivedAt: first.Add(4 * time.Minute),
		Products: []model.ProductStat{{ProductGUID: 2068, Generation: 1}},
	}
	publish(st, srv, full)

	if event, _ := s.nextEvent(t); event != "snapshot" {
		t.Fatalf("event after the second publication = %q, want snapshot first", event)
	}
	status = nextStatus(t, s)
	if status.WarmingUp {
		t.Error("warmingUp = true after a tick with products, want false")
	}
	if status.Connection.LastFrameAt == nil || !status.Connection.LastFrameAt.Equal(full.ReceivedAt) {
		t.Errorf("connection.lastFrameAt = %v, want the second tick's receive time %v",
			status.Connection.LastFrameAt, full.ReceivedAt)
	}
}

// publish stores a snapshot the way the pipeline does - the receive time
// first, then the island - and hands it to the stream.
func publish(st *state.State, srv *server.Server, snap model.IslandSnapshot) {
	st.UpdateConnection(func(c *state.Connection) { c.LastFrameAt = snap.ReceivedAt })
	st.Put(snap)
	srv.PublishSnapshot(snap)
}

// nextStatus reads the next event, insists it is a status and decodes it.
func nextStatus(t *testing.T, s *stream) statusJSON {
	t.Helper()
	event, data := s.nextEvent(t)
	if event != "status" {
		t.Fatalf("event = %q, want status", event)
	}
	var out statusJSON
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		t.Fatalf("decode the status event %q: %v", data, err)
	}
	return out
}
