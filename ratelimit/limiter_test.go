package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitCombinesDuplicateCharges(t *testing.T) {
	t.Parallel()

	limiter, err := New(Rule{Key: "weight", Limit: 3, Window: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := limiter.Wait(
		context.Background(),
		Charge{Key: "weight", Units: 1},
		Charge{Key: "weight", Units: 2},
	); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	snapshot, err := limiter.Snapshot("weight")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Used != 3 {
		t.Fatalf("Used = %d, want 3", snapshot.Used)
	}
}

func TestWaitDoesNotPartiallyCharge(t *testing.T) {
	t.Parallel()

	limiter, err := New(
		Rule{Key: "available", Limit: 10, Window: time.Second},
		Rule{Key: "full", Limit: 1, Window: time.Second},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := limiter.Wait(context.Background(), Charge{Key: "full", Units: 1}); err != nil {
		t.Fatalf("initial Wait() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = limiter.Wait(ctx,
		Charge{Key: "available", Units: 2},
		Charge{Key: "full", Units: 1},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait() error = %v, want context deadline", err)
	}
	snapshot, err := limiter.Snapshot("available")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Used != 0 {
		t.Fatalf("available Used = %d, want 0", snapshot.Used)
	}
}

func TestObserveUsedOnlyRaisesLocalUsage(t *testing.T) {
	t.Parallel()

	limiter, err := New(Rule{Key: "weight", Limit: 100, Window: time.Minute})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := limiter.ObserveUsed("weight", 50); err != nil {
		t.Fatalf("ObserveUsed(50) error = %v", err)
	}
	if err := limiter.ObserveUsed("weight", 20); err != nil {
		t.Fatalf("ObserveUsed(20) error = %v", err)
	}
	snapshot, err := limiter.Snapshot("weight")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Used != 50 {
		t.Fatalf("Used = %d, want 50", snapshot.Used)
	}
}

func TestBlockForHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	limiter, err := New(Rule{Key: "weight", Limit: 100, Window: time.Minute})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := limiter.BlockFor([]string{"weight"}, time.Second); err != nil {
		t.Fatalf("BlockFor() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := limiter.Wait(ctx, Charge{Key: "weight", Units: 1}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait() error = %v, want context deadline", err)
	}
}

func TestMemoryBackendDoesNotBurstAcrossWallClockBoundary(t *testing.T) {
	t.Parallel()
	backend := newMemoryBackend()
	rule := Rule{Key: "weight", Limit: 1, Window: time.Second}
	if err := backend.SetRule(rule); err != nil {
		t.Fatalf("SetRule() error = %v", err)
	}
	first := time.Unix(100, 999*int64(time.Millisecond))
	if waitUntil, err := backend.tryAcquire(first, []Charge{{Key: "weight", Units: 1}}); err != nil || !waitUntil.IsZero() {
		t.Fatalf("first tryAcquire() = %v, %v", waitUntil, err)
	}
	second := first.Add(2 * time.Millisecond)
	waitUntil, err := backend.tryAcquire(second, []Charge{{Key: "weight", Units: 1}})
	if err != nil {
		t.Fatalf("second tryAcquire() error = %v", err)
	}
	if want := first.Add(time.Second); !waitUntil.Equal(want) {
		t.Fatalf("second tryAcquire() wait = %v, want %v", waitUntil, want)
	}
}
