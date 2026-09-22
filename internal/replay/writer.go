package replay

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/source"
)

// record is one line of the JSONL format.
type record struct {
	T     string `json:"t"`
	Frame string `json:"frame"`
}

// Writer appends frames to a recording.
//
// It does not buffer, flush or close anything: the caller owns the underlying
// writer and decides how it is buffered and closed.
type Writer struct {
	w io.Writer
}

// NewWriter returns a Writer that appends lines to w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Write appends one frame as a single line.
func (w *Writer) Write(f source.Frame) error {
	line, err := json.Marshal(record{
		T:     f.ReceivedAt.UTC().Format(time.RFC3339Nano),
		Frame: base64.StdEncoding.EncodeToString(f.Payload),
	})
	if err != nil {
		return fmt.Errorf("replay: encode record: %w", err)
	}
	if _, err := w.w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("replay: write record: %w", err)
	}
	return nil
}
