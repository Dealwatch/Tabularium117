package pipe

import "time"

// State is the client's connection state, as the UI will show it
// (KONZEPT.md section 8).
type State int

const (
	// StateWaiting means the pipe does not exist yet: the game is not
	// running, or it was started without /pipe.
	StateWaiting State = iota
	// StateConnected means the pipe is open and frames are being read.
	StateConnected
	// StateDisconnected means a connection existed and was lost or given up:
	// the game closed the pipe, the stream went out of sync, or the dial
	// failed for a reason other than a missing pipe.
	StateDisconnected
)

// String reports the state's name, or its numeric value when unknown.
func (s State) String() string {
	switch s {
	case StateWaiting:
		return "waiting"
	case StateConnected:
		return "connected"
	case StateDisconnected:
		return "disconnected"
	default:
		return "unknown"
	}
}

// Status is one connection status event. Err carries the reason for a
// StateDisconnected and is nil otherwise.
type Status struct {
	State State
	Err   error
	At    time.Time
}

// StatusFunc receives status events. It is called from the client's Run
// goroutine and must not block.
type StatusFunc func(Status)
