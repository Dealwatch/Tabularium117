package catalog

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

const testJSON = `{
	"source": "anno-mods/anno-117-calculator",
	"revision": "deadbeef",
	"license": "MIT",
	"languages": ["english", "german"],
	"products": [
		{"guid": 2068, "names": {"english": "Oats", "german": "Hafer"}, "category": "agricultural", "regions": ["Roman"], "abstract": false},
		{"guid": 9001, "names": {"german": "Nur Deutsch"}, "category": "", "regions": [], "abstract": false}
	],
	"buildings": [
		{"guid": 2955, "names": {"english": "Sardine Fishery"}, "regions": ["Roman"], "kind": "factory"}
	],
	"workforce": [
		{"guid": 2181, "names": {"english": "Peasants"}, "regions": ["Roman"]}
	],
	"regions": [
		{"guid": 3225, "id": "Roman", "names": {"english": "Latium"}}
	],
	"sessions": [
		{"guid": 3245, "names": {"english": "Latium"}, "region": 3225}
	]
}`

func TestParse(t *testing.T) {
	c, err := Parse([]byte(testJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Revision() != "deadbeef" {
		t.Errorf("Revision() = %q, want deadbeef", c.Revision())
	}
	if got, want := c.Languages(), []string{"english", "german"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Languages() = %v, want %v", got, want)
	}
	counts := c.Counts()
	if counts[KindProduct] != 2 {
		t.Errorf("Counts()[KindProduct] = %d, want 2", counts[KindProduct])
	}

	entry, ok := c.Product(2068)
	if !ok {
		t.Fatalf("Product(2068) not found")
	}
	if entry.Category != "agricultural" {
		t.Errorf("entry.Category = %q, want agricultural", entry.Category)
	}

	if _, ok := c.Building(2955); !ok {
		t.Errorf("Building(2955) not found")
	}
	if _, ok := c.Workforce(2181); !ok {
		t.Errorf("Workforce(2181) not found")
	}
	regionEntry, ok := c.Region(3225)
	if !ok || regionEntry.Category != "Roman" {
		t.Errorf("Region(3225) = %+v, ok=%v, want id Roman", regionEntry, ok)
	}
	sessionEntry, ok := c.Session(3245)
	if !ok || sessionEntry.Region != 3225 {
		t.Errorf("Session(3245) = %+v, ok=%v, want region 3225", sessionEntry, ok)
	}

	// Lookup finds any kind.
	if e, ok := c.Lookup(2955); !ok || e.Kind != KindBuilding {
		t.Errorf("Lookup(2955) = %+v, ok=%v, want kind building", e, ok)
	}
	if _, ok := c.Lookup(999999); ok {
		t.Errorf("Lookup(999999) should not be found")
	}
}

func TestNameFallbackChain(t *testing.T) {
	c, err := Parse([]byte(testJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got, want := c.Name(2068, "german"), "Hafer"; got != want {
		t.Errorf("Name(2068, german) = %q, want %q", got, want)
	}
	// Requested language missing on this entry -> falls back to english.
	if got, want := c.Name(2068, "french"), "Oats"; got != want {
		t.Errorf("Name(2068, french) = %q, want %q (english fallback)", got, want)
	}
	// Entry has no english name at all -> falls back to "#GUID".
	if got, want := c.Name(9001, "french"), "#9001"; got != want {
		t.Errorf("Name(9001, french) = %q, want %q", got, want)
	}
	// Unknown guid entirely -> "#GUID".
	if got, want := c.Name(424242, "english"), "#424242"; got != want {
		t.Errorf("Name(424242, english) = %q, want %q", got, want)
	}
}

func TestUnknownLoggedOncePerGUID(t *testing.T) {
	c, err := Parse([]byte(testJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var buf bytes.Buffer
	c.Logger = slog.New(slog.NewTextHandler(&buf, nil))

	for i := 0; i < 20; i++ {
		c.Name(555, "english")
	}
	for i := 0; i < 5; i++ {
		c.Name(556, "english")
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 log lines (one per unknown guid), got %d:\n%s", len(lines), buf.String())
	}

	unknown := c.Unknown()
	if len(unknown) != 2 || unknown[0] != 555 || unknown[1] != 556 {
		t.Errorf("Unknown() = %v, want [555 556]", unknown)
	}
}

func TestDefault(t *testing.T) {
	c := Default()
	counts := c.Counts()
	if counts[KindProduct] <= 100 {
		t.Errorf("Counts()[KindProduct] = %d, want > 100", counts[KindProduct])
	}
	if c.Revision() == "" {
		t.Errorf("Revision() is empty")
	}
}

// TestConcurrent exercises Name/Lookup/Unknown from many goroutines at once.
// Run with -race to catch data races.
func TestConcurrent(t *testing.T) {
	c := Default()
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Name(int32(2068), "english")
			c.Name(int32(900000+n), "english") // distinct unknown guids
			c.Lookup(2955)
			c.Unknown()
		}(g)
	}
	wg.Wait()
}
