package keypool

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Sync worker pool configuration constants.
const (
	defaultSyncConcurrency = 4
	minSyncConcurrency     = 1
	maxSyncConcurrency     = 32
	defaultSyncPacing      = 200 * time.Millisecond
	maxSyncPacing          = 60 * time.Second
)

// WorkerPool processes items using a configurable worker pool with waitForSlot pacing.
// Each worker calls waitForSlot before processing to enforce a minimum interval between
// request starts, preventing upstream rate limiting. Returns aggregate success and error counts.
//
// Based on TavilyProxyManager's SyncAllWithConcurrencyAndInterval pattern.
func WorkerPool[T any](ctx context.Context, items []T, concurrency int, interval time.Duration, processFn func(ctx context.Context, item T) error) (successes, errors int) {
	if len(items) == 0 {
		return 0, 0
	}

	// Clamp concurrency to [min, max] and cap at item count.
	if concurrency < minSyncConcurrency {
		concurrency = minSyncConcurrency
	}
	if concurrency > maxSyncConcurrency {
		concurrency = maxSyncConcurrency
	}
	if concurrency > len(items) {
		concurrency = len(items)
	}

	// Clamp interval to [0, max].
	if interval < 0 {
		interval = 0
	}
	if interval > maxSyncPacing {
		interval = maxSyncPacing
	}

	pacer := NewPacer(interval)
	jobs := make(chan T, len(items))

	type result struct {
		err error
	}
	var (
		results []result
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	// Launch workers.
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if err := pacer.WaitForSlot(ctx); err != nil {
					mu.Lock()
					results = append(results, result{err: err})
					mu.Unlock()
					continue
				}
				err := processFn(ctx, item)
				mu.Lock()
				results = append(results, result{err: err})
				mu.Unlock()
			}
		}()
	}

	// Dispatch all items.
	for _, item := range items {
		jobs <- item
	}
	close(jobs)

	wg.Wait()

	for _, r := range results {
		if r.err == nil {
			successes++
		} else {
			errors++
		}
	}
	return
}

// Pacer implements a token-bucket-style serial pacer. Each call to WaitForSlot
// reserves the next available time slot by advancing a shared nextStart timestamp.
// Workers that arrive early sleep until their reserved slot. This guarantees that
// no two API calls start within the configured interval of each other, regardless
// of how many concurrent workers are running.
//
// Based on TavilyProxyManager's waitForSlot closure pattern.
type Pacer struct {
	mu        sync.Mutex
	nextStart time.Time
	interval  time.Duration
}

// NewPacer creates a Pacer with the given minimum interval between request starts.
// If interval is zero or negative, WaitForSlot returns immediately (no pacing).
func NewPacer(interval time.Duration) *Pacer {
	if interval < 0 {
		interval = 0
	}
	return &Pacer{interval: interval}
}

// WaitForSlot blocks until the next available time slot, respecting context cancellation.
// If the pacer's interval is zero, it returns immediately without blocking.
func (p *Pacer) WaitForSlot(ctx context.Context) error {
	if p.interval <= 0 {
		return nil
	}

	p.mu.Lock()
	now := time.Now()
	if now.After(p.nextStart) {
		// We're past the next start time — claim this slot and advance.
		p.nextStart = now.Add(p.interval)
		p.mu.Unlock()
		return nil
	}

	// Need to wait: reserve the next slot and calculate sleep duration.
	sleep := time.Until(p.nextStart)
	p.nextStart = p.nextStart.Add(p.interval)
	p.mu.Unlock()

	timer := time.NewTimer(sleep)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("pacer: context cancelled during wait: %w", ctx.Err())
	}
}

// Interval returns the configured pacing interval.
func (p *Pacer) Interval() time.Duration {
	return p.interval
}
