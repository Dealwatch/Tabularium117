package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeBuffer is a buffer the test reads while run's goroutines write to it.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// serverURL matches the line the run logs once the listener is open. It is
// how the test learns the port that --port 0 picked.
var serverURL = regexp.MustCompile(`msg="serving the user interface" url=(\S+)`)

// waitForURL waits until the run has logged the URL it serves.
func waitForURL(t *testing.T, log *safeBuffer) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := serverURL.FindStringSubmatch(log.String()); m != nil {
			return m[1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the run never reported a URL:\n%s", log.String())
	return ""
}

// --serve-after-replay keeps the UI up once the recording has ended, so that
// replayed data can be looked at in the browser. The console summary of task
// the replay must still be printed, and Ctrl+C must still end the run cleanly.
func TestServeAfterReplayKeepsServing(t *testing.T) {
	cfg := parse(t, "--replay", fixturePath, "--replay-speed", "0", "--data-dir", t.TempDir(),
		"--port", "0", "--no-browser", "--serve-after-replay")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout, stderr := &safeBuffer{}, &safeBuffer{}
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, stdout, stderr) }()

	url := waitForURL(t, stderr)

	// The replay ends by itself, but the run keeps serving.
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(stdout.String(), "14 islands, 423 products") {
		if time.Now().After(deadline) {
			t.Fatalf("the replay summary never appeared:\n%s", stdout.String())
		}
		select {
		case err := <-done:
			t.Fatalf("the run ended although it was told to keep serving: %v", err)
		case <-time.After(5 * time.Millisecond):
		}
	}

	var islands []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Products int    `json:"products"`
	}
	resp, err := http.Get(url + "api/v1/islands")
	if err != nil {
		t.Fatalf("GET the island list: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET the island list = %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&islands); err != nil {
		t.Fatalf("decode the island list: %v", err)
	}
	if len(islands) != 14 {
		t.Errorf("the UI shows %d islands, want the replayed 14", len(islands))
	}

	// The UI itself is served from the executable, without any build step.
	page, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET the UI: %v", err)
	}
	defer page.Body.Close()
	body, _ := io.ReadAll(page.Body)
	if page.StatusCode != http.StatusOK || !strings.Contains(string(body), "Tabularium 117") {
		t.Errorf("GET / = %d, body:\n%s", page.StatusCode, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Ctrl+C must end the run cleanly, got: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not stop after the context was cancelled")
	}

	// Once the run is over, nothing is listening any more.
	if resp, err := http.Get(url + "api/v1/status"); err == nil {
		resp.Body.Close()
		t.Error("the server is still serving after the run returned")
	}
}

// Without --serve-after-replay the process still exits when the replay ends;
// the server goes with it.
func TestReplayWithoutServeAfterReplayStillExits(t *testing.T) {
	cfg := runCfg(t, "--replay", fixturePath, "--replay-speed", "0", "--data-dir", t.TempDir())

	stdout, stderr := &safeBuffer{}, &safeBuffer{}
	done := make(chan error, 1)
	go func() { done <- run(context.Background(), cfg, stdout, stderr) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v\n%s", err, stderr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the run did not exit when the replay ended")
	}
	if !strings.Contains(stdout.String(), "14 islands, 423 products") {
		t.Errorf("the replay summary is missing:\n%s", stdout.String())
	}
}

// A port that is already in use must be a clear failure, which main turns
// into exit code 2.
func TestPortInUseIsReported(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	_, port, err := net.SplitHostPort(busy.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	cfg := parse(t, "--replay", fixturePath, "--replay-speed", "0", "--data-dir", t.TempDir(),
		"--no-browser", "--port", port)
	err = run(context.Background(), cfg, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("a port that is in use must fail the run")
	}
	if !strings.Contains(err.Error(), "cannot serve on") || !strings.Contains(err.Error(), "--port") {
		t.Errorf("error = %v, want a clear message that names the way out", err)
	}
}

// The browser is opened with the platform's own helper, and the URL is always
// the last argument. Nothing here starts a browser.
func TestBrowserCommand(t *testing.T) {
	name, args := browserCommand("http://127.0.0.1:53118/")
	if name == "" || len(args) == 0 {
		t.Fatalf("browserCommand = %q %v", name, args)
	}
	if args[len(args)-1] != "http://127.0.0.1:53118/" {
		t.Errorf("browserCommand = %q %v, want the URL as the last argument", name, args)
	}
}

// Once a recording has ended, nothing is being delivered any more, even
// though --serve-after-replay keeps the UI up. The status has to say so, and
// tabularium117_connection_up has to drop to 0: a Prometheus alert on a dead
// source must not be fooled by a server that merely stayed open.
func TestServeAfterReplayReportsTheEnd(t *testing.T) {
	cfg := parse(t, "--replay", fixturePath, "--replay-speed", "0", "--data-dir", t.TempDir(),
		"--port", "0", "--no-browser", "--serve-after-replay")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout, stderr := &safeBuffer{}, &safeBuffer{}
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, stdout, stderr) }()
	url := waitForURL(t, stderr)

	// The summary is printed after the replay has ended.
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(stdout.String(), "14 islands, 423 products") {
		if time.Now().After(deadline) {
			t.Fatalf("the replay summary never appeared:\n%s", stdout.String())
		}
		time.Sleep(5 * time.Millisecond)
	}

	var status struct {
		Connection struct {
			Mode  string `json:"mode"`
			State string `json:"state"`
		} `json:"connection"`
		Islands int `json:"islands"`
	}
	resp, err := http.Get(url + "api/v1/status")
	if err != nil {
		t.Fatalf("GET the status: %v", err)
	}
	err = json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("decode the status: %v", err)
	}
	if status.Connection.Mode != "replay" || status.Connection.State != "ended" {
		t.Errorf("connection = %+v after the replay, want mode replay, state ended", status.Connection)
	}
	if status.Islands != 14 {
		t.Errorf("islands = %d, want the replayed 14 to stay visible", status.Islands)
	}

	metrics, err := http.Get(url + "metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	body, _ := io.ReadAll(metrics.Body)
	metrics.Body.Close()
	if !strings.Contains(string(body), "\ntabularium117_connection_up{mode=\"replay\"} 0\n") {
		t.Errorf("connection_up is not 0 after the replay ended:\n%s", firstLines(string(body), 6))
	}
	if !strings.Contains(string(body), "\ntabularium117_islands 14\n") {
		t.Error("the replayed islands are gone from /metrics")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Ctrl+C must end the run cleanly, got: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not stop after the context was cancelled")
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	return strings.Join(lines[:min(n, len(lines))], "\n")
}
