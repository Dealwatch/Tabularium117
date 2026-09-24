package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/replay"
	"github.com/Dealwatch/Tabularium117/internal/source"
	"github.com/Dealwatch/Tabularium117/internal/store"
)

// fixturePath is the re-encoded connector capture (see testdata/README.md).
const fixturePath = "../../testdata/connector-reencoded.jsonl"

// tunicsGUID is the tunics product, which Juliana consumes in the capture
// without a building of its own for it.
const tunicsGUID = 2141

// parse builds a config the way main does, failing the test on a bad line.
func parse(t *testing.T, args ...string) config {
	t.Helper()
	cfg, err := parseFlags(args, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags(%v): %v", args, err)
	}
	return cfg
}

// runCfg is parse for the tests that actually call run: they must never open
// a browser on the machine running the suite, and they must not fight over
// the default port with a Tabularium 117 that happens to be running - so they take
// any free port.
func runCfg(t *testing.T, args ...string) config {
	t.Helper()
	return parse(t, append([]string{"--port", "0", "--no-browser"}, args...)...)
}

// TestReplayAcceptance is what a replay has to do: replaying the
// fixture reports the islands and their product counts on the console.
func TestReplayAcceptance(t *testing.T) {
	cfg := runCfg(t, "--replay", fixturePath, "--replay-speed", "0", "--data-dir", t.TempDir())

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), cfg, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "14 islands, 423 products") {
		t.Errorf("summary totals missing from the output:\n%s", out)
	}
	if !strings.Contains(out, `island "Juliana" session=3245 id=5 products=56`) {
		t.Errorf("per-island line missing from the output:\n%s", out)
	}
	// A name is stored as delivered, including the trailing space.
	if !strings.Contains(out, `island "Zycada " session=3245 id=13 products=0`) {
		t.Errorf("island with no products missing from the output:\n%s", out)
	}
	if !strings.Contains(out, `session "reencoded from connector capture"`) {
		t.Errorf("session headline missing from the summary:\n%s", out)
	}
	// The table is ordered by session and then island.
	if first, second := strings.Index(out, "6627     4"), strings.Index(out, "3245     36"); first < second {
		t.Errorf("summary rows are not ordered by (SessionGUID, IslandID):\n%s", out)
	}
}

func TestRecordRoundTrip(t *testing.T) {
	recording := t.TempDir() + "/capture.jsonl"
	cfg := runCfg(t, "--replay", fixturePath, "--replay-speed", "0",
		"--record", recording, "--data-dir", t.TempDir())

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), cfg, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}

	original := readRecording(t, fixturePath)
	again := readRecording(t, recording)
	if len(again) != len(original) {
		t.Fatalf("recorded %d frames, want %d", len(again), len(original))
	}
	for i := range original {
		if !bytes.Equal(again[i].Payload, original[i].Payload) {
			t.Fatalf("recorded frame %d differs from the replayed one", i)
		}
	}
}

func TestRecordTruncatesExistingFile(t *testing.T) {
	recording := t.TempDir() + "/capture.jsonl"
	if err := os.WriteFile(recording, []byte("stale content that must not survive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := runCfg(t, "--replay", fixturePath, "--replay-speed", "0",
		"--record", recording, "--data-dir", t.TempDir())

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), cfg, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}
	if data, err := os.ReadFile(recording); err != nil {
		t.Fatal(err)
	} else if strings.Contains(string(data), "stale content") {
		t.Error("--record did not truncate the existing file")
	}
}

func TestCancelledRunShutsDownCleanly(t *testing.T) {
	// Real pacing plus --replay-loop: the replay would never end on its own,
	// so only the cancellation can stop it.
	cfg := runCfg(t, "--replay", fixturePath, "--replay-loop", "--data-dir", t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout, stderr := &safeBuffer{}, &safeBuffer{}
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, stdout, stderr) }()

	// Cancel once the run is actually up rather than after a fixed delay: a
	// delay races against opening SQLite, which on a loaded machine can win
	// and turn this into a test of start-up instead. Waiting for the URL is
	// what the other tests in this package do, and it tests what the name
	// promises - a run that is serving and is then stopped.
	waitForURL(t, stderr)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a cancelled run must exit cleanly, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop after the context was cancelled")
	}
	if !strings.Contains(stdout.String(), "islands,") {
		t.Errorf("no summary was printed on shutdown:\n%s", stdout.String())
	}
}

