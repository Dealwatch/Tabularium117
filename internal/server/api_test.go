package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/server"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

// --- the shapes the UI is written against ---

type islandJSON struct {
	ID          string     `json:"id"`
	SessionGUID int32      `json:"sessionGuid"`
	SessionName string     `json:"sessionName"`
	IslandID    int32      `json:"islandId"`
	Name        string     `json:"name"`
	Products    int        `json:"products"`
	Deficits    int        `json:"deficits"`
	ReceivedAt  *time.Time `json:"receivedAt"`
	Tick        *int64     `json:"tick"`
}

type statusJSON struct {
	Version    string `json:"version"`
	Connection struct {
		Mode            string     `json:"mode"`
		State           string     `json:"state"`
		Err             string     `json:"err"`
		Since           *time.Time `json:"since"`
		ProtocolVersion int32      `json:"protocolVersion"`
		LastFrameAt     *time.Time `json:"lastFrameAt"`
	} `json:"connection"`
	Session struct {
		Headline  string     `json:"headline"`
		StartedAt *time.Time `json:"startedAt"`
	} `json:"session"`
	LAN struct {
		Enabled bool `json:"enabled"`
		Local   bool `json:"local"`
	} `json:"lan"`
	Islands int `json:"islands"`
	History struct {
		Enabled        bool       `json:"enabled"`
		LastSampleTime *time.Time `json:"lastSampleTime"`
	} `json:"history"`
	Alerts struct {
		Active int `json:"active"`
	} `json:"alerts"`
	Tick      *int64 `json:"tick"`
	WarmingUp bool   `json:"warmingUp"`
}

type namedAmountJSON struct {
	Name   string `json:"name"`
	Amount int32  `json:"amount"`
}

type productJSON struct {
	GUID               int32                      `json:"guid"`
	Name               string                     `json:"name"`
	Category           string                     `json:"category"`
	Generation         float32                    `json:"generation"`
	Consumption        float32                    `json:"consumption"`
	Delta              float32                    `json:"delta"`
	PerfectGeneration  float32                    `json:"perfectGeneration"`
	PerfectConsumption float32                    `json:"perfectConsumption"`
	Buildings          int32                      `json:"buildings"`
	Maintenance        int32                      `json:"maintenance"`
	Income             float32                    `json:"income"`
	Profit             int32                      `json:"profit"`
	AvgProductivity    float32                    `json:"avgProductivity"`
	SummedProductivity float32                    `json:"summedProductivity"`
	Workforce          map[string]namedAmountJSON `json:"workforce"`
	BuildingsByGUID    map[string]namedAmountJSON `json:"buildingsByGuid"`
}

type productsJSON struct {
	Island   islandJSON    `json:"island"`
	Products []productJSON `json:"products"`
}

type efficiencyJSON struct {
	Island   islandJSON `json:"island"`
	Products []struct {
		GUID              int32    `json:"guid"`
		Name              string   `json:"name"`
		Generation        float32  `json:"generation"`
		PerfectGeneration float32  `json:"perfectGeneration"`
		Efficiency        *float64 `json:"efficiency"`
		Wasted            float32  `json:"wasted"`
		AvgProductivity   float32  `json:"avgProductivity"`
	} `json:"products"`
}

type historyJSON struct {
	Island  islandJSON `json:"island"`
	Product struct {
		GUID int32  `json:"guid"`
		Name string `json:"name"`
	} `json:"product"`
	Range  string     `json:"range"`
	From   *time.Time `json:"from"`
	To     time.Time  `json:"to"`
	Points []struct {
		TS              time.Time `json:"ts"`
		Generation      float32   `json:"generation"`
		Consumption     float32   `json:"consumption"`
		Delta           float32   `json:"delta"`
		Buildings       int32     `json:"buildings"`
		AvgProductivity float32   `json:"avgProductivity"`
		Aggregated      bool      `json:"aggregated"`
		BucketMs        int64     `json:"bucketMs"`
		Tick            int64     `json:"tick"`
	} `json:"points"`
}

