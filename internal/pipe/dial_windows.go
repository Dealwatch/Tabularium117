//go:build windows

package pipe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	winio "github.com/Microsoft/go-winio"
)

// dialTimeout bounds one dial attempt. go-winio retries ERROR_PIPE_BUSY
// internally every 10 ms for as long as the context allows, so without a
// bound a permanently busy pipe would block forever and never produce a
// status event. Whether the game accepts a second client at all is still open
// (docs/protocol.md, open question 1, simultaneous pipe clients).
const dialTimeout = 30 * time.Second

// windowsDialer opens the named pipe with go-winio.
type windowsDialer struct {
	name string
}

// NewDialer returns a Dialer for the named pipe.
func NewDialer(pipeName string) Dialer {
	return &windowsDialer{name: pipeName}
}

// Dial opens the pipe for reading only.
//
// The handle is requested with GENERIC_READ alone, like the reference reader
// (docs/protocol.md): the game may well serve an outbound-only pipe, and
// Tabularium 117 is read-only towards the game either way. That is why this uses
// DialPipeAccess and not DialPipeContext, which would ask for write access too.
func (d *windowsDialer) Dial(ctx context.Context) (io.ReadCloser, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	conn, err := winio.DialPipeAccess(attemptCtx, d.name, syscall.GENERIC_READ)
	if err != nil {
		return nil, d.classify(ctx, attemptCtx, err)
	}
	return conn, nil
}

// classify turns a dial failure into the error the reconnect loop expects.
func (d *windowsDialer) classify(ctx, attemptCtx context.Context, err error) error {
	// The caller's own cancellation is reported unchanged.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Only this attempt timed out: the pipe exists but never became
	// available (busy), or the open hung. Retryable, but not "not found".
	if errors.Is(err, context.DeadlineExceeded) && attemptCtx.Err() != nil {
		return fmt.Errorf("dial %s: no connection within %s (pipe busy?)", d.name, dialTimeout)
	}
	if isNotFound(err) {
		return fmt.Errorf("dial %s: %w", d.name, ErrPipeNotFound)
	}
	return fmt.Errorf("dial %s: %w", d.name, err)
}

// isNotFound reports whether err means the pipe does not exist.
//
// ERROR_FILE_NOT_FOUND and ERROR_PATH_NOT_FOUND mean "game not running or
// started without /pipe". ERROR_PIPE_BUSY deliberately does not count: the
// pipe exists, somebody else has it (go-winio retries that case internally).
func isNotFound(err error) bool {
	if os.IsNotExist(err) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.ERROR_FILE_NOT_FOUND || errno == syscall.ERROR_PATH_NOT_FOUND
	}
	return false
}
