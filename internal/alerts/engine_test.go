package alerts_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/model"
)

// base is the clock the tables count from. The engine only ever compares
// snapshot times with each other, so the absolute value is arbitrary.
var base = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

const (
	island  = int32(3245)
	product = int32(2068)
)

// snapshot builds a one-product snapshot at base+offset.
func snapshot(offset time.Duration, p model.ProductStat) model.IslandSnapshot {
	return model.IslandSnapshot{
		Key:        model.IslandKey{SessionGUID: island, IslandID: 5},
		Name:       " Juliana ",
		ReceivedAt: base.Add(offset),
		Products:   []model.ProductStat{p},
	}
}

// delta is a product whose only interesting value is its delta, on an island
// that produces it itself - so a shortfall is a deficit, not a
// no_local_production. Its productivity is a constant zero, so the drop rule
// stays out of the way.
func delta(v float32) model.ProductStat {
	return model.ProductStat{ProductGUID: product, Delta: v, Buildings: 1}
}

// imported is a product with the given delta on an island with no building
// for it: it has to come from elsewhere.
func imported(v float32) model.ProductStat {
	return model.ProductStat{ProductGUID: product, Delta: v, Consumption: -v}
}

// productivity is a product whose buildings run at the given productivity in
// percent while the island is short of it - the drop rule's case.
func productivity(percent float32) model.ProductStat {
	return model.ProductStat{
		ProductGUID:     product,
		Buildings:       4,
		AvgProductivity: percent,
		Consumption:     2,
		Delta:           -1,
	}
}

// dropOnly is an engine for the drop rule's tests. The drop rule needs a
// negative delta, which would raise a deficit after three samples as well;
// a deficit threshold nothing reaches keeps those events out of the tables.
func dropOnly(cfg alerts.Config) *alerts.Engine {
	cfg.DeficitSamples = 1 << 20
	return alerts.New(cfg)
}

// kinds renders a run's events as "raised"/"cleared" per sample, so a table
// can state the whole expectation in one line.
func kinds(events []alerts.Event) string {
	if len(events) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(events))
	for _, ev := range events {
		parts = append(parts, ev.Kind)
	}
	return strings.Join(parts, "+")
}

// run applies every sample and returns one kinds() string per sample.
func run(e *alerts.Engine, samples []model.ProductStat) []string {
	out := make([]string, 0, len(samples))
	for i, p := range samples {
		out = append(out, kinds(e.Apply(snapshot(time.Duration(i)*time.Second, p))))
	}
	return out
}

func TestDeficitRule(t *testing.T) {
	tests := []struct {
		name    string
		deltas  []float32
		want    []string
		wantEnd int // alerts still active at the end
	}{
		{
			name:    "raises on the third negative sample, not the second",
			deltas:  []float32{-1, -1, -1, -1},
			want:    []string{"-", "-", "raised", "-"},
			wantEnd: 1,
		},
		{
			name:    "clears only after three non-negative samples",
			deltas:  []float32{-1, -1, -1, 0, 1, 2},
			want:    []string{"-", "-", "raised", "-", "-", "cleared"},
			wantEnd: 0,
		},
		{
			name:    "a single recovery does not clear and does not reraise",
			deltas:  []float32{-1, -1, -1, 5, -1, -1},
			want:    []string{"-", "-", "raised", "-", "-", "-"},
			wantEnd: 1,
		},
		{
			name:    "alternating signs never raise",
			deltas:  []float32{-1, 1, -1, 1, -1, 1, -1, 1},
			want:    []string{"-", "-", "-", "-", "-", "-", "-", "-"},
			wantEnd: 0,
		},
		{
			name:    "two negatives then a recovery restart the streak",
			deltas:  []float32{-1, -1, 0, -1, -1, -1},
			want:    []string{"-", "-", "-", "-", "-", "raised"},
			wantEnd: 1,
		},
		{
			name:    "a delta of exactly zero is not a deficit",
			deltas:  []float32{0, 0, 0, 0},
			want:    []string{"-", "-", "-", "-"},
			wantEnd: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := alerts.New(alerts.Config{})
			samples := make([]model.ProductStat, 0, len(tc.deltas))
			for _, d := range tc.deltas {
				samples = append(samples, delta(d))
			}
			got := run(e, samples)
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("sample %d (delta %v): events = %q, want %q", i, tc.deltas[i], got[i], tc.want[i])
				}
			}
			if n := len(e.Active()); n != tc.wantEnd {
				t.Errorf("active alerts at the end = %d, want %d", n, tc.wantEnd)
			}
		})
	}
}

