package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/metrics"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// InterceptorChain 构造客户端拦截器链：trace → timeout → resilience(breaker+limiter)
// 顺序：trace 最外层（让 timeout/breaker 内部错误也落入 span），timeout 包裹整个调用，resilience 内部一次 Entry 同时处理断路器和限流
func InterceptorChain(serviceName, instanceID string, cfg config.ResilienceConfig) []grpc.UnaryClientInterceptor {
	return []grpc.UnaryClientInterceptor{
		otelgrpc.UnaryClientInterceptor(),
		timeoutInterceptor(cfg.Timeout),
		resilienceInterceptor(serviceName, instanceID, cfg.Breaker, cfg.Limiter),
	}
}

// timeoutInterceptor 为每次 RPC 设置默认超时（若 ctx 未设置 deadline）
func timeoutInterceptor(cfg config.TimeoutConfig) grpc.UnaryClientInterceptor {
	if !cfg.Enable {
		return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
	}
	defaultTimeout := cfg.DefaultDuration()
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if _, ok := ctx.Deadline(); ok {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// resilienceInterceptor 合并断路器 + 限流器，一次 Entry 同时检查
// resource = <svc>:<inst>:<method>
func resilienceInterceptor(serviceName, instanceID string, breakerCfg config.BreakerConfig, limiterCfg config.LimiterConfig) grpc.UnaryClientInterceptor {
	if !breakerCfg.Enable && !limiterCfg.Enable {
		return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
	}
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		mtd := methodShort(method)
		resource := fmt.Sprintf("%s:%s:%s", serviceName, instanceID, mtd)

		// 注册规则（幂等）
		ensureBreaker(resource, breakerCfg)
		if limiterCfg.Enable {
			ensureLimiter(resource, limiterCfg.ClientQPS)
		}

		// 一次 Entry 同时经过断路器 slot 和限流器 slot
		entry, blockErr := sentinel.Entry(resource)
		if blockErr != nil {
			if limiterCfg.Enable {
				metrics.GRPCRateLimitRejected.WithLabelValues("client", serviceName, mtd).Inc()
			}
			return status.Errorf(codes.Unavailable, "resilience blocked: %s", blockErr.BlockMsg())
		}
		defer entry.Exit()

		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			sentinel.TraceError(entry, err)
		}
		return err
	}
}

// methodShort 从完整 method 路径提取短名
// /royal.LogicService/InsertUserMessage -> InsertUserMessage
func methodShort(method string) string {
	idx := strings.LastIndex(method, "/")
	if idx < 0 {
		return method
	}
	return method[idx+1:]
}

// 静态断言：确保 time 包被使用（timeoutInterceptor 用到）
var _ = time.Second