// --- the endpoints ---

func TestStatus(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	var got statusJSON
	getJSON(t, url+"/api/v1/status", http.StatusOK, &got)

	if got.Version != "test" {
		t.Errorf("version = %q, want the configured one", got.Version)
	}
	if got.Connection.Mode != "replay" || got.Connection.State != "replaying" {
		t.Errorf("connection = %+v, want the replay source's status", got.Connection)
	}
	if got.Connection.ProtocolVersion != 2 {
		t.Errorf("protocol version = %d, want 2", got.Connection.ProtocolVersion)
	}
	if got.Connection.LastFrameAt == nil {
		t.Error("lastFrameAt is null although frames were received")
	}
	if got.Islands != 14 {
		t.Errorf("islands = %d, want the capture's 14", got.Islands)
	}
	if got.Session.Headline != "reencoded from connector capture" {
		t.Errorf("session headline = %q", got.Session.Headline)
	}
	if got.LAN.Enabled {
		t.Error("LAN mode is reported as enabled although nothing switched it on")
	}
	if !got.LAN.Local {
		t.Error("a request that did not come over the LAN listener is not reported as local")
	}
	if !got.History.Enabled || got.History.LastSampleTime == nil {
		t.Errorf("history = %+v, want it enabled with a last sample time", got.History)
	}
	// The capture's islands all report production, so this is not a warm-up.
	if got.WarmingUp {
		t.Error("warmingUp is true although every island reports products")
	}
	if got.Tick == nil || *got.Tick == 0 {
		t.Errorf("tick = %v, want the capture's game timestamp", got.Tick)
	}
}

// Without a store the live view still works, and the status says so instead
// of implying a history that is not being kept.
func TestStatusWithoutHistory(t *testing.T) {
	fx := loadFixture(t, false)
	_, url := newServer(t, fx)

	var got statusJSON
	getJSON(t, url+"/api/v1/status", http.StatusOK, &got)
	if got.History.Enabled || got.History.LastSampleTime != nil {
		t.Errorf("history = %+v, want it reported as off", got.History)
	}
	if got.Islands != 14 {
		t.Errorf("islands = %d, want 14 even without a history", got.Islands)
	}
}

func TestIslands(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	var got []islandJSON
	getJSON(t, url+"/api/v1/islands", http.StatusOK, &got)

	if len(got) != 14 {
		t.Fatalf("got %d islands, want the capture's 14", len(got))
	}
	// The order is state.Islands(): by session GUID, then island id.
	for i := 1; i < len(got); i++ {
		if got[i-1].SessionGUID > got[i].SessionGUID ||
			(got[i-1].SessionGUID == got[i].SessionGUID && got[i-1].IslandID >= got[i].IslandID) {
			t.Fatalf("islands are not ordered by (session, island): %+v then %+v", got[i-1], got[i])
		}
	}

	first := got[0]
	if first.ID != julianaID || first.SessionGUID != 3245 || first.IslandID != 5 {
		t.Errorf("first island = %+v, want the composite id %q", first, julianaID)
	}
	if first.Name != "Juliana" {
		t.Errorf("island name = %q, want Juliana", first.Name)
	}
	if first.SessionName != "Latium" {
		t.Errorf("session name = %q, want the catalog's Latium", first.SessionName)
	}
	if first.Products != 56 || first.Deficits != 25 {
		t.Errorf("Juliana: %d products, %d deficits; want 56 and 25", first.Products, first.Deficits)
	}
	if first.ReceivedAt == nil {
		t.Error("receivedAt is null although the island was reported")
	}
	// The capture delivers one island name with a trailing space; the API is
	// the display side and trims it.
	for _, island := range got {
		if island.Name != strings.TrimSpace(island.Name) {
			t.Errorf("island name %q is not trimmed for display", island.Name)
		}
	}
}

