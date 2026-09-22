package catalog_test

import (
	"testing"

	"github.com/Dealwatch/Tabularium117/internal/catalog"
)

// Spot checks against facts verified directly in the calculator data.
func TestEmbeddedCatalogSpotChecks(t *testing.T) {
	c := catalog.Default()

	cases := []struct {
		guid int32
		lang string
		want string
	}{
		{2068, "german", "Hafer"},
		{2068, "english", "Oats"},
		{2068, "klingon", "Oats"}, // unknown language falls back to English
		{3245, "english", "Latium"},
		{6627, "german", "Albion"},
		{2181, "english", "Libertus Workforce"},
		{2955, "german", "Sardinenhütte"},
		// GUID 0 is the pipe's "no product" - one building, no output. It is
		// a synthetic catalog entry (tools/gen-catalog), so it is named
		// rather than rendered as "#0" and never logged as unknown.
		{0, "english", "(no product)"},
		{0, "german", "(kein Produkt)"},
		{0, "klingon", "(no product)"},
	}
	for _, tc := range cases {
		if got := c.Name(tc.guid, tc.lang); got != tc.want {
			t.Errorf("Name(%d, %q) = %q, want %q", tc.guid, tc.lang, got, tc.want)
		}
	}

	if e, ok := c.Product(2068); !ok || e.Category != "agricultural" || e.Kind != catalog.KindProduct {
		t.Errorf("Product(2068) = %+v, %v; want agricultural product", e, ok)
	}
	if e, ok := c.Session(3245); !ok || e.Region != 3225 {
		t.Errorf("Session(3245) = %+v, %v; want region 3225", e, ok)
	}
	if e, ok := c.Region(3225); !ok || e.Category != "Roman" {
		t.Errorf("Region(3225) = %+v, %v; want id Roman", e, ok)
	}
	if e, ok := c.Lookup(0); !ok || e.Kind != catalog.KindProduct {
		t.Errorf("Lookup(0) = %+v, %v; want the synthetic product entry", e, ok)
	}
	// Default() is shared with the other tests, so only membership is
	// checked. GUID 0 is known now, so it must never be logged as unknown -
	// that log is for real catalog gaps, such as building 153793.
	for _, g := range c.Unknown() {
		if g == 0 {
			t.Errorf("Unknown() = %v, want it not to contain 0", c.Unknown())
		}
	}
	// A GUID the pinned calculator revision really does not know stays
	// unknown and keeps the "#123456" rendering.
	if got := c.Name(153793, "english"); got != "#153793" {
		t.Errorf("Name(153793) = %q, want #153793", got)
	}
	if _, ok := c.Lookup(153793); ok {
		t.Error("Lookup(153793) must be unknown")
	}
	if c.Revision() != "16ce9aa4e49c7baac45d8c3f75da6d2ba27ab564" {
		t.Errorf("Revision() = %q", c.Revision())
	}
}
