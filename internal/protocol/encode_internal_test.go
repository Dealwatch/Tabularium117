package protocol

import (
	"errors"
	"testing"
)

// otherMessage is a Message implementation the package does not know how to
// encode. Only an in-package test can build one, because Message is closed.
type otherMessage struct{}

func (otherMessage) message() {}

func TestEncodeRejectsUnknownMessage(t *testing.T) {
	if _, err := Encode(otherMessage{}); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("Encode: %v, want ErrUnknownType", err)
	}
	if _, err := Type(otherMessage{}); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("Type: %v, want ErrUnknownType", err)
	}
}

func TestMessageTypeString(t *testing.T) {
	if got := TypeAreaProductionStatistics.String(); got != "AreaProductionStatistics" {
		t.Errorf("String = %q", got)
	}
	if got := MessageType(200).String(); got != "MessageType(200)" {
		t.Errorf("String = %q", got)
	}
}
