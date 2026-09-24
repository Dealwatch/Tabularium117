package state

import (
	"sort"
	"sync"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// The values of Connection.Mode and Connection.State. They are strings
// because they go out unchanged in /api/v1/status, and the UI (web/js/app.js)
// and anyone reading the API compare against these exact words: renaming one
// is an API change, which state_test.go pins.
const (
	ModePipe   = "pipe"
	ModeReplay = "replay"

	// StateWaiting: the pipe does not exist yet, the game is not running or
	// was started without /pipe.
	StateWaiting = "waiting"
	// StateConnected: the pipe is open and frames can arrive.
	StateConnected = "connected"
	// StateDisconnected: a connection existed and was lost or given up.
	StateDisconnected = "disconnected"
	// StateReplaying: a recording is being played back instead of the pipe.
	StateReplaying = "replaying"
	// StateEnded: the recording has been played to its end (or failed). The
	// last picture stays visible with --serve-after-replay, but nothing is
	// delivered any more.
	StateEnded = "ended"
)

// Connection describes the data source's status as plain data, ready for the
// HTTP layer to serialise. It deliberately holds no error value and
// no package types: Err is the message as it should be shown.
type Connection struct {
	// Mode is ModePipe or ModeReplay.
	Mode string
	// State is one of the State constants above.
	State string
	// Err is the reason for the current state, empty when there is none.
	Err string
	// Since is when the current State was entered.
	Since time.Time
	// ProtocolVersion is the version the game announced on this connection,
	// 0 when it has not announced one yet.
	ProtocolVersion int32
	// LastFrameAt is the receive time of the most recent frame.
	LastFrameAt time.Time
}

// Delivering reports whether the source connection is open: the pipe is
// connected, or a recording is still playing. It says nothing about whether
// the frames are usable - an unsupported protocol version leaves the pipe
// connected while the statistics are dropped.
func (c Connection) Delivering() bool {
	return c.State == StateConnected || c.State == StateReplaying
}

// State is the current, in-memory picture of the game.
type State struct {
	mu        sync.RWMutex
	islands   map[model.IslandKey]model.IslandSnapshot
	headline  string
	startedAt time.Time
	conn      Connection

	// receiving is the tick whose islands are arriving now, complete the
	// newest tick known to be whole (see CompleteTick). Once the tick being
	// received is found complete, both share one map, so an island of it
	// that arrives late still lands in the complete tick.
	receiving tick
	complete  tick
}

// tick is the islands of one statistics tick: every snapshot that carried
// one game timestamp. A nil islands map is "no tick".
type tick struct {
	stamp   int64
	islands map[model.IslandKey]model.IslandSnapshot
}

// New returns an empty State.
func New() *State {
	return &State{islands: make(map[model.IslandKey]model.IslandSnapshot)}
}

// Put stores snap as the latest snapshot of its island, replacing any earlier
// one.
//
// The caller hands over ownership: snap and everything reachable from it must
// not be modified afterwards (see the package documentation).
func (s *State) Put(snap model.IslandSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.islands[snap.Key] = snap

	// The islands of one tick arrive one by one over several seconds, and
	// the protocol has no end-of-tick marker (docs/protocol.md,
	// "AreaProductionStatistics"). A different timestamp is the boundary it
	// does have: the tick before it is over.
	if s.receiving.islands == nil || snap.GameTimestamp != s.receiving.stamp {
		if s.receiving.islands != nil {
			s.complete = s.receiving
		}
		s.receiving = tick{stamp: snap.GameTimestamp, islands: make(map[model.IslandKey]model.IslandSnapshot)}
	}
	s.receiving.islands[snap.Key] = snap
	// Waiting for the next tick would hold every complete tick back by two
	// minutes. So a tick also counts as complete as soon as every island
	// this session has reported so far has reported in it - every one, not
	// only those of the previous tick: an island that tick lacked may still
	// be on its way, and a tick declared complete without it would say
	// "none" where it has the answer. That is a conclusion from the islands
	// seen, not a signal. An island missing from the new tick leaves it to
	// the boundary above, and one that is never reported again leaves every
	// later tick to it: two minutes late, but never short.
	//
	// It needs a complete tick before it: in the first tick after a start
	// every island known is one of the tick's, so the count proves nothing.
	// Once receiving is complete, this finds it complete again and changes
	// nothing.
	if s.complete.islands != nil && len(s.receiving.islands) == len(s.islands) {
		s.complete = s.receiving
	}
}

// Snapshot returns the latest snapshot of one island.
//
// The returned value must not be mutated.
func (s *State) Snapshot(key model.IslandKey) (model.IslandSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.islands[key]
	return snap, ok
}

// Islands returns the latest snapshot of every island, ordered by SessionGUID
// and then IslandID, so that lists and console output are stable.
//
// The returned values must not be mutated.
func (s *State) Islands() []model.IslandSnapshot {
	s.mu.RLock()
	out := make([]model.IslandSnapshot, 0, len(s.islands))
	for _, snap := range s.islands {
		out = append(out, snap)
	}
	s.mu.RUnlock()
	sortIslands(out)
	return out
}

// sortIslands orders snapshots by SessionGUID and then IslandID.
func sortIslands(out []model.IslandSnapshot) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.SessionGUID != out[j].Key.SessionGUID {
			return out[i].Key.SessionGUID < out[j].Key.SessionGUID
		}
		return out[i].Key.IslandID < out[j].Key.IslandID
	})
}

