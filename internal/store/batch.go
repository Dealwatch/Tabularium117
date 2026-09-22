package store

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/model"
)

// batchQueue is how many events may wait for the next flush. The pipe delivers
// one snapshot per island per tick - a fortnight of islands fits many times
// over, so the queue only ever fills if the database has stopped accepting
// writes altogether.
const batchQueue = 1024

// batchSize is the number of pending snapshots that triggers an early flush,
// so a busy tick does not wait out the whole interval.
const batchSize = 64

// flushTimeout bounds one write, so a wedged database cannot hold up shutdown
// for ever. It applies to every flush, not just the last one.
const flushTimeout = 5 * time.Second

// eventKind distinguishes the things a Batcher records.
type eventKind uint8

const (
	eventSnapshot eventKind = iota
	eventSessionStart
	eventSessionEnd
	eventAlert
)

// event is one thing to record. Everything goes through a single queue so
// that the database sees it in the order ingest saw it: a session start is
// written before the snapshots that belong to it, and a session end after
// them.
type event struct {
	kind     eventKind
	snapshot model.IslandSnapshot
	headline string
	at       time.Time
	version  int32
	alert    alerts.Event
}

// Batcher collects snapshots and session boundaries and writes them from one
// goroutine.
//
// It exists because ingest hands over one snapshot per island and committing
// each one separately would mean one fsync per island per tick. Every Add is
// non-blocking and never fails: the live view must not be slowed down, let
// alone stopped, by the history. That is also why the session rows are written
// here and not by the caller - a session boundary is a database write like any
// other, and ingest must not wait for it.
type Batcher struct {
	store    *Store
	interval time.Duration
	log      *slog.Logger
	in       chan event
	dropped  atomic.Int64

	// sessionID and sessionUp are owned by Run's goroutine.
	sessionID int64
	sessionUp bool
}

// NewBatcher returns a Batcher that flushes every interval. A nil logger
// discards the messages.
func NewBatcher(s *Store, interval time.Duration, log *slog.Logger) *Batcher {
	if interval <= 0 {
		interval = time.Second
	}
	return &Batcher{
		store:    s,
		interval: interval,
		log:      logOrDiscard(log),
		in:       make(chan event, batchQueue),
	}
}

// Add queues one snapshot. It never blocks: when the queue is full the
// snapshot is dropped and counted, because a stalled history must not stall
// ingest.
//
// The caller hands over ownership of snap, as with state.Put.
func (b *Batcher) Add(snap model.IslandSnapshot) {
	b.enqueue(event{kind: eventSnapshot, snapshot: snap})
}

// AddSessionStart queues the start of a game session. Like Add, it never
// blocks.
func (b *Batcher) AddSessionStart(headline string, at time.Time, protocolVersion int32) {
	b.enqueue(event{kind: eventSessionStart, headline: headline, at: at, version: protocolVersion})
}

// AddSessionEnd queues the end of the current game session. Like Add, it never
// blocks.
func (b *Batcher) AddSessionEnd(at time.Time) {
	b.enqueue(event{kind: eventSessionEnd, at: at})
}

// AddAlertEvent queues one raised or cleared alert. Like Add, it never
// blocks.
//
// It shares the queue with the snapshots on purpose: the snapshot that caused
// an alert is queued before it, so by the time the alert is written its
// island row exists.
func (b *Batcher) AddAlertEvent(ev alerts.Event) {
	b.enqueue(event{kind: eventAlert, alert: ev})
}

// enqueue is the one non-blocking hand-over. The first loss is logged; the
// rest are counted.
func (b *Batcher) enqueue(ev event) {
	select {
	case b.in <- ev:
	default:
		if b.dropped.Add(1) == 1 {
			b.log.Warn("history database cannot keep up, dropping records",
				"queue", batchQueue)
		}
	}
}

// Dropped reports how many records never reached the database: those Add threw
// away because the queue was full, plus those lost in a failed write.
func (b *Batcher) Dropped() int64 { return b.dropped.Load() }

// Run records queued events until ctx is done, then drains the queue, writes
// what is left and closes an open session before returning.
//
// It returns the error of the last write that failed, or nil when everything
// was written. The error is information for the caller, not a reason to fail
// the program: the history is the expendable part, and Run has already logged
// and counted what was lost.
//
// Run must be called at most once.
func (b *Batcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	pending := make([]model.IslandSnapshot, 0, batchSize)
	var lastErr error

	for {
		select {
		case ev := <-b.in:
			pending, lastErr = b.apply(ctx, ev, pending, lastErr)
		case <-ticker.C:
			pending, lastErr = b.flush(ctx, pending, lastErr)
		case <-ctx.Done():
			// Drain what the callbacks already queued: Run is the only
			// consumer, so anything left here would be silently lost.
			for {
				select {
				case ev := <-b.in:
					pending, lastErr = b.apply(ctx, ev, pending, lastErr)
					continue
				default:
				}
				break
			}
			pending, lastErr = b.flush(ctx, pending, lastErr)
			lastErr = b.closeSession(ctx, lastErr)
			return lastErr
		}
	}
}

