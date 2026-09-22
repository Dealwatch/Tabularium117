package replay_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/replay"
	"github.com/Dealwatch/Tabularium117/internal/source"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

// collect runs a reader to completion and returns everything it emitted.
func collect(ctx context.Context, rd *replay.Reader) ([]source.Frame, error) {
	ch := make(chan source.Frame)
	done := make(chan struct{})
	var frames []source.Frame
	go func() {
		defer close(done)
		for f := range ch {
			frames = append(frames, f)
		}
	}()
	err := rd.Run(ctx, ch)
	close(ch)
	<-done
	return frames, err
}

func TestWriteReadRoundTrip(t *testing.T) {
	want := []source.Frame{
		{ReceivedAt: mustTime(t, "2026-09-21T12:00:00Z"), Payload: []byte{0, 2, 0, 0, 0}},
		{ReceivedAt: mustTime(t, "2026-09-21T12:00:01.5Z"), Payload: []byte{1, 2, 'h', 'i'}},
		{ReceivedAt: mustTime(t, "2026-09-21T12:00:02.123456789Z"), Payload: []byte{2}},
	}

	var buf bytes.Buffer
	w := replay.NewWriter(&buf)
	for _, f := range want {
		if err := w.Write(f); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if lines := strings.Count(buf.String(), "\n"); lines != len(want) {
		t.Fatalf("recording has %d lines, want %d", lines, len(want))
	}

	got, err := collect(context.Background(), replay.NewReader(bytes.NewReader(buf.Bytes()), replay.Options{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("read %d frames, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].ReceivedAt.Equal(want[i].ReceivedAt) {
			t.Errorf("frame %d ReceivedAt = %v, want %v", i, got[i].ReceivedAt, want[i].ReceivedAt)
		}
		if !bytes.Equal(got[i].Payload, want[i].Payload) {
			t.Errorf("frame %d payload = %v, want %v", i, got[i].Payload, want[i].Payload)
		}
	}
}

// recording builds a recording whose frames are one hour apart, so any pacing
// would be obvious.
func recording(t *testing.T, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := replay.NewWriter(&buf)
	base := mustTime(t, "2026-09-21T12:00:00Z")
	for i := 0; i < n; i++ {
		f := source.Frame{ReceivedAt: base.Add(time.Duration(i) * time.Hour), Payload: []byte{2}}
		if err := w.Write(f); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	return buf.Bytes()
}

func TestSpeedZeroIgnoresRecordedGaps(t *testing.T) {
	data := recording(t, 5)
	start := time.Now()
	got, err := collect(context.Background(), replay.NewReader(bytes.NewReader(data), replay.Options{Speed: 0}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("read %d frames, want 5", len(got))
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("replay of 5 frames took %v, want no waiting at all", elapsed)
	}
}

func TestSpeedScalesRecordedGaps(t *testing.T) {
	var buf bytes.Buffer
	w := replay.NewWriter(&buf)
	base := mustTime(t, "2026-09-21T12:00:00Z")
	for i := 0; i < 2; i++ {
		if err := w.Write(source.Frame{ReceivedAt: base.Add(time.Duration(i) * 400 * time.Millisecond), Payload: []byte{2}}); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	// Speed 2 means half the recorded 400 ms gap. Only the lower bound is
	// asserted; a loaded machine may always take longer.
	start := time.Now()
	got, err := collect(context.Background(), replay.NewReader(bytes.NewReader(buf.Bytes()), replay.Options{Speed: 2}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)
	if len(got) != 2 {
		t.Fatalf("read %d frames, want 2", len(got))
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("replay took %v, want at least the scaled 200 ms gap", elapsed)
	}
}

func TestMalformedLineReportsLineNumber(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		substr string
	}{
		{"not json", "this is not json", "invalid JSON"},
		{"bad timestamp", `{"t":"yesterday","frame":"Ag=="}`, "invalid timestamp"},
		{"missing timestamp", `{"frame":"Ag=="}`, `missing "t"`},
		{"bad base64", `{"t":"2026-09-21T12:00:00Z","frame":"not base64!!"}`, "invalid base64"},
		{"missing frame", `{"t":"2026-09-21T12:00:00Z"}`, `missing "frame"`},
		{"empty payload", `{"t":"2026-09-21T12:00:00Z","frame":""}`, `missing "frame"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			good := `{"t":"2026-09-21T12:00:00Z","frame":"Ag=="}`
			data := good + "\n" + good + "\n" + tc.line + "\n" + good + "\n"

			got, err := collect(context.Background(), replay.NewReader(strings.NewReader(data), replay.Options{}))
			if err == nil {
				t.Fatalf("Run: nil error, want a failure on line 3")
			}
			if !strings.Contains(err.Error(), "line 3") {
				t.Errorf("error %q does not name line 3", err)
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Errorf("error %q does not mention %q", err, tc.substr)
			}
			if len(got) != 2 {
				t.Errorf("emitted %d frames before the bad line, want 2", len(got))
			}
		})
	}
}

func TestBlankLinesAreTolerated(t *testing.T) {
	good := `{"t":"2026-09-21T12:00:00Z","frame":"Ag=="}`
	data := "\n" + good + "\n\n   \n" + good + "\n"
	got, err := collect(context.Background(), replay.NewReader(strings.NewReader(data), replay.Options{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d frames, want 2", len(got))
	}
}

func TestCancelDuringSleepReturnsPromptly(t *testing.T) {
	data := recording(t, 3) // one hour between frames
	rd := replay.NewReader(bytes.NewReader(data), replay.Options{Speed: 1})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan source.Frame)
	errCh := make(chan error, 1)
	go func() { errCh <- rd.Run(ctx, ch) }()

	select {
	case <-ch: // the first frame is emitted without waiting
	case <-time.After(5 * time.Second):
		t.Fatal("no frame within 5s")
	}

	cancel() // the reader is now sleeping for an hour
	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("Run: %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s of cancellation")
	}
}

func TestCancelledContextStopsFastReplay(t *testing.T) {
	data := recording(t, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := collect(ctx, replay.NewReader(bytes.NewReader(data), replay.Options{Speed: 0}))
	if err != context.Canceled {
		t.Fatalf("Run: %v, want context.Canceled", err)
	}
}

func TestLoopRestartsAtTheBeginning(t *testing.T) {
	data := recording(t, 2)
	rd := replay.NewReader(bytes.NewReader(data), replay.Options{Speed: 0, Loop: true})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan source.Frame)
	errCh := make(chan error, 1)
	go func() { errCh <- rd.Run(ctx, ch) }()

	base := mustTime(t, "2026-09-21T12:00:00Z")
	for i := 0; i < 5; i++ {
		select {
		case f := <-ch:
			want := base.Add(time.Duration(i%2) * time.Hour)
			if !f.ReceivedAt.Equal(want) {
				t.Fatalf("frame %d ReceivedAt = %v, want %v", i, f.ReceivedAt, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no frame %d within 5s", i)
		}
	}
	cancel()
	if err := <-errCh; err != context.Canceled {
		t.Fatalf("Run: %v, want context.Canceled", err)
	}
}

// plainReader hides any Seek method the underlying reader may have.
type plainReader struct{ r io.Reader }

func (p plainReader) Read(b []byte) (int, error) { return p.r.Read(b) }

func TestLoopNeedsASeekableInput(t *testing.T) {
	rd := replay.NewReader(plainReader{strings.NewReader(`{"t":"2026-09-21T12:00:00Z","frame":"Ag=="}`)}, replay.Options{Loop: true})
	_, err := collect(context.Background(), rd)
	if err == nil || !strings.Contains(err.Error(), "seekable") {
		t.Fatalf("Run: %v, want an error about a seekable input", err)
	}
}

func TestEmptyRecordingWithLoopTerminates(t *testing.T) {
	got, err := collect(context.Background(), replay.NewReader(bytes.NewReader(nil), replay.Options{Loop: true}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("read %d frames, want 0", len(got))
	}
}
