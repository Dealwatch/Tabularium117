package server_test

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/catalog"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/server"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

// metricsCatalog is a catalog small enough to write the expected output by
// hand. The second good's name carries every character the label format has
// to escape.
const metricsCatalog = `{
  "languages": ["english", "german"],
  "products": [
    {"guid": 2068, "names": {"english": "Oats", "german": "Hafer"}},
    {"guid": 2069, "names": {"english": "Whe\"at\\\nx", "german": "Weizen"}}
  ],
  "sessions": [{"guid": 3245, "names": {"english": "Latium", "german": "Latium"}}]
}`

// metricsServer serves st with the hand-written catalog.
func metricsServer(t *testing.T, st *state.State) string {
	t.Helper()
	cat, err := catalog.Parse([]byte(metricsCatalog))
	if err != nil {
		t.Fatalf("parse the test catalog: %v", err)
	}
	cat.Logger = slog.New(slog.DiscardHandler)
	srv := server.New(server.Options{State: st, Catalog: cat, Version: "test"})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts.URL
}

// scrape GETs /metrics, checks the status and the content type, and returns
// the body.
func scrape(t *testing.T, base string) string {
	t.Helper()
	resp := get(t, base+"/metrics", nil)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /metrics: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200:\n%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the Prometheus text format", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store like every other live answer", cc)
	}
	return string(body)
}