func TestProducts(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	var got productsJSON
	getJSON(t, url+"/api/v1/islands/"+julianaID+"/products", http.StatusOK, &got)

	if got.Island.ID != julianaID || got.Island.Products != 56 {
		t.Fatalf("island = %+v, want Juliana with 56 products", got.Island)
	}
	if len(got.Products) != 56 {
		t.Fatalf("got %d products, want 56", len(got.Products))
	}
	// Deficits first, then by name.
	for i := 1; i < len(got.Products); i++ {
		prev, cur := got.Products[i-1], got.Products[i]
		if prev.Delta > cur.Delta {
			t.Fatalf("products are not sorted by delta: %+v before %+v", prev, cur)
		}
		if prev.Delta == cur.Delta && prev.Name > cur.Name {
			t.Fatalf("equal deltas are not sorted by name: %q before %q", prev.Name, cur.Name)
		}
	}
	if got.Products[0].Delta >= 0 {
		t.Errorf("the first product has delta %v; the island has 25 deficits", got.Products[0].Delta)
	}

	oats := findProduct(t, got.Products, 2068)
	if oats.Name != "Oats" {
		t.Errorf("product 2068 = %q, want Oats", oats.Name)
	}
	if oats.Category != "agricultural" {
		t.Errorf("product 2068 category = %q, want agricultural", oats.Category)
	}
	if oats.Generation != 23.191875 || oats.Consumption != 24.699997 || oats.Buildings != 6 {
		t.Errorf("product 2068 values changed on the way out: %+v", oats)
	}
	if oats.Maintenance != 24 || oats.Profit != -24 || oats.AvgProductivity != 464.5591 {
		t.Errorf("product 2068 raw fields changed on the way out: %+v", oats)
	}
	// The GUID maps are resolved too, so the UI needs no second lookup.
	if w := oats.Workforce["2181"]; w.Amount != 18 || w.Name != "Libertus Workforce" {
		t.Errorf("workforce of 2068 = %+v, want 18 Libertus Workforce", oats.Workforce)
	}
	if b := oats.BuildingsByGUID["2200"]; b.Amount != 6 || b.Name == "" {
		t.Errorf("buildings of 2068 = %+v, want 6 of building 2200", oats.BuildingsByGUID)
	}
}

// The language comes from ?lang= or, failing that, from the browser.
func TestProductLanguage(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)
	path := url + "/api/v1/islands/" + julianaID + "/products"

	cases := []struct {
		name   string
		url    string
		accept string
		want   string
	}{
		{name: "default", url: path, want: "Oats"},
		{name: "explicit german", url: path + "?lang=german", want: "Hafer"},
		{name: "short code", url: path + "?lang=de", want: "Hafer"},
		{name: "unknown language falls back", url: path + "?lang=klingon", want: "Oats"},
		{name: "accept-language", url: path, accept: "de-DE,de;q=0.9,en;q=0.8", want: "Hafer"},
		{name: "accept-language english wins", url: path, accept: "de;q=0.2,en-GB;q=0.9", want: "Oats"},
		{name: "accept-language unknown", url: path, accept: "fr-FR,fr;q=0.9", want: "Oats"},
		{name: "query beats header", url: path + "?lang=english", accept: "de", want: "Oats"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			if tc.accept != "" {
				header.Set("Accept-Language", tc.accept)
			}
			resp := get(t, tc.url, header)
			defer resp.Body.Close()
			var got productsJSON
			decode(t, resp, &got)
			if name := findProduct(t, got.Products, 2068).Name; name != tc.want {
				t.Errorf("product 2068 = %q, want %q", name, tc.want)
			}
		})
	}
}

