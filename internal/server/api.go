package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/store"
)

// historyRanges are the windows the UI offers (KONZEPT.md section 2.2).
//
// The long ones exist because the game ticks about every two minutes: a day
// is roughly 720 points per island and product, which is a chart, not a
// download (docs/protocol.md, "Consequences for Tabularium 117").
const (
	range1h      = "1h"
	range4h      = "4h"
	range24h     = "24h"
	range7d      = "7d"
	rangeSession = "session"
	// defaultRange is used when ?range= is missing.
	defaultRange = range1h
)

// rangeNames lists the accepted ranges in the order the UI shows them, so
// that the error message and the UI cannot drift apart.
var rangeNames = []string{range1h, range4h, range24h, range7d, rangeSession}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.status(isLocal(r)))
}

func (s *Server) handleIslands(w http.ResponseWriter, r *http.Request) {
	lang := s.language(r)
	islands := s.state.Islands()
	out := make([]islandDTO, 0, len(islands))
	for _, snap := range islands {
		out = append(out, s.islandSummary(snap, lang))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleProducts(w http.ResponseWriter, r *http.Request) {
	snap, lang, ok := s.lookupIsland(w, r)
	if !ok {
		return
	}
	products := make([]productDTO, 0, len(snap.Products))
	for _, p := range snap.Products {
		products = append(products, s.product(p, lang))
	}
	// Deficits first: the whole point of the live view is to see what is
	// running out (KONZEPT.md section 2.1).
	sort.SliceStable(products, func(i, j int) bool {
		if products[i].Delta != products[j].Delta {
			return products[i].Delta < products[j].Delta
		}
		return products[i].Name < products[j].Name
	})
	writeJSON(w, http.StatusOK, productsDTO{Island: s.islandSummary(snap, lang), Products: products})
}

func (s *Server) handleEfficiency(w http.ResponseWriter, r *http.Request) {
	snap, lang, ok := s.lookupIsland(w, r)
	if !ok {
		return
	}
	products := make([]efficiencyProductDTO, 0, len(snap.Products))
	for _, p := range snap.Products {
		products = append(products, s.efficiencyProduct(p, lang))
	}
	// Most wasted production first: that is the question this view answers.
	sort.SliceStable(products, func(i, j int) bool {
		if products[i].Wasted != products[j].Wasted {
			return products[i].Wasted > products[j].Wasted
		}
		return products[i].Name < products[j].Name
	})
	writeJSON(w, http.StatusOK, efficiencyDTO{Island: s.islandSummary(snap, lang), Products: products})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	snap, lang, ok := s.lookupIsland(w, r)
	if !ok {
		return
	}
	guid, err := strconv.ParseInt(r.PathValue("guid"), 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the product must be a numeric GUID")
		return
	}
	if s.store == nil {
		// Started with --no-db: there is no history to serve, and saying so
		// is better than an empty series the UI would read as "nothing
		// happened".
		writeError(w, http.StatusServiceUnavailable, "this run keeps no history (--no-db)")
		return
	}

	name := r.URL.Query().Get("range")
	if name == "" {
		name = defaultRange
	}
	from, to, ok := s.window(r.Context(), name)
	if !ok {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("unknown range %q; use one of %s", name, strings.Join(rangeNames, ", ")))
		return
	}

	points, err := s.store.History(r.Context(), snap.Key, int32(guid), from, to)
	if err != nil {
		s.logError("cannot read the history", err, "island", islandID(snap.Key), "product", guid)
		writeError(w, http.StatusInternalServerError, "cannot read the history")
		return
	}
	out := make([]pointDTO, 0, len(points))
	for _, p := range points {
		out = append(out, point(p))
	}
	writeJSON(w, http.StatusOK, historySeriesDTO{
		Island:  s.islandSummary(snap, lang),
		Product: historyProductDTO{GUID: int32(guid), Name: s.catalog.Name(int32(guid), lang)},
		Range:   name,
		From:    jsonTime(from),
		To:      to,
		Points:  out,
	})
}

// handleAlerts serves the warnings (KONZEPT.md section 5).
//
// ?active=true (the default) is the live picture and comes from the rule
// engine, which is the only place that knows what is open right now. A run
// without the engine answers with an empty list rather than an error: there
// really are no alerts then.
//
// ?active=false is the recorded history - open and closed alerts, newest
// first - and needs the database, so it answers 503 without one, exactly as
// the history endpoint does.
//
// ?info=false leaves the info alerts (no_local_production) out, in both
// lists. The warning history asks for that: an island records one for every
// good it does not produce, and a limit would otherwise fill up with them.
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	lang := s.language(r)
	query := r.URL.Query()

	active := true
	if raw := query.Get("active"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("active must be true or false, not %q", raw))
			return
		}
		active = parsed
	}
	withInfo := true
	if raw := query.Get("info"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("info must be true or false, not %q", raw))
			return
		}
		withInfo = parsed
	}
	limit, ok := alertLimit(w, query.Get("limit"))
	if !ok {
		return
	}

	if active {
		live := s.activeAlerts()
		if !withInfo {
			kept := live[:0:0]
			for _, a := range live {
				if a.Severity != alerts.SeverityInfo {
					kept = append(kept, a)
				}
			}
			live = kept
		}
		if limit > 0 && len(live) > limit {
			live = live[:limit]
		}
		out := make([]alertDTO, 0, len(live))
		for _, a := range live {
			out = append(out, s.alert(a, lang))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "this run keeps no history (--no-db)")
		return
	}
	var rows []store.AlertRow
	var err error
	if withInfo {
		rows, err = s.store.Alerts(r.Context(), false, limit)
	} else {
		rows, err = s.store.AlertsWithout(r.Context(), alerts.SeverityInfo, limit)
	}
	if err != nil {
		s.logError("cannot read the recorded alerts", err)
		writeError(w, http.StatusInternalServerError, "cannot read the recorded alerts")
		return
	}
	out := make([]alertDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.alertRow(row, lang))
	}
	writeJSON(w, http.StatusOK, out)
}

