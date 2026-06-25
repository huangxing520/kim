package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/logger"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

type GRPCServer struct {
	*grpc.Server
	addr string
	hs   *health.Server
}

type Option func(*options)

type options struct {
	serviceName string
	limiter     config.LimiterConfig
	grpcCfg     config.GRPCConfig
	authSecret  string
}

func WithServiceName(name string) Option {
	return func(o *options) { o.serviceName = name }
}

func WithLimiter(cfg config.LimiterConfig) Option {
	return func(o *options) { o.limiter = cfg }
}

func WithGRPCConfig(cfg config.GRPCConfig) Option {
	return func(o *options) { o.grpcCfg = cfg }
}

func WithAuthSecret(secret string) Option {
	return func(o *options) { o.authSecret = secret }
}

func NewGRPCServer(addr string, opts ...Option) (*GRPCServer, error) {
	o := &options{
		limiter: config.DefaultResilienceConfig().Limiter,
		grpcCfg: config.DefaultGRPCConfig(),
	}
	for _, opt := range opts {
		opt(o)
	}

	interceptors := []UnaryInterceptor{
		RecoveryInterceptor,
		UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		LoggingInterceptor(o.serviceName),
		MetricsInterceptor(o.serviceName),
		LimiterInterceptor(o.serviceName, o.limiter),
	}
	if o.grpcCfg.AuthEnable && o.authSecret != "" {
		interceptors = append(interceptors, AuthInterceptor(o.authSecret))
	}
	chain := UnaryChain(interceptors...)

	grpcOpts := []grpc.ServerOption{
		grpc.UnaryInterceptor(grpc.UnaryServerInterceptor(chain)),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
	}

	if o.grpcCfg.TLSEnable {
		tlsCreds, err := loadTLSCredentials(o.grpcCfg)
		if err != nil {
			return nil, fmt.Errorf("load TLS credentials: %w", err)
		}
		grpcOpts = append(grpcOpts, grpc.Creds(tlsCreds))
	}

	s := grpc.NewServer(grpcOpts...)

	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s, hs)

	if o.grpcCfg.Reflection {
		reflection.Register(s)
		logger.CommonLogger.Infof("gRPC reflection enabled for %s", o.serviceName)
	}

	return &GRPCServer{Server: s, addr: addr, hs: hs}, nil
}

func loadTLSCredentials(cfg config.GRPCConfig) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if cfg.TLSCAFile != "" {
		caPEM, err := os.ReadFile(cfg.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsCfg.ClientCAs = certPool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return credentials.NewTLS(tlsCfg), nil
}

func (s *GRPCServer) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	return s.Serve(lis)
}

func (s *GRPCServer) HealthServer() *health.Server {
	return s.hs
}
