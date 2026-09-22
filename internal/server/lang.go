package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// defaultLanguage is what an unrecognised wish falls back to. The catalog
// always has English names, so it is the one code that is certain to resolve.
const defaultLanguage = "english"

// shortCodes map the language tags a browser sends to the catalog's language
// codes. Everything else falls back to English, as catalog.Name does.
var shortCodes = map[string]string{
	"de": "german",
	"en": "english",
}

// language decides which language a request is answered in: an explicit
// ?lang= wins, otherwise the browser's Accept-Language is honoured.
//
// An unknown ?lang= is passed through rather than rejected; the catalog falls
// back to English for a language it does not have, which is a better answer
// than a 400 for a query parameter the user never typed.
func (s *Server) language(r *http.Request) string {
	if wanted := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang"))); wanted != "" {
		if full, ok := shortCodes[wanted]; ok {
			return full
		}
		return wanted
	}
	return acceptLanguage(r.Header.Get("Accept-Language"))
}

// acceptLanguage picks a catalog language from an Accept-Language header,
// honouring the q values. It understands the primary subtag only ("de-AT" is
// German), which is all the catalog distinguishes.
func acceptLanguage(header string) string {
	type candidate struct {
		tag string
		q   float64
		pos int
	}
	var candidates []candidate
	for i, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		tag := strings.ToLower(strings.TrimSpace(fields[0]))
		if tag == "" {
			continue
		}
		q := 1.0
		for _, f := range fields[1:] {
			value, ok := strings.CutPrefix(strings.TrimSpace(f), "q=")
			if !ok {
				continue
			}
			if parsed, err := strconv.ParseFloat(value, 64); err == nil {
				q = parsed
			}
		}
		candidates = append(candidates, candidate{tag: tag, q: q, pos: i})
	}
	// Equal weights keep the order they were sent in, which is what a client
	// that omits q values expects.
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].q > candidates[j].q })

	for _, c := range candidates {
		if c.q <= 0 {
			continue // "q=0" means "not this one".
		}
		primary, _, _ := strings.Cut(c.tag, "-")
		if full, ok := shortCodes[primary]; ok {
			return full
		}
	}
	return defaultLanguage
}
