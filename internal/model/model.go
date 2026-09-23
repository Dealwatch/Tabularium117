package model

import "time"

// IslandKey identifies an island. Only (SessionGUID, IslandID) is unique:
// the game reuses IslandID across sessions (see docs/protocol.md, "Identity").
type IslandKey struct {
	SessionGUID int32
	IslandID    int32 // a uint8 on the wire, widened for storage and JSON
}

// ProductStat is one product's situation on one island at one point in time.
//
// SummedProductivity is the sum of the per-building productivity factors
// (16 buildings at 100 % give 16.0) and AvgProductivity their mean in percent,
// boosts included, so it can exceed 100: AvgProductivity = SummedProductivity
// / Buildings * 100 (docs/protocol.md, "Productivity fields, resolved"). Both
// are passed through unchanged.
type ProductStat struct {
	ProductGUID        int32
	Generation         float32
	Consumption        float32
	Delta              float32
	PerfectGeneration  float32
	PerfectConsumption float32
	Buildings          int32
	Maintenance        int32
	Income             float32
	Profit             int32
	SummedProductivity float32
	AvgProductivity    float32
	Workforce          map[int32]int32 // workforce GUID -> amount
	BuildingsByGUID    map[int32]int32 // building GUID -> amount
}

// IslandSnapshot is the state of one island as reported by one message.
//
// ReceivedAt is the receiver's clock and the basis of every time series.
// GameTimestamp is the game's own value, kept opaque.
type IslandSnapshot struct {
	Key           IslandKey
	SessionID     uint8
	AreaIndex     uint8
	Name          string // as delivered, not trimmed
	ReceivedAt    time.Time
	GameTimestamp int64
	Products      []ProductStat
}
