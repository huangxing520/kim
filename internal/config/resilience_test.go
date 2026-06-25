package config

import (
	"testing"
	"time"
)

func TestDefaultResilienceConfig(t *testing.T) {
	cfg := DefaultResilienceConfig()

	// Breaker
	if !cfg.Breaker.Enable {
		t.Error("Breaker.Enable should default to true")
	}
	if cfg.Breaker.Strategy != "both" {
		t.Errorf("Breaker.Strategy = %q, want %q", cfg.Breaker.Strategy, "both")
	}
	if cfg.Breaker.Threshold != 0.5 {
		t.Errorf("Breaker.Threshold = %v, want 0.5", cfg.Breaker.Threshold)
	}
	if cfg.Breaker.MinRequestAmount != 10 {
		t.Errorf("Breaker.MinRequestAmount = %v, want 10", cfg.Breaker.MinRequestAmount)
	}

	// Retry
	if !cfg.Retry.Enable {
		t.Error("Retry.Enable should default to true")
	}
	if cfg.Retry.MaxAttempts != 3 {
		t.Errorf("Retry.MaxAttempts = %v, want 3", cfg.Retry.MaxAttempts)
	}
	if cfg.Retry.Multiplier != 2.0 {
		t.Errorf("Retry.Multiplier = %v, want 2.0", cfg.Retry.Multiplier)
	}

	// Timeout
	if !cfg.Timeout.Enable {
		t.Error("Timeout.Enable should default to true")
	}

	// Limiter
	if !cfg.Limiter.Enable {
		t.Error("Limiter.Enable should default to true")
	}
	if cfg.Limiter.ClientQPS != 100 {
		t.Errorf("Limiter.ClientQPS = %v, want 100", cfg.Limiter.ClientQPS)
	}
	if cfg.Limiter.ServerQPS != 200 {
		t.Errorf("Limiter.ServerQPS = %v, want 200", cfg.Limiter.ServerQPS)
	}
}

func TestBreakerConfigSlowCallRTTDuration(t *testing.T) {
	cfg := DefaultResilienceConfig()
	got := cfg.Breaker.SlowCallRTTDuration()
	want := 500 * time.Millisecond
	if got != want {
		t.Errorf("SlowCallRTTDuration() = %v, want %v", got, want)
	}
}

func TestRetryConfigBackoffDurations(t *testing.T) {
	cfg := DefaultResilienceConfig()
	if cfg.Retry.InitialBackoffDuration() != 50*time.Millisecond {
		t.Errorf("InitialBackoffDuration = %v, want 50ms", cfg.Retry.InitialBackoffDuration())
	}
	if cfg.Retry.MaxBackoffDuration() != 500*time.Millisecond {
		t.Errorf("MaxBackoffDuration = %v, want 500ms", cfg.Retry.MaxBackoffDuration())
	}
}

func TestTimeoutConfigDefaultDuration(t *testing.T) {
	cfg := DefaultResilienceConfig()
	if cfg.Timeout.DefaultDuration() != 3*time.Second {
		t.Errorf("DefaultDuration = %v, want 3s", cfg.Timeout.DefaultDuration())
	}
}
