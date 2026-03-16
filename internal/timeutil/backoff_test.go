package timeutil

import (
	"testing"
	"time"
)

func TestBackoffConfig_Calculate(t *testing.T) {
	tests := []struct {
		name     string
		config   BackoffConfig
		attempt  int
		wantMin  time.Duration
		wantMax  time.Duration
	}{
		{
			name:    "first attempt with defaults",
			config:  DefaultBackoffConfig(),
			attempt: 0,
			wantMin: 1 * time.Second,
			wantMax: 2 * time.Second, // Base + max jitter
		},
		{
			name:    "second attempt",
			config:  DefaultBackoffConfig(),
			attempt: 1,
			wantMin: 2 * time.Second,
			wantMax: 4 * time.Second,
		},
		{
			name:    "third attempt",
			config:  DefaultBackoffConfig(),
			attempt: 2,
			wantMin: 4 * time.Second,
			wantMax: 8 * time.Second,
		},
		{
			name: "respects max delay",
			config: BackoffConfig{
				BaseDelay:    1 * time.Second,
				MaxDelay:     5 * time.Second,
				Multiplier:   2.0,
				JitterFactor: 0,
			},
			attempt: 10, // Would be 1024s without cap
			wantMin: 5 * time.Second,
			wantMax: 5 * time.Second,
		},
		{
			name: "no jitter",
			config: BackoffConfig{
				BaseDelay:    1 * time.Second,
				MaxDelay:     1 * time.Hour,
				Multiplier:   2.0,
				JitterFactor: 0,
			},
			attempt: 2,
			wantMin: 4 * time.Second,
			wantMax: 4 * time.Second,
		},
		{
			name:    "negative attempt treated as 0",
			config:  DefaultBackoffConfig(),
			attempt: -5,
			wantMin: 1 * time.Second,
			wantMax: 2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run multiple times to account for jitter
			for i := 0; i < 100; i++ {
				got := tt.config.Calculate(tt.attempt)
				if got < tt.wantMin || got > tt.wantMax {
					t.Errorf("Calculate(%d) = %v, want between %v and %v",
						tt.attempt, got, tt.wantMin, tt.wantMax)
				}
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	// Test the convenience function
	delay := Backoff(0)
	if delay < 1*time.Second || delay > 2*time.Second {
		t.Errorf("Backoff(0) = %v, want between 1s and 2s", delay)
	}
}

func TestBackoffWithConfig(t *testing.T) {
	delay := BackoffWithConfig(0, 500*time.Millisecond, 10*time.Second)
	if delay < 500*time.Millisecond || delay > 1*time.Second {
		t.Errorf("BackoffWithConfig(0) = %v, want between 500ms and 1s", delay)
	}
}

func TestParseInterval(t *testing.T) {
	tests := []struct {
		spec      string
		wantDur   time.Duration
		wantValid bool
	}{
		{"@every 5m", 5 * time.Minute, true},
		{"@every 1h30m", 90 * time.Minute, true},
		{"@every 100ms", 100 * time.Millisecond, true},
		{"@daily", 24 * time.Hour, true},
		{"@midnight", 24 * time.Hour, true},
		{"@hourly", 1 * time.Hour, true},
		{"invalid", 0, false},
		{"@every invalid", 0, false},
		{"@weekly", 0, false}, // Not supported
		{"", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			dur, valid := ParseInterval(tt.spec)
			if valid != tt.wantValid {
				t.Errorf("ParseInterval(%q) valid = %v, want %v", tt.spec, valid, tt.wantValid)
			}
			if valid && dur != tt.wantDur {
				t.Errorf("ParseInterval(%q) = %v, want %v", tt.spec, dur, tt.wantDur)
			}
		})
	}
}

func BenchmarkBackoffCalculate(b *testing.B) {
	cfg := DefaultBackoffConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg.Calculate(i % 10)
	}
}
