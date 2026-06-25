package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	serverLimiterOnce  sync.Once
	serverLimiterRules sync.Map
)

func InitServerLimiter() {
	serverLimiterOnce.Do(func() {})
}

func LimiterInterceptor(serviceName string, cfg config.LimiterConfig) UnaryInterceptor {
	if !cfg.Enable {
		return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return handler(ctx, req)
		}
	}
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		mtd := methodShort(info.FullMethod)
		resource := fmt.Sprintf("%s:%s", serviceName, mtd)

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
				logger.CommonLogger.Errorf("load rate limit rule for %s failed: %v", resource, err)
			}
		}

		entry, blockErr := sentinel.Entry(resource)
		if blockErr != nil {
			metrics.GRPCRateLimitRejected.WithLabelValues("server", serviceName, mtd).Inc()
			return nil, status.Errorf(codes.ResourceExhausted, "rate limited (server): %s", blockErr.BlockMsg())
		}
		defer entry.Exit()

		return handler(ctx, req)
	}
}

func methodShort(method string) string {
	idx := strings.LastIndex(method, "/")
	if idx < 0 {
		return method
	}
	return method[idx+1:]
}
