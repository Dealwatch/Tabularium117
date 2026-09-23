package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/pipe"
	"github.com/Dealwatch/Tabularium117/internal/protocol"
	"github.com/Dealwatch/Tabularium117/internal/replay"
	"github.com/Dealwatch/Tabularium117/internal/source"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

// Pipeline wires a source to the current state. Set the exported fields
// before calling Run and do not copy a Pipeline afterwards.
type Pipeline struct {
	// State receives the decoded snapshots. Required.
	State *state.State
	// Record, when set, writes every raw frame to a recording before it is
	// decoded. nil disables recording.
	Record *replay.Writer
	// Log receives decode and version problems. nil discards them.
	Log *slog.Logger
	// OnSnapshot, when set, is called for every stored snapshot, in the
	// pipeline's goroutine. It must not block.
	OnSnapshot func(model.IslandSnapshot)
	// OnSessionStart, when set, is called after a session start has been
	// applied to the state, in the pipeline's goroutine. It must not block.
	// at is the frame's receive time, the time base of everything stored.
	OnSessionStart func(headline string, at time.Time)
	// OnSessionEnd, when set, is called after a session end has been applied
	// to the state, in the pipeline's goroutine. It must not block.
	OnSessionEnd func(at time.Time)
	// OnStatus, when set, is called after anything the status describes has
	// changed: a connection event, an announced protocol version, a session
	// boundary. It is not called per frame - the receive time moves with
	// every frame, and a notification per frame would be noise.
	//
	// It runs in the goroutine that applied the change, which is the
	// pipeline's for frames and the source's for connection events, so it
	// must be safe for concurrent use and must not block.
	OnStatus func()

	mu sync.Mutex
	// versionRejected is the per-connection version gate: true once the game
	// has announced a version this decoder cannot read.
	versionRejected bool
	// versionLogged keeps the "unsupported version" error to one line per
	// connection instead of one per frame.
	versionLogged bool
	// duplicateLogged keeps the duplicate-product warning to one line per
	// run: one is enough to know the assumption broke, and a save that
	// triggers it would do so every tick.
	duplicateLogged bool
	stats           Stats
}

// Stats counts what the pipeline has seen. It is a snapshot, not a live view.
type Stats struct {
	// Frames is every frame received, decodable or not.
	Frames int
	// Snapshots is the number of island snapshots stored.
	Snapshots int
	// DecodeErrors is the number of frames that could not be decoded.
	DecodeErrors int
	// VersionDropped is the number of statistics frames dropped because the
	// announced protocol version is unsupported.
	VersionDropped int
	// RecordErrors is the number of frames that could not be recorded.
	RecordErrors int
	// DuplicateProducts is the number of product entries dropped because the
	// same message listed that good again (see collapseDuplicates).
	DuplicateProducts int
}

