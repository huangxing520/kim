package client

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newTestPool 构造测试用 Pool（同包可访问私有字段，conn 填 nil，测试 InvokeFunc 忽略 conn）
func newTestPool(ids ...string) *Pool {
	p := &Pool{
		serviceName: "test",
		conns:       make(map[string]*grpc.ClientConn),
		rr:          newRoundRobin(),
		cfg:         config.DefaultResilienceConfig(),
	}
	for _, id := range ids {
		p.conns[id] = nil
	}
	return p
}

// fakeInvoker 模拟 gRPC 调用，可控制返回值和延迟
type fakeInvoker struct {
	calls     int32
	err       error
	latency   time.Duration
	failFirst int32 // 前 N 次失败
}

func (f *fakeInvoker) invoke(ctx context.Context, conn *grpc.ClientConn, method string) (interface{}, error) {
	n := atomic.AddInt32(&f.calls, 1)
	if int32(n) <= f.failFirst {
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

func (f *fakeInvoker) callCount() int {
	return int(atomic.LoadInt32(&f.calls))
}

func TestResilientClient_Success(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false

	pool := newTestPool("inst-1")
	rc := NewResilientClient(pool, "test", cfg)
	fi := &fakeInvoker{err: nil}

	result, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		return fi.invoke(ctx, conn, "TestMethod")
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %v", result)
	}
	if fi.callCount() != 1 {
		t.Errorf("expected 1 call, got %d", fi.callCount())
	}
}

func TestResilientClient_RetryOnFailure(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.MaxAttempts = 3

	pool := newTestPool("inst-1", "inst-2", "inst-3")
	rc := NewResilientClient(pool, "test", cfg)
	fi := &fakeInvoker{err: errors.New("transient"), failFirst: 2}

	result, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		return fi.invoke(ctx, conn, "TestMethod")
	})

	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %v", result)
	}
	if fi.callCount() != 3 {
		t.Errorf("expected 3 calls (2 fail + 1 success), got %d", fi.callCount())
	}
}

func TestResilientClient_ExhaustedRetries(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.MaxAttempts = 2

	pool := newTestPool("inst-1", "inst-2")
	rc := NewResilientClient(pool, "test", cfg)
	fi := &fakeInvoker{err: errors.New("permanent"), failFirst: 999}

	_, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		return fi.invoke(ctx, conn, "TestMethod")
	})

	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	if fi.callCount() != 2 {
		t.Errorf("expected 2 calls (max attempts), got %d", fi.callCount())
	}
}

func TestResilientClient_NoRetryOnContextCancel(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.MaxAttempts = 3

	pool := newTestPool("inst-1")
	rc := NewResilientClient(pool, "test", cfg)
	fi := &fakeInvoker{err: context.Canceled, failFirst: 999}

	_, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		return fi.invoke(ctx, conn, "TestMethod")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if fi.callCount() != 1 {
		t.Errorf("expected 1 call (no retry on context.Canceled), got %d", fi.callCount())
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

	pool := newTestPool("inst-1", "inst-2", "inst-3")
	rc := NewResilientClient(pool, "test", cfg)

	start := time.Now()
	fi := &fakeInvoker{err: errors.New("fail"), failFirst: 2}
	_, _ = rc.Call(context.Background(), "TestMethod", func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		return fi.invoke(ctx, conn, "TestMethod")
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

	pool := newTestPool("inst-1")
	rc := NewResilientClient(pool, "test", cfg)
	fi := &fakeInvoker{err: errors.New("fail"), failFirst: 999}

	_, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		return fi.invoke(ctx, conn, "TestMethod")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if fi.callCount() != 1 {
		t.Errorf("expected 1 call (retry disabled), got %d", fi.callCount())
	}
}

func TestResilientClient_RespectsDeadline(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.MaxAttempts = 10
	cfg.Retry.InitialBackoff = "100ms"

	pool := newTestPool("inst-1", "inst-2")
	rc := NewResilientClient(pool, "test", cfg)
	fi := &fakeInvoker{err: errors.New("fail"), failFirst: 999}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := rc.Call(ctx, "TestMethod", func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		return fi.invoke(ctx, conn, "TestMethod")
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error due to deadline")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("expected to respect deadline (~50ms), got %v", elapsed)
	}
}

func TestResilientClient_Fallback(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.Enable = false // 禁用重试，测试 fallback

	pool := newTestPool("inst-1")
	rc := NewResilientClient(pool, "test", cfg)

	primaryErr := errors.New("primary failed")
	fallbackErr := errors.New("fallback failed")

	callCount := 0
	result, err := rc.CallWithFallback(context.Background(), "TestMethod",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			callCount++
			if callCount == 1 {
				return nil, primaryErr
			}
			return "fallback-ok", nil
		},
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
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
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			callCount++
			return nil, primaryErr
		},
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
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

// TestResilientClient_FallbackSwitchesInstance 验证重试时通过 GetAnyExcluding 切换实例
func TestResilientClient_FallbackSwitchesInstance(t *testing.T) {
	cfg := config.DefaultResilienceConfig()
	cfg.Breaker.Enable = false
	cfg.Limiter.Enable = false
	cfg.Retry.MaxAttempts = 2
	cfg.Retry.InitialBackoff = "1ms"

	pool := newTestPool("inst-A", "inst-B")
	rc := NewResilientClient(pool, "test", cfg)

	// 重试时应该通过 GetAnyExcluding 换实例。
	// 用一个 InvokeFunc 在第一次失败、第二次成功，验证调用了 2 次且最终成功。
	fi := &fakeInvoker{err: errors.New("fail"), failFirst: 1}

	_, err := rc.Call(context.Background(), "TestMethod", func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		return fi.invoke(ctx, conn, "TestMethod")
	})

	if err != nil {
		t.Fatalf("expected success after retry on different instance, got %v", err)
	}
	if fi.callCount() != 2 {
		t.Errorf("expected 2 calls, got %d", fi.callCount())
	}
}

func TestIsNoRetryError_ContextCanceled(t *testing.T) {
	assert.True(t, isNoRetryError(context.Canceled))
	assert.True(t, isNoRetryError(context.DeadlineExceeded))
}

func TestIsNoRetryError_GrpcStatusCodes(t *testing.T) {
	noRetryCodes := []codes.Code{
		codes.InvalidArgument,
		codes.PermissionDenied,
		codes.NotFound,
		codes.AlreadyExists,
		codes.FailedPrecondition,
		codes.OutOfRange,
		codes.Unimplemented,
		codes.Unauthenticated,
	}
	for _, c := range noRetryCodes {
		err := status.Error(c, "test error")
		assert.True(t, isNoRetryError(err), "code %s should not be retried", c)
	}
}

func TestIsNoRetryError_RetriableCodes(t *testing.T) {
	retryCodes := []codes.Code{
		codes.Unavailable,
		codes.DeadlineExceeded,
		codes.ResourceExhausted,
		codes.Internal,
	}
	for _, c := range retryCodes {
		err := status.Error(c, "test error")
		assert.False(t, isNoRetryError(err), "code %s should be retried", c)
	}
}

func TestIsNoRetryError_NilError(t *testing.T) {
	assert.False(t, isNoRetryError(nil))
}

func TestIsNoRetryError_GenericError(t *testing.T) {
	assert.False(t, isNoRetryError(errors.New("generic error")))
}
