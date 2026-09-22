package store_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/store"
)

// Everything handed to Add before the shutdown has to reach the database,
// including the part that is still pending when the context ends.
func TestBatcherFlushesEverythingOnShutdown(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	// An interval far longer than the test, so the only flushes are the
	// size-triggered ones and the final one.
	b := store.NewBatcher(s, time.Hour, nil)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- b.Run(runCtx) }()

	const n = 100
	for i := range n {
		b.Add(snap(key(1, int32(i)), "Isle", base.Add(time.Duration(i)*time.Second), 42))
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
	if dropped := b.Dropped(); dropped != 0 {
		t.Fatalf("%d snapshots were dropped; the queue should be nowhere near full", dropped)
	}

	islands, err := s.Islands(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(islands) != n {
		t.Fatalf("%d islands in the database, want all %d", len(islands), n)
	}
	for i, island := range islands {
		points, err := s.History(ctx, island.Key, 42, base, base.Add(n*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if len(points) != 1 {
			t.Fatalf("island %d has %d points, want the one sample that was added", i, len(points))
		}
	}
}

// A Batcher whose Run never starts must still not block Add: ingest is not
// allowed to wait for the history under any circumstances.
func TestAddNeverBlocks(t *testing.T) {
	s := open(t)
	b := store.NewBatcher(s, time.Hour, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Far more than the queue holds, with nothing draining it.
		for i := range 5000 {
			b.Add(snap(key(1, int32(i)), "Isle", base, 42))
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Add blocked when the queue was full")
	}
	if b.Dropped() == 0 {
		t.Error("overflowing the queue must be counted as dropped snapshots")
	}
}

// A database that refuses writes must cost the history, not the program: the
// failure is logged, the batch counted as lost, and Run still returns - with
// the error, so the caller can say the history is incomplete.
func TestFlushFailureIsLoggedAndCounted(t *testing.T) {
	s := open(t)
	var logged bytes.Buffer
	b := store.NewBatcher(s, time.Hour, slog.New(slog.NewTextHandler(&logged, nil)))

	// Closing the store is the bluntest way to make every write fail.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	b.Add(snap(key(1, 1), "Isle", base, 42))
	b.Add(snap(key(1, 2), "Isle", base, 42))
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil although the flush failed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after a failed flush")
	}
	if !strings.Contains(logged.String(), "cannot write snapshots") {
		t.Errorf("the failed write was not reported:\n%s", logged.String())
	}
	// Every snapshot in the failed batch counts as lost, so the caller can
	// tell the user how much of the history is missing.
	if got := b.Dropped(); got != 2 {
		t.Errorf("Dropped() = %d after a failed flush of two snapshots, want 2", got)
	}
}

// A periodic flush must not be cancelled by the shutdown signal. This is the
// realistic shape of Ctrl+C: the context ends while snapshots are pending, and
// they still have to be written.
func TestFlushIsNotCancelledByShutdown(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	b := store.NewBatcher(s, 10*time.Millisecond, nil)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- b.Run(runCtx) }()

	for i := range 10 {
		b.Add(snap(key(2, int32(i)), "Isle", base.Add(time.Duration(i)*time.Second), 42))
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v; a shutdown must not make the flush fail", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
	if b.Dropped() != 0 {
		t.Errorf("Dropped() = %d, want nothing lost on a clean shutdown", b.Dropped())
	}
	if islands, err := s.Islands(ctx); err != nil {
		t.Fatal(err)
	} else if len(islands) != 10 {
		t.Errorf("%d islands stored, want all 10", len(islands))
	}
}

// Session boundaries and snapshots share one queue, so the database sees them
// in the order ingest did: the session row exists before the snapshots that
// followed it, and is closed afterwards.
func TestSessionEventsAreOrderedWithSnapshots(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	b := store.NewBatcher(s, time.Hour, nil)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- b.Run(runCtx) }()

	b.AddSessionStart("a savegame", base, 2)
	b.Add(snap(key(1, 1), "Isle", base.Add(time.Second), 42))
	b.AddSessionEnd(base.Add(2 * time.Second))

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}

	sessions, err := s.Sessions(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %+v, want one", sessions)
	}
	if sessions[0].Headline != "a savegame" || sessions[0].ProtocolVersion != 2 {
		t.Errorf("session = %+v, want the headline and version that were queued", sessions[0])
	}
	if !sessions[0].StartedAt.Equal(base) || !sessions[0].EndedAt.Equal(base.Add(2*time.Second)) {
		t.Errorf("session times = %v..%v, want %v..%v",
			sessions[0].StartedAt, sessions[0].EndedAt, base, base.Add(2*time.Second))
	}
	if islands, err := s.Islands(ctx); err != nil {
		t.Fatal(err)
	} else if len(islands) != 1 {
		t.Errorf("islands = %+v, want the one queued between the boundaries", islands)
	}
}

// A session still open at shutdown is closed with the data's own newest time,
// not the wall clock: a replayed recording from last year must not end today.
func TestShutdownClosesTheSessionAtTheLastSampleTime(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	b := store.NewBatcher(s, time.Hour, nil)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- b.Run(runCtx) }()

	last := base.Add(5 * time.Second)
	b.AddSessionStart("a savegame", base, 2)
	b.Add(snap(key(1, 1), "Isle", last, 42))
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}

	sessions, err := s.Sessions(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].EndedAt.IsZero() {
		t.Fatalf("sessions = %+v, want one closed session", sessions)
	}
	if !sessions[0].EndedAt.Equal(last) {
		t.Errorf("session ended at %v, want the last sample time %v", sessions[0].EndedAt, last)
	}
}

