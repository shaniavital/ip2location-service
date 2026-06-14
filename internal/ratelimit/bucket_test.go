package ratelimit_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shaniavital/ip2location-service/internal/ratelimit"
)

func TestTokenBucket_InitialBurst(t *testing.T) {
	clk := newFakeClock()
	b := ratelimit.NewTokenBucket(5, ratelimit.WithClock(clk.now))

	// Starts full: the first 5 requests succeed, the 6th is denied because no
	// time has passed to refill anything.
	drain(t, b, 5)
	if b.Allow() {
		t.Fatal("6th request should be denied (bucket empty)")
	}
}

func TestTokenBucket_RefillOverTime(t *testing.T) {
	clk := newFakeClock()
	b := ratelimit.NewTokenBucket(5, ratelimit.WithClock(clk.now)) // 5 tokens/sec

	drain(t, b, 5)
	if b.Allow() {
		t.Fatal("bucket should be empty before any time passes")
	}

	clk.advance(400 * time.Millisecond) // 0.4s * 5/s = exactly 2 tokens
	if !b.Allow() {
		t.Fatal("1st request after refill should be allowed")
	}
	if !b.Allow() {
		t.Fatal("2nd request after refill should be allowed")
	}
	if b.Allow() {
		t.Fatal("3rd request should be denied (only 2 tokens refilled)")
	}
}

func TestTokenBucket_CapacityIsCapped(t *testing.T) {
	clk := newFakeClock()
	b := ratelimit.NewTokenBucket(5, ratelimit.WithClock(clk.now))

	drain(t, b, 5)
	clk.advance(100 * time.Second) // would be 500 tokens, but capacity caps at 5

	drain(t, b, 5)
	if b.Allow() {
		t.Fatal("tokens should be capped at capacity (5), not accumulate unbounded")
	}
}

func TestTokenBucket_FractionalRate(t *testing.T) {
	clk := newFakeClock()
	b := ratelimit.NewTokenBucket(0.5, ratelimit.WithClock(clk.now)) // one token every 2s

	if !b.Allow() {
		t.Fatal("first request should use the initial token")
	}
	if b.Allow() {
		t.Fatal("second request should be denied immediately")
	}

	clk.advance(2 * time.Second) // 2s * 0.5/s = 1 token
	if !b.Allow() {
		t.Fatal("request should be allowed after 2s")
	}
	clk.advance(1 * time.Second) // only 0.5 token, not enough
	if b.Allow() {
		t.Fatal("request should be denied with only 0.5 token accrued")
	}
}

// TestTokenBucket_ConcurrentAllow checks thread-safety and correctness under
// concurrency. With a frozen clock no tokens refill, so across 1000 racing
// goroutines exactly `capacity` requests may succeed. Run with -race.
func TestTokenBucket_ConcurrentAllow(t *testing.T) {
	clk := newFakeClock() // never advances
	const capacity = 100
	b := ratelimit.NewTokenBucket(capacity, ratelimit.WithClock(clk.now))

	var allowed int64
	var wg sync.WaitGroup
	for range 1000 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.Allow() {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	wg.Wait()

	if allowed != capacity {
		t.Fatalf("allowed = %d, want exactly %d", allowed, capacity)
	}
}

// drain asserts that the next n requests are all allowed.
func drain(t *testing.T, b *ratelimit.TokenBucket, n int) {
	t.Helper()
	for i := range n {
		if !b.Allow() {
			t.Fatalf("drain: request %d should be allowed", i+1)
		}
	}
}

// fakeClock is a manually-advanced time source for deterministic tests. It is
// safe for concurrent use because the concurrency test reads it from many
// goroutines.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