// The raised alert has to carry everything the API and the history need
// without a second lookup.
func TestRaisedDeficitAlertIsComplete(t *testing.T) {
	e := alerts.New(alerts.DefaultConfig())
	var raised alerts.Alert
	for i, d := range []float32{-10.3, -10.3, -10.3} {
		for _, ev := range e.Apply(snapshot(time.Duration(i)*time.Second, delta(d))) {
			raised = ev.Alert
		}
	}
	if raised.Rule != alerts.RuleDeficit || raised.Severity != alerts.SeverityWarning {
		t.Errorf("rule/severity = %q/%q", raised.Rule, raised.Severity)
	}
	if raised.Island.SessionGUID != island || raised.Island.IslandID != 5 {
		t.Errorf("island = %+v", raised.Island)
	}
	if raised.IslandName != "Juliana" {
		t.Errorf("island name = %q, want the trimmed name", raised.IslandName)
	}
	if raised.ProductGUID != product {
		t.Errorf("product = %d, want %d", raised.ProductGUID, product)
	}
	if want := base.Add(2 * time.Second); !raised.RaisedAt.Equal(want) {
		t.Errorf("raisedAt = %v, want the third sample's time %v", raised.RaisedAt, want)
	}
	if !raised.Active() {
		t.Error("a raised alert must be active")
	}
	if want := "delta -10.3 for 3 samples"; raised.Detail != want {
		t.Errorf("detail = %q, want %q", raised.Detail, want)
	}
	if raised.Value > -10.2 || raised.Value < -10.4 {
		t.Errorf("value = %v, want the current delta", raised.Value)
	}
}

func TestProductivityDropRule(t *testing.T) {
	// Six readings at 80 % establish the trailing mean, then the series
	// drops and recovers. The mean moves with the readings, which is why
	// 65 % is still an alert: the mean by then is about 76 %.
	tests := []struct {
		name    string
		series  []float32
		want    []string
		wantEnd int
	}{
		{
			name:    "a drop of 30 points raises, a partial recovery keeps it, a full one clears",
			series:  []float32{80, 80, 80, 80, 80, 80, 50, 65, 75},
			want:    []string{"-", "-", "-", "-", "-", "-", "raised", "-", "cleared"},
			wantEnd: 0,
		},
		{
			name:    "fewer samples than MinSamplesForDrop never raise",
			series:  []float32{80, 80, 20},
			want:    []string{"-", "-", "-"},
			wantEnd: 0,
		},
		{
			name:    "exactly MinSamplesForDrop samples are enough",
			series:  []float32{80, 80, 80, 20},
			want:    []string{"-", "-", "-", "raised"},
			wantEnd: 1,
		},
		{
			name:    "a drop of exactly the threshold does not raise",
			series:  []float32{80, 80, 80, 60},
			want:    []string{"-", "-", "-", "-"},
			wantEnd: 0,
		},
		{
			// 59 % is 21 points below the mean and raises; 61 % is only two
			// points better and must not clear, because clearing needs to
			// come back within 10 points of the (by then lower) mean.
			name:    "oscillating around the raise threshold does not flicker",
			series:  []float32{80, 80, 80, 59, 61, 59},
			want:    []string{"-", "-", "-", "raised", "-", "-"},
			wantEnd: 1,
		},
		{
			// The mean follows the readings, so a lasting recovery to the
			// old level does clear the alert - that is the rule working, not
			// flicker.
			name:    "a full recovery clears",
			series:  []float32{80, 80, 80, 55, 80},
			want:    []string{"-", "-", "-", "raised", "cleared"},
			wantEnd: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := dropOnly(alerts.Config{})
			samples := make([]model.ProductStat, 0, len(tc.series))
			for _, v := range tc.series {
				samples = append(samples, productivity(v))
			}
			got := run(e, samples)
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("sample %d (%v %%): events = %q, want %q", i, tc.series[i], got[i], tc.want[i])
				}
			}
			if n := len(e.Active()); n != tc.wantEnd {
				t.Errorf("active alerts at the end = %d, want %d", n, tc.wantEnd)
			}
		})
	}
}

