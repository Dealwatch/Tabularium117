package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

//go:embed catalog.json
var embeddedData []byte

// Kind identifies which section of the catalog an Entry comes from.
type Kind string

const (
	KindProduct   Kind = "product"
	KindBuilding  Kind = "building"
	KindWorkforce Kind = "workforce"
	KindRegion    Kind = "region"
	KindSession   Kind = "session"
)

// kindPriority is the order in which Lookup resolves a GUID that happens to
// exist in more than one kind (which tools/gen-catalog warns about, but does
// not prevent). Earlier wins.
var kindPriority = []Kind{KindProduct, KindBuilding, KindWorkforce, KindRegion, KindSession}

// Entry is one catalog record. Category and Region are reused across kinds
// rather than adding a field per kind: Category holds the product category
// id for products and the region id (e.g. "Roman") for regions; Region
// holds the region GUID a session belongs to.
type Entry struct {
	GUID     int32
	Kind     Kind
	Names    map[string]string
	Category string
	Regions  []string
	Region   int32 // sessions only: GUID of the region the session belongs to
}

// Catalog resolves game GUIDs to names, categories, and regions.
//
// The zero value is not usable; construct one with Parse or Default.
type Catalog struct {
	// Logger receives one warning per unknown GUID. A nil Logger falls back
	// to slog.Default() at call time, so it may be left unset.
	Logger *slog.Logger

	revision  string
	license   string
	source    string
	languages []string

	entries map[Kind]map[int32]Entry

	mu      sync.Mutex
	unknown map[int32]bool
}

var (
	defaultOnce sync.Once
	defaultCat  *Catalog
	defaultErr  error
)

// Default returns the process-wide Catalog parsed from the embedded
// catalog.json. It panics if the embedded file fails to parse, which would
// mean the file shipped with the binary is corrupt.
func Default() *Catalog {
	defaultOnce.Do(func() {
		defaultCat, defaultErr = Parse(embeddedData)
	})
	if defaultErr != nil {
		panic(fmt.Sprintf("catalog: embedded catalog.json is invalid: %v", defaultErr))
	}
	return defaultCat
}

// --- on-disk shape of catalog.json, as written by tools/gen-catalog ---

type fileProduct struct {
	GUID     int32             `json:"guid"`
	Names    map[string]string `json:"names"`
	Category string            `json:"category"`
	Regions  []string          `json:"regions"`
	Abstract bool              `json:"abstract"`
}

type fileBuilding struct {
	GUID    int32             `json:"guid"`
	Names   map[string]string `json:"names"`
	Regions []string          `json:"regions"`
	Kind    string            `json:"kind"`
}

type fileWorkforce struct {
	GUID    int32             `json:"guid"`
	Names   map[string]string `json:"names"`
	Regions []string          `json:"regions"`
}

type fileRegion struct {
	GUID  int32             `json:"guid"`
	ID    string            `json:"id"`
	Names map[string]string `json:"names"`
}

type fileSession struct {
	GUID   int32             `json:"guid"`
	Names  map[string]string `json:"names"`
	Region int32             `json:"region"`
}

type file struct {
	Source    string          `json:"source"`
	Revision  string          `json:"revision"`
	License   string          `json:"license"`
	Languages []string        `json:"languages"`
	Products  []fileProduct   `json:"products"`
	Buildings []fileBuilding  `json:"buildings"`
	Workforce []fileWorkforce `json:"workforce"`
	Regions   []fileRegion    `json:"regions"`
	Sessions  []fileSession   `json:"sessions"`
}