// The whole output for a fixed state, byte for byte: values, HELP and TYPE,
// label order, the sort order, the escaping and the unknown good.
func TestMetricsOutput(t *testing.T) {
	st := state.New()
	st.SetConnection(state.Connection{
		Mode:            "pipe",
		State:           "connected",
		ProtocolVersion: 2,
		LastFrameAt:     time.Date(2026, 9, 22, 12, 0, 0, 500_000_000, time.UTC),
	})
	st.Put(model.IslandSnapshot{
		Key: model.IslandKey{SessionGUID: 3245, IslandID: 5},
		// Surrounding blanks are trimmed as everywhere on the display side;
		// the quote, the backslash and the newline are escaped.
		Name: " Juliana \"the\\first\"\nline ",
		// Deliberately out of GUID order.
		Products: []model.ProductStat{
			{ProductGUID: 999999, Generation: 1, Consumption: 0, Delta: 1, PerfectGeneration: 1, Buildings: 1},
			{ProductGUID: 2069, Generation: 3, Consumption: 7.25, Delta: -4.25, PerfectGeneration: 6, Buildings: 3},
			{ProductGUID: 2068, Generation: 13.7, Consumption: 2, Delta: 11.7, PerfectGeneration: 28.1, Buildings: 12},
		},
	})
	st.Put(model.IslandSnapshot{
		Key:      model.IslandKey{SessionGUID: 3245, IslandID: 2},
		Name:     "Ostia",
		Products: []model.ProductStat{{ProductGUID: 2068, Generation: 0, Consumption: 0.5, Delta: -0.5, PerfectGeneration: 0, Buildings: 0}},
	})

	want := `# HELP tabularium117_connection_up Whether the source connection is open: 1 while the pipe is connected or a recording is still playing, otherwise 0. Not a sign of usable statistics: with an unsupported protocol version the pipe stays connected while statistics are ignored.
# TYPE tabularium117_connection_up gauge
tabularium117_connection_up{mode="pipe"} 1
# HELP tabularium117_last_frame_timestamp_seconds Unix time of the most recent frame received from the source. Age: time() - this.
# TYPE tabularium117_last_frame_timestamp_seconds gauge
tabularium117_last_frame_timestamp_seconds 1.7900784005e+09
# HELP tabularium117_protocol_version Protocol version the game announced on this connection.
# TYPE tabularium117_protocol_version gauge
tabularium117_protocol_version 2
# HELP tabularium117_warming_up 1 while the islands are known but none has reported goods yet, as after loading a save; the product series are absent meanwhile.
# TYPE tabularium117_warming_up gauge
tabularium117_warming_up 0
# HELP tabularium117_islands Number of islands in the current picture.
# TYPE tabularium117_islands gauge
tabularium117_islands 2
# HELP tabularium117_island_info Names of an island and its session. The value is always 1.
# TYPE tabularium117_island_info gauge
tabularium117_island_info{session_guid="3245",island_id="2",island_name="Ostia",session_name="Latium"} 1
tabularium117_island_info{session_guid="3245",island_id="5",island_name="Juliana \"the\\first\"\nline",session_name="Latium"} 1
# HELP tabularium117_product_info Name of a good; an unknown GUID is named "#<guid>". The value is always 1.
# TYPE tabularium117_product_info gauge
tabularium117_product_info{product_guid="2068",product_name="Oats"} 1
tabularium117_product_info{product_guid="2069",product_name="Whe\"at\\\nx"} 1
tabularium117_product_info{product_guid="999999",product_name="#999999"} 1
# HELP tabularium117_product_generation_per_minute Production of a good on an island, per minute.
# TYPE tabularium117_product_generation_per_minute gauge
tabularium117_product_generation_per_minute{session_guid="3245",island_id="2",product_guid="2068"} 0
tabularium117_product_generation_per_minute{session_guid="3245",island_id="5",product_guid="2068"} 13.7
tabularium117_product_generation_per_minute{session_guid="3245",island_id="5",product_guid="2069"} 3
tabularium117_product_generation_per_minute{session_guid="3245",island_id="5",product_guid="999999"} 1
# HELP tabularium117_product_consumption_per_minute Consumption of a good on an island, per minute.
# TYPE tabularium117_product_consumption_per_minute gauge
tabularium117_product_consumption_per_minute{session_guid="3245",island_id="2",product_guid="2068"} 0.5
tabularium117_product_consumption_per_minute{session_guid="3245",island_id="5",product_guid="2068"} 2
tabularium117_product_consumption_per_minute{session_guid="3245",island_id="5",product_guid="2069"} 7.25
tabularium117_product_consumption_per_minute{session_guid="3245",island_id="5",product_guid="999999"} 0
# HELP tabularium117_product_balance_per_minute Balance of a good on an island, per minute, as the game reports it; negative is a deficit.
# TYPE tabularium117_product_balance_per_minute gauge
tabularium117_product_balance_per_minute{session_guid="3245",island_id="2",product_guid="2068"} -0.5
tabularium117_product_balance_per_minute{session_guid="3245",island_id="5",product_guid="2068"} 11.7
tabularium117_product_balance_per_minute{session_guid="3245",island_id="5",product_guid="2069"} -4.25
tabularium117_product_balance_per_minute{session_guid="3245",island_id="5",product_guid="999999"} 1
# HELP tabularium117_product_perfect_generation_per_minute Production of a good on an island if every building worked at 100 %, per minute.
# TYPE tabularium117_product_perfect_generation_per_minute gauge
tabularium117_product_perfect_generation_per_minute{session_guid="3245",island_id="2",product_guid="2068"} 0
tabularium117_product_perfect_generation_per_minute{session_guid="3245",island_id="5",product_guid="2068"} 28.1
tabularium117_product_perfect_generation_per_minute{session_guid="3245",island_id="5",product_guid="2069"} 6
tabularium117_product_perfect_generation_per_minute{session_guid="3245",island_id="5",product_guid="999999"} 1
# HELP tabularium117_product_buildings Number of buildings producing a good on an island.
# TYPE tabularium117_product_buildings gauge
tabularium117_product_buildings{session_guid="3245",island_id="2",product_guid="2068"} 0
tabularium117_product_buildings{session_guid="3245",island_id="5",product_guid="2068"} 12
tabularium117_product_buildings{session_guid="3245",island_id="5",product_guid="2069"} 3
tabularium117_product_buildings{session_guid="3245",island_id="5",product_guid="999999"} 1
`
	base := metricsServer(t, st)
	got := scrape(t, base)
	if got != want {
		t.Errorf("/metrics differs from the expected output.\n--- got:\n%s\n--- want:\n%s", got, want)
	}
	// The labels are the key of a time series: the next scrape of the same
	// state has to produce the very same text, not the same set in another
	// order.
	if again := scrape(t, base); again != got {
		t.Error("two scrapes of the same state differ")
	}
}

