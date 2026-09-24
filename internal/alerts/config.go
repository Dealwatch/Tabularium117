package alerts

import "time"

// Config holds the rule thresholds. The defaults are KONZEPT.md section 2's;
// the command line exposes the two a player is most likely to
// want to change.
//
// The zero Config is not a configuration: New replaces every value that is
// not usable with the default, so a caller can set one field and get sensible
// behaviour for the rest.
type Config struct {
	// DeficitSamples is how many consecutive samples with Delta < 0 raise a
	// deficit alert, or a no_local_production alert on an island without
	// buildings of its own for the product. Default 3.
	//
	// One sample is one statistics tick, and the game produces a tick about
	// every two minutes (docs/protocol.md, "Live capture 2026-09-22"), so
	// three of them are roughly six minutes of sustained deficit. That is
	// the point: a warning here means "this has been true for a while", not
	// "one reading looked bad".
	DeficitSamples int
	// DeficitClearSamples is how many consecutive samples with Delta >= 0
	// clear it again. Default 3.
	DeficitClearSamples int
	// DropPercentagePoints is how far, in percentage points, the buildings'
	// productivity has to fall below its trailing mean to raise a
	// productivity_drop alert. Default 20.
	DropPercentagePoints float64
	// DropClearPercentagePoints is how close to the trailing mean the
	// productivity has to come back for that alert to clear. It is smaller than
	// DropPercentagePoints on purpose - that gap is the hysteresis.
	// Default 10.
	DropClearPercentagePoints float64
	// DropNegativeSamples is how many consecutive samples with Delta < 0 -
	// the island's reported production below its reported consumption - a
	// productivity drop needs before it is raised. Default 2.
	//
	// A drop alone can be a full storage, which the pipe cannot see; a drop
	// while the island's own production falls behind its consumption is
	// worth a warning. Two samples rather than one, because a single tick
	// can dip below zero from how production cycles fall into the ticks.
	DropNegativeSamples int
	// DropWindow is the length of the trailing mean's window. Default 15 min.
	//
	// It has to be long enough to hold MinSamplesForDrop ticks: the game
	// delivers a statistics tick about every two minutes, and it does so on
	// a real-time schedule that drifts (115 s, 121 s and 138 s in the live
	// capture of 2026-09-22). A five-minute window held at most two ticks,
	// so the rule could never reach its minimum sample count and never
	// fired at all. Fifteen minutes holds about seven ticks and keeps a
	// margin for a slow or interrupted stream.
	DropWindow time.Duration
	// MinSamplesForDrop is how many samples the window needs before the rule
	// is allowed to fire at all. Default 3.
	//
	// Three ticks are about six minutes of history to compare against,
	// which is the shortest trailing mean worth calling one.
	MinSamplesForDrop int
}

// DefaultConfig returns the default thresholds.
func DefaultConfig() Config {
	return Config{
		DeficitSamples:            3,
		DeficitClearSamples:       3,
		DropPercentagePoints:      20,
		DropClearPercentagePoints: 10,
		DropNegativeSamples:       2,
		DropWindow:                15 * time.Minute,
		MinSamplesForDrop:         3,
	}
}

// withDefaults fills in every value that cannot work. A clear threshold that
// is not smaller than the raise threshold would remove the hysteresis, so it
// is capped rather than trusted: an alert that raises and clears at the same
// value is exactly the flicker this package exists to prevent.
func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.DeficitSamples < 1 {
		c.DeficitSamples = d.DeficitSamples
	}
	if c.DeficitClearSamples < 1 {
		c.DeficitClearSamples = d.DeficitClearSamples
	}
	if c.DropPercentagePoints <= 0 {
		c.DropPercentagePoints = d.DropPercentagePoints
	}
	if c.DropClearPercentagePoints <= 0 {
		c.DropClearPercentagePoints = d.DropClearPercentagePoints
	}
	if c.DropClearPercentagePoints >= c.DropPercentagePoints {
		c.DropClearPercentagePoints = c.DropPercentagePoints / 2
	}
	if c.DropNegativeSamples < 1 {
		c.DropNegativeSamples = d.DropNegativeSamples
	}
	if c.DropWindow <= 0 {
		c.DropWindow = d.DropWindow
	}
	if c.MinSamplesForDrop < 1 {
		c.MinSamplesForDrop = d.MinSamplesForDrop
	}
	return c
}
