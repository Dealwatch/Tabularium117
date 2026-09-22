package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// lengthPrefixSize is the size of the int32 little-endian length prefix.
const lengthPrefixSize = 4

// ReadFrame reads one length-prefixed frame and returns its payload (type byte
// plus body, without the prefix).
//
// At a frame boundary a clean end of stream is reported as io.EOF unchanged, so
// callers can use it as their stop condition. A stream that ends inside a frame
// yields an error wrapping io.ErrUnexpectedEOF. A length outside [1,
// MaxFrameSize] yields an error wrapping ErrShortFrame or ErrFrameTooLarge; the
// stream is then out of sync and must not be read further.
func ReadFrame(r io.Reader) ([]byte, error) {
	var prefix [lengthPrefixSize]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		switch {
		case errors.Is(err, io.ErrUnexpectedEOF):
			return nil, fmt.Errorf("length prefix truncated: %w", io.ErrUnexpectedEOF)
		case errors.Is(err, io.EOF):
			return nil, io.EOF
		}
		return nil, fmt.Errorf("read length prefix: %w", err)
	}

	length := int32(binary.LittleEndian.Uint32(prefix[:]))
	switch {
	case length < 1:
		return nil, fmt.Errorf("frame length %d: %w", length, ErrShortFrame)
	case length > MaxFrameSize:
		return nil, fmt.Errorf("frame length %d exceeds %d: %w", length, MaxFrameSize, ErrFrameTooLarge)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("frame body of %d bytes truncated: %w", length, io.ErrUnexpectedEOF)
		}
		return nil, fmt.Errorf("read frame body: %w", err)
	}
	return payload, nil
}

// WriteFrame writes a payload as a length-prefixed frame. It is used for
// fixtures and tests only; Tabularium 117 never writes to the game.
func WriteFrame(w io.Writer, payload []byte) error {
	switch {
	case len(payload) < 1:
		return fmt.Errorf("empty payload: %w", ErrShortFrame)
	case len(payload) > MaxFrameSize:
		return fmt.Errorf("payload of %d bytes exceeds %d: %w", len(payload), MaxFrameSize, ErrFrameTooLarge)
	}
	var prefix [lengthPrefixSize]byte
	binary.LittleEndian.PutUint32(prefix[:], uint32(len(payload)))
	if _, err := w.Write(prefix[:]); err != nil {
		return fmt.Errorf("write length prefix: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write frame body: %w", err)
	}
	return nil
}