// The island row an alert references is created by the snapshot that caused
// it, and both go through the same queue. Writing the alert therefore has to
// flush the snapshots queued before it, or the reference would point at
// nothing.
func TestBatcherWritesAlertsAfterTheirSnapshot(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	var logged bytes.Buffer
	// An interval far longer than the test: if the alert were written
	// without flushing first, nothing would have created the island.
	b := store.NewBatcher(s, time.Hour, slog.New(slog.NewTextHandler(&logged, nil)))
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- b.Run(runCtx) }()

	k := key(3245, 5)
	b.Add(snap(k, "Juliana", base, 2068))
	b.AddAlertEvent(alerts.Event{
		Kind:  alerts.KindRaised,
		Alert: alert(k, "Juliana", 2068, alerts.RuleDeficit, base),
	})
	b.AddAlertEvent(alerts.Event{
		Kind:  alerts.KindCleared,
		Alert: cleared(alert(k, "Juliana", 2068, alerts.RuleDeficit, base), base.Add(time.Minute)),
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
	if dropped := b.Dropped(); dropped != 0 {
		t.Errorf("%d records were dropped; log: %s", dropped, logged.String())
	}

	rows, err := s.Alerts(ctx, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("stored alerts = %d, want 1", len(rows))
	}
	if rows[0].Active() {
		t.Error("the alert should have been cleared by the second event")
	}
	if rows[0].IslandName != "Juliana" {
		t.Errorf("island name = %q; the snapshot should have created the island row", rows[0].IslandName)
	}
}

// cleared turns a raised alert into the cleared one the engine emits.
func cleared(a alerts.Alert, at time.Time) alerts.Alert {
	a.ClearedAt = at
	return a
}

// An alert event of an unknown kind is a wiring mistake, not a reason to
// count a lost record or to stop the writer.
func TestBatcherIgnoresUnknownAlertKinds(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	var logged bytes.Buffer
	b := store.NewBatcher(s, time.Hour, slog.New(slog.NewTextHandler(&logged, nil)))
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- b.Run(runCtx) }()

	b.AddAlertEvent(alerts.Event{Kind: "nonsense", Alert: alert(key(1, 1), "I", 1, alerts.RuleDeficit, base)})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rows, _ := s.Alerts(ctx, false, 0); len(rows) != 0 {
		t.Errorf("stored alerts = %d, want none", len(rows))
	}
	if !strings.Contains(logged.String(), "unknown kind") {
		t.Errorf("the unknown kind was not logged: %s", logged.String())
	}
}
