package server

import (
	"net"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// GRPCServer 封装 gRPC server，提供统一的拦截器、健康检查、反射
type GRPCServer struct {
	*grpc.Server
	addr string
}

// Option 函数式选项
type Option func(*options)

type options struct {
	serviceName string
	limiter     config.LimiterConfig
}

// WithServiceName 设置服务名（用于日志和指标）
func WithServiceName(name string) Option {
	return func(o *options) { o.serviceName = name }
}

// WithLimiter 设置限流配置
func WithLimiter(cfg config.LimiterConfig) Option {
	return func(o *options) { o.limiter = cfg }
}

// NewGRPCServer 创建 gRPC server
func NewGRPCServer(addr string, opts ...Option) (*GRPCServer, error) {
	o := &options{
		limiter: config.DefaultResilienceConfig().Limiter,
	}
	for _, opt := range opts {
		opt(o)
	}

	// 构建拦截器链：recovery → logging → metrics → limiter
	chain := UnaryChain(
		RecoveryInterceptor,
		LoggingInterceptor(o.serviceName),
		MetricsInterceptor(o.serviceName),
		LimiterInterceptor(o.serviceName, o.limiter),
	)

	s := grpc.NewServer(
		grpc.UnaryInterceptor(grpc.UnaryServerInterceptor(chain)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
	)

	// gRPC Health Protocol
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s, hs)

	// 反射（grpcurl 调试用）
	reflection.Register(s)

	return &GRPCServer{Server: s, addr: addr}, nil
}

// Start 启动 gRPC server（阻塞）
func (s *GRPCServer) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	return s.Serve(lis)
}
