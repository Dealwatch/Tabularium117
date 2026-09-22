package replay

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/source"
)

// maxLineBytes bounds one JSONL line. A 1 MiB frame is about 1.4 MiB of
// base64, so this leaves generous headroom.
const maxLineBytes = 4 << 20

// Options configures a Reader.
type Options struct {
	// Speed scales the recorded pacing: 1 replays with the original gaps
	// between frames, 2 replays twice as fast, 0 (or less) replays as fast as
	// the consumer accepts frames.
	Speed float64
	// Loop restarts from the beginning when the recording ends. It requires a
	// seekable input, and the restart is immediate: there is no gap between
	// the last frame of one pass and the first frame of the next.
	Loop bool
}

// Reader replays a JSONL recording. It implements source.Source.
//
// Frames keep their recorded timestamp: ReceivedAt is the t of the line, not
// the time of the replay.
type Reader struct {
	r      io.Reader
	seeker io.Seeker
	opts   Options
	// initErr is a construction problem reported by the first Run call,
	// because NewReader has no error to return.
	initErr error
}

// NewReader returns a Reader over r.
//
// A configuration that cannot work - Loop without a seekable input - is
// reported by Run, not here.
func NewReader(r io.Reader, opts Options) *Reader {
	rd := &Reader{r: r, opts: opts}
	if opts.Loop {
		seeker, ok := r.(io.ReadSeeker)
		if !ok {
			rd.initErr = errors.New("replay: Loop requires a seekable input")
			return rd
		}
		rd.seeker = seeker
	}
	return rd
}

// Run replays the recording into out until the input is exhausted (returns
// nil), the context is done (returns ctx.Err()), or a line is malformed
// (returns an error naming the line). It never closes out.
func (rd *Reader) Run(ctx context.Context, out chan<- source.Frame) error {
	if rd.initErr != nil {
		return rd.initErr
	}
	for {
		frames, err := rd.playOnce(ctx, out)
		if err != nil {
			return err
		}
		// An empty recording would otherwise spin forever.
		if !rd.opts.Loop || frames == 0 {
			return nil
		}
		if _, err := rd.seeker.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("replay: rewind for loop: %w", err)
		}
	}
}

// playOnce replays the input once and reports how many frames it emitted.
func (rd *Reader) playOnce(ctx context.Context, out chan<- source.Frame) (int, error) {
	scanner := bufio.NewScanner(rd.r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var (
		emitted  int
		prev     time.Time
		havePrev bool
		lineNo   int
	)
	for scanner.Scan() {
		lineNo++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		frame, err := parseLine(scanner.Bytes())
		if err != nil {
			return emitted, fmt.Errorf("replay: line %d: %w", lineNo, err)
		}
		if havePrev {
			if err := rd.wait(ctx, frame.ReceivedAt.Sub(prev)); err != nil {
				return emitted, err
			}
		}
		prev, havePrev = frame.ReceivedAt, true

		select {
		case <-ctx.Done():
			return emitted, ctx.Err()
		case out <- frame:
			emitted++
		}
	}
	if err := scanner.Err(); err != nil {
		return emitted, fmt.Errorf("replay: read line %d: %w", lineNo+1, err)
	}
	return emitted, nil
}

// wait sleeps for the recorded gap, scaled by Speed, and stays responsive to
// ctx while doing so.
func (rd *Reader) wait(ctx context.Context, gap time.Duration) error {
	if rd.opts.Speed <= 0 || gap <= 0 {
		// Even without a sleep, a cancelled context must stop the replay.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	d := time.Duration(float64(gap) / rd.opts.Speed)
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// parseLine turns one JSONL line into a frame. Anything unreadable is an
// error; lines are never skipped silently.
func parseLine(line []byte) (source.Frame, error) {
	var rec record
	if err := json.Unmarshal(line, &rec); err != nil {
		return source.Frame{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if rec.T == "" {
		return source.Frame{}, errors.New(`missing "t"`)
	}
	ts, err := time.Parse(time.RFC3339Nano, rec.T)
	if err != nil {
		return source.Frame{}, fmt.Errorf("invalid timestamp %q: %w", rec.T, err)
	}
	if rec.Frame == "" {
		return source.Frame{}, errors.New(`missing "frame"`)
	}
	payload, err := base64.StdEncoding.DecodeString(rec.Frame)
	if err != nil {
		return source.Frame{}, fmt.Errorf("invalid base64 frame: %w", err)
	}
	if len(payload) == 0 {
		return source.Frame{}, errors.New("empty frame payload")
	}
	return source.Frame{ReceivedAt: ts, Payload: payload}, nil
}
