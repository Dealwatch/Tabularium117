package server

import (
	"bytes"
	"cmp"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// metricsPath serves the live state in the Prometheus text format
// (KONZEPT.md section 5). It is optional tooling for people who already run
// Prometheus; the UI does not use it.
const metricsPath = "/metrics"

// contentTypeMetrics is the Prometheus text exposition format, version 0.0.4.
const contentTypeMetrics = "text/plain; version=0.0.4; charset=utf-8"

// metricsLanguage is the one language the metric labels are resolved in.
// A label that followed Accept-Language would split one good into two time
// series the moment a second browser or a second scraper asked.
const metricsLanguage = "english"

// handleMetrics renders the live state as it is at the moment of the scrape.
//
// There is no loop, cache or history behind it: every value is read from
// state.State when the request arrives, and storing the series over time is
// Prometheus's job. On the LAN listener the endpoint does not exist - a phone
// has no use for it, and it would widen what the token opens.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !isLocal(r) {
		http.NotFound(w, r)
		return
	}
	body := s.metrics()
	w.Header().Set("Content-Type", contentTypeMetrics)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// metrics builds the whole exposition. It is kept apart from the handler so
// that the output is built before the first byte goes out, as writeJSON does.
func (s *Server) metrics() []byte {
	conn := s.state.Connection()
	islands := s.state.Islands()
	e := &exposition{}

	up := "0"
	if conn.Delivering() {
		up = "1"
	}
	e.family("tabularium117_connection_up",
		"Whether the source connection is open: 1 while the pipe is connected or a recording is still playing, otherwise 0. Not a sign of usable statistics: with an unsupported protocol version the pipe stays connected while statistics are ignored.")
	e.sample("tabularium117_connection_up", labels{{"mode", conn.Mode}}, up)

	// A value that has never been set is left out rather than written as 0:
	// a frame at the Unix epoch or protocol version 0 would be a lie a graph
	// cannot tell from the truth.
	if !conn.LastFrameAt.IsZero() {
		e.family("tabularium117_last_frame_timestamp_seconds",
			"Unix time of the most recent frame received from the source. Age: time() - this.")
		e.sample("tabularium117_last_frame_timestamp_seconds", nil, f64(unixSeconds(conn.LastFrameAt)))
	}
	if conn.ProtocolVersion != 0 {
		e.family("tabularium117_protocol_version", "Protocol version the game announced on this connection.")
		e.sample("tabularium117_protocol_version", nil, itoa(conn.ProtocolVersion))
	}

	warm := "0"
	if warmingUp(islands) {
		warm = "1"
	}
	e.family("tabularium117_warming_up",
		"1 while the islands are known but none has reported goods yet, as after loading a save; the product series are absent meanwhile.")
	e.sample("tabularium117_warming_up", nil, warm)

	e.family("tabularium117_islands", "Number of islands in the current picture.")
	e.sample("tabularium117_islands", nil, strconv.Itoa(len(islands)))

	e.family("tabularium117_island_info", "Names of an island and its session. The value is always 1.")
	for _, snap := range islands {
		e.sample("tabularium117_island_info", labels{
			{"session_guid", itoa(snap.Key.SessionGUID)},
			{"island_id", itoa(snap.Key.IslandID)},
			{"island_name", strings.TrimSpace(snap.Name)},
			{"session_name", s.kindName(s.catalog.Session, snap.Key.SessionGUID, metricsLanguage)},
		}, "1")
	}

	// Each island's goods in GUID order, so that the output does not depend
	// on the order the game happened to send them in.
	products := make([][]model.ProductStat, len(islands))
	var guids []int32
	for i, snap := range islands {
		products[i] = sortedProducts(snap.Products)
		for _, p := range products[i] {
			guids = append(guids, p.ProductGUID)
		}
	}
	slices.Sort(guids)
	guids = slices.Compact(guids)

	e.family("tabularium117_product_info",
		"Name of a good; an unknown GUID is named \"#<guid>\". The value is always 1.")
	for _, guid := range guids {
		e.sample("tabularium117_product_info", labels{
			{"product_guid", itoa(guid)},
			{"product_name", s.catalog.Name(guid, metricsLanguage)},
		}, "1")
	}

	perProduct := []struct {
		name, help string
		value      func(model.ProductStat) string
	}{
		{"tabularium117_product_generation_per_minute", "Production of a good on an island, per minute.",
			func(p model.ProductStat) string { return f32(p.Generation) }},
		{"tabularium117_product_consumption_per_minute", "Consumption of a good on an island, per minute.",
			func(p model.ProductStat) string { return f32(p.Consumption) }},
		{"tabularium117_product_balance_per_minute", "Balance of a good on an island, per minute, as the game reports it; negative is a deficit.",
			func(p model.ProductStat) string { return f32(p.Delta) }},
		{"tabularium117_product_perfect_generation_per_minute", "Production of a good on an island if every building worked at 100 %, per minute.",
			func(p model.ProductStat) string { return f32(p.PerfectGeneration) }},
		{"tabularium117_product_buildings", "Number of buildings producing a good on an island.",
			func(p model.ProductStat) string { return itoa(p.Buildings) }},
	}
	for _, m := range perProduct {
		e.family(m.name, m.help)
		for i, snap := range islands {
			for _, p := range products[i] {
				e.sample(m.name, labels{
					{"session_guid", itoa(snap.Key.SessionGUID)},
					{"island_id", itoa(snap.Key.IslandID)},
					{"product_guid", itoa(p.ProductGUID)},
				}, m.value(p))
			}
		}
	}
	return e.buf.Bytes()
}

