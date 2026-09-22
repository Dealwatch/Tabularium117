package server_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/server"
)

// alertJSON is the shape KONZEPT.md section 5 promises for one warning.
type alertJSON struct {
	ID          *int64     `json:"id"`
	IslandID    string     `json:"islandId"`
	IslandName  string     `json:"islandName"`
	SessionName string     `json:"sessionName"`
	ProductGUID int32      `json:"productGuid"`
	ProductName string     `json:"productName"`
	Rule        string     `json:"rule"`
	Severity    string     `json:"severity"`
	RaisedAt    time.Time  `json:"raisedAt"`
	ClearedAt   *time.Time `json:"clearedAt"`
	Detail      string     `json:"detail"`
	Value       float64    `json:"value"`
}

type alertEventJSON struct {
	Kind  string    `json:"kind"`
	Alert alertJSON `json:"alert"`
}

// fixedAlerts is an AlertSource with a fixed answer, so the endpoint can be
// tested without driving a whole rule engine.
type fixedAlerts []alerts.Alert

func (f fixedAlerts) Active() []alerts.Alert { return []alerts.Alert(f) }

// julianaKey is the island the API tests use.
var julianaKey = model.IslandKey{SessionGUID: 3245, IslandID: 5}

// oatsDeficit is a deficit alert on Juliana's oats (product 2068).
func oatsDeficit(at time.Time) alerts.Alert {
	return alerts.Alert{
		Island:      julianaKey,
		IslandName:  "Juliana",
		ProductGUID: 2068,
		Rule:        alerts.RuleDeficit,
		Severity:    alerts.SeverityWarning,
		RaisedAt:    at,
		Detail:      "delta -10.3 for 3 samples",
		Value:       -10.3,
	}
}

