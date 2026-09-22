package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/ingest"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/replay"
	"github.com/Dealwatch/Tabularium117/internal/server"
	"github.com/Dealwatch/Tabularium117/internal/state"
	"github.com/Dealwatch/Tabularium117/internal/store"
)

// fixturePath is the re-encoded connector capture (see testdata/README.md).
// It is the only data these tests use: the API is exercised against the same
// frames the decoder and the store are tested with.
const fixturePath = "../../testdata/connector-reencoded.jsonl"

// julianaID is the island the detail endpoints are tested on: 56 products,
// 25 of them in deficit, and product 2068 ("Oats") among them.
const julianaID = "3245-5"

// fixture is a state and a history filled from the capture, plus the clock
// the server is given. The capture carries its own timestamps, so "now" has
// to follow the data rather than the wall clock - otherwise the one-hour
// window would be empty on every machine but one.
type fixture struct {
	state     *state.State
	store     *store.Store
	snapshots []model.IslandSnapshot
	now       time.Time
	// sessionStart is the start of the recorded session, one minute before
	// the first frame.
	sessionStart time.Time
}

// loadFixture replays the capture through the real pipeline into a fresh
// state, and writes the same snapshots into an in-memory history.
func loadFixture(t *testing.T, withStore bool) fixture {
	t.Helper()
	ctx := context.Background()

	st := state.New()
	var snaps []model.IslandSnapshot
	pipeline := &ingest.Pipeline{
		State:      st,
		OnSnapshot: func(snap model.IslandSnapshot) { snaps = append(snaps, snap) },
	}

	f, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("open the capture: %v", err)
	}
	defer f.Close()
	if err := pipeline.Run(ctx, replay.NewReader(f, replay.Options{Speed: 0})); err != nil {
		t.Fatalf("replay the capture: %v", err)
	}
	if len(snaps) == 0 {
		t.Fatal("the capture produced no snapshots")
	}
	st.SetConnection(state.Connection{
		Mode:            "replay",
		State:           "replaying",
		Since:           snaps[0].ReceivedAt,
		ProtocolVersion: 2,
		LastFrameAt:     snaps[len(snaps)-1].ReceivedAt,
	})

	fx := fixture{
		state:        st,
		snapshots:    snaps,
		now:          snaps[len(snaps)-1].ReceivedAt.Add(time.Minute),
		sessionStart: snaps[0].ReceivedAt.Add(-time.Minute),
	}
	if !withStore {
		return fx
	}

	db, err := store.Open(ctx, store.MemoryPath)
	if err != nil {
		t.Fatalf("open the in-memory history: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	headline, _ := st.Session()
	if _, err := db.StartSession(ctx, fx.sessionStart, 2, headline); err != nil {
		t.Fatalf("record the session: %v", err)
	}
	if err := db.WriteSnapshots(ctx, snaps); err != nil {
		t.Fatalf("write the snapshots: %v", err)
	}
	fx.store = db
	return fx
}

// newServer builds a server on the fixture and serves it through httptest.
// The returned URL has no trailing slash.
func newServer(t *testing.T, fx fixture) (*server.Server, string) {
	t.Helper()
	srv := server.New(server.Options{
		State:   fx.state,
		Store:   fx.store,
		Version: "test",
		Now:     func() time.Time { return fx.now },
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts.URL
}

// get performs a GET and returns the response. The caller closes the body.
func get(t *testing.T, url string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build the request for %s: %v", url, err)
	}
	for k, values := range header {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// getJSON performs a GET, checks the status and decodes the body into out.
func getJSON(t *testing.T, url string, want int, out any) {
	t.Helper()
	resp := get(t, url, nil)
	defer resp.Body.Close()

	if resp.StatusCode != want {
		t.Fatalf("GET %s = %d, want %d", url, resp.StatusCode, want)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("GET %s content type = %q", url, got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("GET %s cache control = %q, want no-store", url, got)
	}
	if out == nil {
		return
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

// errorOf decodes the {"error": ...} body of a failed request.
func errorOf(t *testing.T, url string, want int) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	getJSON(t, url, want, &body)
	if body.Error == "" {
		t.Errorf("GET %s returned %d without an error message", url, want)
	}
	return body.Error
}

// decode reads a JSON body that the caller has already checked the status of.
func decode(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s = %d, want 200", resp.Request.URL, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", resp.Request.URL, err)
	}
}

// readAll returns a response body as a string.
func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", resp.Request.URL, err)
	}
	return string(body)
}
