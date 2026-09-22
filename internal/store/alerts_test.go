package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/store"
)

// alert builds one raised alert for the given island and product.
func alert(k model.IslandKey, name string, guid int32, rule string, at time.Time) alerts.Alert {
	return alerts.Alert{
		Island:      k,
		IslandName:  name,
		ProductGUID: guid,
		Rule:        rule,
		Severity:    alerts.SeverityWarning,
		RaisedAt:    at,
		Detail:      "delta -1.0 for 3 samples",
		Value:       -1,
	}
}

func TestRaiseAndClearAlert(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	k := key(3245, 5)

	// The snapshot comes first, exactly as the batcher's queue delivers it.
	if err := s.WriteSnapshots(ctx, []model.IslandSnapshot{snap(k, "Juliana", base, 2068)}); err != nil {
		t.Fatalf("WriteSnapshots: %v", err)
	}
	id, err := s.RaiseAlert(ctx, alert(k, "Juliana", 2068, alerts.RuleDeficit, base))
	if err != nil {
		t.Fatalf("RaiseAlert: %v", err)
	}
	if id == 0 {
		t.Error("RaiseAlert returned id 0")
	}

	rows, err := s.Alerts(ctx, true, 0)
	if err != nil {
		t.Fatalf("Alerts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("active alerts = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.ID != id {
		t.Errorf("id = %d, want %d", got.ID, id)
	}
	if got.Island != k {
		t.Errorf("island = %+v, want %+v", got.Island, k)
	}
	if got.IslandName != "Juliana" {
		t.Errorf("island name = %q, want the name joined in from the island row", got.IslandName)
	}
	if got.ProductGUID != 2068 || got.Rule != alerts.RuleDeficit || got.Severity != alerts.SeverityWarning {
		t.Errorf("row = %+v", got)
	}
	if !got.RaisedAt.Equal(base) {
		t.Errorf("raisedAt = %v, want %v", got.RaisedAt, base)
	}
	if !got.Active() {
		t.Error("a freshly raised alert must be active")
	}
	if got.Detail == "" {
		t.Error("detail was not stored")
	}

	// --- clearing ---
	end := base.Add(time.Minute)
	if err := s.ClearAlert(ctx, k, 2068, alerts.RuleDeficit, end); err != nil {
		t.Fatalf("ClearAlert: %v", err)
	}
	active, err := s.Alerts(ctx, true, 0)
	if err != nil {
		t.Fatalf("Alerts: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("active alerts after clearing = %d, want 0", len(active))
	}
	all, err := s.Alerts(ctx, false, 0)
	if err != nil {
		t.Fatalf("Alerts(all): %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("stored alerts = %d, want the cleared one to stay", len(all))
	}
	if !all[0].ClearedAt.Equal(end) {
		t.Errorf("clearedAt = %v, want %v", all[0].ClearedAt, end)
	}

	// A second clear must not move the end time.
	if err := s.ClearAlert(ctx, k, 2068, alerts.RuleDeficit, end.Add(time.Hour)); err != nil {
		t.Fatalf("second ClearAlert: %v", err)
	}
	all, _ = s.Alerts(ctx, false, 0)
	if !all[0].ClearedAt.Equal(end) {
		t.Errorf("clearedAt after a second clear = %v, want the first end time %v", all[0].ClearedAt, end)
	}
}

// Clearing something that was never raised is the disagreement between the
// engine's memory and the database after a restart, not a failure.
func TestClearAlertWithoutAMatchingRow(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	if err := s.ClearAlert(ctx, key(1, 1), 2068, alerts.RuleDeficit, base); err != nil {
		t.Errorf("clearing an unknown island: %v", err)
	}
	if err := s.WriteSnapshots(ctx, []model.IslandSnapshot{snap(key(1, 1), "I", base, 2068)}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearAlert(ctx, key(1, 1), 2068, alerts.RuleDeficit, base); err != nil {
		t.Errorf("clearing a known island without an open alert: %v", err)
	}
}

// The island row normally exists already. When it does not, the alert must
// still be stored rather than dropped.
func TestRaiseAlertCreatesTheIslandWhenItIsMissing(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	k := key(77, 3)

	if _, err := s.RaiseAlert(ctx, alert(k, "Lost Isle", 1234, alerts.RuleProductivityDrop, base)); err != nil {
		t.Fatalf("RaiseAlert: %v", err)
	}
	rows, err := s.Alerts(ctx, true, 0)
	if err != nil {
		t.Fatalf("Alerts: %v", err)
	}
	if len(rows) != 1 || rows[0].IslandName != "Lost Isle" || rows[0].Island != k {
		t.Fatalf("rows = %+v, want one alert on the created island", rows)
	}

	islands, err := s.Islands(ctx)
	if err != nil {
		t.Fatalf("Islands: %v", err)
	}
	if len(islands) != 1 || islands[0].Key != k {
		t.Fatalf("islands = %+v, want the one created by the alert", islands)
	}
	if !islands[0].FirstSeen.Equal(base) {
		t.Errorf("firstSeen = %v, want the alert's time %v", islands[0].FirstSeen, base)
	}
}

// A second alert on the same island must reuse the island row, not create a
// second one - the unique key is what guarantees it.
func TestRaiseAlertReusesTheIslandRow(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	k := key(77, 3)

	for i, rule := range []string{alerts.RuleDeficit, alerts.RuleProductivityDrop} {
		if _, err := s.RaiseAlert(ctx, alert(k, "Lost Isle", 1234, rule, base.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatalf("RaiseAlert %s: %v", rule, err)
		}
	}
	islands, err := s.Islands(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(islands) != 1 {
		t.Errorf("islands = %d, want 1", len(islands))
	}
	rows, _ := s.Alerts(ctx, true, 0)
	if len(rows) != 2 {
		t.Errorf("alerts = %d, want 2", len(rows))
	}
}

func TestAlertsAreNewestFirstAndLimited(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	k := key(3245, 5)

	for i := range 5 {
		at := base.Add(time.Duration(i) * time.Minute)
		if _, err := s.RaiseAlert(ctx, alert(k, "Juliana", int32(1000+i), alerts.RuleDeficit, at)); err != nil {
			t.Fatalf("RaiseAlert %d: %v", i, err)
		}
	}

	rows, err := s.Alerts(ctx, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("alerts = %d, want 5", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].RaisedAt.After(rows[i-1].RaisedAt) {
			t.Fatalf("alerts are not newest first: %v before %v", rows[i-1].RaisedAt, rows[i].RaisedAt)
		}
	}
	if rows[0].ProductGUID != 1004 {
		t.Errorf("first row is product %d, want the newest (1004)", rows[0].ProductGUID)
	}

	limited, err := s.Alerts(ctx, false, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 || limited[0].ProductGUID != 1004 {
		t.Errorf("limited = %+v, want the two newest", limited)
	}
}

// The rule engine's state is in memory, so an alert that survived a crash has
// nobody left to clear it. Opening the database must close it.
func TestOpenClosesAlertsLeftOpenByAnEarlierRun(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tabularium117.db")
	k := key(3245, 5)

	s, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.RaiseAlert(ctx, alert(k, "Juliana", 2068, alerts.RuleDeficit, base)); err != nil {
		t.Fatalf("RaiseAlert: %v", err)
	}
	s.Close()

	again, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()

	active, err := again.Alerts(ctx, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Errorf("active alerts after a restart = %d, want 0", len(active))
	}
	all, err := again.Alerts(ctx, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Active() {
		t.Errorf("the alert should still be recorded, but closed: %+v", all)
	}
}
