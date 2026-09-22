package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// A caller's deadline has to bound the whole write, including the wait for the
// write lock. Otherwise a long compaction holding the lock would freeze ingest
// for as long as it runs, and the timeout would be a decoration.
func TestWriteLockWaitIsBoundedByTheContext(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, MemoryPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Stand in for a compaction that holds the lock far longer than the
	// caller is willing to wait.
	if err := s.acquireWrite(ctx); err != nil {
		t.Fatalf("acquireWrite: %v", err)
	}
	defer s.releaseWrite()

	writeCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = s.WriteSnapshots(writeCtx, []model.IslandSnapshot{{
		Key: model.IslandKey{SessionGUID: 1, IslandID: 1}, Name: "Isle",
		ReceivedAt: time.Now(),
	}})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("the write succeeded although the lock was held")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to wrap context.DeadlineExceeded", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("the write waited %v; the context deadline did not bound it", elapsed)
	}
	// A write that never happened must not move the retention anchor.
	if !s.LastSampleTime().IsZero() {
		t.Errorf("LastSampleTime = %v after a failed write, want the zero time", s.LastSampleTime())
	}
}
