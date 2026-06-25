package client

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// InvokeFunc 表示一次 RPC 调用，由 ResilientClient 注入 conn（支持重试换实例）
type InvokeFunc func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error)

// ResilientClient 包装重试 + fallback 逻辑（断路器/限流器由拦截器处理）
type ResilientClient struct {
	pool        *Pool
	serviceName string
	cfg         config.ResilienceConfig
}

// NewResilientClient 创建弹性客户端
func NewResilientClient(pool *Pool, serviceName string, cfg config.ResilienceConfig) *ResilientClient {
	return &ResilientClient{
		pool:        pool,
		serviceName: serviceName,
		cfg:         cfg,
	}
}

// Call 执行带重试的调用，重试时通过 GetAnyExcluding 切换实例
func (c *ResilientClient) Call(ctx context.Context, method string, invoke InvokeFunc) (interface{}, error) {
	if !c.cfg.Retry.Enable {
		instanceID, conn, err := c.pool.GetAnyWithID()
		if err != nil {
			return nil, err
		}
		_ = instanceID
		return invoke(ctx, conn)
	}

	maxAttempts := c.cfg.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	prevID := ""
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// 检查 context 是否已取消
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// 获取连接：首次 GetAny，重试时 GetAnyExcluding 换实例
		var conn *grpc.ClientConn
		var instanceID string
		var err error
		if attempt == 1 || prevID == "" {
			instanceID, conn, err = c.pool.GetAnyWithID()
		} else {
			conn, err = c.pool.GetAnyExcluding(prevID)
			// 若无其他实例可换，回退到 GetAny（复用同实例）
			if err != nil {
				instanceID, conn, err = c.pool.GetAnyWithID()
			}
		}
		if err != nil {
			return nil, err
		}
		prevID = instanceID

		result, invokeErr := invoke(ctx, conn)
		if invokeErr == nil {
			return result, nil
		}

		lastErr = invokeErr

		// 不可重试的错误：context 取消/超时
		if isNoRetryError(invokeErr) {
			return nil, invokeErr
		}

		// 最后一次尝试不再退避
		if attempt == maxAttempts {
			break
		}

		// 计算退避时间
		backoff := c.calculateBackoff(attempt)
		metrics.GRPCRetryTotal.WithLabelValues(c.serviceName, method, "retry").Inc()
		logger.CommonLogger.Debugf("retry %s attempt %d/%d after %v: %v", method, attempt, maxAttempts, backoff, invokeErr)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, lastErr
}

// CallWithFallback 执行主调用，失败时执行 fallback
func (c *ResilientClient) CallWithFallback(ctx context.Context, method string, primary, fallback InvokeFunc) (interface{}, error) {
	result, err := c.Call(ctx, method, primary)
	if err == nil {
		return result, nil
	}

	logger.CommonLogger.Warnf("primary call %s failed, invoking fallback: %v", method, err)
	// fallback 自行获取连接
	instanceID, conn, ferr := c.pool.GetAnyWithID()
	if ferr != nil {
		return nil, ferr
	}
	_ = instanceID
	return fallback(ctx, conn)
}

// calculateBackoff 计算指数退避 + 抖动
func (c *ResilientClient) calculateBackoff(attempt int) time.Duration {
	initial := c.cfg.Retry.InitialBackoffDuration()
	maxBackoff := c.cfg.Retry.MaxBackoffDuration()
	multiplier := c.cfg.Retry.Multiplier
	if multiplier <= 0 {
		multiplier = 2.0
	}

	// backoff = initial * multiplier^(attempt-1)
	backoff := time.Duration(float64(initial) * math.Pow(multiplier, float64(attempt-1)))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	// 添加抖动：[0, jitter * backoff]
	jitter := c.cfg.Retry.Jitter
	if jitter > 0 {
		jitterDuration := time.Duration(float64(backoff) * jitter * rand.Float64())
		backoff += jitterDuration
	}

	return backoff
}

func isNoRetryError(err error) bool {
	if err == nil {
		return false
	}
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.InvalidArgument,
			codes.PermissionDenied,
			codes.NotFound,
			codes.AlreadyExists,
			codes.FailedPrecondition,
			codes.OutOfRange,
			codes.Unimplemented,
			codes.Unauthenticated:
			return true
		}
	}
	return false
}
