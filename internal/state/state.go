package state

import (
	"sort"
	"sync"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// Connection describes the data source's status as plain data, ready for the
// HTTP layer to serialise in phase 4. It deliberately holds no error value and
// no package types: Err is the message as it should be shown.
type Connection struct {
	// Mode is "pipe" or "replay".
	Mode string
	// State is the source's state, e.g. the pipe's "waiting", "connected" or
	// "disconnected".
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

// State is the current, in-memory picture of the game.
type State struct {
	mu        sync.RWMutex
	islands   map[model.IslandKey]model.IslandSnapshot
	headline  string
	startedAt time.Time
	conn      Connection
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

	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.SessionGUID != out[j].Key.SessionGUID {
			return out[i].Key.SessionGUID < out[j].Key.SessionGUID
		}
		return out[i].Key.IslandID < out[j].Key.IslandID
	})
	return out
}

// Reset drops every island.
//
// It is called at a session boundary (SessionStart or SessionEnd), where
// island identity stops being meaningful. It leaves the session headline and
// the connection status alone; those are set by their own methods.
func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.islands)
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