// Before the game has said anything: the source is waiting, nothing has
// arrived, no version is known. The families that describe the connection
// and the counts still answer; nothing is invented for the rest.
func TestMetricsWhileWaiting(t *testing.T) {
	st := state.New()
	st.SetConnection(state.Connection{Mode: "pipe", State: "waiting"})
	got := scrape(t, metricsServer(t, st))

	for _, line := range []string{
		`tabularium117_connection_up{mode="pipe"} 0`,
		"tabularium117_warming_up 0",
		"tabularium117_islands 0",
	} {
		if !containsLine(got, line) {
			t.Errorf("missing %q in:\n%s", line, got)
		}
	}
	for _, absent := range []string{"tabularium117_last_frame_timestamp_seconds", "tabularium117_protocol_version"} {
		if strings.Contains(got, absent) {
			t.Errorf("%s is present although it was never set:\n%s", absent, got)
		}
	}
	if n := len(samples(t, got)); n != 3 {
		t.Errorf("%d samples while waiting, want 3 (up, warming up, islands):\n%s", n, got)
	}
}

// Right after a save loads, every island reports zero goods for up to two
// minutes (docs/protocol.md). That must not look like production collapsing
// to zero: the product series are absent, and warming_up says why.
func TestMetricsWhileWarmingUp(t *testing.T) {
	st := state.New()
	st.SetConnection(state.Connection{Mode: "pipe", State: "connected", ProtocolVersion: 2})
	st.Put(model.IslandSnapshot{Key: model.IslandKey{SessionGUID: 3245, IslandID: 5}, Name: "Juliana"})
	got := scrape(t, metricsServer(t, st))

	if !containsLine(got, "tabularium117_warming_up 1") {
		t.Errorf("warming_up is not 1:\n%s", got)
	}
	if !containsLine(got, "tabularium117_islands 1") {
		t.Errorf("the island is not counted:\n%s", got)
	}
	for _, s := range samples(t, got) {
		if strings.HasPrefix(s.name, "tabularium117_product_") {
			t.Errorf("product sample %q during the warm-up", s.line)
		}
	}
}

