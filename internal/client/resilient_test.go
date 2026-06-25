package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/klintcheng/kim/internal/config"
)

// fakeInvoker 模拟 gRPC 调用，可控制返回值和延迟
type fakeInvoker struct {
	calls     int
	err       error
	latency   time.Duration
	failFirst int // 前 N 次失败
}

func (f *fakeInvoker) invoke(ctx context.Context, method string) (interface{}, error) {
	f.calls++
	if f.calls <= f.failFirst {
		if f.latency > 0 {
			time.Sleep(f.latency)
		}
		return nil, f.err
	}
	if f.latency > 0 {
		time.Sleep(f.latency)
	}
	return "ok", nil
}

func TestResilientClient_Success(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false // 禁用断路器避免干扰重试测试
	cfg.Limiter.Enable = false

	rc := NewResilientClient("test", cfg)
	fi := &fakeInvoker{err: nil}

	result, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context) (interface{}, error) {
		return fi.invoke(ctx, "TestMethod")
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %v", result)
	}
	if fi.calls != 1 {
		t.Errorf("expected 1 call, got %d", fi.calls)
	}
}

func TestResilientClient_RetryOnFailure(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.MaxAttempts = 3

	rc := NewResilientClient("test", cfg)
	fi := &fakeInvoker{err: errors.New("transient"), failFirst: 2}

	result, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context) (interface{}, error) {
		return fi.invoke(ctx, "TestMethod")
	})

	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %v", result)
	}
	if fi.calls != 3 {
		t.Errorf("expected 3 calls (2 fail + 1 success), got %d", fi.calls)
	}
}

func TestResilientClient_ExhaustedRetries(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.MaxAttempts = 2

	rc := NewResilientClient("test", cfg)
	fi := &fakeInvoker{err: errors.New("permanent"), failFirst: 999}

	_, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context) (interface{}, error) {
		return fi.invoke(ctx, "TestMethod")
	})

	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	if fi.calls != 2 {
		t.Errorf("expected 2 calls (max attempts), got %d", fi.calls)
	}
}

func TestResilientClient_NoRetryOnContextCancel(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.MaxAttempts = 3

	rc := NewResilientClient("test", cfg)
	fi := &fakeInvoker{err: context.Canceled, failFirst: 999}

	_, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context) (interface{}, error) {
		return fi.invoke(ctx, "TestMethod")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if fi.calls != 1 {
		t.Errorf("expected 1 call (no retry on context.Canceled), got %d", fi.calls)
	}
}

func TestResilientClient_BackoffIncreases(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.MaxAttempts = 3
	cfg.Retry.InitialBackoff = "10ms"
	cfg.Retry.MaxBackoff = "100ms"
	cfg.Retry.Multiplier = 2.0

	rc := NewResilientClient("test", cfg)

	start := time.Now()
	fi := &fakeInvoker{err: errors.New("fail"), failFirst: 2}
	_, _ = rc.Call(context.Background(), "TestMethod", func(ctx context.Context) (interface{}, error) {
		return fi.invoke(ctx, "TestMethod")
	})
	elapsed := time.Since(start)

	// 2 次退避：10ms + 20ms = 30ms（最小）
	if elapsed < 25*time.Millisecond {
		t.Errorf("expected backoff delay >= 30ms, got %v", elapsed)
	}
}

func TestResilientClient_RetryDisabled(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.Enable = false

	rc := NewResilientClient("test", cfg)
	fi := &fakeInvoker{err: errors.New("fail"), failFirst: 999}

	_, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context) (interface{}, error) {
		return fi.invoke(ctx, "TestMethod")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if fi.calls != 1 {
		t.Errorf("expected 1 call (retry disabled), got %d", fi.calls)
	}
}

func TestResilientClient_RespectsDeadline(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.MaxAttempts = 10
	cfg.Retry.InitialBackoff = "100ms"

	rc := NewResilientClient("test", cfg)
	fi := &fakeInvoker{err: errors.New("fail"), failFirst: 999}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := rc.Call(ctx, "TestMethod", func(ctx context.Context) (interface{}, error) {
		return fi.invoke(ctx, "TestMethod")
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error due to deadline")
	}
	// 应该在 deadline 后很快返回，而不是重试完所有次数
	if elapsed > 200*time.Millisecond {
		t.Errorf("expected to respect deadline (~50ms), got %v", elapsed)
	}
}

func TestResilientClient_Fallback(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.Enable = false // 禁用重试，测试 fallback

	rc := NewResilientClient("test", cfg)

	primaryErr := errors.New("primary failed")
	fallbackErr := errors.New("fallback failed")

	callCount := 0
	result, err := rc.CallWithFallback(context.Background(), "TestMethod",
		func(ctx context.Context) (interface{}, error) {
			callCount++
			if callCount == 1 {
				return nil, primaryErr
			}
			return "fallback-ok", nil
		},
		func(ctx context.Context) (interface{}, error) {
			callCount++
			return "fallback-ok", nil
		},
	)

	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if result != "fallback-ok" {
		t.Errorf("expected 'fallback-ok', got %v", result)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (primary + fallback), got %d", callCount)
	}

	// 测试 fallback 也失败
	callCount = 0
	_, err = rc.CallWithFallback(context.Background(), "TestMethod",
		func(ctx context.Context) (interface{}, error) {
			callCount++
			return nil, primaryErr
		},
		func(ctx context.Context) (interface{}, error) {
			callCount++
			return nil, fallbackErr
		},
	)
	if err == nil {
		t.Fatal("expected error when both primary and fallback fail")
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}