func TestEfficiency(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	var got efficiencyJSON
	getJSON(t, url+"/api/v1/islands/"+julianaID+"/efficiency?lang=german", http.StatusOK, &got)

	if got.Island.ID != julianaID {
		t.Fatalf("island = %+v", got.Island)
	}
	if len(got.Products) != 56 {
		t.Fatalf("got %d products, want 56", len(got.Products))
	}
	// Most wasted production first: that is what the view is for.
	for i := 1; i < len(got.Products); i++ {
		if got.Products[i-1].Wasted < got.Products[i].Wasted {
			t.Fatalf("not sorted by wasted production: %v before %v",
				got.Products[i-1].Wasted, got.Products[i].Wasted)
		}
	}

	var withoutPerfect int
	for _, p := range got.Products {
		switch {
		case p.PerfectGeneration == 0:
			withoutPerfect++
			if p.Efficiency != nil {
				t.Errorf("product %d has no perfect generation but efficiency %v", p.GUID, *p.Efficiency)
			}
		case p.Efficiency == nil:
			t.Errorf("product %d has perfect generation %v but no efficiency", p.GUID, p.PerfectGeneration)
		}
	}
	if withoutPerfect != 21 {
		t.Errorf("%d products without a perfect generation, want the capture's 21", withoutPerfect)
	}

	for _, p := range got.Products {
		if p.GUID != 2068 {
			continue
		}
		if p.Name != "Hafer" {
			t.Errorf("product 2068 = %q, want the German name", p.Name)
		}
		if want := float32(27.916876) - float32(23.191875); p.Wasted != want {
			t.Errorf("wasted = %v, want %v", p.Wasted, want)
		}
		if p.Efficiency == nil || *p.Efficiency < 0.83 || *p.Efficiency > 0.84 {
			t.Errorf("efficiency = %v, want generation/perfect generation (~0.83)", p.Efficiency)
		}
	}
}

func TestHistory(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)
	base := url + "/api/v1/islands/" + julianaID + "/products/2068/history"

	t.Run("default range", func(t *testing.T) {
		var got historyJSON
		getJSON(t, base, http.StatusOK, &got)
		if got.Range != "1h" {
			t.Errorf("range = %q, want the default 1h", got.Range)
		}
		if got.From == nil || !got.From.Equal(got.To.Add(-time.Hour)) {
			t.Errorf("from/to = %v/%v, want a one-hour window", got.From, got.To)
		}
		if got.Product.GUID != 2068 || got.Product.Name != "Oats" {
			t.Errorf("product = %+v, want 2068 Oats", got.Product)
		}
		if len(got.Points) != 1 {
			t.Fatalf("got %d points, want the capture's single measurement", len(got.Points))
		}
		p := got.Points[0]
		if p.Generation != 23.191875 || p.Consumption != 24.699997 || p.Buildings != 6 {
			t.Errorf("point = %+v, want the stored measurement", p)
		}
		if p.Aggregated {
			t.Error("a full-resolution sample is reported as aggregated")
		}
		if !p.TS.Equal(*got.Island.ReceivedAt) {
			t.Errorf("point ts = %v, want the island's receive time %v", p.TS, got.Island.ReceivedAt)
		}
	})

	t.Run("four hours", func(t *testing.T) {
		var got historyJSON
		getJSON(t, base+"?range=4h", http.StatusOK, &got)
		if got.Range != "4h" || got.From == nil || !got.From.Equal(got.To.Add(-4*time.Hour)) {
			t.Errorf("range = %q, from = %v, to = %v", got.Range, got.From, got.To)
		}
		if len(got.Points) != 1 {
			t.Errorf("got %d points, want 1", len(got.Points))
		}
	})

	t.Run("session", func(t *testing.T) {
		var got historyJSON
		getJSON(t, base+"?range=session", http.StatusOK, &got)
		if got.Range != "session" {
			t.Fatalf("range = %q", got.Range)
		}
		if got.From == nil || !got.From.Equal(fx.sessionStart) {
			t.Errorf("from = %v, want the recorded session start %v", got.From, fx.sessionStart)
		}
		if len(got.Points) != 1 {
			t.Errorf("got %d points, want 1", len(got.Points))
		}
	})

	t.Run("unknown range", func(t *testing.T) {
		if msg := errorOf(t, base+"?range=7h", http.StatusBadRequest); !strings.Contains(msg, "7h") {
			t.Errorf("error = %q, want it to name the rejected range", msg)
		}
	})

	t.Run("product without samples", func(t *testing.T) {
		var got historyJSON
		getJSON(t, url+"/api/v1/islands/"+julianaID+"/products/999999/history", http.StatusOK, &got)
		if len(got.Points) != 0 {
			t.Errorf("got %d points for a product that was never measured", len(got.Points))
		}
		// An unknown GUID is shown, not swallowed.
		if got.Product.Name != "#999999" {
			t.Errorf("unknown product name = %q, want #999999", got.Product.Name)
		}
	})

	t.Run("non-numeric product", func(t *testing.T) {
		errorOf(t, url+"/api/v1/islands/"+julianaID+"/products/oats/history", http.StatusBadRequest)
	})
}

