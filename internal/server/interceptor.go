package server

import (
	"context"
	"runtime/debug"
	"time"

	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryInterceptor 单个一元拦截器
type UnaryInterceptor func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error)

// UnaryChain 组合多个一元拦截器为一个链
func UnaryChain(interceptors ...UnaryInterceptor) UnaryInterceptor {
	n := len(interceptors)
	if n == 0 {
		return nil
	}
	if n == 1 {
		return interceptors[0]
	}
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		chain := handler
		for i := n - 1; i >= 0; i-- {
			chain = buildInterceptor(interceptors[i], info, chain)
		}
		return chain(ctx, req)
	}
}

func buildInterceptor(interceptor UnaryInterceptor, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) grpc.UnaryHandler {
	return func(ctx context.Context, req interface{}) (interface{}, error) {
		return interceptor(ctx, req, info, handler)
	}
}

// RecoveryInterceptor 捕获 panic
func RecoveryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			logger.CommonLogger.Errorf("gRPC panic in %s: %v\n%s", info.FullMethod, r, stack)
			err = status.Errorf(codes.Internal, "internal server error")
		}
	}()
	return handler(ctx, req)
}

// LoggingInterceptor 记录请求日志
func LoggingInterceptor(serviceName string) UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		code := status.Code(err)
		if err != nil {
			logger.CommonLogger.Infof("gRPC %s.%s %v %s", serviceName, info.FullMethod, code, duration)
		} else {
			logger.CommonLogger.Debugf("gRPC %s.%s %v %s", serviceName, info.FullMethod, code, duration)
		}
		return resp, err
	}
}

// MetricsInterceptor 采集 Prometheus 指标
func MetricsInterceptor(serviceName string) UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start).Seconds()

		code := status.Code(err).String()
		metrics.GRPCServerHandledTotal.WithLabelValues(serviceName, info.FullMethod, code).Inc()
		metrics.GRPCServerHandlingSeconds.WithLabelValues(serviceName, info.FullMethod).Observe(duration)

		return resp, err
	}
}