// newServerWithAlerts is newServer plus a rule engine.
func newServerWithAlerts(t *testing.T, fx fixture, src server.AlertSource) (*server.Server, string) {
	t.Helper()
	srv := server.New(server.Options{
		State:   fx.state,
		Store:   fx.store,
		Alerts:  src,
		Version: "test",
		Now:     func() time.Time { return fx.now },
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts.URL
}

// The live list comes from the engine, with every GUID resolved through the
// catalog in the requested language.
func TestActiveAlertsComeFromTheEngine(t *testing.T) {
	fx := loadFixture(t, true)
	raised := fx.now.Add(-time.Minute)
	_, url := newServerWithAlerts(t, fx, fixedAlerts{oatsDeficit(raised)})

	var got []alertJSON
	getJSON(t, url+"/api/v1/alerts?active=true&lang=english", http.StatusOK, &got)
	if len(got) != 1 {
		t.Fatalf("alerts = %d, want 1", len(got))
	}
	a := got[0]
	if a.ID != nil {
		t.Errorf("id = %v, want none for a live alert", *a.ID)
	}
	if a.IslandID != julianaID {
		t.Errorf("islandId = %q, want %q", a.IslandID, julianaID)
	}
	if a.IslandName != "Juliana" {
		t.Errorf("islandName = %q", a.IslandName)
	}
	if a.SessionName == "" {
		t.Error("sessionName is empty; the session GUID must be resolved")
	}
	if a.ProductGUID != 2068 || a.ProductName == "" || strings.HasPrefix(a.ProductName, "#") {
		t.Errorf("product = %d/%q, want the catalog name", a.ProductGUID, a.ProductName)
	}
	if a.Rule != alerts.RuleDeficit || a.Severity != alerts.SeverityWarning {
		t.Errorf("rule/severity = %q/%q", a.Rule, a.Severity)
	}
	if !a.RaisedAt.Equal(raised) {
		t.Errorf("raisedAt = %v, want %v", a.RaisedAt, raised)
	}
	if a.ClearedAt != nil {
		t.Errorf("clearedAt = %v, want null for an active alert", *a.ClearedAt)
	}
	if a.Detail == "" {
		t.Error("detail is empty")
	}
	if a.Value != -10.3 {
		t.Errorf("value = %v, want the current delta", a.Value)
	}

	// The same alert in German has to name the same product differently.
	var german []alertJSON
	getJSON(t, url+"/api/v1/alerts?active=true&lang=german", http.StatusOK, &german)
	if len(german) != 1 {
		t.Fatalf("german alerts = %d, want 1", len(german))
	}
	if german[0].ProductName == "" {
		t.Error("the German product name is empty")
	}
	if german[0].Rule != alerts.RuleDeficit {
		t.Errorf("rule = %q; rule names are identifiers and must not be translated", german[0].Rule)
	}
}

// Active alerts are listed newest first, whatever order the engine keeps them
// in, and ?limit= cuts the list.
func TestActiveAlertsAreNewestFirstAndLimited(t *testing.T) {
	fx := loadFixture(t, true)
	old := oatsDeficit(fx.now.Add(-time.Hour))
	recent := oatsDeficit(fx.now.Add(-time.Minute))
	recent.ProductGUID = 1010
	_, url := newServerWithAlerts(t, fx, fixedAlerts{old, recent})

	var got []alertJSON
	getJSON(t, url+"/api/v1/alerts", http.StatusOK, &got)
	if len(got) != 2 {
		t.Fatalf("alerts = %d, want 2 (active is the default)", len(got))
	}
	if got[0].ProductGUID != 1010 {
		t.Errorf("first alert is product %d, want the newest (1010)", got[0].ProductGUID)
	}

	var limited []alertJSON
	getJSON(t, url+"/api/v1/alerts?active=true&limit=1", http.StatusOK, &limited)
	if len(limited) != 1 || limited[0].ProductGUID != 1010 {
		t.Errorf("limited = %+v, want only the newest", limited)
	}
}

// The recorded list is the database's, and it carries the row id and the end
// time.
func TestRecordedAlertsComeFromTheStore(t *testing.T) {
	ctx := context.Background()
	fx := loadFixture(t, true)
	raised := fx.now.Add(-time.Hour)
	if _, err := fx.store.RaiseAlert(ctx, oatsDeficit(raised)); err != nil {
		t.Fatalf("RaiseAlert: %v", err)
	}
	cleared := raised.Add(time.Minute)
	if err := fx.store.ClearAlert(ctx, julianaKey, 2068, alerts.RuleDeficit, cleared); err != nil {
		t.Fatalf("ClearAlert: %v", err)
	}
	_, url := newServerWithAlerts(t, fx, fixedAlerts{})

	var got []alertJSON
	getJSON(t, url+"/api/v1/alerts?active=false", http.StatusOK, &got)
	if len(got) != 1 {
		t.Fatalf("recorded alerts = %d, want 1", len(got))
	}
	if got[0].ID == nil || *got[0].ID == 0 {
		t.Error("a stored alert must carry its row id")
	}
	if got[0].ClearedAt == nil || !got[0].ClearedAt.Equal(cleared) {
		t.Errorf("clearedAt = %v, want %v", got[0].ClearedAt, cleared)
	}
	if got[0].IslandName != "Juliana" {
		t.Errorf("islandName = %q, want the name from the island row", got[0].IslandName)
	}

	// The live list is still empty: the engine, not the database, decides
	// what is open right now.
	var live []alertJSON
	getJSON(t, url+"/api/v1/alerts?active=true", http.StatusOK, &live)
	if len(live) != 0 {
		t.Errorf("active alerts = %d, want 0", len(live))
	}
}

// Without a database there is no history to list, and saying so is better
// than an empty list that reads as "nothing ever happened".
func TestRecordedAlertsNeedTheHistory(t *testing.T) {
	fx := loadFixture(t, false)
	_, url := newServerWithAlerts(t, fx, fixedAlerts{oatsDeficit(fx.now)})

	if msg := errorOf(t, url+"/api/v1/alerts?active=false", http.StatusServiceUnavailable); !strings.Contains(msg, "no-db") {
		t.Errorf("error = %q, want it to name the flag", msg)
	}
	// The live list still works without a database.
	var live []alertJSON
	getJSON(t, url+"/api/v1/alerts?active=true", http.StatusOK, &live)
	if len(live) != 1 {
		t.Errorf("active alerts = %d, want 1", len(live))
	}
}

func TestAlertQueryIsValidated(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServerWithAlerts(t, fx, fixedAlerts{})

	for _, q := range []string{"?active=maybe", "?limit=-1", "?limit=lots"} {
		if msg := errorOf(t, url+"/api/v1/alerts"+q, http.StatusBadRequest); msg == "" {
			t.Errorf("GET /api/v1/alerts%s: no message", q)
		}
	}
}

// The status bar's badge has to agree with the list.
func TestStatusCountsTheActiveAlerts(t *testing.T) {
	fx := loadFixture(t, true)

	_, none := newServer(t, fx)
	var empty statusJSON
	getJSON(t, none+"/api/v1/status", http.StatusOK, &empty)
	if empty.Alerts.Active != 0 {
		t.Errorf("alerts.active = %d without an engine, want 0", empty.Alerts.Active)
	}

	second := oatsDeficit(fx.now)
	second.ProductGUID = 1010
	_, url := newServerWithAlerts(t, fx, fixedAlerts{oatsDeficit(fx.now), second})
	var got statusJSON
	getJSON(t, url+"/api/v1/status", http.StatusOK, &got)
	if got.Alerts.Active != 2 {
		t.Errorf("alerts.active = %d, want 2", got.Alerts.Active)
	}
}

// A client that connects late has to be told about the alerts that are
// already open, or its badge stays at zero until something changes.
func TestEventStreamCatchesUpOnAlerts(t *testing.T) {
	fx := loadFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan string, 1)
	srv := server.New(server.Options{
		State:     fx.state,
		Store:     fx.store,
		Alerts:    fixedAlerts{oatsDeficit(fx.now.Add(-time.Minute))},
		Version:   "test",
		Heartbeat: time.Hour,
		Now:       func() time.Time { return fx.now },
		OnReady:   func(addr net.Addr) { ready <- "http://" + addr.String() },
	})
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, "127.0.0.1:0") }()
	var base string
	select {
	case base = <-ready:
	case err := <-done:
		t.Fatalf("the server stopped instead of binding: %v", err)
	}

	s := openStream(t, ctx, base+"/api/v1/events?lang=english")

	// status, then one snapshot per island, then the alerts.
	if name, _ := s.nextEvent(t); name != "status" {
		t.Fatalf("first event = %q, want status", name)
	}
	islands := len(fx.state.Islands())
	for i := range islands {
		if name, _ := s.nextEvent(t); name != "snapshot" {
			t.Fatalf("event %d = %q, want snapshot", i, name)
		}
	}
	name, data := s.nextEvent(t)
	if name != "alert" {
		t.Fatalf("event after the snapshots = %q, want alert", name)
	}
	var ev alertEventJSON
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		t.Fatalf("decode the alert event %q: %v", data, err)
	}
	if ev.Kind != alerts.KindRaised {
		t.Errorf("kind = %q, want %q", ev.Kind, alerts.KindRaised)
	}
	if ev.Alert.IslandID != julianaID || ev.Alert.ProductGUID != 2068 {
		t.Errorf("alert = %+v, want the open one", ev.Alert)
	}

	// --- a live event reaches the same client ---
	srv.PublishAlert(alerts.Event{Kind: alerts.KindCleared, Alert: clearedAlert(oatsDeficit(fx.now), fx.now)})
	name, data = s.nextEvent(t)
	if name != "alert" {
		t.Fatalf("live event = %q, want alert", name)
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		t.Fatalf("decode the live alert event: %v", err)
	}
	if ev.Kind != alerts.KindCleared {
		t.Errorf("kind = %q, want %q", ev.Kind, alerts.KindCleared)
	}
	if ev.Alert.ClearedAt == nil {
		t.Error("a cleared alert must carry clearedAt")
	}

	cancel()
	<-done
}

// clearedAlert is the cleared form of a raised alert.
func clearedAlert(a alerts.Alert, at time.Time) alerts.Alert {
	a.ClearedAt = at
	return a
}

// A run without alerts must still publish nothing rather than panic.
func TestPublishAlertWithoutClients(t *testing.T) {
	fx := loadFixture(t, false)
	srv, _ := newServerWithAlerts(t, fx, fixedAlerts{})
	srv.PublishAlert(alerts.Event{Kind: alerts.KindRaised, Alert: oatsDeficit(fx.now)})
}
