// Command gen-catalog builds internal/catalog/catalog.json from a pinned
// revision of anno-mods/anno-117-calculator (MIT license; see
// THIRD_PARTY_NOTICES.md). It reads the calculator's js/params.js, a JSON
// object behind a small JavaScript prefix, and extracts the game's
// products, buildings (factories, modules, residences), workforce types,
// regions, and sessions with their localized names.
//
// Usage:
//
//	go run ./tools/gen-catalog --calculator-dir <path> [--out internal/catalog/catalog.json] [--revision <sha>]
//
// The output is deterministic: entries are sorted by GUID, and
// encoding/json already sorts map keys, so two runs against the same input
// produce byte-identical files.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	jsPrefix = "if(window.params == null)window.params="
	source   = "anno-mods/anno-117-calculator"
	license  = "MIT"
)

// syntheticProducts are entries the calculator cannot provide because they
// are not game objects, but which the pipe delivers all the same.
//
// GUID 0 appears as a product guid in the live capture of 2026-09-22 on an
// entry with one building and no output, and as a workforce guid in the
// connector capture - a building that produces nothing (docs/protocol.md,
// open questions). Rendering it as "#0" told a player nothing and, worse,
// made it look like a catalog gap: "never swallow an unknown GUID" exists
// for GUIDs the catalog is missing, and this one is not missing, it means
// "none". Naming it here keeps Lookup, Name and the unknown-GUID log
// consistent with each other. Only English and German are given; every other
// language falls back to English, as it does for any missing translation.
var syntheticProducts = []outProduct{{
	GUID: 0,
	Names: map[string]string{
		"english": "(no product)",
		"german":  "(kein Produkt)",
	},
	Regions: []string{},
}}

// categoryByFilterName maps a productFilter's English locaText to the
// stable category id used in catalog.json.
var categoryByFilterName = map[string]string{
	"Consumer Goods":         "consumer",
	"Construction Materials": "construction",
	"Raw Materials":          "raw",
	"Agricultural Goods":     "agricultural",
	"Intermediate Goods":     "intermediate",
}

// --- input shapes (subset of js/params.js we care about) ---

type calcParams struct {
	Languages          []string      `json:"languages"`
	Products           []calcNamed   `json:"products"`
	Factories          []calcNamed   `json:"factories"`
	Modules            []calcNamed   `json:"modules"`
	ResidenceBuildings []calcNamed   `json:"residenceBuildings"`
	Workforce          []calcNamed   `json:"workforce"`
	Regions            []calcRegion  `json:"regions"`
	Sessions           []calcSession `json:"sessions"`
	ProductFilters     []calcFilter  `json:"productFilters"`
}

type calcNamed struct {
	GUID              int32             `json:"guid"`
	LocaText          map[string]string `json:"locaText"`
	AssociatedRegions []string          `json:"associatedRegions"`
	IsAbstract        bool              `json:"isAbstract"`
}

type calcRegion struct {
	GUID     int32             `json:"guid"`
	ID       string            `json:"id"`
	LocaText map[string]string `json:"locaText"`
}

type calcSession struct {
	GUID     int32             `json:"guid"`
	LocaText map[string]string `json:"locaText"`
	Region   int32             `json:"region"`
}

type calcFilter struct {
	GUID     int32             `json:"guid"`
	LocaText map[string]string `json:"locaText"`
	Products []int32           `json:"products"`
}

// --- output shapes (internal/catalog/catalog.json) ---

type outProduct struct {
	GUID     int32             `json:"guid"`
	Names    map[string]string `json:"names"`
	Category string            `json:"category"`
	Regions  []string          `json:"regions"`
	Abstract bool              `json:"abstract"`
}

type outBuilding struct {
	GUID    int32             `json:"guid"`
	Names   map[string]string `json:"names"`
	Regions []string          `json:"regions"`
	Kind    string            `json:"kind"`
}

type outWorkforce struct {
	GUID    int32             `json:"guid"`
	Names   map[string]string `json:"names"`
	Regions []string          `json:"regions"`
}

type outRegion struct {
	GUID  int32             `json:"guid"`
	ID    string            `json:"id"`
	Names map[string]string `json:"names"`
}

type outSession struct {
	GUID   int32             `json:"guid"`
	Names  map[string]string `json:"names"`
	Region int32             `json:"region"`
}

