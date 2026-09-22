package alerts

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// maxDropSamples bounds one product's efficiency history. Pruning by time is
// what normally keeps it small, but a recording that is replayed in a loop
// delivers the same timestamps over and over, and a clock that jumps
// backwards would defeat the time bound altogether. This is the backstop that
// turns "unbounded" into "at most a few kilobytes per product".
const maxDropSamples = 512

// Engine keeps the per-product rule state and turns snapshots into events.
//
// It is safe for concurrent use: Apply runs in the ingest goroutine while
// Active is read by HTTP handlers.
type Engine struct {
	cfg Config

	mu      sync.Mutex
	islands map[model.IslandKey]*islandState
	// newest is the newest snapshot time Apply has seen. It is the clock
	// Reset stamps its events with, so that replayed data is not marked with
	// today's date (the same rule the history follows, see
	// store.LastSampleTime).
	newest time.Time
}

type islandState struct {
	name     string
	products map[int32]*productState
}

// productState is one product's rule state on one island.
type productState struct {
	// negative and nonNegative are the deficit rule's streak counters. Only
	// one of them is ever non-zero.
	negative    int
	nonNegative int
	deficit     *Alert

	// samples is the efficiency history the drop rule averages over, oldest
	// first, and drop is its alert.
	samples []efficiencySample
	drop    *Alert
}

// efficiencySample is one efficiency reading, in percent.
type efficiencySample struct {
	at    time.Time
	value float64
}

// New returns an engine configured with cfg. Values that cannot work are
// replaced by the defaults (see Config).
func New(cfg Config) *Engine {
	return &Engine{
		cfg:     cfg.withDefaults(),
		islands: make(map[model.IslandKey]*islandState),
	}
}

// Config reports the thresholds in force, after the defaults were applied.
func (e *Engine) Config() Config { return e.cfg }

// Apply evaluates every product of snap and returns the alerts that were
// raised or cleared by it, ordered by product GUID and then rule so that the
// same input always produces the same output.
//
// A product that is missing from snap is not evaluated and keeps whatever
// state it had (see the package documentation).
func (e *Engine) Apply(snap model.IslandSnapshot) []Event {
	e.mu.Lock()
	defer e.mu.Unlock()

	if snap.ReceivedAt.After(e.newest) {
		e.newest = snap.ReceivedAt
	}

	is := e.islands[snap.Key]
	if is == nil {
		is = &islandState{products: make(map[int32]*productState, len(snap.Products))}
		e.islands[snap.Key] = is
	}
	// The name is stored as delivered and trimmed on the display side
	// (docs/protocol.md); an alert is display.
	is.name = strings.TrimSpace(snap.Name)

	var events []Event
	for _, p := range snap.Products {
		ps := is.products[p.ProductGUID]
		if ps == nil {
			ps = &productState{}
			is.products[p.ProductGUID] = ps
		}
		events = e.applyDeficit(events, snap, is.name, ps, p)
		events = e.applyDrop(events, snap, is.name, ps, p)
	}

	sort.SliceStable(events, func(i, j int) bool {
		a, b := events[i].Alert, events[j].Alert
		if a.ProductGUID != b.ProductGUID {
			return a.ProductGUID < b.ProductGUID
		}
		return a.Rule < b.Rule
	})
	return events
}

