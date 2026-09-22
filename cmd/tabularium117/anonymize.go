package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Dealwatch/Tabularium117/internal/protocol"
	"github.com/Dealwatch/Tabularium117/internal/replay"
	"github.com/Dealwatch/Tabularium117/internal/source"
)

// anonymousHeadline replaces the profile or savegame name the game sends in
// its SessionStart message. It is the one personal name a recording carries
// (docs/beta.md).
const anonymousHeadline = "Player"

// anonymizeSuffix is appended to the input path when --out is not given.
const anonymizeSuffix = ".anon.jsonl"

// anonymizeOptions is what the command line asks the anonymiser to do.
type anonymizeOptions struct {
	in  string
	out string
	// islands additionally replaces every island name with its identity,
	// "Island <sessionGUID>-<islandID>". Island names are the player's own
	// wording and can be as personal as the profile name.
	islands bool
}

// anonymizeStats is the summary the command prints.
type anonymizeStats struct {
	frames int
	// headlines is the number of SessionStart frames whose headline was
	// replaced.
	headlines int
	// islands is the number of statistics frames whose island name was
	// replaced.
	islands int
	// undecodable is the number of frames written through unchanged because
	// this build cannot decode them. They are kept rather than dropped: an
	// undecodable frame is exactly what a bug report needs, and dropping it
	// would silently change the recording.
	undecodable int
}

// anonymizedPath is where the result goes when --out is not given.
func anonymizedPath(in string) string { return in + anonymizeSuffix }

// anonymizeRecording rewrites a recording without the names in it.
//
// It exists because the advice it replaces did not work: a recording is
// JSONL, but the frame itself is base64, so searching the file for the
// profile name never finds it. The payloads therefore have to be decoded,
// rewritten and encoded again.
//
// Only the frames that actually carry a name are re-encoded; everything else
// is copied byte for byte, so a recording stays as close to what the game
// sent as anonymising allows. The recorded times are kept as they are: the
// gaps between frames are the statistics cadence, which is the very thing a
// protocol bug report is about.
func anonymizeRecording(ctx context.Context, opts anonymizeOptions, stderr io.Writer) (stats anonymizeStats, err error) {

	inPath, err := filepath.Abs(opts.in)
	if err != nil {
		return stats, fmt.Errorf("resolve %q: %w", opts.in, err)
	}
	outPath, err := filepath.Abs(opts.out)
	if err != nil {
		return stats, fmt.Errorf("resolve %q: %w", opts.out, err)
	}
	if inPath == outPath {
		return stats, fmt.Errorf("the output %s is the recording itself; name a different file with --out", opts.out)
	}

	in, err := os.Open(opts.in)
	if err != nil {
		return stats, fmt.Errorf("open recording: %w", err)
	}
	defer in.Close()

	out, err := os.Create(opts.out)
	if err != nil {
		return stats, fmt.Errorf("create %s: %w", opts.out, err)
	}
	// A half-written result is worse than none: it looks anonymised and is
	// not. Anything that goes wrong takes the file with it.
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(opts.out)
		}
	}()
	buf := bufio.NewWriter(out)
	writer := replay.NewWriter(buf)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Speed 0 replays as fast as the loop accepts frames: this is a file
	// rewrite, not a playback.
	reader := replay.NewReader(in, replay.Options{Speed: 0})
	frames := make(chan source.Frame)
	readErr := make(chan error, 1)
	go func() { readErr <- reader.Run(ctx, frames) }()

	for done := false; !done; {
		select {
		case frame := <-frames:
			stats.frames++
			cleaned, ferr := anonymizeFrame(frame, opts.islands, &stats, stderr)
			if ferr != nil {
				return stats, ferr
			}
			if werr := writer.Write(cleaned); werr != nil {
				return stats, werr
			}
		case rerr := <-readErr:
			// The reader only reports once every frame it sent has been
			// received, so nothing is left in flight here.
			if rerr != nil {
				return stats, rerr
			}
			done = true
		}
	}

	if ferr := buf.Flush(); ferr != nil {
		return stats, fmt.Errorf("write %s: %w", opts.out, ferr)
	}
	if cerr := out.Close(); cerr != nil {
		return stats, fmt.Errorf("close %s: %w", opts.out, cerr)
	}
	return stats, nil
}

// anonymizeFrame returns the frame to write for one recorded frame. The
// recorded time is never touched.
func anonymizeFrame(frame source.Frame, renameIslands bool, stats *anonymizeStats, stderr io.Writer) (source.Frame, error) {
	msg, err := protocol.Decode(frame.Payload, frame.ReceivedAt)
	if err != nil {
		stats.undecodable++
		fmt.Fprintf(stderr, "tabularium117: frame %d cannot be decoded and is kept unchanged: %v\n", stats.frames, err)
		return frame, nil
	}

	switch m := msg.(type) {
	case protocol.SessionStart:
		if m.Headline == anonymousHeadline {
			return frame, nil
		}
		m.Headline = anonymousHeadline
		stats.headlines++
		return reencode(frame, m)
	case protocol.AreaStatistics:
		if !renameIslands {
			return frame, nil
		}
		name := fmt.Sprintf("Island %d-%d", m.Snapshot.Key.SessionGUID, m.Snapshot.Key.IslandID)
		if m.Snapshot.Name == name {
			return frame, nil
		}
		m.Snapshot.Name = name
		stats.islands++
		return reencode(frame, m)
	default:
		// Version and SessionEnd carry no name at all.
		return frame, nil
	}
}

// reencode builds a frame with a rewritten payload and the recorded time.
func reencode(frame source.Frame, msg protocol.Message) (source.Frame, error) {
	payload, err := protocol.Encode(msg)
	if err != nil {
		return source.Frame{}, fmt.Errorf("re-encode a %T frame: %w", msg, err)
	}
	return source.Frame{ReceivedAt: frame.ReceivedAt, Payload: payload}, nil
}

// printAnonymizeSummary reports what was replaced, so that whoever is about
// to attach the file to a bug report can see that it happened.
func printAnonymizeSummary(w io.Writer, opts anonymizeOptions, stats anonymizeStats) {
	fmt.Fprintf(w, "wrote %s\n", opts.out)
	fmt.Fprintf(w, "frames:             %d\n", stats.frames)
	fmt.Fprintf(w, "headlines replaced: %d\n", stats.headlines)
	fmt.Fprintf(w, "islands renamed:    %d\n", stats.islands)
	fmt.Fprintf(w, "undecodable frames: %d\n", stats.undecodable)
}