// alertLimit parses ?limit=. Empty means "no limit"; anything that is not a
// non-negative number is a mistake worth naming, because silently ignoring it
// would return far more rows than the caller asked for.
func alertLimit(w http.ResponseWriter, raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be a positive number, not %q", raw))
		return 0, false
	}
	return limit, true
}

// lookupIsland resolves the {id} path value against the live view. It answers the
// request itself when the island is unknown and reports ok=false; an id that
// cannot be parsed cannot name an island either, so it gets the same 404.
func (s *Server) lookupIsland(w http.ResponseWriter, r *http.Request) (model.IslandSnapshot, string, bool) {
	lang := s.language(r)
	id := r.PathValue("id")
	key, ok := parseIslandID(id)
	if ok {
		if snap, found := s.state.Snapshot(key); found {
			return snap, lang, true
		}
	}
	writeError(w, http.StatusNotFound, fmt.Sprintf("unknown island %q", id))
	return model.IslandSnapshot{}, lang, false
}

// window turns a range name into the closed interval to query. A zero from
// means "everything up to to": that happens for range=session when no session
// start is known, where cutting the series short would hide data.
func (s *Server) window(ctx context.Context, name string) (from, to time.Time, ok bool) {
	to = s.now()
	switch name {
	case range1h:
		return to.Add(-time.Hour), to, true
	case range4h:
		return to.Add(-4 * time.Hour), to, true
	case range24h:
		return to.Add(-24 * time.Hour), to, true
	case range7d:
		return to.Add(-7 * 24 * time.Hour), to, true
	case rangeSession:
		return s.sessionStart(ctx), to, true
	default:
		return time.Time{}, time.Time{}, false
	}
}

// sessionStart is when the current game session began: the newest recorded
// session, or - when nothing is recorded - what the live view saw.
func (s *Server) sessionStart(ctx context.Context) time.Time {
	if s.store != nil {
		sessions, err := s.store.Sessions(ctx, 1)
		if err != nil {
			s.logError("cannot read the recorded sessions", err)
		} else if len(sessions) > 0 {
			return sessions[0].StartedAt
		}
	}
	_, startedAt := s.state.Session()
	return startedAt
}