// Run consumes src until it stops or ctx is done.
//
// It returns the source's error, or ctx.Err() when the context is done first.
// The source runs in its own goroutine; Run waits for it to finish before
// returning, so no goroutine outlives the call.
func (p *Pipeline) Run(ctx context.Context, src source.Source) error {
	if p.State == nil {
		return fmt.Errorf("ingest: no state configured")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	frames := make(chan source.Frame)
	srcErr := make(chan error, 1)
	go func() { srcErr <- src.Run(ctx, frames) }()

	for {
		select {
		case frame := <-frames:
			p.handle(frame)
		case err := <-srcErr:
			return err
		case <-ctx.Done():
			// Stop the source and drain whatever it still emits, so that it
			// can return instead of blocking on the channel forever.
			cancel()
			for {
				select {
				case <-frames:
				case <-srcErr:
					return ctx.Err()
				}
			}
		}
	}
}

// Stats returns what the pipeline has seen so far.
func (p *Pipeline) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

// handle records, decodes and applies one frame. It never fails: a frame that
// cannot be recorded or decoded is logged and counted.
func (p *Pipeline) handle(frame source.Frame) {
	p.mu.Lock()
	p.stats.Frames++
	p.mu.Unlock()

	if p.Record != nil {
		if err := p.Record.Write(frame); err != nil {
			p.mu.Lock()
			first := p.stats.RecordErrors == 0
			p.stats.RecordErrors++
			p.mu.Unlock()
			if first {
				// Recording is a side job; a broken recording must not stop
				// the live view. Only the first failure is logged.
				p.log().Error("cannot record frame", "err", err)
			}
		}
	}

	// The receive time is the time base of every sample (docs/protocol.md).
	p.State.UpdateConnection(func(c *state.Connection) { c.LastFrameAt = frame.ReceivedAt })

	msg, err := protocol.Decode(frame.Payload, frame.ReceivedAt)
	if err != nil {
		p.mu.Lock()
		p.stats.DecodeErrors++
		p.mu.Unlock()
		p.log().Warn("cannot decode frame", "err", err, "bytes", len(frame.Payload))
		return
	}

	switch m := msg.(type) {
	case protocol.Version:
		p.handleVersion(m)
	case protocol.SessionStart:
		// A SessionStart is not guaranteed to follow a SessionEnd
		// (docs/protocol.md), so it resets too.
		p.State.Reset()
		p.State.SetSession(m.Headline)
		p.log().Info("session started", "headline", m.Headline)
		if p.OnSessionStart != nil {
			p.OnSessionStart(m.Headline, frame.ReceivedAt)
		}
		p.notifyStatus()
	case protocol.SessionEnd:
		p.State.Reset()
		p.log().Info("session ended")
		if p.OnSessionEnd != nil {
			p.OnSessionEnd(frame.ReceivedAt)
		}
		p.notifyStatus()
	case protocol.AreaStatistics:
		p.handleStatistics(m)
	}
}

// handleVersion applies the version gate of KONZEPT.md section 8.
func (p *Pipeline) handleVersion(m protocol.Version) {
	err := protocol.CheckVersion(m)

	p.mu.Lock()
	p.versionRejected = err != nil
	logIt := err != nil && !p.versionLogged
	if err != nil {
		p.versionLogged = true
	} else {
		p.versionLogged = false
	}
	p.mu.Unlock()

	if err == nil {
		p.State.UpdateConnection(func(c *state.Connection) {
			c.ProtocolVersion = m.Version
			c.Err = ""
		})
		p.notifyStatus()
		return
	}

	message := fmt.Sprintf("Anno 117 speaks pipe protocol version %d, Tabularium 117 understands version %d. "+
		"Statistics are ignored until a supported version is announced.", m.Version, protocol.SupportedVersion)
	p.State.UpdateConnection(func(c *state.Connection) {
		// The announced version is recorded either way, so the UI can name it.
		c.ProtocolVersion = m.Version
		c.Err = message
	})
	if logIt {
		p.log().Error("unsupported pipe protocol version", "err", err,
			"got", m.Version, "supported", protocol.SupportedVersion)
	}
	p.notifyStatus()
}

// notifyStatus tells the UI that the status changed, if anyone is listening.
func (p *Pipeline) notifyStatus() {
	if p.OnStatus != nil {
		p.OnStatus()
	}
}

// handleStatistics stores one island snapshot unless the version gate is shut.
func (p *Pipeline) handleStatistics(m protocol.AreaStatistics) {
	p.mu.Lock()
	rejected := p.versionRejected
	if rejected {
		p.stats.VersionDropped++
	} else {
		p.stats.Snapshots++
	}
	p.mu.Unlock()
	if rejected {
		return
	}

	p.collapseDuplicates(&m.Snapshot)
	p.State.Put(m.Snapshot)
	if p.OnSnapshot != nil {
		p.OnSnapshot(m.Snapshot)
	}
}

// collapseDuplicates keeps one entry per good in snap. No capture has ever
// listed a good twice in one message, but nothing in the format forbids it,
// and every consumer would handle it differently: the table would show two
// rows, the history keeps the later one (INSERT OR REPLACE), and /metrics
// would emit a duplicate series, which makes Prometheus drop the whole
// scrape. So it is decided once, here, the way the history already decides
// it: the later entry wins, and stays where it was sent.
//
// The decoder hands over a fresh slice, so it is filtered in place.
func (p *Pipeline) collapseDuplicates(snap *model.IslandSnapshot) {
	last := make(map[int32]int, len(snap.Products))
	for i, prod := range snap.Products {
		last[prod.ProductGUID] = i
	}
	dropped := len(snap.Products) - len(last)
	if dropped == 0 {
		return
	}

	// Only the first duplicate is named: the log line is a hint where to
	// look, not an inventory. A bool, because GUID 0 is a real product.
	var duplicate int32
	named := false
	kept := snap.Products[:0]
	for i, prod := range snap.Products {
		if last[prod.ProductGUID] == i {
			kept = append(kept, prod)
		} else if !named {
			duplicate, named = prod.ProductGUID, true
		}
	}
	snap.Products = kept

	p.mu.Lock()
	p.stats.DuplicateProducts += dropped
	logIt := !p.duplicateLogged
	p.duplicateLogged = true
	p.mu.Unlock()
	if logIt {
		p.log().Warn("a message listed a good more than once; kept the later entry (logged once per run)",
			"session", snap.Key.SessionGUID, "island", snap.Key.IslandID, "guid", duplicate, "dropped", dropped)
	}
}

// PipeStatus returns the callback that feeds pipe status events into the
// state. It is only used when the source is the pipe.
func (p *Pipeline) PipeStatus() pipe.StatusFunc {
	return func(s pipe.Status) {
		if s.State == pipe.StateConnected {
			// Every connection starts with its own Version frame, so the
			// gate is re-opened here. The pipe client emits this status
			// before it sends that connection's first frame, so the reset
			// can never swallow the new version.
			p.mu.Lock()
			p.versionRejected = false
			p.versionLogged = false
			p.mu.Unlock()
		}

		var errText string
		if s.Err != nil {
			errText = s.Err.Error()
		}
		stateName := connectionState(s.State)

		p.State.UpdateConnection(func(c *state.Connection) {
			c.Mode = state.ModePipe
			if c.State != stateName {
				c.Since = s.At
			}
			c.State = stateName
			c.Err = errText
			if s.State == pipe.StateConnected {
				c.ProtocolVersion = 0
			}
		})
		// Losing the connection deliberately keeps the islands (see doc.go).
		p.notifyStatus()
	}
}

// connectionState maps the pipe client's state onto the API's words. The
// mapping is spelled out rather than taken from pipe.State.String(): that is
// the pipe package's own display name, and renaming it there must not change
// what /api/v1/status says.
func connectionState(s pipe.State) string {
	switch s {
	case pipe.StateWaiting:
		return state.StateWaiting
	case pipe.StateConnected:
		return state.StateConnected
	default:
		// StateDisconnected, and anything the pipe client may add later:
		// a state this code does not know is not one that delivers frames.
		return state.StateDisconnected
	}
}

// log returns the configured logger or a discarding one.
func (p *Pipeline) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.New(slog.DiscardHandler)
}
