package alerts

import (
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// Rule names. They are part of the API and the database, so they are stable
// identifiers, not display text: the UI translates them.
const (
	// RuleDeficit fires when a product's delta stays negative on an island
	// that produces the product itself.
	RuleDeficit = "deficit"
	// RuleImport fires when a product's delta stays negative on an island
	// with no building of its own for it: the island lives on imports of it.
	// That is how most goods reach most islands, not a fault, so it carries
	// SeverityInfo.
	RuleImport = "import"
	// RuleProductivityDrop fires when the productivity of a product's
	// buildings falls well below its own trailing mean.
	RuleProductivityDrop = "productivity_drop"
)

// Severities. A warning is something to act on and is what the UI counts,
// announces and badges; info is kept and listed, but quietly.
const (
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// Event kinds.
const (
	// KindRaised announces a new alert.
	KindRaised = "raised"
	// KindCleared announces that an alert's condition is over. Its Alert
	// carries the raise time and a non-zero ClearedAt.
	KindCleared = "cleared"
)

// Alert is one warning about one product on one island.
//
// Island is the only unique identity there is (KONZEPT.md section 4);
// IslandName is carried along so that consumers - the API, the history - do
// not have to look the island up again.
//
// ClearedAt is the zero time while the alert is active. Detail is short
// English text with the numbers that triggered the rule, ready to be shown
// next to a translated rule name. Value is the current delta (deficit,
// import) or the current productivity in percent (productivity_drop); it is
// refreshed while the alert is active, so a live view shows how bad it is
// now, not how bad it was when it started.
type Alert struct {
	Island      model.IslandKey
	IslandName  string
	ProductGUID int32
	Rule        string
	Severity    string
	RaisedAt    time.Time
	ClearedAt   time.Time
	Detail      string
	Value       float64
}

// Active reports whether the alert is still open.
func (a Alert) Active() bool { return a.ClearedAt.IsZero() }

// Event is one change to the set of active alerts.
type Event struct {
	Kind  string
	Alert Alert
}

// less orders alerts the way every list in Tabularium 117 is ordered: by island
// identity, then product, then rule.
func less(a, b Alert) bool {
	if a.Island.SessionGUID != b.Island.SessionGUID {
		return a.Island.SessionGUID < b.Island.SessionGUID
	}
	if a.Island.IslandID != b.Island.IslandID {
		return a.Island.IslandID < b.Island.IslandID
	}
	if a.ProductGUID != b.ProductGUID {
		return a.ProductGUID < b.ProductGUID
	}
	return a.Rule < b.Rule
}
