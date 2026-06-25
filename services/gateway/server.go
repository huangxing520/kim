// 文件：server.go
// 职责：Gateway 服务入口——组装 WS/TCP 接入层、gRPC server（接收 Comet Push）、gRPC client（转发到 Comet）。
//
// 定义的类型：
//   - Server 结构体：Gateway 服务实例（持有 config / wsSrv / grpcSrv / forwarder / naming）
//
// 方法：
//   - New(ctx, cfg, routePath, protocol) → 创建 Gateway 服务：初始化 logger/naming/selector/forwarder/handler →
//                                          创建 WS/TCP server → 创建 gRPC server → 注册 Consul
//   - (Server).Start(ctx)                → 启动 gRPC server（非阻塞）+ WS/TCP server（阻塞）
//   - (Server).Stop(ctx)                 → 反注册 Consul + 关闭 forwarder + GracefulStop + Shutdown

package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/naming"
	"github.com/klintcheng/kim/internal/server"
	"github.com/klintcheng/kim/services/gateway/serv"
	"github.com/klintcheng/kim/tcp"
	"github.com/klintcheng/kim/websocket"
	"github.com/klintcheng/kim/wire"
)

// Server Gateway 服务实例
type Server struct {
	config    *Config
	routePath string
	protocol  string
	wsSrv     kim.Server         // 客户端接入（WS/TCP）
	grpcSrv   *server.GRPCServer // 接收 Comet Push
	forwarder *CometForwarder    // gRPC client 调用 Comet
	naming    naming.Naming
}

// New 创建 Gateway 服务实例
func New(ctx context.Context, cfg *Config, routePath string, protocol string) (*Server, error) {
	// 1. 初始化 logger
	log, err := logger.Init(logger.Settings{
		Level:       cfg.LogLevel,
		Filename:    "./data/gateway.log",
		ServiceName: "gateway",
		Kafka:       cfg.Kafka,
	})
	if err != nil {
		return nil, err
	}
	logger.GatewayLogger = log.Sugar()
	defer log.Close()

	// 2. Consul naming
	ns, err := naming.NewNaming(cfg.ConsulURL)
	if err != nil {
		return nil, err
	}

	// 2.5 初始化 Sentinel（断路器 + 限流器）
	if err := client.InitSentinel(); err != nil {
		logger.GatewayLogger.Warnf("init sentinel (resilience disabled): %v", err)
	}

	// 3. 路由选择器
	selector, err := serv.NewRouteSelector(routePath)
	if err != nil {
		return nil, err
	}

	// 4. gRPC forwarder（调 Comet，挂载弹性拦截器）
	forwarder := NewCometForwarder(ns, selector, cfg.ServiceID, cfg.Resilience)

	// 5. WS/TCP 接入层
	handler := &serv.Handler{
		ServiceID: cfg.ServiceID,
		AppSecret: cfg.AppSecret,
		Forwarder: forwarder,
	}
	meta := map[string]string{
		"domain": cfg.Domain,
	}
	service := &naming.DefaultService{
		Id:       cfg.ServiceID,
		Name:     cfg.ServiceName,
		Address:  cfg.PublicAddress,
		Port:     cfg.PublicPort,
		Protocol: protocol,
		Tags:     cfg.Tags,
		Meta:     meta,
	}
	srvOpts := []kim.ServerOption{
		kim.WithConnectionGPool(cfg.ConnectionGPool),
		kim.WithMessageGPool(cfg.MessageGPool),
	}
	var wsSrv kim.Server
	if protocol == "tcp" {
		wsSrv = tcp.NewServer(cfg.Listen, service, srvOpts...)
	} else {
		wsSrv = websocket.NewServer(cfg.Listen, service, srvOpts...)
	}
	wsSrv.SetReadWait(time.Minute * 2)
	wsSrv.SetAcceptor(handler)
	wsSrv.SetMessageListener(handler)
	wsSrv.SetStateListener(handler)

	// 6. gRPC server（接收 Comet Push，挂载服务端限流）
	grpcSrv, err := server.NewGRPCServer(cfg.GRPCListen,
		server.WithServiceName("gateway"),
		server.WithLimiter(cfg.Resilience.Limiter),
	)
	if err != nil {
		return nil, err
	}
	pusher := NewPusher(wsSrv.Push)
	rpc.RegisterGatewayServiceServer(grpcSrv, pusher)

	// 7. Consul 注册（注册 gRPC 端口，Comet 通过 Consul 发现 Gateway 的 gRPC 地址来调用 Push）
	grpcService := &naming.DefaultService{
		Id:       cfg.ServiceID,
		Name:     wire.SNWGateway, // "wgateway"
		Address:  cfg.PublicAddress,
		Port:     cfg.GRPCPort,
		Protocol: "grpc",
		Tags:     cfg.Tags,
		Meta: map[string]string{
			naming.KeyHealthURL: fmt.Sprintf("http://%s:%d/health", cfg.PublicAddress, cfg.MonitorPort),
			"domain":            cfg.Domain,
		},
	}
	_ = ns.Register(grpcService)

	return &Server{
		config:    cfg,
		routePath: routePath,
		protocol:  protocol,
		wsSrv:     wsSrv,
		grpcSrv:   grpcSrv,
		forwarder: forwarder,
		naming:    ns,
	}, nil
}

// Start 启动 Gateway 服务
func (s *Server) Start(ctx context.Context) error {
	// 启动 gRPC server（非阻塞）
	go func() {
		if err := s.grpcSrv.Start(); err != nil {
			logger.GatewayLogger.Errorf("grpc server error: %v", err)
		}
	}()
	logger.GatewayLogger.Infof("gateway grpc listening on %s", s.config.GRPCListen)
	logger.GatewayLogger.Infof("gateway %s listening on %s", s.protocol, s.config.Listen)
	// 启动 WS/TCP server（阻塞）
	return s.wsSrv.Start()
}

// Stop 优雅关闭 Gateway 服务
func (s *Server) Stop(ctx context.Context) error {
	if s.naming != nil {
		_ = s.naming.Deregister(s.config.ServiceID)
	}
	s.forwarder.Close()
	s.grpcSrv.GracefulStop()
	return s.wsSrv.Shutdown(ctx)
}
