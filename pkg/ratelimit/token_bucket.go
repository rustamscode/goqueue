// Package ratelimit provides rate limiting implementations for GoQueue.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

// TokenBucket implements the token bucket algorithm for rate limiting.
// Tokens are added at a constant rate up to a maximum capacity.
// Each operation consumes one or more tokens.
type TokenBucket struct {
	rate       float64   // tokens per second
	capacity   float64   // maximum tokens
	tokens     float64   // current token count
	lastRefill time.Time // last time tokens were added
	mu         sync.Mutex
}

// NewTokenBucket creates a new token bucket with the given rate and capacity.
// Rate is tokens per second. Capacity is the maximum burst size.
func NewTokenBucket(rate, capacity float64) *TokenBucket {
	if rate <= 0 {
		rate = 1
	}
	if capacity <= 0 {
		capacity = 1
	}
	return &TokenBucket{
		rate:       rate,
		capacity:   capacity,
		tokens:     capacity, // Start full
		lastRefill: time.Now(),
	}
}

// Allow checks if one token is available and consumes it.
// Returns true if the request is allowed, false if rate limited.
func (tb *TokenBucket) Allow() bool {
	return tb.AllowN(1)
}

// AllowN checks if n tokens are available and consumes them.
// Returns true if the request is allowed, false if rate limited.
func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if float64(n) > tb.tokens {
		return false
	}

	tb.tokens -= float64(n)
	return true
}

// Wait blocks until a token is available or the context is cancelled.
// Returns nil if a token was acquired, or the context error.
func (tb *TokenBucket) Wait(ctx context.Context) error {
	return tb.WaitN(ctx, 1)
}

// WaitN blocks until n tokens are available or the context is cancelled.
func (tb *TokenBucket) WaitN(ctx context.Context, n int) error {
	for {
		if tb.AllowN(n) {
			return nil
		}

		// Calculate wait time until enough tokens are available
		tb.mu.Lock()
		needed := float64(n) - tb.tokens
		waitTime := time.Duration(needed / tb.rate * float64(time.Second))
		tb.mu.Unlock()

		// Wait with minimum of 10ms to avoid busy spinning
		if waitTime < 10*time.Millisecond {
			waitTime = 10 * time.Millisecond
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// Try again
		}
	}
}

// Reserve reserves a token and returns the wait time.
// If the wait time is 0, the token is immediately available.
// If the wait time is positive, the caller should wait before proceeding.
// Returns -1 if the request cannot be satisfied even with waiting.
func (tb *TokenBucket) Reserve() time.Duration {
	return tb.ReserveN(1)
}

// ReserveN reserves n tokens and returns the wait time.
func (tb *TokenBucket) ReserveN(n int) time.Duration {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if float64(n) <= tb.tokens {
		tb.tokens -= float64(n)
		return 0
	}

	// Calculate how long until we have enough tokens
	needed := float64(n) - tb.tokens
	waitTime := time.Duration(needed / tb.rate * float64(time.Second))

	// Reserve the tokens (will be available after waiting)
	tb.tokens -= float64(n)

	return waitTime
}

// Tokens returns the current number of available tokens.
func (tb *TokenBucket) Tokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	return tb.tokens
}

// Rate returns the token generation rate (tokens per second).
func (tb *TokenBucket) Rate() float64 {
	return tb.rate
}

// Capacity returns the maximum token capacity.
func (tb *TokenBucket) Capacity() float64 {
	return tb.capacity
}

// SetRate updates the token generation rate.
func (tb *TokenBucket) SetRate(rate float64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	if rate > 0 {
		tb.rate = rate
	}
}

// Reset fills the bucket to capacity.
func (tb *TokenBucket) Reset() {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.tokens = tb.capacity
	tb.lastRefill = time.Now()
}

// refill adds tokens based on elapsed time.
// Must be called with the mutex held.
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()

	if elapsed <= 0 {
		return
	}

	// Add tokens based on elapsed time
	tb.tokens += elapsed * tb.rate

	// Cap at capacity
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	tb.lastRefill = now
}

// Limiter is an interface for rate limiters.
type Limiter interface {
	// Allow returns true if the request should be allowed.
	Allow() bool
	// Wait blocks until the request is allowed or context is cancelled.
	Wait(ctx context.Context) error
}

// ensure TokenBucket implements Limiter
var _ Limiter = (*TokenBucket)(nil)

// MultiLimiter combines multiple limiters, all must allow for request to proceed.
type MultiLimiter struct {
	limiters []Limiter
}

// NewMultiLimiter creates a limiter that combines multiple limiters.
func NewMultiLimiter(limiters ...Limiter) *MultiLimiter {
	return &MultiLimiter{limiters: limiters}
}

// Allow returns true only if all limiters allow the request.
func (ml *MultiLimiter) Allow() bool {
	for _, l := range ml.limiters {
		if !l.Allow() {
			return false
		}
	}
	return true
}

// Wait blocks until all limiters allow the request.
func (ml *MultiLimiter) Wait(ctx context.Context) error {
	for _, l := range ml.limiters {
		if err := l.Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

// PerSecond creates a token bucket that allows n operations per second.
func PerSecond(n int) *TokenBucket {
	return NewTokenBucket(float64(n), float64(n))
}

// PerMinute creates a token bucket that allows n operations per minute.
func PerMinute(n int) *TokenBucket {
	rate := float64(n) / 60.0
	return NewTokenBucket(rate, float64(n))
}

// PerHour creates a token bucket that allows n operations per hour.
func PerHour(n int) *TokenBucket {
	rate := float64(n) / 3600.0
	return NewTokenBucket(rate, float64(n))
}