// Without buildings there is no productivity, so the rule has to stay silent
// however the numbers look.
func TestProductivityDropNeedsBuildings(t *testing.T) {
	e := alerts.New(alerts.DefaultConfig())
	for i := range 10 {
		p := model.ProductStat{ProductGUID: product, AvgProductivity: float32(100 - 10*i), Consumption: 1}
		if evs := e.Apply(snapshot(time.Duration(i)*time.Second, p)); len(evs) != 0 {
			t.Fatalf("sample %d produced %d events, want none", i, len(evs))
		}
	}
	if n := len(e.Active()); n != 0 {
		t.Errorf("active alerts = %d, want 0", n)
	}
}

// Readings older than the window must not hold the mean up for ever.
func TestTrailingMeanForgetsOldSamples(t *testing.T) {
	e := dropOnly(alerts.Config{DropWindow: time.Minute})

	// Three readings at 90 %, then a gap longer than the window, then three
	// at 40 %. The old readings are out of the window by then, so the drop
	// to 40 % is measured against 40 %, not against 90 %.
	for i := range 3 {
		e.Apply(snapshot(time.Duration(i)*time.Second, productivity(90)))
	}
	for i := range 3 {
		offset := 5*time.Minute + time.Duration(i)*time.Second
		if evs := e.Apply(snapshot(offset, productivity(40))); len(evs) != 0 {
			t.Fatalf("sample %d after the gap produced %v, want no event", i, evs)
		}
	}
	if n := len(e.Active()); n != 0 {
		t.Errorf("active alerts = %d, want 0; the old readings should have expired", n)
	}
}

// A product that stops being reported keeps its alert: "not mentioned" is not
// evidence that the problem is over (see the package documentation).
func TestMissingProductKeepsItsAlert(t *testing.T) {
	e := alerts.New(alerts.DefaultConfig())
	for i := range 3 {
		e.Apply(snapshot(time.Duration(i)*time.Second, delta(-5)))
	}
	if len(e.Active()) != 1 {
		t.Fatalf("setup: active = %d, want 1", len(e.Active()))
	}

	empty := snapshot(10*time.Second, delta(-5))
	empty.Products = nil
	for i := range 5 {
		empty.ReceivedAt = base.Add(time.Duration(10+i) * time.Second)
		if evs := e.Apply(empty); len(evs) != 0 {
			t.Fatalf("an empty snapshot produced %v", evs)
		}
	}
	if n := len(e.Active()); n != 1 {
		t.Errorf("active alerts = %d, want the alert to survive", n)
	}
}

