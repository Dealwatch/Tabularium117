package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/store"
)

// The history's workers must outlive the shutdown signal and stop only when
// Close says so.
//
// Ctrl+C reaches the pipeline and the history at the same instant. If the
// batch writer stopped on that signal it could exit while the pipeline is
// still handing over the frame it was decoding, and that snapshot would be
// dropped into a channel nobody reads again. Close runs after the pipeline has
// returned, which is the only moment at which no more snapshots can arrive.
func TestHistoryWorkersStopOnlyOnClose(t *testing.T) {
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	signalCtx, signal := context.WithCancel(context.Background())
	h, err := openHistory(signalCtx, dir, log)
	if err != nil {
		t.Fatalf("openHistory: %v", err)
	}

	// The signal fires; ingest is still finishing its last frame.
	signal()
	// Long enough that a worker tied to the signal context would be gone.
	time.Sleep(100 * time.Millisecond)

	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	h.batcher.AddSessionStart("a savegame", at, 2)
	h.batcher.Add(model.IslandSnapshot{
		Key:        model.IslandKey{SessionGUID: 3245, IslandID: 5},
		Name:       "Juliana",
		ReceivedAt: at,
		Products:   []model.ProductStat{{ProductGUID: 2063, Generation: 7}},
	})

	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(dir, historyFile))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	islands, err := s.Islands(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(islands) != 1 || islands[0].Name != "Juliana" {
		t.Fatalf("islands = %+v, want the snapshot handed over after the signal", islands)
	}
	points, err := s.History(ctx, islands[0].Key, 2063, at.Add(-time.Minute), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].Generation != 7 {
		t.Fatalf("history = %+v, want the one sample that was handed over", points)
	}
	sessions, err := s.Sessions(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Headline != "a savegame" {
		t.Fatalf("sessions = %+v, want the session started after the signal", sessions)
	}
	// Shutdown closes the session at the newest sample time, not at the wall
	// clock: the data is from the recording, so the session ends there too.
	if !sessions[0].EndedAt.Equal(at) {
		t.Errorf("session ended at %v, want the last sample time %v", sessions[0].EndedAt, at)
	}
}
