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
// Tokens accrue continuously at refillRate (tokens/second) up to capacity, and
// each allowed request spends one. Refill is "lazy": rather than running a
// background ticker, Allow computes how many tokens have accrued since the last
// call. That keeps it O(1) in time and memory with no goroutines.
type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	capacity   float64
	refillRate float64 // tokens per second; equals the configured requests/sec
	last       time.Time
	now        func() time.Time // time source; injectable so tests can control it
}

// Option customizes a TokenBucket at construction.
type Option func(*TokenBucket)

// WithClock overrides the time source. Tests use it to advance time
// deterministically instead of sleeping, which makes time-based assertions
// exact and fast rather than flaky.
func WithClock(now func() time.Time) Option {
	return func(b *TokenBucket) { b.now = now }
}

// NewTokenBucket returns a limiter that permits ratePerSec requests/second.
//
// The burst capacity is one second's worth of tokens (ratePerSec), with a floor
// of 1 so that fractional rates still work — e.g. a rate of 0.5 accumulates a
// single token every two seconds rather than never reaching one. The bucket
// starts full, allowing an initial burst up to capacity.
func NewTokenBucket(ratePerSec float64, opts ...Option) *TokenBucket {
	capacity := ratePerSec
	if capacity < 1 {
		capacity = 1
	}

	b := &TokenBucket{
		capacity:   capacity,
		refillRate: ratePerSec,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(b)
	}

	b.tokens = b.capacity // start full
	b.last = b.now()
	return b
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