// Two rules on the same product, and two products on the same island, have to
// come back in a fixed order.
func TestEventsAndActiveAreOrdered(t *testing.T) {
	e := alerts.New(alerts.Config{DeficitSamples: 1, MinSamplesForDrop: 1})

	snap := model.IslandSnapshot{
		Key:        model.IslandKey{SessionGUID: 3245, IslandID: 5},
		Name:       "Juliana",
		ReceivedAt: base,
		Products: []model.ProductStat{
			{ProductGUID: 9000, Buildings: 1, AvgProductivity: 90, Consumption: 1, Delta: -1},
			{ProductGUID: 1000, Buildings: 1, AvgProductivity: 90, Consumption: 1, Delta: -1},
		},
	}
	e.Apply(snap)

	snap.ReceivedAt = base.Add(time.Second)
	snap.Products = []model.ProductStat{
		{ProductGUID: 9000, Buildings: 1, AvgProductivity: 10, Consumption: 1, Delta: -1},
		{ProductGUID: 1000, Buildings: 1, AvgProductivity: 10, Consumption: 1, Delta: -1},
	}
	events := e.Apply(snap)
	if len(events) != 2 {
		t.Fatalf("events = %d, want the two drop alerts", len(events))
	}
	if events[0].Alert.ProductGUID != 1000 || events[1].Alert.ProductGUID != 9000 {
		t.Errorf("events are not ordered by product: %d, %d",
			events[0].Alert.ProductGUID, events[1].Alert.ProductGUID)
	}

	active := e.Active()
	if len(active) != 4 {
		t.Fatalf("active = %d, want two rules on two products", len(active))
	}
	want := []struct {
		guid int32
		rule string
	}{
		{1000, alerts.RuleDeficit},
		{1000, alerts.RuleProductivityDrop},
		{9000, alerts.RuleDeficit},
		{9000, alerts.RuleProductivityDrop},
	}
	for i, w := range want {
		if active[i].ProductGUID != w.guid || active[i].Rule != w.rule {
			t.Errorf("active[%d] = %d/%s, want %d/%s", i, active[i].ProductGUID, active[i].Rule, w.guid, w.rule)
		}
	}
}

// Reset is the session boundary: everything active is cleared, and the
// caller gets the events it needs to close the rows in the history.
func TestResetClearsEverything(t *testing.T) {
	e := alerts.New(alerts.DefaultConfig())
	for i := range 3 {
		e.Apply(snapshot(time.Duration(i)*time.Second, delta(-5)))
	}
	if len(e.Active()) != 1 {
		t.Fatalf("setup: active = %d, want 1", len(e.Active()))
	}

	events := e.Reset()
	if len(events) != 1 {
		t.Fatalf("Reset returned %d events, want 1", len(events))
	}
	if events[0].Kind != alerts.KindCleared {
		t.Errorf("kind = %q, want %q", events[0].Kind, alerts.KindCleared)
	}
	if want := base.Add(2 * time.Second); !events[0].Alert.ClearedAt.Equal(want) {
		t.Errorf("clearedAt = %v, want the newest sample's time %v", events[0].Alert.ClearedAt, want)
	}
	if events[0].Alert.Active() {
		t.Error("a cleared alert must not report itself as active")
	}
	if n := len(e.Active()); n != 0 {
		t.Errorf("active after Reset = %d, want 0", n)
	}
	if evs := e.Reset(); len(evs) != 0 {
		t.Errorf("a second Reset returned %d events, want none", len(evs))
	}

	// The streak counters are gone too: three fresh samples are needed again.
	for i, want := range []string{"-", "-", "raised"} {
		got := kinds(e.Apply(snapshot(time.Duration(10+i)*time.Second, delta(-5))))
		if got != want {
			t.Errorf("sample %d after Reset = %q, want %q", i, got, want)
		}
	}
}

// Reset without any data at all still has to produce a usable timestamp.
func TestResetOnAnEmptyEngine(t *testing.T) {
	e := alerts.New(alerts.DefaultConfig())
	if evs := e.Reset(); len(evs) != 0 {
		t.Errorf("Reset on an empty engine returned %v", evs)
	}
}

// A zero Config is a caller that did not configure anything, not a caller
// that wants "raise on zero samples".
func TestZeroConfigUsesTheDefaults(t *testing.T) {
	got := alerts.New(alerts.Config{}).Config()
	if want := alerts.DefaultConfig(); got != want {
		t.Errorf("Config() = %+v, want %+v", got, want)
	}
}

