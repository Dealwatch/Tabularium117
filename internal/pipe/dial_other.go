//go:build !windows

package pipe

import (
	"context"
	"fmt"
	"io"
)

// unsupportedDialer stands in for the Windows dialer on every other platform,
// so that the reconnect loop, the command and the tests build and run on
// Linux. Development without a game uses internal/replay instead.
type unsupportedDialer struct {
	name string
}

// NewDialer returns a Dialer that always fails with ErrUnsupportedPlatform.
func NewDialer(pipeName string) Dialer {
	return &unsupportedDialer{name: pipeName}
}

// Dial always fails; the failure is fatal, not retryable.
func (d *unsupportedDialer) Dial(ctx context.Context) (io.ReadCloser, error) {
	return nil, fmt.Errorf("dial %s: %w", d.name, ErrUnsupportedPlatform)
}
