package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTokenBucket_Allow(t *testing.T) {
	// 10 tokens per second, capacity of 10
	tb := NewTokenBucket(10, 10)

	// Should allow initial burst
	for i := 0; i < 10; i++ {
		if !tb.Allow() {
			t.Errorf("Allow() should return true for request %d", i)
		}
	}

	// Should deny when exhausted
	if tb.Allow() {
		t.Error("Allow() should return false when tokens exhausted")
	}
}

func TestTokenBucket_AllowN(t *testing.T) {
	tb := NewTokenBucket(10, 10)

	// Request 5 tokens
	if !tb.AllowN(5) {
		t.Error("AllowN(5) should return true")
	}

	// Request 5 more
	if !tb.AllowN(5) {
		t.Error("AllowN(5) should return true for second batch")
	}

	// Should deny when exhausted
	if tb.AllowN(1) {
		t.Error("AllowN(1) should return false when exhausted")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	// 100 tokens per second for faster testing
	tb := NewTokenBucket(100, 10)

	// Exhaust all tokens
	for i := 0; i < 10; i++ {
		tb.Allow()
	}

	// Wait for refill (100ms = 10 tokens at 100/s)
	time.Sleep(110 * time.Millisecond)

	// Should have refilled
	if !tb.Allow() {
		t.Error("Allow() should return true after refill")
	}
}

func TestTokenBucket_Wait(t *testing.T) {
	// 100 tokens per second
	tb := NewTokenBucket(100, 5)

	// Exhaust tokens
	for i := 0; i < 5; i++ {
		tb.Allow()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := tb.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Wait() error = %v", err)
	}

	// Should have waited ~10ms for 1 token at 100/s
	if elapsed < 10*time.Millisecond {
		t.Errorf("Wait() took %v, expected at least 10ms", elapsed)
	}
}

func TestTokenBucket_Wait_ContextCancelled(t *testing.T) {
	tb := NewTokenBucket(0.1, 1) // Very slow refill
	tb.Allow()                   // Exhaust

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := tb.Wait(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Wait() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestTokenBucket_Reserve(t *testing.T) {
	tb := NewTokenBucket(10, 10)

	// Immediate reservation
	wait := tb.Reserve()
	if wait != 0 {
		t.Errorf("Reserve() = %v, want 0 for immediate", wait)
	}

	// Exhaust remaining
	for i := 0; i < 9; i++ {
		tb.Reserve()
	}

	// Should return wait time
	wait = tb.Reserve()
	if wait <= 0 {
		t.Errorf("Reserve() = %v, want positive wait time", wait)
	}
}

func TestTokenBucket_Tokens(t *testing.T) {
	tb := NewTokenBucket(10, 10)

	if tokens := tb.Tokens(); tokens != 10 {
		t.Errorf("Tokens() = %v, want 10", tokens)
	}

	tb.Allow()

	if tokens := tb.Tokens(); tokens != 9 {
		t.Errorf("Tokens() = %v, want 9 after one Allow", tokens)
	}
}

func TestTokenBucket_SetRate(t *testing.T) {
	tb := NewTokenBucket(10, 10)
	tb.SetRate(20)

	if rate := tb.Rate(); rate != 20 {
		t.Errorf("Rate() = %v, want 20", rate)
	}
}

func TestTokenBucket_Reset(t *testing.T) {
	tb := NewTokenBucket(10, 10)

	// Exhaust
	for i := 0; i < 10; i++ {
		tb.Allow()
	}

	tb.Reset()

	if tokens := tb.Tokens(); tokens != 10 {
		t.Errorf("Tokens() after Reset() = %v, want 10", tokens)
	}
}

func TestTokenBucket_Concurrent(t *testing.T) {
	tb := NewTokenBucket(1000, 100)

	var wg sync.WaitGroup
	allowed := make(chan bool, 1000)

	// Spawn 100 goroutines each trying to get 10 tokens
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if tb.Allow() {
					allowed <- true
				}
			}
		}()
	}

	wg.Wait()
	close(allowed)

	// Count allowed requests (should not exceed capacity much)
	count := 0
	for range allowed {
		count++
	}

	// Initial capacity is 100, plus some refill during execution
	if count > 200 {
		t.Errorf("Too many requests allowed: %d (expected ~100-200)", count)
	}
}

func TestPerSecond(t *testing.T) {
	tb := PerSecond(10)
	if tb.Rate() != 10 {
		t.Errorf("Rate() = %v, want 10", tb.Rate())
	}
	if tb.Capacity() != 10 {
		t.Errorf("Capacity() = %v, want 10", tb.Capacity())
	}
}

func TestPerMinute(t *testing.T) {
	tb := PerMinute(60)
	if tb.Rate() != 1 {
		t.Errorf("Rate() = %v, want 1", tb.Rate())
	}
	if tb.Capacity() != 60 {
		t.Errorf("Capacity() = %v, want 60", tb.Capacity())
	}
}

func TestPerHour(t *testing.T) {
	tb := PerHour(3600)
	if tb.Rate() != 1 {
		t.Errorf("Rate() = %v, want 1", tb.Rate())
	}
	if tb.Capacity() != 3600 {
		t.Errorf("Capacity() = %v, want 3600", tb.Capacity())
	}
}

func TestMultiLimiter(t *testing.T) {
	l1 := NewTokenBucket(10, 5)
	l2 := NewTokenBucket(10, 3)

	ml := NewMultiLimiter(l1, l2)

	// Should allow up to minimum capacity (3)
	for i := 0; i < 3; i++ {
		if !ml.Allow() {
			t.Errorf("Allow() should return true for request %d", i)
		}
	}

	// l2 is now exhausted
	if ml.Allow() {
		t.Error("Allow() should return false when any limiter exhausted")
	}
}

func BenchmarkTokenBucket_Allow(b *testing.B) {
	tb := NewTokenBucket(float64(b.N), float64(b.N))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Allow()
	}
}

func BenchmarkTokenBucket_AllowConcurrent(b *testing.B) {
	tb := NewTokenBucket(float64(b.N), float64(b.N))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tb.Allow()
		}
	})
}
