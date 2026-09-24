package server

import (
	"cmp"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// Possible production sources (KONZEPT.md section 2.4): for a good an island
// needs but does not produce, the other islands that currently produce it
// with a positive local balance.
//
// That is all the data allows. The pipe carries no trade routes, ships, cargo
// or warehouse stock, so which island really supplies another is not known,
// and a positive balance is not a spare amount either: it may already be
// shipped somewhere, or be piling up in a storage. The API and the UI say
// "possible source" and "local balance" for that reason, and never
// "supplier" or "surplus".
//
// It is live context, computed from the state on every request: no alert, no
// history row, nothing stored.

type possibleSourcesDTO struct {
	Island  islandDTO         `json:"island"`
	Product historyProductDTO `json:"product"`
	// Ready is false while no complete statistics tick with goods in it is
	// known - right after connecting or loading a save. Groups is empty
	// then, and that means "not known yet", not "none found".
	Ready bool `json:"ready"`
	// Tick is the game timestamp of the tick every balance below comes from,
	// null while not ready.
	Tick *int64 `json:"tick"`
	// Groups holds the sessions that have at least one possible source: the
	// island's own session first, the others after it by session GUID. The
	// order is for reading only; a source in the own session is not a more
	// likely one.
	Groups []sourceGroupDTO `json:"groups"`
}

type sourceGroupDTO struct {
	SessionGUID int32  `json:"sessionGuid"`
	SessionName string `json:"sessionName"`
	// Own marks the session of the island that was asked about.
	Own bool `json:"own"`
	// Sources are ordered by local balance, highest first.
	Sources []possibleSourceDTO `json:"sources"`
}

type possibleSourceDTO struct {
	ID       string `json:"id"`
	IslandID int32  `json:"islandId"`
	Name     string `json:"name"`
	// Delta is the island's reported local balance of the good per minute:
	// generation minus consumption on that island. It is not an amount that
	// is free to ship.
	Delta float32 `json:"delta"`
}

// handleSources serves GET /api/v1/islands/{id}/products/{guid}/sources.
//
// It is an endpoint of its own rather than a field of /products because it
// has a different clock. /products is reloaded when that island's snapshot
// arrives, which is exactly when the other islands are still on the previous
// tick. The sources come from the newest complete tick instead
// (state.CompleteTick), and the UI asks for them only when someone opens
// them.
func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	snap, lang, ok := s.lookupIsland(w, r)
	if !ok {
		return
	}
	guid, err := strconv.ParseInt(r.PathValue("guid"), 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the product must be a numeric GUID")
		return
	}

	out := possibleSourcesDTO{
		Island:  s.islandSummary(snap, lang),
		Product: historyProductDTO{GUID: int32(guid), Name: s.catalog.Name(int32(guid), lang)},
		Groups:  []sourceGroupDTO{},
	}
	stamp, islands, ok := s.state.CompleteTick()
	// The all-empty tick after loading a save is complete and says nothing:
	// "no island with a positive balance" would be wrong for up to two
	// minutes (see warmingUp).
	if !ok || len(islands) == 0 || warmingUp(islands) {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Ready = true
	out.Tick = jsonTick(stamp)

	for _, group := range possibleSources(snap.Key, int32(guid), islands) {
		dto := sourceGroupDTO{
			SessionGUID: group.session,
			SessionName: s.kindName(s.catalog.Session, group.session, lang),
			Own:         group.session == snap.Key.SessionGUID,
			Sources:     make([]possibleSourceDTO, 0, len(group.sources)),
		}
		for _, src := range group.sources {
			// The balance is the complete tick's, the name the newest one:
			// an island renamed a few seconds ago is shown as it is called
			// now, and nobody wants the old name back.
			name := src.name
			if current, found := s.state.Snapshot(src.key); found {
				name = current.Name
			}
			dto.Sources = append(dto.Sources, possibleSourceDTO{
				ID:       islandID(src.key),
				IslandID: src.key.IslandID,
				Name:     strings.TrimSpace(name),
				Delta:    src.delta,
			})
		}
		out.Groups = append(out.Groups, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

type sourceGroup struct {
	session int32
	sources []possibleSource
}

type possibleSource struct {
	key   model.IslandKey
	name  string
	delta float32
}

// possibleSources picks the possible sources of guid for the island target
// from the islands of one tick, grouped by session.
//
// A possible source is another island - any session, since goods cross
// between provinces - with at least one building for the good and a positive,
// finite local balance. No building means the island does not produce the
// good itself, and a balance of zero or less means it keeps all of it. NaN
// and infinity are not balances at all, and JSON cannot carry them: one of
// them would turn the whole answer into an error.
//
// Groups come in the order the handler documents; within a group the highest
// balance comes first, ties by name and then island ID so the order is stable.
func possibleSources(target model.IslandKey, guid int32, islands []model.IslandSnapshot) []sourceGroup {
	bySession := map[int32][]possibleSource{}
	for _, snap := range islands {
		if snap.Key == target {
			continue
		}
		p, ok := lastEntry(snap.Products, guid)
		if !ok || p.Buildings <= 0 || !(p.Delta > 0) || math.IsInf(float64(p.Delta), 1) {
			continue
		}
		bySession[snap.Key.SessionGUID] = append(bySession[snap.Key.SessionGUID],
			possibleSource{key: snap.Key, name: strings.TrimSpace(snap.Name), delta: p.Delta})
	}

	groups := make([]sourceGroup, 0, len(bySession))
	for session, sources := range bySession {
		slices.SortFunc(sources, func(a, b possibleSource) int {
			return cmp.Or(
				cmp.Compare(b.delta, a.delta),
				strings.Compare(a.name, b.name),
				cmp.Compare(a.key.IslandID, b.key.IslandID),
			)
		})
		groups = append(groups, sourceGroup{session: session, sources: sources})
	}
	slices.SortFunc(groups, func(a, b sourceGroup) int {
		aOwn, bOwn := a.session == target.SessionGUID, b.session == target.SessionGUID
		switch {
		case aOwn && !bOwn:
			return -1
		case bOwn && !aOwn:
			return 1
		}
		return cmp.Compare(a.session, b.session)
	})
	return groups
}

// lastEntry finds guid in products. The pipeline keeps one entry per good,
// but the state accepts whatever it is given; if a good is listed twice, the
// later entry counts, as everywhere else (ingest.collapseDuplicates).
func lastEntry(products []model.ProductStat, guid int32) (model.ProductStat, bool) {
	for i := len(products) - 1; i >= 0; i-- {
		if products[i].ProductGUID == guid {
			return products[i], true
		}
	}
	return model.ProductStat{}, false
}