// A clear threshold at or above the raise threshold would remove the
// hysteresis, so it is capped.
func TestClearThresholdCannotRemoveTheHysteresis(t *testing.T) {
	got := alerts.New(alerts.Config{DropPercentagePoints: 10, DropClearPercentagePoints: 30}).Config()
	if got.DropClearPercentagePoints >= got.DropPercentagePoints {
		t.Errorf("clear = %v, raise = %v; the clear threshold must stay below the raise threshold",
			got.DropClearPercentagePoints, got.DropPercentagePoints)
	}
}

// The engine runs in the ingest goroutine and is read by HTTP handlers; the
// race detector has to find nothing.
func TestApplyAndActiveAreConcurrencySafe(t *testing.T) {
	e := alerts.New(alerts.Config{DeficitSamples: 1})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 200 {
			e.Apply(snapshot(time.Duration(i)*time.Second, delta(-1)))
		}
	}()
	for range 200 {
		e.Active()
		e.Config()
	}
	<-done
}

// 14 islands with 56 products each is the shape of the real capture. One tick
// must stay cheap - the pipeline goroutine waits for it.
func BenchmarkApplyOneTick(b *testing.B) {
	e := alerts.New(alerts.DefaultConfig())
	snaps := make([]model.IslandSnapshot, 0, 14)
	for i := range 14 {
		products := make([]model.ProductStat, 0, 56)
		for g := range 56 {
			products = append(products, model.ProductStat{
				ProductGUID:     int32(1000 + g),
				Delta:           float32(g%3) - 1,
				Buildings:       int32(g % 2),
				AvgProductivity: float32(50 + g%40),
				Consumption:     1,
			})
		}
		snaps = append(snaps, model.IslandSnapshot{
			Key:      model.IslandKey{SessionGUID: 3245, IslandID: int32(i)},
			Name:     "island",
			Products: products,
		})
	}

	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		for j := range snaps {
			snaps[j].ReceivedAt = base.Add(time.Duration(i) * time.Second)
			e.Apply(snaps[j])
		}
	}
}

// The game delivers one statistics tick about every two minutes
// (docs/protocol.md, "Live capture 2026-09-22"). Three ticks at 80 % and a
// fourth at 50 % is the smallest real drop the rule can see, and it must
// fire: with the old five-minute window only two of those readings were ever
// in the window at once, so MinSamplesForDrop was never reached and the rule
// could not fire at all.
func TestProductivityDropFiresOnATwoMinuteTickCadence(t *testing.T) {
	const tick = 2 * time.Minute

	e := dropOnly(alerts.DefaultConfig())
	for i := range 3 {
		if evs := e.Apply(snapshot(time.Duration(i)*tick, productivity(80))); len(evs) != 0 {
			t.Fatalf("tick %d at 80%% produced %v, want no event", i, evs)
		}
	}

	evs := e.Apply(snapshot(3*tick, productivity(50)))
	if len(evs) != 1 || evs[0].Kind != alerts.KindRaised || evs[0].Alert.Rule != alerts.RuleProductivityDrop {
		t.Fatalf("the fourth tick at 50%% produced %v, want one raised productivity_drop", kinds(evs))
	}
	if got := evs[0].Alert.Detail; !strings.Contains(got, "15-min") {
		t.Errorf("detail = %q, want it to name the 15-min window", got)
	}

	// The same run against the pre-2026-09-22 five-minute window cannot
	// fire: the window holds at most two of these ticks.
	old := dropOnly(alerts.Config{DropWindow: 5 * time.Minute})
	for i := range 3 {
		old.Apply(snapshot(time.Duration(i)*tick, productivity(80)))
	}
	if evs := old.Apply(snapshot(3*tick, productivity(50))); len(evs) != 0 {
		t.Fatalf("the five-minute window produced %v; the test no longer proves what it claims", kinds(evs))
	}
}