// Cancelling during start-up is a shutdown too. The database is the step
// that takes long enough to be interrupted, and SQLite's own error for it
// reads like a fault ("context deadline exceeded") - run must not pass that
// on as an exit code, or a Ctrl+C at the wrong moment looks like a crash.
func TestCancelledDuringStartupIsNotAFailure(t *testing.T) {
	cfg := runCfg(t, "--replay", fixturePath, "--replay-loop", "--data-dir", t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already over before run gets to look at anything

	stdout, stderr := &safeBuffer{}, &safeBuffer{}
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, stdout, stderr) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a run cancelled during start-up must exit cleanly, got: %v\n%s", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not stop although its context was cancelled before it started")
	}
}

func TestMissingRecordingIsReported(t *testing.T) {
	cfg := runCfg(t, "--replay", t.TempDir()+"/nope.jsonl", "--data-dir", t.TempDir())
	err := run(context.Background(), cfg, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "open recording") {
		t.Fatalf("run err = %v, want a clear 'open recording' failure", err)
	}
}

func TestDataDirIsCreated(t *testing.T) {
	dir := t.TempDir() + "/nested/Tabularium117"
	cfg := runCfg(t, "--replay", fixturePath, "--replay-speed", "0", "--data-dir", dir)
	if err := run(context.Background(), cfg, io.Discard, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("data directory %s was not created: %v", dir, err)
	}
}

func TestFlagValidation(t *testing.T) {
	cases := map[string][]string{
		"port too large":       {"--port", "70000"},
		"negative port":        {"--port", "-1"},
		"negative speed":       {"--replay-speed", "-1"},
		"loop without replay":  {"--replay-loop"},
		"serve without replay": {"--serve-after-replay"},
		"stray argument":       {"extra"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseFlags(args, io.Discard); err == nil {
				t.Fatalf("parseFlags(%v) accepted an invalid command line", args)
			}
		})
	}
}

func TestFlagDefaults(t *testing.T) {
	cfg := parse(t)
	if cfg.port != defaultPort {
		t.Errorf("default port = %d, want %d", cfg.port, defaultPort)
	}
	if cfg.replaySpeed != 1 {
		t.Errorf("default replay speed = %v, want 1", cfg.replaySpeed)
	}
	if cfg.replayPath != "" || cfg.recordPath != "" || cfg.lan || cfg.noBrowser {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	// --lan is accepted long before it does anything, so that the documented
	// command line does not change later.
	cfg = parse(t, "--port", "8080", "--lan", "--no-browser")
	if cfg.port != 8080 || !cfg.lan || !cfg.noBrowser {
		t.Errorf("flags not parsed: %+v", cfg)
	}
	// Port 0 means "any free port"; it is what the tests bind with.
	if cfg = parse(t, "--port", "0"); cfg.port != 0 {
		t.Errorf("--port 0 was not accepted: %+v", cfg)
	}
}

func TestResolveDataDir(t *testing.T) {
	dir, err := resolveDataDir("")
	if err != nil {
		t.Fatalf("resolveDataDir(\"\"): %v", err)
	}
	if !strings.HasSuffix(dir, "Tabularium117") {
		t.Errorf("default data dir = %q, want it to end in Tabularium117", dir)
	}
	// The separator is the host's, so the expectation is built with
	// filepath rather than spelled out: on Windows an absolute path
	// starts with a drive letter and has no leading slash.
	rel := filepath.Join("relative", "path")
	if dir, err = resolveDataDir(rel); err != nil {
		t.Fatalf("resolveDataDir: %v", err)
	}
	if !strings.HasSuffix(dir, rel) || !filepath.IsAbs(dir) {
		t.Errorf("--data-dir was not made absolute: %q", dir)
	}
}

// readRecording replays a JSONL file into a slice of frames.
func readRecording(t *testing.T, path string) []source.Frame {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	out := make(chan source.Frame)
	done := make(chan error, 1)
	go func() { done <- replay.NewReader(f, replay.Options{Speed: 0}).Run(context.Background(), out) }()

	var frames []source.Frame
	for {
		select {
		case fr := <-out:
			frames = append(frames, fr)
		case err := <-done:
			if err != nil {
				t.Fatalf("replay %s: %v", path, err)
			}
			return frames
		case <-time.After(5 * time.Second):
			t.Fatalf("replay of %s did not finish", path)
		}
	}
}

// Replaying with a data directory has to leave a usable history behind: the
// fixture's 14 islands, their samples, and a closed session row. This is how
// development gets real history data without the game.
func TestReplayWritesHistory(t *testing.T) {
	dir := t.TempDir()
	cfg := runCfg(t, "--replay", fixturePath, "--replay-speed", "0", "--data-dir", dir)

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), cfg, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}
	// The console summary must survive the extra callback.
	if !strings.Contains(stdout.String(), "14 islands, 423 products") {
		t.Fatalf("the replay summary disappeared:\n%s", stdout.String())
	}

	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(dir, "tabularium117.db"))
	if err != nil {
		t.Fatalf("open the history written by the run: %v", err)
	}
	defer s.Close()

	islands, err := s.Islands(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(islands) != 14 {
		t.Fatalf("history holds %d islands, want the fixture's 14", len(islands))
	}

	// One island's samples, to prove the products landed too.
	var juliana store.IslandRow
	for _, island := range islands {
		if island.Key.SessionGUID == 3245 && island.Key.IslandID == 5 {
			juliana = island
		}
	}
	if juliana.Name != "Juliana" {
		t.Fatalf("island (3245,5) = %+v, want Juliana", juliana)
	}
	// Product 2063 is one of the 56 the fixture reports for Juliana.
	points, err := s.History(ctx, juliana.Key, 2063,
		juliana.FirstSeen.Add(-time.Hour), juliana.LastSeen.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 {
		t.Error("no samples were stored for a product of Juliana")
	}

	// The session the recording announced is recorded and closed cleanly.
	sessions, err := s.Sessions(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %+v, want the one the recording announced", sessions)
	}
	if sessions[0].Headline != "reencoded from connector capture" {
		t.Errorf("session headline = %q", sessions[0].Headline)
	}
	if sessions[0].EndedAt.IsZero() {
		t.Error("a clean shutdown must close the session it opened")
	}
}

// --no-db is for users who only want the live view, and for tests: it must
// leave no database behind at all.
func TestNoDBWritesNoDatabase(t *testing.T) {
	dir := t.TempDir()
	cfg := runCfg(t, "--replay", fixturePath, "--replay-speed", "0", "--data-dir", dir, "--no-db")

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), cfg, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "14 islands, 423 products") {
		t.Fatalf("--no-db changed the console output:\n%s", stdout.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("--no-db left %q in the data directory", e.Name())
	}
}

