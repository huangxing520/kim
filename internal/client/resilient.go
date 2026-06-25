package client

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/metrics"
)

// InvokeFunc 表示一次 RPC 调用
type InvokeFunc func(ctx context.Context) (interface{}, error)

// ResilientClient 包装重试 + fallback 逻辑（断路器/限流器由拦截器处理）
type ResilientClient struct {
	serviceName string
	cfg         config.ResilienceConfig
}

// NewResilientClient 创建弹性客户端
func NewResilientClient(serviceName string, cfg config.ResilienceConfig) *ResilientClient {
	return &ResilientClient{
		serviceName: serviceName,
		cfg:         cfg,
	}
}

// Call 执行带重试的调用
func (c *ResilientClient) Call(ctx context.Context, method string, invoke InvokeFunc) (interface{}, error) {
	if !c.cfg.Retry.Enable {
		return invoke(ctx)
	}

	maxAttempts := c.cfg.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// 检查 context 是否已取消
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		result, err := invoke(ctx)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// 不可重试的错误：context 取消/超时
		if isNoRetryError(err) {
			return nil, err
		}

		// 最后一次尝试不再退避
		if attempt == maxAttempts {
			break
		}

		// 计算退避时间
		backoff := c.calculateBackoff(attempt)
		metrics.GRPCRetryTotal.WithLabelValues(c.serviceName, method, "retry").Inc()
		logger.CommonLogger.Debugf("retry %s attempt %d/%d after %v: %v", method, attempt, maxAttempts, backoff, err)

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
	return fallback(ctx)
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

// isNoRetryError 判断是否不可重试的错误
func isNoRetryError(err error) bool {
	if err == nil {
		return false
	}
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	return false
}