// The default window has to hold MinSamplesForDrop ticks of the real cadence,
// with room for the schedule's drift (115-138 s between ticks live).
func TestDefaultDropWindowHoldsEnoughTicks(t *testing.T) {
	cfg := alerts.DefaultConfig()
	const slowestTick = 140 * time.Second
	if need := time.Duration(cfg.MinSamplesForDrop) * slowestTick; cfg.DropWindow < need {
		t.Errorf("DropWindow = %v, too short for %d ticks of up to %v",
			cfg.DropWindow, cfg.MinSamplesForDrop, slowestTick)
	}
}

// The case that made the rule switch from generation to productivity: the
// pipe counts completed production cycles per tick, so a building running
// without pause reports its full rate in one tick and nothing in the next
// (Margum's tunics in the live capture of 2026-09-23: 1.0, 0.0, 0.0, 0.0,
// 1.0 per minute at 86-100 % productivity). The rule must not see a drop.
func TestProductionCyclesDoNotLookLikeADrop(t *testing.T) {
	e := alerts.New(alerts.DefaultConfig())
	const tick = 2 * time.Minute
	gens := []float32{1, 0, 0, 0, 1, 1, 1, 0, 0, 1, 1, 1, 0, 0, 1}
	prods := []float32{100, 100, 93, 86, 93, 99, 100, 94, 87, 92, 97, 100, 96, 89, 90}
	for i := range gens {
		p := model.ProductStat{
			ProductGUID: product, Buildings: 1, Consumption: 0.6,
			Generation: gens[i], PerfectGeneration: 1, AvgProductivity: prods[i],
		}
		if evs := e.Apply(snapshot(time.Duration(i)*tick, p)); len(evs) != 0 {
			t.Fatalf("tick %d (generation %v, productivity %v %%) produced %v", i, gens[i], prods[i], kinds(evs))
		}
	}
}

// A building whose storage is full stops, and the pipe does not report the
// storage. An island with far more capacity than it consumes keeps its
// balance at zero or above while its buildings idle; a stalled chain drives
// it below. Only the second is raised - and one negative tick is not yet a
// shortfall, because a production cycle can fall on either side of a tick.
func TestProductivityDropNeedsAShortfall(t *testing.T) {
	const tick = 2 * time.Minute
	series := []float32{100, 100, 100, 66, 26, 0}
	run := func(deltas []float32) string {
		e := dropOnly(alerts.DefaultConfig())
		var got []string
		for i, v := range series {
			p := productivity(v)
			p.Delta = deltas[i]
			got = append(got, kinds(e.Apply(snapshot(time.Duration(i)*tick, p))))
		}
		return strings.Join(got, " ")
	}

	if got := run([]float32{0, 0, 0, 0, 0, 0}); got != "- - - - - -" {
		t.Errorf("full storage, balance at zero: events = %q, want none", got)
	}
	if got := run([]float32{-1, -1, -1, -1, -1, -1}); got != "- - - raised - -" {
		t.Errorf("short all along: events = %q, want the drop raised at 66 %%", got)
	}
	if got := run([]float32{0, 0, 0, -1, 0, -1}); got != "- - - - - -" {
		t.Errorf("single negative ticks: events = %q, want none", got)
	}
	if got := run([]float32{0, 0, 0, -1, -1, -1}); got != "- - - - raised -" {
		t.Errorf("short from the drop on: events = %q, want it raised on the second short tick", got)
	}
}

