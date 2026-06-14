// Package ratelimit provides a hand-rolled token-bucket rate limiter. It has no
// HTTP dependency: it exposes only Allow(), so it can be wrapped by an HTTP
// middleware (or used anywhere else) and unit-tested in isolation.
package ratelimit

import (
	"sync"
	"time"
)

// TokenBucket is a thread-safe token-bucket limiter.
//
// Tokens refill at refillRate (tokens/second) up to capacity, and each allowed
// request spends one. Refill is "lazy": instead of a background ticker, Allow
// computes how many tokens have accrued since the last call from the elapsed
// time. That keeps it O(1) with no goroutines.
type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	capacity   float64
	refillRate float64 // tokens per second; equals the configured requests/sec
	last       time.Time
	now        func() time.Time // time source; time.Now in production, a fake in tests
}

// NewTokenBucket returns a limiter that permits ratePerSec requests/second.
//
// Capacity (the burst size) is one second's worth of tokens, floored at 1 so
// fractional rates still work — a rate of 0.5 accrues one token every two
// seconds rather than never reaching one. The bucket starts full.
func NewTokenBucket(ratePerSec float64) *TokenBucket {
	return newTokenBucket(ratePerSec, time.Now)
}

// newTokenBucket is the real constructor; the exported one fixes the clock to
// time.Now, while tests pass a controllable clock for deterministic timing.
func newTokenBucket(ratePerSec float64, now func() time.Time) *TokenBucket {
	capacity := ratePerSec
	if capacity < 1 {
		capacity = 1
	}
	return &TokenBucket{
		tokens:     capacity, // start full
		capacity:   capacity,
		refillRate: ratePerSec,
		last:       now(),
		now:        now,
	}
}

// Allow reports whether a request may proceed, consuming one token if so.
func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = min(b.capacity, b.tokens+elapsed*b.refillRate)
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}
