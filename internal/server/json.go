package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/catalog"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/store"
)

// contentTypeJSON is sent on every API response, including the errors.
const contentTypeJSON = "application/json; charset=utf-8"

// errorResponse is the body of every API failure.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON serialises v and sends it with status. The body is built before
// anything is written, so a serialisation failure cannot leave a half-written
// response behind a 200.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot serialise the response")
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	w.Write(body)
}

// writeError sends {"error": message} with status.
func writeError(w http.ResponseWriter, status int, message string) {
	body, err := json.Marshal(errorResponse{Error: message})
	if err != nil {
		// errorResponse is a struct of one string; this cannot happen.
		body = []byte(`{"error":"internal error"}`)
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	w.Write(body)
}

// jsonTime renders a time that may be unset: the zero time becomes null
// rather than the year 1, which no client can do anything sensible with.
func jsonTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// --- island identity in the API ---

// islandID is the island's identity in URLs and JSON: "<sessionGUID>-<islandID>".
// The pair is the only unique key there is (KONZEPT.md section 4).
func islandID(key model.IslandKey) string {
	return fmt.Sprintf("%d-%d", key.SessionGUID, key.IslandID)
}

// parseIslandID is the inverse of islandID. The session GUID may be negative,
// so the parts are split at the last separator, not the first.
func parseIslandID(id string) (model.IslandKey, bool) {
	cut := strings.LastIndex(id, "-")
	if cut <= 0 || cut == len(id)-1 {
		return model.IslandKey{}, false
	}
	session, err := strconv.ParseInt(id[:cut], 10, 32)
	if err != nil {
		return model.IslandKey{}, false
	}
	island, err := strconv.ParseInt(id[cut+1:], 10, 32)
	if err != nil {
		return model.IslandKey{}, false
	}
	return model.IslandKey{SessionGUID: int32(session), IslandID: int32(island)}, true
}

// --- names ---

// localized picks the name for lang out of a catalog entry, falling back to
// English and then to the GUID, exactly as catalog.Name does for the generic
// lookup.
func localized(e catalog.Entry, lang string) string {
	if name := e.Names[lang]; name != "" {
		return name
	}
	if name := e.Names["english"]; name != "" {
		return name
	}
	return fmt.Sprintf("#%d", e.GUID)
}

// kindName resolves a GUID that is known to be of one kind - a building, a
// workforce type, a session - through that kind first. Falling back to
// catalog.Name keeps the "never swallow an unknown GUID" rule: it is what
// logs the GUID once and renders it as "#123".
func (s *Server) kindName(lookup func(int32) (catalog.Entry, bool), guid int32, lang string) string {
	if e, ok := lookup(guid); ok {
		return localized(e, lang)
	}
	return s.catalog.Name(guid, lang)
}

// --- status ---

type connectionDTO struct {
	Mode            string     `json:"mode"`
	State           string     `json:"state"`
	Err             string     `json:"err"`
	Since           *time.Time `json:"since"`
	ProtocolVersion int32      `json:"protocolVersion"`
	LastFrameAt     *time.Time `json:"lastFrameAt"`
}

type sessionDTO struct {
	Headline  string     `json:"headline"`
	StartedAt *time.Time `json:"startedAt"`
}

// lanDTO is the status bar's view of LAN mode: is the second listener open,
// and is this client the PC itself? Local decides what the UI offers - the
// switch, the QR code and the URL with the token exist on the loopback side
// only (KONZEPT.md section 6).
type lanDTO struct {
	Enabled bool `json:"enabled"`
	Local   bool `json:"local"`
}

type historyDTO struct {
	Enabled        bool       `json:"enabled"`
	LastSampleTime *time.Time `json:"lastSampleTime"`
}

// alertsStatusDTO is the status bar's badge: how many warnings are open right
// now. Info alerts - the imports - are not warnings and are not counted; the
// list itself, with both, is /api/v1/alerts.
type alertsStatusDTO struct {
	Active int `json:"active"`
}

// statusDTO is the status bar's whole picture.
//
// Tick is the game's own timestamp of the newest snapshot - the tick id, not
// a wall clock (docs/protocol.md). It is null while nothing has been
// received. WarmingUp says the islands are known but none of them has
// reported any production yet, which is what the first tick after a
// SessionStart looks like; see warmingUp.
type statusDTO struct {
	Version    string          `json:"version"`
	Connection connectionDTO   `json:"connection"`
	Session    sessionDTO      `json:"session"`
	LAN        lanDTO          `json:"lan"`
	Islands    int             `json:"islands"`
	History    historyDTO      `json:"history"`
	Alerts     alertsStatusDTO `json:"alerts"`
	Tick       *int64          `json:"tick"`
	WarmingUp  bool            `json:"warmingUp"`
}

// status builds the current status. Nothing in it is a catalog name, so it
// takes no language; local is the one part that differs per client, because
// it says which listener asked.
func (s *Server) status(local bool) statusDTO {
	conn := s.state.Connection()
	headline, startedAt := s.state.Session()
	islands := s.state.Islands()

	out := statusDTO{
		Version: s.version,
		Connection: connectionDTO{
			Mode:            conn.Mode,
			State:           conn.State,
			Err:             conn.Err,
			Since:           jsonTime(conn.Since),
			ProtocolVersion: conn.ProtocolVersion,
			LastFrameAt:     jsonTime(conn.LastFrameAt),
		},
		Session:   sessionDTO{Headline: headline, StartedAt: jsonTime(startedAt)},
		LAN:       lanDTO{Enabled: s.lan.isEnabled(), Local: local},
		Islands:   len(islands),
		Alerts:    alertsStatusDTO{Active: countWarnings(s.activeAlerts())},
		Tick:      jsonTick(latestTick(islands)),
		WarmingUp: warmingUp(islands),
	}
	if s.store != nil {
		out.History = historyDTO{Enabled: true, LastSampleTime: jsonTime(s.store.LastSampleTime())}
	}
	return out
}

// latestTick is the newest game timestamp any island carries. The islands of
// one tick all carry the same one, so the maximum is "the tick we are
// showing" even while a tick is still arriving island by island.
func latestTick(islands []model.IslandSnapshot) int64 {
	var newest int64
	for _, snap := range islands {
		newest = max(newest, snap.GameTimestamp)
	}
	return newest
}

// warmingUp reports whether the live view is waiting for the first statistics
// of a session.
//
// Loading a save sends SessionStart and then one tick in which every island
// reports zero products (docs/protocol.md, "Live capture 2026-09-22"); the
// real numbers follow up to two minutes later. Islands with no production at
// all do exist, so "every island is empty" is the only signal there is - and
// with no islands at all there is nothing to warm up, which is the
// disconnected case and already has its own display.
//
// It is computed here rather than kept in state: it is a question about the
// current picture, and the picture is what state already holds.
func warmingUp(islands []model.IslandSnapshot) bool {
	if len(islands) == 0 {
		return false
	}
	for _, snap := range islands {
		if len(snap.Products) > 0 {
			return false
		}
	}
	return true
}

// jsonTick renders a game timestamp that may be unset: zero becomes null,
// because 0 ms of game time is "we have not been told" and not a measurement.
func jsonTick(tick int64) *int64 {
	if tick == 0 {
		return nil
	}
	return &tick
}

// --- islands ---

type islandDTO struct {
	ID          string     `json:"id"`
	SessionGUID int32      `json:"sessionGuid"`
	SessionName string     `json:"sessionName"`
	IslandID    int32      `json:"islandId"`
	Name        string     `json:"name"`
	Products    int        `json:"products"`
	Deficits    int        `json:"deficits"`
	ReceivedAt  *time.Time `json:"receivedAt"`
	// Tick is the game timestamp this snapshot came with: the id of the tick,
	// shared by every island of it. null when the snapshot carried none.
	Tick *int64 `json:"tick"`
}

// islandSummary summarises one snapshot. The island name is stored as delivered and
// trimmed here, because this is the display side (docs/protocol.md).
func (s *Server) islandSummary(snap model.IslandSnapshot, lang string) islandDTO {
	deficits := 0
	for _, p := range snap.Products {
		if p.Delta < 0 {
			deficits++
		}
	}
	return islandDTO{
		ID:          islandID(snap.Key),
		SessionGUID: snap.Key.SessionGUID,
		SessionName: s.kindName(s.catalog.Session, snap.Key.SessionGUID, lang),
		IslandID:    snap.Key.IslandID,
		Name:        strings.TrimSpace(snap.Name),
		Products:    len(snap.Products),
		Deficits:    deficits,
		ReceivedAt:  jsonTime(snap.ReceivedAt),
		Tick:        jsonTick(snap.GameTimestamp),
	}
}

// --- products ---

// namedAmount is one entry of the workforce and building maps: the GUID's
// name next to its amount, so the UI needs no second lookup.
type namedAmount struct {
	Name   string `json:"name"`
	Amount int32  `json:"amount"`
}

type productDTO struct {
	GUID               int32                  `json:"guid"`
	Name               string                 `json:"name"`
	Category           string                 `json:"category"`
	Generation         float32                `json:"generation"`
	Consumption        float32                `json:"consumption"`
	Delta              float32                `json:"delta"`
	PerfectGeneration  float32                `json:"perfectGeneration"`
	PerfectConsumption float32                `json:"perfectConsumption"`
	Buildings          int32                  `json:"buildings"`
	Maintenance        int32                  `json:"maintenance"`
	Income             float32                `json:"income"`
	Profit             int32                  `json:"profit"`
	AvgProductivity    float32                `json:"avgProductivity"`
	SummedProductivity float32                `json:"summedProductivity"`
	Workforce          map[string]namedAmount `json:"workforce"`
	BuildingsByGUID    map[string]namedAmount `json:"buildingsByGuid"`
}

type productsDTO struct {
	Island   islandDTO    `json:"island"`
	Products []productDTO `json:"products"`
}

// product converts one product stat, resolving every GUID it carries.
func (s *Server) product(p model.ProductStat, lang string) productDTO {
	out := productDTO{
		GUID:               p.ProductGUID,
		Name:               s.catalog.Name(p.ProductGUID, lang),
		Generation:         p.Generation,
		Consumption:        p.Consumption,
		Delta:              p.Delta,
		PerfectGeneration:  p.PerfectGeneration,
		PerfectConsumption: p.PerfectConsumption,
		Buildings:          p.Buildings,
		Maintenance:        p.Maintenance,
		Income:             p.Income,
		Profit:             p.Profit,
		AvgProductivity:    p.AvgProductivity,
		SummedProductivity: p.SummedProductivity,
		Workforce:          s.amounts(p.Workforce, s.catalog.Workforce, lang),
		BuildingsByGUID:    s.amounts(p.BuildingsByGUID, s.catalog.Building, lang),
	}
	if e, ok := s.catalog.Product(p.ProductGUID); ok {
		out.Category = e.Category
	}
	return out
}

// amounts renders a GUID -> amount map with names. JSON object keys are
// strings, so the GUIDs are formatted as decimal strings.
func (s *Server) amounts(in map[int32]int32, lookup func(int32) (catalog.Entry, bool), lang string) map[string]namedAmount {
	out := make(map[string]namedAmount, len(in))
	for guid, amount := range in {
		out[strconv.FormatInt(int64(guid), 10)] = namedAmount{
			Name:   s.kindName(lookup, guid, lang),
			Amount: amount,
		}
	}
	return out
}

// --- efficiency ---

type efficiencyProductDTO struct {
	GUID              int32    `json:"guid"`
	Name              string   `json:"name"`
	Generation        float32  `json:"generation"`
	PerfectGeneration float32  `json:"perfectGeneration"`
	Buildings         int32    `json:"buildings"`
	Efficiency        *float64 `json:"efficiency"`
	Wasted            float32  `json:"wasted"`
	AvgProductivity   float32  `json:"avgProductivity"`
}

type efficiencyDTO struct {
	Island   islandDTO              `json:"island"`
	Products []efficiencyProductDTO `json:"products"`
}

// efficiency is Generation / PerfectGeneration, the measure KONZEPT.md
// section 2.3 settled on. Without a perfect value there is nothing to compare
// against, and the answer is null rather than a misleading zero.
func (s *Server) efficiencyProduct(p model.ProductStat, lang string) efficiencyProductDTO {
	out := efficiencyProductDTO{
		GUID:              p.ProductGUID,
		Name:              s.catalog.Name(p.ProductGUID, lang),
		Generation:        p.Generation,
		PerfectGeneration: p.PerfectGeneration,
		Buildings:         p.Buildings,
		Wasted:            p.PerfectGeneration - p.Generation,
		AvgProductivity:   p.AvgProductivity,
	}
	if p.PerfectGeneration != 0 {
		ratio := float64(p.Generation) / float64(p.PerfectGeneration)
		out.Efficiency = &ratio
	}
	return out
}

// --- alerts ---

// alertDTO is one warning as the API and the event stream render it
// (KONZEPT.md section 5). ID is absent for an alert that comes from the live
// engine: only a stored alert has a row id.
//
// Value is the rule's current number - the delta, or the productivity in
// percent. The history does not keep it (see internal/store, schema 2), so it
// is zero for a stored alert; Detail carries the numbers it was raised with.
type alertDTO struct {
	ID          *int64     `json:"id,omitempty"`
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

// alertEventDTO is the body of an "alert" event on the stream.
type alertEventDTO struct {
	Kind  string   `json:"kind"`
	Alert alertDTO `json:"alert"`
}

// alert converts one alert from the rule engine.
func (s *Server) alert(a alerts.Alert, lang string) alertDTO {
	return alertDTO{
		IslandID:    islandID(a.Island),
		IslandName:  strings.TrimSpace(a.IslandName),
		SessionName: s.kindName(s.catalog.Session, a.Island.SessionGUID, lang),
		ProductGUID: a.ProductGUID,
		ProductName: s.catalog.Name(a.ProductGUID, lang),
		Rule:        a.Rule,
		Severity:    a.Severity,
		RaisedAt:    a.RaisedAt,
		ClearedAt:   jsonTime(a.ClearedAt),
		Detail:      a.Detail,
		Value:       a.Value,
	}
}

// alertRow converts one alert from the history.
func (s *Server) alertRow(r store.AlertRow, lang string) alertDTO {
	out := s.alert(alerts.Alert{
		Island:      r.Island,
		IslandName:  r.IslandName,
		ProductGUID: r.ProductGUID,
		Rule:        r.Rule,
		Severity:    r.Severity,
		RaisedAt:    r.RaisedAt,
		ClearedAt:   r.ClearedAt,
		Detail:      r.Detail,
	}, lang)
	id := r.ID
	out.ID = &id
	return out
}

// countWarnings counts the alerts of severity warning.
func countWarnings(active []alerts.Alert) int {
	n := 0
	for _, a := range active {
		if a.Severity == alerts.SeverityWarning {
			n++
		}
	}
	return n
}

// activeAlerts returns the open alerts, newest first, or nothing when this
// run evaluates no rules.
func (s *Server) activeAlerts() []alerts.Alert {
	if s.alerts == nil {
		return nil
	}
	active := s.alerts.Active()
	// The engine orders by island and product; a list of warnings is read
	// newest first.
	sort.SliceStable(active, func(i, j int) bool { return active[i].RaisedAt.After(active[j].RaisedAt) })
	return active
}

// --- history ---

type historyProductDTO struct {
	GUID int32  `json:"guid"`
	Name string `json:"name"`
}

// pointDTO is one point of a series. Aggregated marks a compacted point and
// BucketMs says how wide it is (0 for a raw reading), so a client can tell a
// ten-minute mean from a one-minute one an older build left behind. Tick is
// the game timestamp, the largest one an aggregate covers.
type pointDTO struct {
	TS                 time.Time `json:"ts"`
	Generation         float32   `json:"generation"`
	Consumption        float32   `json:"consumption"`
	Delta              float32   `json:"delta"`
	PerfectGeneration  float32   `json:"perfectGeneration"`
	PerfectConsumption float32   `json:"perfectConsumption"`
	Buildings          int32     `json:"buildings"`
	AvgProductivity    float32   `json:"avgProductivity"`
	Aggregated         bool      `json:"aggregated"`
	BucketMs           int64     `json:"bucketMs"`
	Tick               int64     `json:"tick"`
}

type historySeriesDTO struct {
	Island  islandDTO         `json:"island"`
	Product historyProductDTO `json:"product"`
	Range   string            `json:"range"`
	From    *time.Time        `json:"from"`
	To      time.Time         `json:"to"`
	Points  []pointDTO        `json:"points"`
}

// point converts one stored measurement.
func point(p store.Point) pointDTO {
	return pointDTO{
		TS:                 p.TS,
		Generation:         p.Generation,
		Consumption:        p.Consumption,
		Delta:              p.Delta,
		PerfectGeneration:  p.PerfectGeneration,
		PerfectConsumption: p.PerfectConsumption,
		Buildings:          p.Buildings,
		AvgProductivity:    p.AvgProductivity,
		Aggregated:         p.Aggregated,
		BucketMs:           p.BucketMs,
		Tick:               p.GameTS,
	}
}

// logAttrs is a small helper so that handlers log a failure the same way.
func (s *Server) logError(msg string, err error, attrs ...any) {
	s.log.Error(msg, append([]any{"err", err}, attrs...)...)
}