// islandLine matches the replay console printer's output, which is the record
// of what the pipeline actually delivered.
var islandLine = regexp.MustCompile(`island "[^"]*" session=(-?\d+) id=(-?\d+) products=\d+`)

// Ctrl+C must not cost the snapshots that are still queued. The batch writer
// is only stopped after the pipeline has returned, so everything the printer
// saw has to be in the database - including the islands delivered after the
// last periodic flush.
func TestShutdownKeepsSnapshotsAlreadyDelivered(t *testing.T) {
	dir := t.TempDir()
	// Recorded pace: one frame per second, so cancelling part-way through
	// leaves snapshots that no interval flush has written yet.
	cfg := runCfg(t, "--replay", fixturePath, "--data-dir", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	if err := run(ctx, cfg, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}

	delivered := map[[2]int64]bool{}
	for _, m := range islandLine.FindAllStringSubmatch(stdout.String(), -1) {
		session, _ := strconv.ParseInt(m[1], 10, 64)
		island, _ := strconv.ParseInt(m[2], 10, 64)
		delivered[[2]int64{session, island}] = true
	}
	if len(delivered) < 2 {
		t.Fatalf("only %d islands were delivered before the shutdown; "+
			"the test cannot prove anything:\n%s", len(delivered), stdout.String())
	}

	s, err := store.Open(context.Background(), filepath.Join(dir, "tabularium117.db"))
	if err != nil {
		t.Fatalf("open the history written by the run: %v", err)
	}
	defer s.Close()
	islands, err := s.Islands(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	stored := map[[2]int64]bool{}
	for _, island := range islands {
		stored[[2]int64{int64(island.Key.SessionGUID), int64(island.Key.IslandID)}] = true
	}
	for k := range delivered {
		if !stored[k] {
			t.Errorf("island (session %d, id %d) was delivered to the console "+
				"but is missing from the database", k[0], k[1])
		}
	}
	if t.Failed() {
		t.Logf("%d delivered, %d stored", len(delivered), len(stored))
	}
}

// The rule engine is wired into the same callback chain as the console
// printer and the history, and its events reach the database. One negative
// measurement is enough here, so that the sixteen-frame fixture produces
// alerts without being looped.
func TestReplayRaisesAndRecordsAlerts(t *testing.T) {
	dir := t.TempDir()
	cfg := runCfg(t, "--replay", fixturePath, "--replay-speed", "0",
		"--alert-deficit-samples", "1", "--data-dir", dir)

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), cfg, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}
	// The earlier consumers in the chain must still see everything.
	if !strings.Contains(stdout.String(), "14 islands, 423 products") {
		t.Fatalf("the replay summary disappeared:\n%s", stdout.String())
	}

	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(dir, "tabularium117.db"))
	if err != nil {
		t.Fatalf("open the history: %v", err)
	}
	defer s.Close()

	// Opening closes what the finished run left open, so the question is
	// what was recorded, not what is still active.
	rows, err := s.Alerts(ctx, false, 0)
	if err != nil {
		t.Fatalf("Alerts: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("no alerts were recorded\n%s", stderr.String())
	}
	found := false
	for _, r := range rows {
		if r.Island.SessionGUID != 3245 || r.Island.IslandID != 5 || r.ProductGUID != 2068 {
			continue
		}
		found = true
		if r.Rule != alerts.RuleDeficit {
			t.Errorf("rule = %q, want %q", r.Rule, alerts.RuleDeficit)
		}
		if r.IslandName != "Juliana" {
			t.Errorf("island name = %q, want Juliana", r.IslandName)
		}
		if r.Detail == "" {
			t.Error("the recorded alert has no detail")
		}
	}
	if !found {
		t.Errorf("no deficit alert for Juliana's oats among %d recorded alerts", len(rows))
	}

	// Juliana consumes tunics but has no building for them (8.4/min, 0
	// buildings in the capture): that is an import, recorded as info.
	var tunics *store.AlertRow
	for i, r := range rows {
		if r.Island == (model.IslandKey{SessionGUID: 3245, IslandID: 5}) && r.ProductGUID == tunicsGUID {
			tunics = &rows[i]
		}
	}
	switch {
	case tunics == nil:
		t.Errorf("no alert for Juliana's tunics among %d recorded alerts", len(rows))
	case tunics.Rule != alerts.RuleImport || tunics.Severity != alerts.SeverityInfo:
		t.Errorf("tunics: rule/severity = %q/%q, want %q/%q",
			tunics.Rule, tunics.Severity, alerts.RuleImport, alerts.SeverityInfo)
	}
}