// CompleteTick returns the islands of the newest statistics tick known to be
// complete, ordered like Islands, and that tick's game timestamp. ok is false
// while no tick is known to be complete: after a Reset, until the first tick
// is over - which only the start of the next one shows, up to two minutes
// later.
//
// Islands mixes ticks while one is arriving - for a few seconds, the islands
// that already reported carry the new tick and the rest the previous one.
// CompleteTick never does: every snapshot it returns carries the same
// timestamp. A tick is complete when the next one begins, or earlier, once
// every island of this session has reported in it. The price is that it can
// lag the newest numbers by those few seconds, and by a whole tick while an
// island is missing.
//
// The returned values must not be mutated.
func (s *State) CompleteTick() (stamp int64, islands []model.IslandSnapshot, ok bool) {
	s.mu.RLock()
	if s.complete.islands == nil {
		s.mu.RUnlock()
		return 0, nil, false
	}
	stamp = s.complete.stamp
	islands = make([]model.IslandSnapshot, 0, len(s.complete.islands))
	for _, snap := range s.complete.islands {
		islands = append(islands, snap)
	}
	s.mu.RUnlock()
	sortIslands(islands)
	return stamp, islands, true
}

// Reset drops every island and every tick.
//
// It is called at a session boundary (SessionStart or SessionEnd), where
// island identity stops being meaningful. It leaves the session headline and
// the connection status alone; those are set by their own methods.
func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.islands)
	// A tick of the old session is no tick of the new one.
	s.receiving, s.complete = tick{}, tick{}
}

// SetSession records the headline of a newly started session and stamps its
// start with the current time. The headline's content is undocumented
// (docs/protocol.md, "SessionStart").
func (s *State) SetSession(headline string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headline = headline
	s.startedAt = time.Now()
}

// Session returns the current session's headline and when it was seen.
func (s *State) Session() (headline string, startedAt time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.headline, s.startedAt
}

// SetConnection replaces the connection status.
func (s *State) SetConnection(c Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn = c
}

// Connection returns the current connection status.
func (s *State) Connection() Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.conn
}

// UpdateConnection applies fn to the connection status under the lock.
//
// Read-modify-write through Connection and SetConnection would lose updates:
// the status fields are written by two goroutines at once - the source's
// status callback sets Mode, State, Err and Since while the ingest loop sets
// LastFrameAt and ProtocolVersion. fn must not call back into State.
func (s *State) UpdateConnection(fn func(*Connection)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.conn)
}
