//go:build !windows

package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Dealwatch/Tabularium117/internal/pipe"
)

// Without --replay the source is the game's pipe, which this build cannot
// open. That has to fail immediately with an explanation, not retry forever.
func TestPipeSourceFailsOffWindows(t *testing.T) {
	cfg := runCfg(t, "--data-dir", t.TempDir())
	err := run(context.Background(), cfg, io.Discard, io.Discard)
	if !errors.Is(err, pipe.ErrUnsupportedPlatform) {
		t.Fatalf("run err = %v, want ErrUnsupportedPlatform", err)
	}
	if !strings.Contains(err.Error(), "--replay") {
		t.Errorf("error does not point at the way out: %v", err)
	}
}