// apply records one event. A session boundary flushes the snapshots queued
// before it first, so the database sees everything in arrival order.
func (b *Batcher) apply(ctx context.Context, ev event, pending []model.IslandSnapshot, lastErr error) ([]model.IslandSnapshot, error) {
	switch ev.kind {
	case eventSnapshot:
		pending = append(pending, ev.snapshot)
		if len(pending) >= batchSize {
			return b.flush(ctx, pending, lastErr)
		}
		return pending, lastErr
	case eventSessionStart:
		pending, lastErr = b.flush(ctx, pending, lastErr)
		return pending, b.startSession(ctx, ev, lastErr)
	case eventSessionEnd:
		pending, lastErr = b.flush(ctx, pending, lastErr)
		return pending, b.endSession(ctx, ev.at, lastErr)
	case eventAlert:
		// The island row has to exist before the alert can reference it, and
		// it is created by the snapshots still waiting in pending.
		pending, lastErr = b.flush(ctx, pending, lastErr)
		return pending, b.writeAlert(ctx, ev.alert, lastErr)
	}
	return pending, lastErr
}

// writeAlert records one alert event. A failure is logged and counted, like
// every other write here: the history is the expendable part.
func (b *Batcher) writeAlert(ctx context.Context, ev alerts.Event, lastErr error) error {
	writeCtx, cancel := b.writeContext(ctx)
	defer cancel()

	var err error
	switch ev.Kind {
	case alerts.KindRaised:
		_, err = b.store.RaiseAlert(writeCtx, ev.Alert)
	case alerts.KindCleared:
		err = b.store.ClearAlert(writeCtx, ev.Alert.Island, ev.Alert.ProductGUID, ev.Alert.Rule, ev.Alert.ClearedAt)
	default:
		b.log.Warn("ignoring an alert event of unknown kind", "kind", ev.Kind)
		return lastErr
	}
	if err != nil {
		b.dropped.Add(1)
		b.log.Error("cannot record the alert", "err", err,
			"kind", ev.Kind, "rule", ev.Alert.Rule, "product", ev.Alert.ProductGUID)
		return err
	}
	return lastErr
}

// startSession opens a session row. A start without a preceding end - which
// the protocol allows - closes the previous one first, so two sessions are
// never open at the same time.
func (b *Batcher) startSession(ctx context.Context, ev event, lastErr error) error {
	lastErr = b.endSession(ctx, ev.at, lastErr)

	writeCtx, cancel := b.writeContext(ctx)
	defer cancel()
	id, err := b.store.StartSession(writeCtx, ev.at, ev.version, ev.headline)
	if err != nil {
		b.dropped.Add(1)
		b.log.Error("cannot record the session start", "err", err)
		return err
	}
	b.sessionID, b.sessionUp = id, true
	return lastErr
}

// endSession closes the open session, if there is one, and is a no-op
// otherwise.
func (b *Batcher) endSession(ctx context.Context, at time.Time, lastErr error) error {
	if !b.sessionUp {
		return lastErr
	}
	b.sessionUp = false

	writeCtx, cancel := b.writeContext(ctx)
	defer cancel()
	if err := b.store.EndSession(writeCtx, b.sessionID, at); err != nil {
		b.dropped.Add(1)
		b.log.Error("cannot record the session end", "err", err, "session", b.sessionID)
		return err
	}
	return lastErr
}

// closeSession ends the session still open at shutdown. Its end time is the
// newest sample the store holds - the last thing the game actually reported -
// so a replayed recording is not stamped with today's date. Only a session
// that saw no data at all falls back to the wall clock.
func (b *Batcher) closeSession(ctx context.Context, lastErr error) error {
	if !b.sessionUp {
		return lastErr
	}
	at := b.store.LastSampleTime()
	if at.IsZero() {
		at = time.Now()
	}
	return b.endSession(ctx, at, lastErr)
}

// writeContext derives the context every database write uses.
//
// It is deliberately detached from ctx: ctx is the shutdown signal, and a
// flush triggered by the interval, by the batch size or by shutdown itself
// must still be allowed to finish. Cancelling it mid-transaction would roll
// back a batch that was already handed over, and report an error for what is
// in fact a clean stop. The timeout is what bounds it instead.
func (b *Batcher) writeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
}

// flush writes pending and returns the emptied slice. A failed write is logged
// and the batch counted as lost: retrying would grow the queue behind a
// database that is already refusing writes, and the history is the expendable
// part.
func (b *Batcher) flush(ctx context.Context, pending []model.IslandSnapshot, lastErr error) ([]model.IslandSnapshot, error) {
	if len(pending) == 0 {
		return pending, lastErr
	}
	writeCtx, cancel := b.writeContext(ctx)
	defer cancel()

	if err := b.store.WriteSnapshots(writeCtx, pending); err != nil {
		b.dropped.Add(int64(len(pending)))
		b.log.Error("cannot write snapshots to the history database",
			"err", err, "snapshots", len(pending))
		lastErr = err
	}
	clear(pending)
	return pending[:0], lastErr
}
