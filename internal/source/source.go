package source

import (
	"context"
	"time"
)

// Frame is one raw message with the time it was received.
//
// Payload is the message type byte followed by the body, without the 4-byte
// length prefix of the wire format.
type Frame struct {
	ReceivedAt time.Time
	Payload    []byte
}

// Source produces frames until it is stopped or runs out of data.
//
// Run blocks. It returns nil when the source is exhausted, ctx.Err() when the
// context is done, or another error when the source fails. Implementations
// must not close out: the caller owns the channel.
type Source interface {
	Run(ctx context.Context, out chan<- Frame) error
}