// Parse decodes catalog.json data into a Catalog. It is exported mainly for
// tests; production code should use Default().
func Parse(data []byte) (*Catalog, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("catalog: decode: %w", err)
	}

	c := &Catalog{
		revision:  f.Revision,
		license:   f.License,
		source:    f.Source,
		languages: append([]string(nil), f.Languages...),
		entries:   make(map[Kind]map[int32]Entry),
		unknown:   make(map[int32]bool),
	}

	products := make(map[int32]Entry, len(f.Products))
	for _, p := range f.Products {
		products[p.GUID] = Entry{GUID: p.GUID, Kind: KindProduct, Names: p.Names, Category: p.Category, Regions: p.Regions}
	}
	c.entries[KindProduct] = products

	buildings := make(map[int32]Entry, len(f.Buildings))
	for _, b := range f.Buildings {
		buildings[b.GUID] = Entry{GUID: b.GUID, Kind: KindBuilding, Names: b.Names, Category: b.Kind, Regions: b.Regions}
	}
	c.entries[KindBuilding] = buildings

	workforce := make(map[int32]Entry, len(f.Workforce))
	for _, w := range f.Workforce {
		workforce[w.GUID] = Entry{GUID: w.GUID, Kind: KindWorkforce, Names: w.Names, Regions: w.Regions}
	}
	c.entries[KindWorkforce] = workforce

	regions := make(map[int32]Entry, len(f.Regions))
	for _, r := range f.Regions {
		regions[r.GUID] = Entry{GUID: r.GUID, Kind: KindRegion, Names: r.Names, Category: r.ID}
	}
	c.entries[KindRegion] = regions

	sessions := make(map[int32]Entry, len(f.Sessions))
	for _, s := range f.Sessions {
		sessions[s.GUID] = Entry{GUID: s.GUID, Kind: KindSession, Names: s.Names, Region: s.Region}
	}
	c.entries[KindSession] = sessions

	return c, nil
}

// Lookup resolves guid against any kind. If the GUID exists in more than
// one kind, the entry wins in kindPriority order (products first).
func (c *Catalog) Lookup(guid int32) (Entry, bool) {
	for _, k := range kindPriority {
		if e, ok := c.entries[k][guid]; ok {
			return e, true
		}
	}
	return Entry{}, false
}

// Product looks up guid among products only.
func (c *Catalog) Product(guid int32) (Entry, bool) { return c.lookupKind(KindProduct, guid) }

// Building looks up guid among buildings only.
func (c *Catalog) Building(guid int32) (Entry, bool) { return c.lookupKind(KindBuilding, guid) }

// Workforce looks up guid among workforce types only.
func (c *Catalog) Workforce(guid int32) (Entry, bool) { return c.lookupKind(KindWorkforce, guid) }

// Region looks up guid among regions only.
func (c *Catalog) Region(guid int32) (Entry, bool) { return c.lookupKind(KindRegion, guid) }

// Session looks up guid among sessions only.
func (c *Catalog) Session(guid int32) (Entry, bool) { return c.lookupKind(KindSession, guid) }

func (c *Catalog) lookupKind(kind Kind, guid int32) (Entry, bool) {
	e, ok := c.entries[kind][guid]
	return e, ok
}

// Name returns the localized name for guid in lang, falling back to
// English, then to "#GUID" for a GUID the catalog does not know. An unknown
// GUID is logged once (not once per call) at warn level.
func (c *Catalog) Name(guid int32, lang string) string {
	entry, ok := c.Lookup(guid)
	if !ok {
		c.logUnknownOnce(guid)
		return fmt.Sprintf("#%d", guid)
	}
	if name, ok := entry.Names[lang]; ok && name != "" {
		return name
	}
	if name, ok := entry.Names["english"]; ok && name != "" {
		return name
	}
	return fmt.Sprintf("#%d", guid)
}

func (c *Catalog) logUnknownOnce(guid int32) {
	c.mu.Lock()
	alreadyLogged := c.unknown[guid]
	if !alreadyLogged {
		c.unknown[guid] = true
	}
	c.mu.Unlock()

	if alreadyLogged {
		return
	}
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("catalog: unknown guid", "guid", guid)
}

// Unknown returns the sorted GUIDs Name has logged as unknown so far.
func (c *Catalog) Unknown() []int32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]int32, 0, len(c.unknown))
	for guid := range c.unknown {
		out = append(out, guid)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Revision returns the pinned anno-117-calculator revision this catalog was
// generated from.
func (c *Catalog) Revision() string { return c.revision }

// Languages returns the language codes present in the catalog.
func (c *Catalog) Languages() []string { return append([]string(nil), c.languages...) }

// Counts returns the number of entries per kind.
func (c *Catalog) Counts() map[Kind]int {
	out := make(map[Kind]int, len(c.entries))
	for k, m := range c.entries {
		out[k] = len(m)
	}
	return out
}
