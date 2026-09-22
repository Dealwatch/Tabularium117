package replay_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/protocol"
	"github.com/Dealwatch/Tabularium117/internal/replay"
	"github.com/Dealwatch/Tabularium117/internal/source"
)

// collectFixture replays the re-encoded fixture as fast as possible.
func collectFixture(t *testing.T, opts replay.Options, limit int) ([]source.Frame, error) {
	t.Helper()
	f, err := os.Open(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan source.Frame)
	errc := make(chan error, 1)
	go func() { errc <- replay.NewReader(f, opts).Run(ctx, out) }()

	var frames []source.Frame
	for {
		select {
		case fr := <-out:
			frames = append(frames, fr)
			if len(frames) == limit {
				cancel()
				return frames, <-errc
			}
		case err := <-errc:
			return frames, err
		case <-time.After(5 * time.Second):
			t.Fatal("replay did not finish")
		}
	}
}

// Every fixture payload must survive Decode → Encode byte-for-byte. Both the
// generator and Encode sort map keys, so this proves the two agree on the
// wire layout and that Decode loses nothing.
func TestFixtureDecodeEncodeIsIdentity(t *testing.T) {
	frames, err := collectFixture(t, replay.Options{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 16 {
		t.Fatalf("got %d frames, want 16", len(frames))
	}
	for i, fr := range frames {
		msg, err := protocol.Decode(fr.Payload, fr.ReceivedAt)
		if err != nil {
			t.Fatalf("frame %d: decode: %v", i, err)
		}
		again, err := protocol.Encode(msg)
		if err != nil {
			t.Fatalf("frame %d: encode: %v", i, err)
		}
		if !bytes.Equal(again, fr.Payload) {
			t.Fatalf("frame %d: re-encoded payload differs (%d vs %d bytes)", i, len(again), len(fr.Payload))
		}
		// Trailing bytes are tolerated and must not change the result.
		withTail := append(append([]byte{}, fr.Payload...), 0xde, 0xad)
		msg2, err := protocol.Decode(withTail, fr.ReceivedAt)
		if err != nil {
			t.Fatalf("frame %d: decode with trailing bytes: %v", i, err)
		}
		enc2, _ := protocol.Encode(msg2)
		if !bytes.Equal(enc2, again) {
			t.Fatalf("frame %d: trailing bytes changed the decoded message", i)
		}
	}
}

// Loop must wrap around and stay cancellable.
func TestLoopWrapsAndCancels(t *testing.T) {
	frames, err := collectFixture(t, replay.Options{Loop: true}, 40)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(frames) != 40 {
		t.Fatalf("got %d frames, want 40", len(frames))
	}
	if !bytes.Equal(frames[0].Payload, frames[16].Payload) {
		t.Fatal("second pass does not restart at the first frame")
	}
}

// Frame length boundary: exactly MaxFrameSize is a frame, one more is not.
func TestReadFrameSizeBoundary(t *testing.T) {
	var buf bytes.Buffer
	if err := protocol.WriteFrame(&buf, make([]byte, protocol.MaxFrameSize)); err != nil {
		t.Fatal(err)
	}
	if p, err := protocol.ReadFrame(&buf); err != nil || len(p) != protocol.MaxFrameSize {
		t.Fatalf("MaxFrameSize frame: len=%d err=%v", len(p), err)
	}
	if err := protocol.WriteFrame(&buf, make([]byte, protocol.MaxFrameSize+1)); !errors.Is(err, protocol.ErrFrameTooLarge) {
		t.Fatalf("WriteFrame over limit: err = %v", err)
	}
	// Hand-built oversize prefix must be rejected by ReadFrame too.
	over := []byte{0x01, 0x00, 0x10, 0x00} // 0x00100001 = MaxFrameSize+1
	if _, err := protocol.ReadFrame(bytes.NewReader(over)); !errors.Is(err, protocol.ErrFrameTooLarge) {
		t.Fatalf("ReadFrame over limit: err = %v", err)
	}
}