// Active returns every open alert, ordered by island, product and rule.
func (e *Engine) Active() []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()

	var out []Alert
	for _, is := range e.islands {
		for _, ps := range is.products {
			if ps.deficit != nil {
				out = append(out, *ps.deficit)
			}
			if ps.drop != nil {
				out = append(out, *ps.drop)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

// Reset drops all state and returns a cleared event for everything that was
// active, in the order Active uses.
//
// It belongs at a session boundary: island identity stops being meaningful
// there (state.Reset does the same), and a streak counted across two
// different savegames would be meaningless. The events are stamped with the
// newest snapshot time the engine has seen, or - having seen none - with the
// wall clock.
func (e *Engine) Reset() []Event {
	e.mu.Lock()
	defer e.mu.Unlock()

	at := e.newest
	if at.IsZero() {
		at = time.Now()
	}

	var open []Alert
	for _, is := range e.islands {
		for _, ps := range is.products {
			if ps.deficit != nil {
				open = append(open, *ps.deficit)
			}
			if ps.drop != nil {
				open = append(open, *ps.drop)
			}
		}
	}
	sort.Slice(open, func(i, j int) bool { return less(open[i], open[j]) })

	events := make([]Event, 0, len(open))
	for _, a := range open {
		a.ClearedAt = at
		events = append(events, Event{Kind: KindCleared, Alert: a})
	}

	e.islands = make(map[model.IslandKey]*islandState)
	e.newest = time.Time{}
	return events
}

// applyDeficit runs the deficit rule for one product.
func (e *Engine) applyDeficit(events []Event, snap model.IslandSnapshot, name string, ps *productState, p model.ProductStat) []Event {
	delta := float64(p.Delta)
	if delta < 0 {
		ps.negative++
		ps.nonNegative = 0
	} else {
		ps.nonNegative++
		ps.negative = 0
	}

	switch {
	case ps.deficit == nil && ps.negative >= e.cfg.DeficitSamples:
		a := Alert{
			Island:      snap.Key,
			IslandName:  name,
			ProductGUID: p.ProductGUID,
			Rule:        RuleDeficit,
			Severity:    SeverityWarning,
			RaisedAt:    snap.ReceivedAt,
			Detail:      deficitDetail(delta, ps.negative),
			Value:       delta,
		}
		ps.deficit = &a
		return append(events, Event{Kind: KindRaised, Alert: a})

	case ps.deficit != nil && ps.nonNegative >= e.cfg.DeficitClearSamples:
		a := *ps.deficit
		ps.deficit = nil
		a.ClearedAt = snap.ReceivedAt
		a.Value = delta
		a.Detail = deficitDetail(delta, ps.nonNegative)
		return append(events, Event{Kind: KindCleared, Alert: a})

	case ps.deficit != nil:
		// Still in deficit: keep the alert's numbers current so that a live
		// view shows the situation now, not the one that raised it.
		ps.deficit.IslandName = name
		ps.deficit.Value = delta
		ps.deficit.Detail = deficitDetail(delta, ps.negative)
	}
	return events
}

// applyDrop runs the productivity_drop rule for one product.
//
// Without a perfect generation there is nothing to be a percentage of, so the
// rule is inactive for that sample: no reading is recorded and an alert that
// is already open stays open until a defined efficiency clears it.
func (e *Engine) applyDrop(events []Event, snap model.IslandSnapshot, name string, ps *productState, p model.ProductStat) []Event {
	if p.PerfectGeneration == 0 {
		return events
	}
	eff := float64(p.Generation) / float64(p.PerfectGeneration) * 100
	if math.IsNaN(eff) || math.IsInf(eff, 0) {
		return events
	}

	ps.samples = prune(ps.samples, snap.ReceivedAt.Add(-e.cfg.DropWindow))
	mean, n := meanOf(ps.samples)
	defer func() { ps.samples = appendSample(ps.samples, efficiencySample{at: snap.ReceivedAt, value: eff}) }()

	if n < e.cfg.MinSamplesForDrop {
		return events
	}
	drop := mean - eff

	switch {
	case ps.drop == nil && drop > e.cfg.DropPercentagePoints:
		a := Alert{
			Island:      snap.Key,
			IslandName:  name,
			ProductGUID: p.ProductGUID,
			Rule:        RuleProductivityDrop,
			Severity:    SeverityWarning,
			RaisedAt:    snap.ReceivedAt,
			Detail:      dropDetail(eff, mean, e.cfg.DropWindow),
			Value:       eff,
		}
		ps.drop = &a
		return append(events, Event{Kind: KindRaised, Alert: a})

	case ps.drop != nil && drop <= e.cfg.DropClearPercentagePoints:
		a := *ps.drop
		ps.drop = nil
		a.ClearedAt = snap.ReceivedAt
		a.Value = eff
		a.Detail = dropDetail(eff, mean, e.cfg.DropWindow)
		return append(events, Event{Kind: KindCleared, Alert: a})

	case ps.drop != nil:
		ps.drop.IslandName = name
		ps.drop.Value = eff
		ps.drop.Detail = dropDetail(eff, mean, e.cfg.DropWindow)
	}
	return events
}

// prune drops every reading older than cutoff. The history is kept oldest
// first, so this is a prefix.
func prune(samples []efficiencySample, cutoff time.Time) []efficiencySample {
	cut := 0
	for cut < len(samples) && samples[cut].at.Before(cutoff) {
		cut++
	}
	if cut == 0 {
		return samples
	}
	return append(samples[:0], samples[cut:]...)
}

// appendSample adds one reading and enforces the length backstop.
func appendSample(samples []efficiencySample, s efficiencySample) []efficiencySample {
	samples = append(samples, s)
	if len(samples) > maxDropSamples {
		samples = append(samples[:0], samples[len(samples)-maxDropSamples:]...)
	}
	return samples
}

// meanOf returns the arithmetic mean of the readings and how many there were.
func meanOf(samples []efficiencySample) (float64, int) {
	if len(samples) == 0 {
		return 0, 0
	}
	var sum float64
	for _, s := range samples {
		sum += s.value
	}
	return sum / float64(len(samples)), len(samples)
}

// deficitDetail is the English one-liner shown next to the translated rule
// name. The UI does not parse it.
func deficitDetail(delta float64, samples int) string {
	return fmt.Sprintf("delta %.1f for %d samples", delta, samples)
}

// dropDetail is the productivity rule's one-liner.
func dropDetail(eff, mean float64, window time.Duration) string {
	return fmt.Sprintf("efficiency %.0f%% vs %s mean %.0f%%", eff, windowLabel(window), mean)
}

// windowLabel names the trailing window the way a person would, e.g. "5-min".
func windowLabel(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%d-min", int(d/time.Minute))
	}
	return d.String()
}
