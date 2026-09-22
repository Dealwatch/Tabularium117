package server

import (
	"bytes"
	"log/slog"
	"mime"
	"strings"
	"testing"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// A client that stops reading must not be able to hold up the publisher: the
// pipeline is on the other end of a broadcast. The events are dropped,
// counted, and reported once rather than on every event.
func TestSlowClientDropsEventsInsteadOfBlocking(t *testing.T) {
	var logs bytes.Buffer
	h := newHub(slog.New(slog.NewTextHandler(&logs, nil)))
	client := h.add("english", true)
	if client == nil {
		t.Fatal("a fresh hub refused a client")
	}

	const extra = 5
	for i := 0; i < clientQueue+extra; i++ {
		h.broadcast(eventStatus, func(string, bool) ([]byte, error) { return []byte(`{"islands":1}`), nil })
	}

	if got := len(client.out); got != clientQueue {
		t.Errorf("%d events queued, want the queue's %d", got, clientQueue)
	}
	if client.dropped != extra {
		t.Errorf("dropped = %d, want %d", client.dropped, extra)
	}
	if got := strings.Count(logs.String(), "too slow"); got != 1 {
		t.Errorf("the slow client was reported %d times, want exactly one warning:\n%s", got, logs.String())
	}

	h.remove(client)
	if h.count() != 0 {
		t.Errorf("%d clients left after remove", h.count())
	}
}

// Once the hub is closed, the running streams end and no new one is accepted.
func TestClosedHubRefusesClients(t *testing.T) {
	h := newHub(slog.New(slog.DiscardHandler))
	client := h.add("english", true)
	h.closeAll()

	select {
	case <-client.done:
	default:
		t.Error("closeAll did not end the running stream")
	}
	if h.add("english", false) != nil {
		t.Error("a closed hub accepted a new client")
	}
	// Publishing into a closed hub is a no-op, not a panic.
	h.broadcast(eventSnapshot, func(string, bool) ([]byte, error) { return []byte(`{}`), nil })
}

// The island id is a pair, and the session GUID may be negative, so the last
// separator is the one that splits it.
func TestIslandIDRoundTrip(t *testing.T) {
	keys := []model.IslandKey{
		{SessionGUID: 3245, IslandID: 5},
		{SessionGUID: 6627, IslandID: 28},
		{SessionGUID: -3245, IslandID: 11},
		{SessionGUID: 0, IslandID: 0},
	}
	for _, key := range keys {
		id := islandID(key)
		got, ok := parseIslandID(id)
		if !ok || got != key {
			t.Errorf("parseIslandID(%q) = %+v, %v; want %+v", id, got, ok, key)
		}
	}

	for _, bad := range []string{"", "5", "-5", "3245-", "3245-5-7-", "a-5", "3245-b", "3245 5",
		"99999999999999-5", "3245-99999999999999"} {
		if key, ok := parseIslandID(bad); ok {
			t.Errorf("parseIslandID(%q) accepted a malformed id as %+v", bad, key)
		}
	}
}

// TestPinContentTypesOverridesTheMachine is the mechanism behind
// staticContentTypes: registering a type has to win over what the machine's
// own table says, or pinning would be decoration.
//
// The wrong answer used here is a real one - a GitHub Windows runner
// serves every ES module as "application/javascript" from the same binary
// that says "text/javascript" elsewhere, because mime reads the registry
// there. A machine whose registry says "text/plain" would get a
// blank UI, since a browser refuses a module with that type.
func TestPinContentTypesOverridesTheMachine(t *testing.T) {
	if err := mime.AddExtensionType(".js", "application/javascript"); err != nil {
		t.Fatalf("set up the machine's answer: %v", err)
	}
	// Whatever happens below, the rest of the suite - and any test binary
	// that keeps running - gets the pinned types back.
	t.Cleanup(pinContentTypes)

	if got := mime.TypeByExtension(".js"); got != "application/javascript" {
		t.Fatalf("the test could not install the machine's answer: got %q", got)
	}
	pinContentTypes()
	if got := mime.TypeByExtension(".js"); got != staticContentTypes[".js"] {
		t.Errorf("after pinning, .js = %q, want %q: the machine is still deciding",
			got, staticContentTypes[".js"])
	}
}
