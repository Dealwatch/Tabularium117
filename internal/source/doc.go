// Package source defines the common interface for everything that produces raw
// frames: the named pipe (internal/pipe, Windows only) and the JSONL replay
// (internal/replay).
//
// Frame payloads are opaque here. Only internal/protocol knows what the bytes
// mean, so a source can be written, recorded and replayed without ever
// decoding a message. The payload deliberately excludes the 4-byte length
// prefix: framing belongs to whoever reads or writes a byte stream, not to the
// frame itself.
package source
