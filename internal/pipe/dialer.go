package pipe

import (
	"context"
	"errors"
	"io"
)

// DefaultPipeName is the pipe the game serves when it was started with /pipe
// (docs/protocol.md, "Transport"). It is defined for every platform so that
// callers can name it without build tags; only the Windows dialer can use it.
const DefaultPipeName = `\\.\pipe\anno117`

// Dial errors that the reconnect loop treats specially. Every dialer reports
// them wrapped, so callers use errors.Is.
var (
	// ErrPipeNotFound means the pipe does not exist: the game is not running
	// or was started without /pipe. It is the normal, expected case and is
	// retried quietly once per second.
	ErrPipeNotFound = errors.New("pipe not found")
	// ErrUnsupportedPlatform means this build cannot talk to a named pipe at
	// all. Retrying can never help, so Run gives up on it.
	ErrUnsupportedPlatform = errors.New("named pipes are only available on Windows")
)

// Dialer opens one connection to the pipe.
//
// Dial blocks until the connection is established, the attempt fails, or ctx
// is done. The returned value is read-only by construction: the client never
// writes to the game.
type Dialer interface {
	Dial(ctx context.Context) (io.ReadCloser, error)
}
