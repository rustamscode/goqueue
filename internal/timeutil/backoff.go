// Package timeutil provides time-related utilities for GoQueue.
package timeutil

import (
	"math"
	"math/rand"
	"time"
)

// BackoffConfig holds configuration for exponential backoff calculation.
type BackoffConfig struct {
	// BaseDelay is the initial delay for the first retry.
	BaseDelay time.Duration
	// MaxDelay is the maximum delay cap.
	MaxDelay time.Duration
	// Multiplier is the factor by which delay increases (default: 2).
	Multiplier float64
	// JitterFactor is the maximum jitter as a fraction of delay (default: 0.5).
	JitterFactor float64
}

// DefaultBackoffConfig returns sensible defaults for backoff configuration.
func DefaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		BaseDelay:    1 * time.Second,
		MaxDelay:     1 * time.Hour,
		Multiplier:   2.0,
		JitterFactor: 0.5,
	}
}

// Calculate computes the backoff delay for the given attempt number.
// Attempt numbers start at 0 (first retry = attempt 0).
// The formula is: min(base * multiplier^attempt + jitter, maxDelay)
// where jitter is a random value in [0, jitterFactor * calculatedDelay].
func (c BackoffConfig) Calculate(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	multiplier := c.Multiplier
	if multiplier <= 0 {
		multiplier = 2.0
	}

	// Calculate exponential delay: base * multiplier^attempt
	delayFloat := float64(c.BaseDelay) * math.Pow(multiplier, float64(attempt))

	// Apply max delay cap before jitter
	maxDelayFloat := float64(c.MaxDelay)
	if delayFloat > maxDelayFloat {
		delayFloat = maxDelayFloat
	}

	// Add jitter: random value in [0, jitterFactor * delay]
	jitterFactor := c.JitterFactor
	if jitterFactor < 0 {
		jitterFactor = 0
	}
	if jitterFactor > 0 {
		jitter := rand.Float64() * jitterFactor * delayFloat
		delayFloat += jitter
	}

	// Final cap after jitter
	if delayFloat > maxDelayFloat {
		delayFloat = maxDelayFloat
	}

	return time.Duration(delayFloat)
}

// Backoff is a convenience function that calculates backoff with default config.
func Backoff(attempt int) time.Duration {
	return DefaultBackoffConfig().Calculate(attempt)
}

// BackoffWithConfig calculates backoff delay with custom configuration.
func BackoffWithConfig(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	cfg := BackoffConfig{
		BaseDelay:    baseDelay,
		MaxDelay:     maxDelay,
		Multiplier:   2.0,
		JitterFactor: 0.5,
	}
	return cfg.Calculate(attempt)
}

// RetryAt calculates the absolute time when a retry should be attempted.
func RetryAt(attempt int, cfg BackoffConfig) time.Time {
	return time.Now().Add(cfg.Calculate(attempt))
}

// ParseInterval parses a simple interval string and returns its duration.
// Supported formats:
//   - @every <duration>  e.g., "@every 5m", "@every 1h30m"
//   - @daily             runs once per day at midnight UTC
//   - @hourly            runs once per hour at minute 0
//   - @midnight          alias for @daily
//
// Returns 0 and false if the format is not recognized.
func ParseInterval(spec string) (time.Duration, bool) {
	switch spec {
	case "@daily", "@midnight":
		return 24 * time.Hour, true
	case "@hourly":
		return 1 * time.Hour, true
	}

	// Check for @every format
	if len(spec) > 7 && spec[:7] == "@every " {
		durationStr := spec[7:]
		d, err := time.ParseDuration(durationStr)
		if err != nil {
			return 0, false
		}
		return d, true
	}

	return 0, false
}
