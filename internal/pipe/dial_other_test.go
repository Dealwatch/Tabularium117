//go:build !windows

package pipe_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Dealwatch/Tabularium117/internal/pipe"
)

// On a non-Windows build the real dialer must fail with a clear, fatal error
// instead of pretending to wait for a game that cannot be there.
func TestNewDialerFailsOffWindows(t *testing.T) {
	_, err := pipe.NewDialer(pipe.DefaultPipeName).Dial(context.Background())
	if !errors.Is(err, pipe.ErrUnsupportedPlatform) {
		t.Fatalf("Dial err = %v, want ErrUnsupportedPlatform", err)
	}
	if errors.Is(err, pipe.ErrPipeNotFound) {
		t.Error("an unsupported platform must not look like a missing pipe")
	}
}
