package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/ingest"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/state"
	"github.com/Dealwatch/Tabularium117/internal/store"
)

const (
	// historyFile is the database inside the data directory.
	historyFile = "tabularium117.db"
	// flushInterval is how long a snapshot may wait before it is committed.
	// Long enough to turn one tick's islands into one transaction, short
	// enough that a kill loses almost nothing.
	flushInterval = 2 * time.Second
)

// history is the persistence side of a run: the database, the batch writer
// that feeds it, and the background compactor.
//
// It is a thin wiring layer. Everything that knows about SQL lives in
// internal/store; this type only decides when to call it.
type history struct {
	store   *store.Store
	batcher *store.Batcher
	log     *slog.Logger

	stop func()
	wg   sync.WaitGroup
	// batchErr is the batch writer's last write failure. It is written by the
	// batcher's goroutine and read only after wg.Wait.
	batchErr error
}

// openHistory opens (or creates) the database under dataDir and starts the
// batch writer and the compactor.
//
// ctx is used for opening the database and nothing else. The workers run on a
// context this history owns, so that only Close stops them: they are the last
// consumers of the snapshots the pipeline is still delivering, and a shared
// cancellation would let them exit while ingest is still handing work over -
// which would silently lose every snapshot of the final moments. Close runs
// after the pipeline has returned, so by then there is nothing left to lose.
func openHistory(ctx context.Context, dataDir string, log *slog.Logger) (*history, error) {
	path := filepath.Join(dataDir, historyFile)
	s, err := store.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	log.Info("keeping history", "file", path)

	h := &history{
		store:   s,
		batcher: store.NewBatcher(s, flushInterval, log),
		log:     log,
	}
	runCtx, cancel := context.WithCancel(context.Background())
	h.stop = cancel
	h.wg.Add(2)
	go func() {
		defer h.wg.Done()
		h.batchErr = h.batcher.Run(runCtx)
	}()
	go func() {
		defer h.wg.Done()
		s.RunCompactor(runCtx, store.DefaultCompactInterval, store.DefaultCompactOptions(), log)
	}()
	return h, nil
}

// attach chains the store onto the pipeline's callbacks, keeping whatever was
// already installed. st is read for the protocol version of a new session.
//
// Every callback is a non-blocking hand-over to the batch writer's goroutine,
// including the session boundaries: the pipeline must never wait for a
// database write, not even a small one.
func (h *history) attach(pipeline *ingest.Pipeline, st *state.State) {
	previous := pipeline.OnSnapshot
	pipeline.OnSnapshot = func(snap model.IslandSnapshot) {
		if previous != nil {
			previous(snap)
		}
		h.batcher.Add(snap)
	}
	pipeline.OnSessionStart = func(headline string, at time.Time) {
		h.batcher.AddSessionStart(headline, at, st.Connection().ProtocolVersion)
	}
	pipeline.OnSessionEnd = h.batcher.AddSessionEnd
}

// addAlert hands one alert event to the batch writer. Like every other
// callback here it is a non-blocking hand-over: the pipeline must never wait
// for a database write.
//
// Alerts share the batcher's queue with the snapshots, which is what keeps an
// alert behind the snapshot that caused it - and therefore behind the island
// row that snapshot creates.
func (h *history) addAlert(ev alerts.Event) {
	h.batcher.AddAlertEvent(ev)
}

// Close stops the background goroutines - which writes whatever is still
// queued and closes an open session - and then closes the database.
//
// It must be called after the pipeline has stopped delivering snapshots.
//
// A write that failed is reported but does not become Close's error: the
// history is the expendable part of this program, and a run that produced a
// live view must not exit as a failure because the last batch could not be
// stored.
func (h *history) Close() error {
	h.stop()
	h.wg.Wait()
	if h.batchErr != nil {
		h.log.Error("the history is incomplete", "err", h.batchErr)
	}
	if dropped := h.batcher.Dropped(); dropped > 0 {
		h.log.Warn("records missing from the history", "dropped", dropped)
	}
	return h.store.Close()
}
