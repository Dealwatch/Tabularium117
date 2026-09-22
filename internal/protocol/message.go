package protocol

import (
	"fmt"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// SupportedVersion is the pipe protocol version this decoder was written for.
const SupportedVersion int32 = 2

// MaxFrameSize is the largest payload this package accepts, matching the
// reference reader's 1 MiB limit.
const MaxFrameSize = 1 << 20

// MessageType is the frame's leading type byte.
type MessageType uint8

// The known message types.
const (
	TypeVersion                  MessageType = 0
	TypeSessionStart             MessageType = 1
	TypeSessionEnd               MessageType = 2
	TypeAreaProductionStatistics MessageType = 3
)

// String reports the type's name, or its numeric value when unknown.
func (t MessageType) String() string {
	switch t {
	case TypeVersion:
		return "Version"
	case TypeSessionStart:
		return "SessionStart"
	case TypeSessionEnd:
		return "SessionEnd"
	case TypeAreaProductionStatistics:
		return "AreaProductionStatistics"
	default:
		return fmt.Sprintf("MessageType(%d)", uint8(t))
	}
}

// Message is one decoded frame. The set of implementations is closed.
type Message interface {
	message()
}

// Version is the preamble the game sends right after a client connects.
type Version struct {
	Version int32
}

// SessionStart announces a game session. The headline's content is undocumented.
type SessionStart struct {
	Headline string
}

// SessionEnd ends a game session and has an empty body.
type SessionEnd struct{}

// AreaStatistics carries one island's production statistics.
type AreaStatistics struct {
	Snapshot model.IslandSnapshot
}

func (Version) message()        {}
func (SessionStart) message()   {}
func (SessionEnd) message()     {}
func (AreaStatistics) message() {}

// Type reports the message type a value is encoded as.
func Type(m Message) (MessageType, error) {
	switch m.(type) {
	case Version:
		return TypeVersion, nil
	case SessionStart:
		return TypeSessionStart, nil
	case SessionEnd:
		return TypeSessionEnd, nil
	case AreaStatistics:
		return TypeAreaProductionStatistics, nil
	default:
		return 0, fmt.Errorf("%T: %w", m, ErrUnknownType)
	}
}

// CheckVersion reports whether a Version message announces a format this
// package can decode. Decode itself does not apply this policy; the caller
// decides what to do with a mismatch (KONZEPT.md section 8).
func CheckVersion(v Version) error {
	if v.Version != SupportedVersion {
		return fmt.Errorf("version %d (supported: %d): %w", v.Version, SupportedVersion, ErrUnsupportedVersion)
	}
	return nil
}
