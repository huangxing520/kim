package server

import (
	"net"
	"sync/atomic"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

type GRPCServer struct {
	*grpc.Server
	addr         string
	HealthServer *health.Server
	ready        atomic.Bool
}

type Option func(*options)

type options struct {
	serviceName string
	limiter     config.LimiterConfig
}

func WithServiceName(name string) Option {
	return func(o *options) { o.serviceName = name }
}

func WithLimiter(cfg config.LimiterConfig) Option {
	return func(o *options) { o.limiter = cfg }
}

func NewGRPCServer(addr string, opts ...Option) (*GRPCServer, error) {
	o := &options{
		limiter: config.DefaultResilienceConfig().Limiter,
	}
	for _, opt := range opts {
		opt(o)
	}

	chain := UnaryChain(
		RecoveryInterceptor,
		UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		LoggingInterceptor(o.serviceName),
		MetricsInterceptor(o.serviceName),
		LimiterInterceptor(o.serviceName, o.limiter),
	)

	s := grpc.NewServer(
		grpc.UnaryInterceptor(grpc.UnaryServerInterceptor(chain)),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
	)

	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	healthpb.RegisterHealthServer(s, hs)

	reflection.Register(s)

	return &GRPCServer{Server: s, addr: addr, HealthServer: hs}, nil
}

func (s *GRPCServer) SetReady() {
	s.ready.Store(true)
	s.HealthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
}

func (s *GRPCServer) SetNotReady() {
	s.ready.Store(false)
	s.HealthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
}

func (s *GRPCServer) IsReady() bool {
	return s.ready.Load()
}

func (s *GRPCServer) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	return s.Serve(lis)
}
