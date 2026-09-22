// Package protocol decodes and encodes the raw frames of the Anno 117 pipe.
//
// It is the only package that knows the wire format; docs/protocol.md is the
// source of truth for that format and this package's only reference. Everything
// else works with internal/model.
//
// The decoder is strict where the reference reader is not: a body that ends
// early is an error (wrapping ErrShortFrame), never a zero value. Trailing
// bytes after a fully decoded message are tolerated, so that a future minor
// format addition does not break decoding; SupportedVersion and CheckVersion
// are what guard against a format we cannot read.
//
// Encode and WriteFrame exist for tests, fixtures and the record format.
// Tabularium 117 never writes to the game.
package protocol