// --no-db means there is no history; the endpoint says so rather than
// answering with an empty series that reads like "nothing happened".
func TestHistoryWithoutStore(t *testing.T) {
	fx := loadFixture(t, false)
	_, url := newServer(t, fx)

	msg := errorOf(t, url+"/api/v1/islands/"+julianaID+"/products/2068/history", http.StatusServiceUnavailable)
	if !strings.Contains(msg, "--no-db") {
		t.Errorf("error = %q, want it to name the reason", msg)
	}
}

func TestUnknownIsland(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	// An island that does not exist, and an id that is not one at all. The
	// island id is the pair, so a bare island number is not an island.
	for _, id := range []string{"3245-99", "9999-5", "5", "abc-5", "3245-x"} {
		for _, suffix := range []string{"/products", "/efficiency", "/products/2068/history"} {
			path := url + "/api/v1/islands/" + id + suffix
			if msg := errorOf(t, path, http.StatusNotFound); !strings.Contains(msg, id) {
				t.Errorf("GET %s: error = %q, want it to name the island", path, msg)
			}
		}
	}
}

// A run without a rule engine really has no active alerts, so the answer is
// an empty list rather than a 404 or a 503: the UI is built against the
// endpoint either way.
func TestAlertsAreEmptyWithoutAnEngine(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	var got []any
	getJSON(t, url+"/api/v1/alerts?active=true", http.StatusOK, &got)
	if len(got) != 0 {
		t.Errorf("alerts = %v, want an empty list", got)
	}
}

// Without LAN mode there is no QR code, and saying "404" rather than serving
// one keeps the token out of a run nobody asked to share.
func TestLANQRWithoutLANMode(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	if msg := errorOf(t, url+"/api/v1/lan/qr.png", http.StatusNotFound); !strings.Contains(msg, "LAN") {
		t.Errorf("error = %q, want it to explain that LAN mode is missing", msg)
	}
}

func TestUnknownEndpoint(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	for _, path := range []string{"/api/v1/nonsense", "/api/v1/islands/", "/api/v1/islands/3245-5"} {
		errorOf(t, url+path, http.StatusNotFound)
	}
}

// Nothing here may be written to. Every non-GET method is refused before it
// reaches a handler, which is the guard, not the absence of write handlers.
func TestWriteMethodsAreRefused(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, path := range []string{"/api/v1/status", "/api/v1/islands", "/api/v1/events", "/"} {
			req, err := http.NewRequest(method, url+path, strings.NewReader("{}"))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, path, resp.StatusCode)
			}
			if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") {
				t.Errorf("%s %s: Allow = %q", method, path, allow)
			}
		}
	}
}

// The UI is embedded in the executable and served at the root.
func TestStaticUI(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	resp := get(t, url+"/", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q, want HTML", ct)
	}
	body := readAll(t, resp)
	if !strings.Contains(body, "Tabularium 117") || !strings.Contains(body, "/api/v1/events") {
		t.Errorf("the page does not look like the UI:\n%s", body)
	}
}

