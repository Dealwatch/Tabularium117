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

// maxDropSamples bounds one product's productivity history. Pruning by time is
// what normally keeps it small, but a recording that is replayed in a loop
// delivers the same timestamps over and over, and a clock that jumps
// backwards would defeat the time bound altogether. This is the backstop that
// turns "unbounded" into "at most a few kilobytes per product".
const maxDropSamples = 512

// maxProductivity is where the drop rule caps the productivity (see
// applyDrop).
const maxProductivity = 100

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
	// one of them is ever non-zero. deficit is the open deficit or
	// no_local_production alert - the same streak, told apart by whether the
	// island produces the product itself. The drop rule reads the streak as
	// well: a drop only counts while the local balance is negative.
	negative    int
	nonNegative int
	deficit     *Alert

	// samples is the productivity history the drop rule averages over,
	// oldest first, and drop is its alert. The history runs on its own
	// clock: clock is how much time has passed while readings were being
	// taken, and lastSeen is when the previous reading arrived. Time spent
	// idle (see applyDrop) does not move the clock.
	samples  []productivitySample
	drop     *Alert
	clock    time.Duration
	lastSeen time.Time
}

// productivitySample is one productivity reading, in percent, stamped with
// the product's own clock.
type productivitySample struct {
	at    time.Duration
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

	// A product that has vanished from a snapshot with goods in it has no
	// buildings left to have stalled, so an open drop ends. Everything else
	// about it is kept (see the package documentation), and an empty
	// snapshot - the warm-up after a save loads - changes nothing at all.
	if len(snap.Products) > 0 {
		present := make(map[int32]bool, len(snap.Products))
		for _, p := range snap.Products {
			present[p.ProductGUID] = true
		}
		for guid, ps := range is.products {
			if !present[guid] {
				events = e.endDrop(events, ps, snap.ReceivedAt)
			}
		}
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
//
// A sustained negative delta means two different things. On an island with
// buildings of its own for the product, the island's own production falls
// behind its consumption: a deficit warning - a deficit of local production,
// not proof that the island runs out, which the pipe cannot see. On an island without any, the product has to come from
// elsewhere, and a negative delta is how every such good looks - in the live
// capture of 2026-09-23, 81 of 102 deficit warnings were of this kind. Those
// become the quieter no_local_production alert. It says what the pipe
// shows, no more: whether any ship actually brings the product, or the last
// of the storage is being eaten, is not in the data.
func (e *Engine) applyDeficit(events []Event, snap model.IslandSnapshot, name string, ps *productState, p model.ProductStat) []Event {
	delta := float64(p.Delta)
	if delta < 0 {
		ps.negative++
		ps.nonNegative = 0
	} else {
		ps.nonNegative++
		ps.negative = 0
	}

	rule, severity := RuleDeficit, SeverityWarning
	if p.Buildings == 0 {
		rule, severity = RuleNoLocalProduction, SeverityInfo
	}
	// The island started producing the product itself, or stopped: the open
	// alert describes the other situation and ends here. The streak carries
	// on, so a negative balance that persists is raised again under the
	// right rule in the same tick.
	if ps.deficit != nil && ps.deficit.Rule != rule {
		a := *ps.deficit
		ps.deficit = nil
		a.ClearedAt = snap.ReceivedAt
		a.Value = delta
		events = append(events, Event{Kind: KindCleared, Alert: a})
	}

	switch {
	case ps.deficit == nil && ps.negative >= e.cfg.DeficitSamples:
		a := Alert{
			Island:      snap.Key,
			IslandName:  name,
			ProductGUID: p.ProductGUID,
			Rule:        rule,
			Severity:    severity,
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
// The productivity is capped at 100 %. Items and effects push it well above
// that (live up to 270 %), and such a boost wearing off - 174 % down to
// 151 % - is not a building that stalls; below 100 % it is.
//
// A drop alone says little. The storage is not in the pipe, and a building
// whose storage is full stops exactly like one without workers or input
// goods. So a drop is only raised while the product's local balance - the
// delta, reported production minus reported consumption on this island - has
// been negative in DropNegativeSamples consecutive samples (the deficit
// rule's streak, which runs first). That does not prove the island runs out:
// it may hold stock, or receive the product from elsewhere. It does make the
// drop a reason for the island's own production falling behind its
// consumption, which is worth a warning. It clears when the productivity is
// back, when the balance has not been negative for DeficitClearSamples
// samples, or when the buildings are gone.
//
// The baseline must not sink while buildings idle for a reason that may be
// no problem. A reading below the baseline taken while the balance is not
// negative - possibly a full storage - is therefore not added to it, and the
// time it covers does not count towards the window either: the history runs
// on a clock that stands still while readings are left out. Otherwise twenty
// minutes of a full storage at 0 % would leave a baseline of 0 % or none at
// all, and a chain that then stalls for real would show no drop. In steady
// operation nothing is left out, and that clock is the receive time.
//
// Without buildings there is no productivity, and no chain that could have
// stalled: an open alert ends, and the history is dropped, so that a chain
// built again later is measured from scratch.
func (e *Engine) applyDrop(events []Event, snap model.IslandSnapshot, name string, ps *productState, p model.ProductStat) []Event {
	if p.Buildings == 0 {
		return e.endDrop(events, ps, snap.ReceivedAt)
	}
	prod := float64(p.AvgProductivity)
	if math.IsNaN(prod) || math.IsInf(prod, 0) {
		return events
	}
	prod = min(prod, maxProductivity)

	// Time since the previous reading. A recording played in a loop, or a
	// clock that jumps back, gives a negative step; the clock does not run
	// backwards (maxDropSamples bounds the history then).
	var step time.Duration
	if !ps.lastSeen.IsZero() {
		step = max(snap.ReceivedAt.Sub(ps.lastSeen), 0)
	}
	ps.lastSeen = snap.ReceivedAt

	mean, n := meanOf(ps.samples)
	idle := p.Delta >= 0 && n > 0 && prod < mean
	if !idle {
		ps.clock += step
		ps.samples = prune(ps.samples, ps.clock-e.cfg.DropWindow)
		mean, n = meanOf(ps.samples)
		defer func() {
			ps.samples = appendSample(ps.samples, productivitySample{at: ps.clock, value: prod})
		}()
	}

	// The local balance recovered: whatever the productivity does now, it no
	// longer explains production falling behind consumption.
	if ps.drop != nil && ps.nonNegative >= e.cfg.DeficitClearSamples {
		return e.clearDrop(events, ps, snap.ReceivedAt, prod, mean)
	}
	if n < e.cfg.MinSamplesForDrop {
		return events
	}
	drop := mean - prod
	negative := ps.negative >= e.cfg.DropNegativeSamples

	switch {
	case ps.drop == nil && drop > e.cfg.DropPercentagePoints && negative:
		a := Alert{
			Island:      snap.Key,
			IslandName:  name,
			ProductGUID: p.ProductGUID,
			Rule:        RuleProductivityDrop,
			Severity:    SeverityWarning,
			RaisedAt:    snap.ReceivedAt,
			Detail:      dropDetail(prod, mean, e.cfg.DropWindow),
			Value:       prod,
		}
		ps.drop = &a
		return append(events, Event{Kind: KindRaised, Alert: a})

	case ps.drop != nil && drop <= e.cfg.DropClearPercentagePoints:
		return e.clearDrop(events, ps, snap.ReceivedAt, prod, mean)

	case ps.drop != nil:
		ps.drop.IslandName = name
		ps.drop.Value = prod
		ps.drop.Detail = dropDetail(prod, mean, e.cfg.DropWindow)
	}
	return events
}

// clearDrop closes the open drop alert with the current numbers.
func (e *Engine) clearDrop(events []Event, ps *productState, at time.Time, prod, mean float64) []Event {
	a := *ps.drop
	ps.drop = nil
	a.ClearedAt = at
	a.Value = prod
	a.Detail = dropDetail(prod, mean, e.cfg.DropWindow)
	return append(events, Event{Kind: KindCleared, Alert: a})
}

// endDrop is the drop rule for a product whose buildings are gone: it closes
// an open alert and forgets the productivity history.
func (e *Engine) endDrop(events []Event, ps *productState, at time.Time) []Event {
	ps.samples = ps.samples[:0]
	ps.clock, ps.lastSeen = 0, time.Time{}
	if ps.drop == nil {
		return events
	}
	a := *ps.drop
	ps.drop = nil
	a.ClearedAt = at
	return append(events, Event{Kind: KindCleared, Alert: a})
}

// prune drops every reading older than cutoff. The history is kept oldest
// first, so this is a prefix.
func prune(samples []productivitySample, cutoff time.Duration) []productivitySample {
	cut := 0
	for cut < len(samples) && samples[cut].at < cutoff {
		cut++
	}
	if cut == 0 {
		return samples
	}
	return append(samples[:0], samples[cut:]...)
}

// appendSample adds one reading and enforces the length backstop.
func appendSample(samples []productivitySample, s productivitySample) []productivitySample {
	samples = append(samples, s)
	if len(samples) > maxDropSamples {
		samples = append(samples[:0], samples[len(samples)-maxDropSamples:]...)
	}
	return samples
}

// meanOf returns the arithmetic mean of the readings and how many there were.
func meanOf(samples []productivitySample) (float64, int) {
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
func dropDetail(prod, mean float64, window time.Duration) string {
	return fmt.Sprintf("productivity %.0f%% vs %s mean %.0f%%", prod, windowLabel(window), mean)
}

// windowLabel names the trailing window the way a person would, e.g. "5-min".
func windowLabel(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%d-min", int(d/time.Minute))
	}
	return d.String()
}
