package keypool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- WorkerPool tests ----------

func TestWorkerPool_Empty(t *testing.T) {
	s, e := WorkerPool(context.Background(), []int{}, 4, 10*time.Millisecond, func(ctx context.Context, item int) error {
		t.Fatal("should not be called")
		return nil
	})
	if s != 0 || e != 0 {
		t.Errorf("expected (0,0), got (%d,%d)", s, e)
	}
}

func TestWorkerPool_AllSuccess(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	s, e := WorkerPool(context.Background(), items, 2, 0, func(ctx context.Context, item int) error {
		return nil
	})
	if s != 5 {
		t.Errorf("expected 5 successes, got %d", s)
	}
	if e != 0 {
		t.Errorf("expected 0 errors, got %d", e)
	}
}

func TestWorkerPool_AllErrors(t *testing.T) {
	items := []int{1, 2, 3}
	s, e := WorkerPool(context.Background(), items, 2, 0, func(ctx context.Context, item int) error {
		return fmt.Errorf("fail %d", item)
	})
	if s != 0 {
		t.Errorf("expected 0 successes, got %d", s)
	}
	if e != 3 {
		t.Errorf("expected 3 errors, got %d", e)
	}
}

func TestWorkerPool_MixedResults(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6}
	s, e := WorkerPool(context.Background(), items, 3, 0, func(ctx context.Context, item int) error {
		if item%2 == 0 {
			return nil
		}
		return fmt.Errorf("odd fail")
	})
	if s != 3 {
		t.Errorf("expected 3 successes, got %d", s)
	}
	if e != 3 {
		t.Errorf("expected 3 errors, got %d", e)
	}
}

func TestWorkerPool_LimitsConcurrency(t *testing.T) {
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32

	items := make([]int, 10)
	for i := range items {
		items[i] = i
	}

	WorkerPool(context.Background(), items, 2, 0, func(ctx context.Context, item int) error {
		current := inFlight.Add(1)
		for {
			old := maxInFlight.Load()
			if current <= old || maxInFlight.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		inFlight.Add(-1)
		return nil
	})

	if max := maxInFlight.Load(); max > 2 {
		t.Errorf("max concurrent workers was %d, expected <= 2", max)
	}
}

func TestWorkerPool_RespectsInterval(t *testing.T) {
	const keyCount = 6
	const interval = 50 * time.Millisecond

	items := make([]int, keyCount)
	for i := range items {
		items[i] = i
	}

	start := time.Now()
	WorkerPool(context.Background(), items, 1, interval, func(ctx context.Context, item int) error {
		return nil
	})
	elapsed := time.Since(start)

	minExpected := time.Duration(keyCount-1) * interval
	if elapsed < minExpected {
		t.Errorf("elapsed %v < minimum expected %v (pacing not enforced)", elapsed, minExpected)
	}
	t.Logf("elapsed=%v, minExpected=%v", elapsed, minExpected)
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	items := make([]int, 10)
	for i := range items {
		items[i] = i
	}

	s, e := WorkerPool(ctx, items, 2, 100*time.Millisecond, func(ctx context.Context, item int) error {
		return nil
	})

	total := s + e
	if total != 10 {
		t.Errorf("expected 10 total results, got %d (success=%d, errors=%d)", total, s, e)
	}
}

func TestWorkerPool_ConcurrencyClamped(t *testing.T) {
	var maxInFlight atomic.Int32
	var inFlight atomic.Int32

	items := make([]int, 5)
	for i := range items {
		items[i] = i
	}

	// concurrency=100 should be clamped to maxSyncConcurrency (32) or len(items) (5).
	WorkerPool(context.Background(), items, 100, 0, func(ctx context.Context, item int) error {
		current := inFlight.Add(1)
		for {
			old := maxInFlight.Load()
			if current <= old || maxInFlight.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		return nil
	})

	if max := maxInFlight.Load(); max > 5 {
		t.Errorf("max concurrent was %d, expected <= 5 (clamped to item count)", max)
	}
}

func TestWorkerPool_SingleItem(t *testing.T) {
	s, e := WorkerPool(context.Background(), []int{42}, 4, 100*time.Millisecond, func(ctx context.Context, item int) error {
		if item != 42 {
			return fmt.Errorf("expected 42, got %d", item)
		}
		return nil
	})
	if s != 1 || e != 0 {
		t.Errorf("expected (1,0), got (%d,%d)", s, e)
	}
}

func TestWorkerPool_ConcurrentResultCollection(t *testing.T) {
	const n = 100
	items := make([]int, n)
	for i := range items {
		items[i] = i
	}

	var mu sync.Mutex
	seen := make(map[int]bool)

	s, e := WorkerPool(context.Background(), items, 8, 0, func(ctx context.Context, item int) error {
		mu.Lock()
		seen[item] = true
		mu.Unlock()
		return nil
	})

	if s != n {
		t.Errorf("expected %d successes, got %d", n, s)
	}
	if e != 0 {
		t.Errorf("expected 0 errors, got %d", e)
	}
	if len(seen) != n {
		t.Errorf("expected %d unique items processed, got %d", n, len(seen))
	}
}

// ---------- Pacer tests ----------

func TestPacer_ZeroInterval(t *testing.T) {
	p := NewPacer(0)
	for i := 0; i < 50; i++ {
		if err := p.WaitForSlot(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestPacer_NegativeInterval(t *testing.T) {
	p := NewPacer(-100 * time.Millisecond)
	if p.Interval() != 0 {
		t.Errorf("expected interval 0 for negative input, got %v", p.Interval())
	}
	if err := p.WaitForSlot(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPacer_BasicPacing(t *testing.T) {
	interval := 30 * time.Millisecond
	p := NewPacer(interval)

	start := time.Now()
	for i := 0; i < 4; i++ {
		if err := p.WaitForSlot(context.Background()); err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	// 4 calls = 3 gaps (first is immediate).
	minExpected := 3 * interval
	if elapsed < minExpected {
		t.Errorf("elapsed %v < expected minimum %v", elapsed, minExpected)
	}
	t.Logf("elapsed=%v, minExpected=%v", elapsed, minExpected)
}

func TestPacer_ConcurrentSafety(t *testing.T) {
	p := NewPacer(5 * time.Millisecond)
	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- p.WaitForSlot(context.Background())
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

func TestPacer_ContextCancellation(t *testing.T) {
	p := NewPacer(5 * time.Second)

	// Claim the first slot (immediate).
	if err := p.WaitForSlot(context.Background()); err != nil {
		t.Fatalf("first call should succeed: %v", err)
	}

	// Second call should block; cancel the context quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := p.WaitForSlot(ctx)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestPacer_Interval(t *testing.T) {
	p := NewPacer(42 * time.Millisecond)
	if p.Interval() != 42*time.Millisecond {
		t.Errorf("expected 42ms, got %v", p.Interval())
	}
}
