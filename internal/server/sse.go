package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/model"
)

// clientQueue is how many events may wait for one stream client. A browser
// that cannot keep up with a handful of islands per tick is not going to
// catch up later, so the queue is small on purpose: it bounds the memory one
// stuck client can cost, and the UI recovers by refetching.
const clientQueue = 32

// eventSnapshot and friends are the SSE event names the UI listens for.
const (
	eventSnapshot = "snapshot"
	eventStatus   = "status"
	eventAlert    = "alert"
)

// heartbeatFrame is a comment line: it keeps the connection alive without
// being delivered to any listener in the browser.
var heartbeatFrame = []byte(": ping\n\n")

// streamClient is one connected event stream.
//
// out is never closed. Ending a stream closes done instead, so that a
// publisher holding the hub's lock can never write to a channel that the
// handler has already abandoned.
type streamClient struct {
	lang string
	// local is whether this stream came in over the loopback listener. The
	// status event says so, and the UI decides by it whether to offer the
	// LAN panel at all.
	local bool
	out   chan []byte
	done  chan struct{}

	// dropped and warned are guarded by the hub's mutex.
	dropped int
	warned  bool
}

// hub fans events out to the connected stream clients.
type hub struct {
	log *slog.Logger

	mu      sync.Mutex
	clients map[*streamClient]struct{}
	closed  bool
}

func newHub(log *slog.Logger) *hub {
	return &hub{log: log, clients: make(map[*streamClient]struct{})}
}

// add registers a client that wants its events in lang, arriving over the
// loopback listener or not. It returns nil once the hub is closed, which
// tells the handler there is nothing to serve.
func (h *hub) add(lang string, local bool) *streamClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	c := &streamClient{lang: lang, local: local, out: make(chan []byte, clientQueue), done: make(chan struct{})}
	h.clients[c] = struct{}{}
	return c
}

// remove forgets a client whose request has ended.
func (h *hub) remove(c *streamClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	if c.dropped > 0 {
		h.log.Warn("event stream client disconnected after dropped events", "dropped", c.dropped)
	}
}

// count reports how many clients are connected.
func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// closeAll ends every stream and refuses new ones. It is what turns a
// graceful shutdown into a quick one.
func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for c := range h.clients {
		close(c.done)
		delete(h.clients, c)
	}
}

// frameKey is what makes two clients share one rendered frame: the language
// they asked for and the listener they arrived on. Only the status event
// actually differs by side, but keying both costs nothing and keeps the
// rendering honest.
type frameKey struct {
	lang  string
	local bool
}

// broadcast sends one event to every client.
//
// render is called at most once per (language, side) in use, because the same
// event is the same bytes for every client in that group. A client whose
// queue is full loses the event rather than holding up the publisher - the
// pipeline is on the other end of this call and must never block.
func (h *hub) broadcast(name string, render func(lang string, local bool) ([]byte, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.clients) == 0 {
		return
	}

	frames := make(map[frameKey][]byte, 2)
	for c := range h.clients {
		key := frameKey{lang: c.lang, local: c.local}
		frame, ok := frames[key]
		if !ok {
			data, err := render(c.lang, c.local)
			if err != nil {
				h.log.Error("cannot serialise an event", "err", err, "event", name)
				return
			}
			frame = sseFrame(name, data)
			frames[key] = frame
		}
		select {
		case c.out <- frame:
		default:
			c.dropped++
			if !c.warned {
				c.warned = true
				h.log.Warn("event stream client is too slow, dropping events", "event", name)
			}
		}
	}
}

// sseFrame formats one event. data must not contain a newline, which JSON
// encoding guarantees.
func sseFrame(name string, data []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(name) + len(data) + 16)
	buf.WriteString("event: ")
	buf.WriteString(name)
	buf.WriteString("\ndata: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	return buf.Bytes()
}