// The chain stays slow, but the island is no longer short - the storage
// filled up, or consumption fell. The alert has nothing left to explain.
func TestProductivityDropClearsWhenTheShortfallEnds(t *testing.T) {
	const tick = 2 * time.Minute
	e := dropOnly(alerts.DefaultConfig())
	for i, v := range []float32{100, 100, 100, 40} {
		e.Apply(snapshot(time.Duration(i)*tick, productivity(v)))
	}
	if len(e.Active()) != 1 {
		t.Fatalf("setup: active = %d, want the drop", len(e.Active()))
	}
	var got []string
	for i := range 3 {
		p := productivity(40)
		p.Delta = 0
		got = append(got, kinds(e.Apply(snapshot(time.Duration(4+i)*tick, p))))
	}
	if want := "- - cleared"; strings.Join(got, " ") != want {
		t.Errorf("events = %q, want %q", strings.Join(got, " "), want)
	}
}

// The player tears the stalled chain down. Without buildings nothing is
// stalling any more: the alert ends at once, and a chain built again later
// starts without the old readings.
func TestProductivityDropEndsWhenTheBuildingsAreGone(t *testing.T) {
	const tick = 2 * time.Minute
	e := dropOnly(alerts.DefaultConfig())
	for i, v := range []float32{100, 100, 100, 40} {
		e.Apply(snapshot(time.Duration(i)*tick, productivity(v)))
	}
	if len(e.Active()) != 1 {
		t.Fatalf("setup: active = %d, want the drop", len(e.Active()))
	}

	gone := model.ProductStat{ProductGUID: product, Consumption: 2, Delta: -2}
	evs := e.Apply(snapshot(4*tick, gone))
	var cleared bool
	for _, ev := range evs {
		if ev.Kind == alerts.KindCleared && ev.Alert.Rule == alerts.RuleProductivityDrop {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("tearing the buildings down produced %v, want the drop cleared", evs)
	}

	// Rebuilt at 30 %: without the old 100 % readings there is no drop to
	// see yet, however short the island is.
	for i := range 3 {
		evs := e.Apply(snapshot(time.Duration(5+i)*tick, productivity(30)))
		for _, ev := range evs {
			if ev.Alert.Rule == alerts.RuleProductivityDrop {
				t.Fatalf("tick %d after rebuilding: %s %s, want the old history gone", i, ev.Kind, ev.Alert.Rule)
			}
		}
	}
}

// A product that disappears from the island's statistics altogether has no
// buildings left either. An empty snapshot - the warm-up after loading a
// save - is not evidence of anything and must not clear.
func TestProductivityDropEndsWhenTheProductVanishes(t *testing.T) {
	const tick = 2 * time.Minute
	raise := func() *alerts.Engine {
		e := dropOnly(alerts.DefaultConfig())
		for i, v := range []float32{100, 100, 100, 40} {
			e.Apply(snapshot(time.Duration(i)*tick, productivity(v)))
		}
		if len(e.Active()) != 1 {
			t.Fatalf("setup: active = %d, want the drop", len(e.Active()))
		}
		return e
	}

	e := raise()
	warmup := snapshot(4*tick, productivity(0))
	warmup.Products = nil
	if evs := e.Apply(warmup); len(evs) != 0 {
		t.Errorf("an empty snapshot produced %v, want nothing", evs)
	}
	if len(e.Active()) != 1 {
		t.Error("an empty snapshot cleared the drop")
	}

	other := snapshot(5*tick, model.ProductStat{ProductGUID: product + 1, Buildings: 1, AvgProductivity: 100})
	evs := e.Apply(other)
	if len(evs) != 1 || evs[0].Kind != alerts.KindCleared || evs[0].Alert.Rule != alerts.RuleProductivityDrop {
		t.Fatalf("a snapshot without the product produced %v, want the drop cleared", evs)
	}
}

// A shortfall on an island without any building for the product is
// no_local_production: kept and listed, but as info, not as a warning.
func TestNoLocalProductionIsItsOwnQuietRule(t *testing.T) {
	e := alerts.New(alerts.DefaultConfig())
	var raised []alerts.Event
	for i := range 3 {
		raised = append(raised, e.Apply(snapshot(time.Duration(i)*time.Second, imported(-1.7)))...)
	}
	if len(raised) != 1 || raised[0].Kind != alerts.KindRaised {
		t.Fatalf("events = %v, want one raised after three samples", kinds(raised))
	}
	if a := raised[0].Alert; a.Rule != alerts.RuleNoLocalProduction || a.Severity != alerts.SeverityInfo {
		t.Errorf("rule/severity = %q/%q, want %q/%q", a.Rule, a.Severity, alerts.RuleNoLocalProduction, alerts.SeverityInfo)
	}

	// The same streak with a building of its own is a deficit warning.
	d := alerts.New(alerts.DefaultConfig())
	var deficit []alerts.Event
	for i := range 3 {
		deficit = append(deficit, d.Apply(snapshot(time.Duration(i)*time.Second, delta(-1.7)))...)
	}
	if len(deficit) != 1 || deficit[0].Alert.Rule != alerts.RuleDeficit || deficit[0].Alert.Severity != alerts.SeverityWarning {
		t.Fatalf("with buildings: events = %v, want one deficit warning", deficit)
	}
}

// The player builds the first producer while the island is still short: the
// no_local_production alert ends, and - the shortfall going on - a deficit
// starts in the same
// tick. Tearing it down again turns it back. The alert never silently
// changes its rule, because the history stores the rule it was raised with.
func TestNoLocalProductionAndDeficitHandOver(t *testing.T) {
	e := alerts.New(alerts.DefaultConfig())
	for i := range 3 {
		e.Apply(snapshot(time.Duration(i)*time.Second, imported(-2)))
	}

	// Apply orders events by product and then rule, so the raised deficit
	// comes before the cleared no_local_production; the history keeps one
	// open row per
	// rule, so the order does not matter there.
	evs := e.Apply(snapshot(3*time.Second, delta(-1)))
	var got []string
	for _, ev := range evs {
		got = append(got, ev.Kind+" "+ev.Alert.Rule)
	}
	if want := "raised deficit, cleared no_local_production"; strings.Join(got, ", ") != want {
		t.Fatalf("the first building produced %q, want %q", strings.Join(got, ", "), want)
	}
	if active := e.Active(); len(active) != 1 || active[0].Rule != alerts.RuleDeficit {
		t.Errorf("active = %v, want only the deficit", active)
	}

	// A building that closes the gap ends it and raises nothing.
	f := alerts.New(alerts.DefaultConfig())
	for i := range 3 {
		f.Apply(snapshot(time.Duration(i)*time.Second, imported(-2)))
	}
	evs = f.Apply(snapshot(3*time.Second, delta(0.5)))
	if len(evs) != 1 || evs[0].Kind != alerts.KindCleared || evs[0].Alert.Rule != alerts.RuleNoLocalProduction {
		t.Fatalf("a building that closes the gap produced %v, want only the no_local_production cleared", evs)
	}
	if n := len(f.Active()); n != 0 {
		t.Errorf("active = %d, want none", n)
	}
}

// Items and effects push productivity above 100 % (live up to 270 %). A boost
// that wears off is not a stall: 174 % down to 151 % raises nothing. Falling
// below 100 % is measured from 100 %, so a real stall of a boosted chain is
// still caught.
func TestBoostWearingOffIsNotADrop(t *testing.T) {
	const tick = 2 * time.Minute
	for _, tc := range []struct {
		name   string
		series []float32
		want   string
	}{
		{"a boost wears off", []float32{174, 180, 176, 151}, "- - - -"},
		{"a boosted chain stalls", []float32{174, 180, 176, 60}, "- - - raised"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := dropOnly(alerts.DefaultConfig())
			var got []string
			for i, v := range tc.series {
				got = append(got, kinds(e.Apply(snapshot(time.Duration(i)*tick, productivity(v)))))
			}
			if strings.Join(got, " ") != tc.want {
				t.Errorf("events = %q, want %q", strings.Join(got, " "), tc.want)
			}
		})
	}
}
