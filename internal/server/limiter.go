package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/metrics"
	"google.golang.org/grpc"
)

var (
	serverLimiterOnce sync.Once
	serverLimiterRules sync.Map // resource -> bool
)

// InitServerLimiter 初始化服务端限流（进程级一次，确保 Sentinel 已初始化）
func InitServerLimiter() {
	serverLimiterOnce.Do(func() {
		// Sentinel 核心初始化由 client.InitSentinel 负责
		// 这里只确保服务端限流器可用
	})
}

// LimiterInterceptor 服务端限流拦截器
// resource = <serviceName>:<method>
func LimiterInterceptor(serviceName string, cfg config.LimiterConfig) UnaryInterceptor {
	if !cfg.Enable {
		return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return handler(ctx, req)
		}
	}
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		mtd := methodShort(info.FullMethod)
		resource := fmt.Sprintf("%s:%s", serviceName, mtd)

		// 注册限流规则（幂等）
		if _, loaded := serverLimiterRules.LoadOrStore(resource, true); !loaded {
			_, err := flow.LoadRules([]*flow.Rule{
				{
					Resource:               resource,
					Threshold:              cfg.ServerQPS,
					TokenCalculateStrategy: flow.Direct,
					ControlBehavior:        flow.Reject,
				},
			})
			if err != nil {
				// 限流规则加载失败不阻塞服务，仅记录日志
			}
		}

		entry, blockErr := sentinel.Entry(resource)
		if blockErr != nil {
			metrics.GRPCRateLimitRejected.WithLabelValues("server", serviceName, mtd).Inc()
			return nil, fmt.Errorf("rate limited (server): %s", blockErr.BlockMsg())
		}
		defer entry.Exit()

		return handler(ctx, req)
	}
}

// methodShort 从完整 method 路径提取短名
func methodShort(method string) string {
	idx := strings.LastIndex(method, "/")
	if idx < 0 {
		return method
	}
	return method[idx+1:]
}