// PublishSnapshot tells the connected clients that one island changed. It is
// wired to the ingest pipeline and must not block it.
//
// The status follows the snapshot, because a statistics tick moves more than
// the island: the receive time behind "last frame X ago", the warm-up flag
// and the island count all change with it, and the pipeline's own OnStatus
// only fires on connection, session and version changes. The status is a
// small object and the browser needs it to stay honest between two of those
// rare events, so one extra event per published snapshot is the cheap answer.
//
// The order matters: the snapshot first, so that a client which reconciles
// its island list against status.islands sees the new island before it is
// counted, instead of refetching the whole list for every arrival.
func (s *Server) PublishSnapshot(snap model.IslandSnapshot) {
	s.hub.broadcast(eventSnapshot, func(lang string, _ bool) ([]byte, error) {
		return json.Marshal(s.islandSummary(snap, lang))
	})
	s.PublishStatus()
}

// PublishAlert tells the connected clients that an alert was raised or
// cleared. It is wired to the ingest pipeline through the rule engine and
// must not block it.
func (s *Server) PublishAlert(ev alerts.Event) {
	s.hub.broadcast(eventAlert, func(lang string, _ bool) ([]byte, error) {
		return json.Marshal(alertEventDTO{Kind: ev.Kind, Alert: s.alert(ev.Alert, lang)})
	})
}

// PublishStatus tells the connected clients that the connection, the session,
// the protocol version or LAN mode changed. The status differs per side (it
// carries lan.local), so it is rendered per side rather than once.
func (s *Server) PublishStatus() {
	s.hub.broadcast(eventStatus, func(_ string, local bool) ([]byte, error) {
		return json.Marshal(s.status(local))
	})
}

// StreamClients reports how many event streams are currently connected. It
// exists so that tests and future diagnostics can see the fan-out.
func (s *Server) StreamClients() int { return s.hub.count() }

// handleEvents serves the SSE stream. A connecting client is caught up with
// the current status, every known island and every open alert before it sees
// any live event, so that a browser opened late still shows the whole
// picture.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	lang := s.language(r)
	local := isLocal(r)
	client := s.hub.add(lang, local)
	if client == nil {
		writeError(w, http.StatusServiceUnavailable, "the server is shutting down")
		return
	}
	defer s.hub.remove(client)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	// Tell a reverse proxy not to buffer the stream. Tabularium 117 serves itself,
	// but a user may well put it behind something.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	rc.Flush()

	// The client is registered before the catch-up is built, so an event
	// that arrives meanwhile is queued rather than lost; it is delivered
	// after the catch-up and, being newer, wins.
	if !s.writeCatchUp(w, rc, lang, local) {
		return
	}

	heartbeat := time.NewTicker(s.heartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-client.done:
			return
		case frame := <-client.out:
			if _, err := w.Write(frame); err != nil {
				return
			}
			rc.Flush()
		case <-heartbeat.C:
			if _, err := w.Write(heartbeatFrame); err != nil {
				return
			}
			rc.Flush()
		}
	}
}

// writeCatchUp sends the current status, one snapshot event per known island
// and one raised alert event per open alert. It reports whether the client is
// still there.
//
// The alerts come last: a client that applies them in order has the islands
// they refer to by the time it sees them.
func (s *Server) writeCatchUp(w io.Writer, rc *http.ResponseController, lang string, local bool) bool {
	status, err := json.Marshal(s.status(local))
	if err != nil {
		s.logError("cannot serialise the status", err)
		return false
	}
	if _, err := w.Write(sseFrame(eventStatus, status)); err != nil {
		return false
	}
	for _, snap := range s.state.Islands() {
		data, err := json.Marshal(s.islandSummary(snap, lang))
		if err != nil {
			s.logError("cannot serialise an island", err, "island", islandID(snap.Key))
			continue
		}
		if _, err := w.Write(sseFrame(eventSnapshot, data)); err != nil {
			return false
		}
	}
	for _, a := range s.activeAlerts() {
		data, err := json.Marshal(alertEventDTO{Kind: alerts.KindRaised, Alert: s.alert(a, lang)})
		if err != nil {
			s.logError("cannot serialise an alert", err, "island", islandID(a.Island), "rule", a.Rule)
			continue
		}
		if _, err := w.Write(sseFrame(eventAlert, data)); err != nil {
			return false
		}
	}
	rc.Flush()
	return true
}