type outFile struct {
	Source    string         `json:"source"`
	Revision  string         `json:"revision"`
	License   string         `json:"license"`
	Languages []string       `json:"languages"`
	Products  []outProduct   `json:"products"`
	Buildings []outBuilding  `json:"buildings"`
	Workforce []outWorkforce `json:"workforce"`
	Regions   []outRegion    `json:"regions"`
	Sessions  []outSession   `json:"sessions"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-catalog:", err)
		os.Exit(1)
	}
}

func run() error {
	calculatorDir := flag.String("calculator-dir", "", "path to the anno-117-calculator checkout")
	out := flag.String("out", "internal/catalog/catalog.json", "output path for catalog.json")
	revisionFlag := flag.String("revision", "", "override the source revision (required if it cannot be read from the checkout's .git directory)")
	flag.Parse()

	if *calculatorDir == "" {
		return fmt.Errorf("--calculator-dir is required")
	}

	revision := *revisionFlag
	if revision == "" {
		var err error
		revision, err = readRevision(*calculatorDir)
		if err != nil {
			return fmt.Errorf("determine revision (pass --revision to override): %w", err)
		}
	}

	params, err := readParams(*calculatorDir)
	if err != nil {
		return err
	}

	category, err := buildCategories(params.ProductFilters)
	if err != nil {
		return err
	}

	products := make([]outProduct, 0, len(params.Products)+len(syntheticProducts))
	products = append(products, syntheticProducts...)
	for _, p := range params.Products {
		products = append(products, outProduct{
			GUID:     p.GUID,
			Names:    p.LocaText,
			Category: category[p.GUID],
			Regions:  emptyIfNil(p.AssociatedRegions),
			Abstract: p.IsAbstract,
		})
	}
	sort.Slice(products, func(i, j int) bool { return products[i].GUID < products[j].GUID })

	buildings := make([]outBuilding, 0, len(params.Factories)+len(params.Modules)+len(params.ResidenceBuildings))
	for _, f := range params.Factories {
		buildings = append(buildings, outBuilding{GUID: f.GUID, Names: f.LocaText, Regions: emptyIfNil(f.AssociatedRegions), Kind: "factory"})
	}
	for _, m := range params.Modules {
		buildings = append(buildings, outBuilding{GUID: m.GUID, Names: m.LocaText, Regions: emptyIfNil(m.AssociatedRegions), Kind: "module"})
	}
	for _, r := range params.ResidenceBuildings {
		buildings = append(buildings, outBuilding{GUID: r.GUID, Names: r.LocaText, Regions: emptyIfNil(r.AssociatedRegions), Kind: "residence"})
	}
	sort.Slice(buildings, func(i, j int) bool { return buildings[i].GUID < buildings[j].GUID })

	workforce := make([]outWorkforce, 0, len(params.Workforce))
	for _, w := range params.Workforce {
		workforce = append(workforce, outWorkforce{GUID: w.GUID, Names: w.LocaText, Regions: emptyIfNil(w.AssociatedRegions)})
	}
	sort.Slice(workforce, func(i, j int) bool { return workforce[i].GUID < workforce[j].GUID })

	regions := make([]outRegion, 0, len(params.Regions))
	for _, r := range params.Regions {
		regions = append(regions, outRegion{GUID: r.GUID, ID: r.ID, Names: r.LocaText})
	}
	sort.Slice(regions, func(i, j int) bool { return regions[i].GUID < regions[j].GUID })

	sessions := make([]outSession, 0, len(params.Sessions))
	for _, s := range params.Sessions {
		sessions = append(sessions, outSession{GUID: s.GUID, Names: s.LocaText, Region: s.Region})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].GUID < sessions[j].GUID })

	warnCrossKindCollisions(products, buildings, workforce, regions, sessions)

	languages := append([]string(nil), params.Languages...)
	sort.Strings(languages)

	result := outFile{
		Source:    source,
		Revision:  revision,
		License:   license,
		Languages: languages,
		Products:  products,
		Buildings: buildings,
		Workforce: workforce,
		Regions:   regions,
		Sessions:  sessions,
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal catalog: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}
	return nil
}

// readParams reads and decodes js/params.js.
func readParams(calculatorDir string) (*calcParams, error) {
	path := filepath.Join(calculatorDir, "js", "params.js")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	text := string(raw)
	idx := strings.Index(text, jsPrefix)
	if idx == -1 {
		return nil, fmt.Errorf("%s does not start with the expected prefix %q", path, jsPrefix)
	}
	jsonText := strings.TrimSpace(text[idx+len(jsPrefix):])
	jsonText = strings.TrimSuffix(jsonText, ";")

	var params calcParams
	if err := json.Unmarshal([]byte(jsonText), &params); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &params, nil
}

// readRevision determines the calculator checkout's commit SHA from its
// .git directory, without shelling out to git (a plain read-only checkout
// need not have git on PATH). It never guesses: a HEAD it cannot resolve is
// an error, and the caller is expected to pass --revision instead.
func readRevision(calculatorDir string) (string, error) {
	headPath := filepath.Join(calculatorDir, ".git", "HEAD")
	raw, err := os.ReadFile(headPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", headPath, err)
	}
	head := strings.TrimSpace(string(raw))

	const refPrefix = "ref: "
	if !strings.HasPrefix(head, refPrefix) {
		// Detached HEAD: the file already contains the SHA.
		return head, nil
	}
	ref := strings.TrimPrefix(head, refPrefix)

	refPath := filepath.Join(calculatorDir, ".git", filepath.FromSlash(ref))
	if raw, err := os.ReadFile(refPath); err == nil {
		return strings.TrimSpace(string(raw)), nil
	}

	packedRefsPath := filepath.Join(calculatorDir, ".git", "packed-refs")
	packed, err := os.ReadFile(packedRefsPath)
	if err != nil {
		return "", fmt.Errorf("resolve %s: no loose ref file and no packed-refs: %w", ref, err)
	}
	for _, line := range strings.Split(string(packed), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == ref {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("resolve %s: not found in packed-refs", ref)
}

// buildCategories maps each product GUID that appears in a productFilter to
// the filter's stable category id. A product in more than one filter takes
// the category of the lowest-GUID filter; the generator warns on stderr
// when that happens.
func buildCategories(filters []calcFilter) (map[int32]string, error) {
	sorted := append([]calcFilter(nil), filters...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].GUID < sorted[j].GUID })

	category := make(map[int32]string)
	assignedBy := make(map[int32]int32) // product guid -> filter guid that won
	for _, f := range sorted {
		name := f.LocaText["english"]
		id, ok := categoryByFilterName[name]
		if !ok {
			return nil, fmt.Errorf("productFilter %d has unrecognized English name %q", f.GUID, name)
		}
		for _, p := range f.Products {
			if winner, exists := assignedBy[p]; exists {
				fmt.Fprintf(os.Stderr, "gen-catalog: warning: product %d is in more than one productFilter (%d and %d); using category %q from filter %d\n",
					p, winner, f.GUID, category[p], winner)
				continue
			}
			assignedBy[p] = f.GUID
			category[p] = id
		}
	}
	return category, nil
}

// warnCrossKindCollisions warns on stderr about GUIDs that appear in more
// than one entry kind. internal/catalog.Catalog.Lookup resolves these with
// products taking priority, but a silent collision is worth flagging.
func warnCrossKindCollisions(products []outProduct, buildings []outBuilding, workforce []outWorkforce, regions []outRegion, sessions []outSession) {
	seenIn := make(map[int32][]string)
	for _, p := range products {
		seenIn[p.GUID] = append(seenIn[p.GUID], "product")
	}
	for _, b := range buildings {
		seenIn[b.GUID] = append(seenIn[b.GUID], "building")
	}
	for _, w := range workforce {
		seenIn[w.GUID] = append(seenIn[w.GUID], "workforce")
	}
	for _, r := range regions {
		seenIn[r.GUID] = append(seenIn[r.GUID], "region")
	}
	for _, s := range sessions {
		seenIn[s.GUID] = append(seenIn[s.GUID], "session")
	}

	guids := make([]int32, 0)
	for guid, kinds := range seenIn {
		if len(kinds) > 1 {
			guids = append(guids, guid)
		}
	}
	sort.Slice(guids, func(i, j int) bool { return guids[i] < guids[j] })
	for _, guid := range guids {
		fmt.Fprintf(os.Stderr, "gen-catalog: warning: guid %d appears in multiple kinds: %v; Catalog.Lookup resolves it as \"product\" first\n", guid, seenIn[guid])
	}
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