// Without a matching threshold the same fixture raises nothing: one
// measurement per island is all it delivers, and the default rule needs
// three.
func TestReplayRaisesNoAlertsWithTheDefaultThreshold(t *testing.T) {
	dir := t.TempDir()
	cfg := runCfg(t, "--replay", fixturePath, "--replay-speed", "0", "--data-dir", dir)
	if err := run(context.Background(), cfg, io.Discard, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(dir, "tabularium117.db"))
	if err != nil {
		t.Fatalf("open the history: %v", err)
	}
	defer s.Close()
	rows, err := s.Alerts(ctx, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("%d alerts were recorded, want none: one sample cannot satisfy a three-sample rule", len(rows))
	}
}

func TestAlertFlags(t *testing.T) {
	cfg := parse(t)
	if cfg.alertDeficitSamples != 3 {
		t.Errorf("default --alert-deficit-samples = %d, want 3", cfg.alertDeficitSamples)
	}
	if cfg.alertDropPP != 20 {
		t.Errorf("default --alert-drop-pp = %v, want 20", cfg.alertDropPP)
	}
	cfg = parse(t, "--alert-deficit-samples", "5", "--alert-drop-pp", "12.5")
	if cfg.alertDeficitSamples != 5 || cfg.alertDropPP != 12.5 {
		t.Errorf("alert flags not parsed: %+v", cfg)
	}

	for name, args := range map[string][]string{
		"zero samples":     {"--alert-deficit-samples", "0"},
		"negative samples": {"--alert-deficit-samples", "-1"},
		"zero drop":        {"--alert-drop-pp", "0"},
		"negative drop":    {"--alert-drop-pp", "-5"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseFlags(args, io.Discard); err == nil {
				t.Fatalf("parseFlags(%v) accepted an invalid threshold", args)
			}
		})
	}
}
