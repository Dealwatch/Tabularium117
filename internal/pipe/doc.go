// Package pipe connects to the Anno 117 named pipe and hands raw frames to the decoder.
//
// It is the only package allowed to depend on Windows APIs. It owns reconnect
// and backoff, never interprets frame contents, and reports connection status
// events. docs/protocol.md ("Transport") is the source of truth for the
// connection flow; nothing about the frame layout is repeated here.
//
// # Layout
//
// The reconnect loop (client.go) is platform-neutral and talks to a Dialer.
// Only dial_windows.go touches Windows: it is the single file that imports
// go-winio. On every other platform dial_other.go supplies a Dialer that fails
// with ErrUnsupportedPlatform, so the package builds, vets and unit-tests on
// Linux against a fake Dialer.
//
// # Cancellation
//
// A read that is blocked in Windows ReadFile does not observe a Go context:
// the handle has to be closed to unblock it. The read loop therefore starts a
// watcher goroutine that closes the connection as soon as the context is done,
// which makes the pending read fail and lets Run return ctx.Err(). Every
// connection is wrapped so that closing it twice - once by the watcher, once
// by the loop - is safe.
//
// # Read-only
//
// The client opens the pipe with GENERIC_READ only and the connection is
// handed around as an io.ReadCloser, so there is no code path that could write
// to the game.
package pipe