// findProduct returns the product with guid, failing the test when it is
// missing.
func findProduct(t *testing.T, products []productJSON, guid int32) productJSON {
	t.Helper()
	for _, p := range products {
		if p.GUID == guid {
			return p
		}
	}
	t.Fatalf("product %d is missing from the response", guid)
	return productJSON{}
}

// After a SessionStart the game delivers one tick in which every island has
// zero products (docs/protocol.md, "Live capture 2026-09-22"). That is a
// warm-up, not ten empty islands, and the status has to say so - otherwise
// the UI shows "no production on this island" for up to two minutes.
func TestWarmingUpWhileEveryIslandIsEmpty(t *testing.T) {
	empty := model.ProductStat{}
	cases := []struct {
		name     string
		products [][]model.ProductStat
		want     bool
	}{
		{"no islands at all", nil, false},
		{"every island empty", [][]model.ProductStat{nil, nil}, true},
		{"one island has products", [][]model.ProductStat{nil, {empty}}, false},
		{"all islands have products", [][]model.ProductStat{{empty}, {empty}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := state.New()
			for i, products := range tc.products {
				st.Put(model.IslandSnapshot{
					Key:           model.IslandKey{SessionGUID: 3245, IslandID: int32(i)},
					Name:          "Isle",
					ReceivedAt:    time.Now(),
					GameTimestamp: int64(150_000_000 + i),
					Products:      products,
				})
			}
			srv := server.New(server.Options{State: st, Version: "test"})
			ts := httptest.NewServer(srv.Handler())
			t.Cleanup(ts.Close)

			var got statusJSON
			getJSON(t, ts.URL+"/api/v1/status", http.StatusOK, &got)
			if got.WarmingUp != tc.want {
				t.Errorf("warmingUp = %v, want %v", got.WarmingUp, tc.want)
			}
			if len(tc.products) == 0 {
				if got.Tick != nil {
					t.Errorf("tick = %v, want null before anything was received", *got.Tick)
				}
				return
			}
			// The islands of one tick share their timestamp; the newest wins
			// while a tick is still arriving island by island.
			want := int64(150_000_000 + len(tc.products) - 1)
			if got.Tick == nil || *got.Tick != want {
				t.Errorf("tick = %v, want %d", got.Tick, want)
			}
		})
	}
}

// Every island summary carries the tick it came from, so a client can tell
// which snapshots belong together.
func TestIslandSummaryCarriesTheTick(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)

	var got []islandJSON
	getJSON(t, url+"/api/v1/islands", http.StatusOK, &got)
	if len(got) == 0 {
		t.Fatal("no islands")
	}
	for _, island := range got {
		if island.Tick == nil || *island.Tick == 0 {
			t.Fatalf("island %s has no tick", island.ID)
		}
	}
}

// The long ranges are what the two-minute tick makes worth offering, and an
// unknown one still has to name the alternatives.
func TestHistoryLongRanges(t *testing.T) {
	fx := loadFixture(t, true)
	_, url := newServer(t, fx)
	base := url + "/api/v1/islands/" + julianaID + "/products/2068/history"

	for name, window := range map[string]time.Duration{
		"24h": 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
	} {
		var got historyJSON
		getJSON(t, base+"?range="+name, http.StatusOK, &got)
		if got.Range != name {
			t.Errorf("range = %q, want %q", got.Range, name)
		}
		if got.From == nil || !got.From.Equal(got.To.Add(-window)) {
			t.Errorf("range %s: from = %v, to = %v; want a %v window", name, got.From, got.To, window)
		}
	}

	msg := errorOf(t, base+"?range=30d", http.StatusBadRequest)
	for _, name := range []string{"1h", "4h", "24h", "7d", "session"} {
		if !strings.Contains(msg, name) {
			t.Errorf("error %q does not name the accepted range %s", msg, name)
		}
	}
}