// sortedProducts returns a copy of products in GUID order. The snapshot
// belongs to state and must not be reordered in place.
//
// The pipeline already keeps one entry per good (ingest.collapseDuplicates),
// but state accepts whatever it is given, and here a duplicate would cost
// more than anywhere else: two samples with the same labels make Prometheus
// reject the whole scrape. So the rule is applied once more, the same way -
// the later entry wins.
func sortedProducts(products []model.ProductStat) []model.ProductStat {
	out := slices.Clone(products)
	slices.SortStableFunc(out, func(a, b model.ProductStat) int {
		return cmp.Compare(a.ProductGUID, b.ProductGUID)
	})
	// The sort is stable, so of equal GUIDs the later entry is the last one
	// of its run.
	kept := out[:0]
	for i, p := range out {
		if i+1 < len(out) && out[i+1].ProductGUID == p.ProductGUID {
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

// unixSeconds is t as a Unix timestamp with millisecond precision.
func unixSeconds(t time.Time) float64 {
	return float64(t.UnixMilli()) / 1000
}

func itoa(v int32) string { return strconv.FormatInt(int64(v), 10) }

// --- the text format ---

// label is one name="value" pair. A slice rather than a map, so the labels
// come out in the order they are written.
type label struct{ name, value string }

type labels []label

// exposition writes the Prometheus text format, version 0.0.4. Every family
// is a gauge: each value is the state at scrape time, none of them counts up.
type exposition struct {
	buf bytes.Buffer
}

func (e *exposition) family(name, help string) {
	e.buf.WriteString("# HELP ")
	e.buf.WriteString(name)
	e.buf.WriteByte(' ')
	e.buf.WriteString(helpEscaper.Replace(help))
	e.buf.WriteString("\n# TYPE ")
	e.buf.WriteString(name)
	e.buf.WriteString(" gauge\n")
}

func (e *exposition) sample(name string, ls labels, value string) {
	e.buf.WriteString(name)
	if len(ls) > 0 {
		e.buf.WriteByte('{')
		for i, l := range ls {
			if i > 0 {
				e.buf.WriteByte(',')
			}
			e.buf.WriteString(l.name)
			e.buf.WriteString(`="`)
			e.buf.WriteString(labelEscaper.Replace(l.value))
			e.buf.WriteByte('"')
		}
		e.buf.WriteByte('}')
	}
	e.buf.WriteByte(' ')
	e.buf.WriteString(value)
	e.buf.WriteByte('\n')
}

// f32 formats a value the game sent as a float32. Formatting it as one keeps
// 13.7 from turning into 13.699999809265137. NaN and the infinities come out
// as "NaN", "+Inf" and "-Inf", which is what the format expects.
func f32(v float32) string { return strconv.FormatFloat(float64(v), 'g', -1, 32) }

// f64 formats every other value. A timestamp must never go through f32: the
// shortest text that reads back as the same float32 is not the same number
// once Prometheus reads it as a float64.
func f64(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// The format escapes backslash and newline in HELP text, and additionally the
// double quote in label values.
var (
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	labelEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
)