// HEAD answers like GET without the body, and anything that would write is
// refused as everywhere else in this read-only server.
func TestMetricsMethods(t *testing.T) {
	st := state.New()
	st.SetConnection(state.Connection{Mode: "pipe", State: "connected"})
	base := metricsServer(t, st)
	body := scrape(t, base)

	head, err := http.Head(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	headBody, _ := io.ReadAll(head.Body)
	head.Body.Close()
	if head.StatusCode != http.StatusOK || len(headBody) != 0 {
		t.Errorf("HEAD /metrics = %d with %d bytes, want 200 and no body", head.StatusCode, len(headBody))
	}
	if ct := head.Header.Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("HEAD Content-Type = %q", ct)
	}
	if head.ContentLength != int64(len(body)) {
		t.Errorf("HEAD Content-Length = %d, GET sent %d bytes", head.ContentLength, len(body))
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req, _ := http.NewRequest(method, base+"/metrics", strings.NewReader("x"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "GET, HEAD" {
			t.Errorf("%s /metrics = %d (Allow %q), want 405 with Allow: GET, HEAD",
				method, resp.StatusCode, resp.Header.Get("Allow"))
		}
	}
}

// The real capture, through the real pipeline: every line is valid text
// format, every family is declared before it is used, and no series appears
// twice - one duplicate and Prometheus drops the whole scrape.
func TestMetricsFromCapture(t *testing.T) {
	fx := loadFixture(t, false)
	_, base := newServer(t, fx)
	got := scrape(t, base)

	typed := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if m := typeLine.FindStringSubmatch(line); m != nil {
			if m[2] != "gauge" {
				t.Errorf("%s is a %s, want gauge", m[1], m[2])
			}
			typed[m[1]] = true
		}
	}
	seen := map[string]bool{}
	for _, s := range samples(t, got) {
		if !typed[s.name] {
			t.Errorf("sample without a TYPE line before it: %q", s.line)
		}
		if seen[s.series] {
			t.Errorf("duplicate series %s", s.series)
		}
		seen[s.series] = true
	}
	if !containsLine(got, `tabularium117_product_info{product_guid="2068",product_name="Oats"} 1`) {
		t.Error("Oats, the capture's reference good, has no info line")
	}
	if !containsLine(got, "tabularium117_protocol_version 2") {
		t.Error("the protocol version is missing")
	}
	if !strings.Contains(got, `tabularium117_product_balance_per_minute{session_guid="3245",island_id="5",product_guid="2068"} `) {
		t.Error("Juliana's Oats balance is missing")
	}
}

// --- a small reader for the text format ---

var (
	typeLine   = regexp.MustCompile(`^# TYPE ([a-zA-Z_:][a-zA-Z0-9_:]*) (\w+)$`)
	helpLine   = regexp.MustCompile(`^# HELP [a-zA-Z_:][a-zA-Z0-9_:]* .+$`)
	sampleLine = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{(?:[a-zA-Z_][a-zA-Z0-9_]*="(?:[^"\\\n]|\\[\\"n])*",?)*\})? (\S+)$`)
)

type sample struct {
	name, series, line string
}

// samples parses every line of body, failing the test on one that is neither
// a HELP, a TYPE nor a sample.
func samples(t *testing.T, body string) []sample {
	t.Helper()
	var out []sample
	sc := bufio.NewScanner(bytes.NewBufferString(body))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case typeLine.MatchString(line), helpLine.MatchString(line):
		default:
			m := sampleLine.FindStringSubmatch(line)
			if m == nil {
				t.Errorf("not valid text format: %q", line)
				continue
			}
			out = append(out, sample{name: m[1], series: m[1] + m[2], line: line})
		}
	}
	return out
}

func containsLine(body, line string) bool {
	return strings.Contains("\n"+body, "\n"+line+"\n")
}

// The pipeline keeps one entry per good, but state takes whatever it is
// given, and a duplicate series would cost the whole scrape. /metrics applies
// the pipeline's rule once more: the later entry wins.
func TestMetricsKeepTheLaterDuplicate(t *testing.T) {
	st := state.New()
	st.Put(model.IslandSnapshot{
		Key:  model.IslandKey{SessionGUID: 3245, IslandID: 5},
		Name: "Juliana",
		Products: []model.ProductStat{
			{ProductGUID: 2068, Generation: 1},
			{ProductGUID: 2069, Generation: 7},
			{ProductGUID: 2068, Generation: 3},
		},
	})
	got := scrape(t, metricsServer(t, st))

	seen := map[string]bool{}
	for _, s := range samples(t, got) {
		if seen[s.series] {
			t.Errorf("duplicate series %s", s.series)
		}
		seen[s.series] = true
	}
	if !containsLine(got, `tabularium117_product_generation_per_minute{session_guid="3245",island_id="5",product_guid="2068"} 3`) {
		t.Errorf("Oats does not carry the later entry's value 3:\n%s", got)
	}
}

// Renaming an island changes its name label and nothing else. The value
// series are keyed by GUIDs only, so a dashboard keeps its history across a
// rename; the old island_info series simply ends. The name arrives as the
// pipe sends it: umlauts as "_" (docs/protocol.md).
func TestMetricsRenameChangesOnlyTheInfoSeries(t *testing.T) {
	st := state.New()
	key := model.IslandKey{SessionGUID: 3245, IslandID: 5}
	products := []model.ProductStat{{ProductGUID: 2068, Generation: 13.7, Buildings: 12}}
	st.Put(model.IslandSnapshot{Key: key, Name: "Juliana", Products: products})
	base := metricsServer(t, st)
	before := scrape(t, base)

	st.Put(model.IslandSnapshot{Key: key, Name: "R_mische K_ste", Products: products})
	after := scrape(t, base)

	beforeLines, afterLines := strings.Split(before, "\n"), strings.Split(after, "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("the rename changed the number of lines: %d, then %d", len(beforeLines), len(afterLines))
	}
	var changed []string
	for i := range beforeLines {
		if beforeLines[i] != afterLines[i] {
			changed = append(changed, afterLines[i])
		}
	}
	want := `tabularium117_island_info{session_guid="3245",island_id="5",island_name="R_mische K_ste",session_name="Latium"} 1`
	if len(changed) != 1 || changed[0] != want {
		t.Errorf("the rename changed %q, want only the island_info line:\n%s", changed, want)
	}
}
