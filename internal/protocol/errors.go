package protocol

import "errors"

// Sentinel errors. Every error this package returns for a malformed or
// unsupported frame wraps one of these, so callers can use errors.Is.
var (
	// ErrShortFrame means a frame or body ended before the format allows.
	ErrShortFrame = errors.New("short frame")
	// ErrFrameTooLarge means the length prefix exceeds MaxFrameSize.
	ErrFrameTooLarge = errors.New("frame too large")
	// ErrUnknownType means the type byte is not a known message type.
	ErrUnknownType = errors.New("unknown message type")
	// ErrUnsupportedVersion means the reported protocol version is not
	// SupportedVersion.
	ErrUnsupportedVersion = errors.New("unsupported protocol version")
)
