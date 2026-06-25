// 文件：server.go
// 职责：Comet 服务入口——gRPC 服务器，组装 Router、Handler、Redis 缓存、gRPC 客户端池、Consul 服务注册。
//
// 定义的类型：
//   - Server 结构体：gRPC 服务实例（持有 config / grpcSrv / naming / logicPool / gwPool）
//
// 方法：
//   - New(ctx, cfg)       → 创建 Comet 服务：初始化 logger/Redis/Naming/ClientPool → 注册 Handler → 创建 gRPC Server → 注册 Consul
//   - (Server).Start(ctx) → 启动 gRPC 服务（阻塞）
//   - (Server).Stop(ctx)  → 反注册 Consul + 关闭连接池 + GracefulStop
//   - initRedis(addr)     → 初始化单机 Redis 客户端

package comet

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	kim "github.com/klintcheng/kim/internal/kim"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/naming"
	"github.com/klintcheng/kim/internal/server"
	"github.com/klintcheng/kim/internal/trace"
	"github.com/klintcheng/kim/middleware"
	"github.com/klintcheng/kim/services/comet/handler"
	"github.com/klintcheng/kim/services/comet/service"
	"github.com/klintcheng/kim/storage"
	"github.com/klintcheng/kim/wire"
)

// Server Comet gRPC 服务
type Server struct {
	config        *Config
	grpcSrv       *server.GRPCServer
	naming        naming.Naming
	logicPool     *client.Pool
	gwPool        *client.Pool
	log           *logger.Logger
	traceShutdown func()
}

// New 创建 Comet 服务实例
func New(ctx context.Context, cfg *Config) (*Server, error) {
	// 1. 初始化 logger
	log, err := logger.Init(logger.Settings{
		Level:       cfg.LogLevel,
		Filename:    "./data/comet.log",
		ServiceName: "comet",
		Kafka:       cfg.Kafka,
	})
	if err != nil {
		return nil, err
	}
	logger.CometLogger = log.Sugar()
	logClosed := false
	defer func() {
		if !logClosed {
			_ = log.Close()
		}
	}()

	// 2. 初始化 Redis
	rdb, err := initRedis(ctx, cfg.RedisAddrs, cfg.RedisPassword)
	if err != nil {
		return nil, err
	}
	cache := storage.NewRedisStorage(rdb)

	// 3. Consul naming
	ns, err := naming.NewNaming(cfg.ConsulURL)
	if err != nil {
		return nil, err
	}

	// 3.5 初始化 Sentinel（断路器 + 限流器）
	if err := client.InitSentinel(); err != nil {
		logger.CometLogger.Warnf("init sentinel (resilience disabled): %v", err)
	}

	// 3.6 初始化链路追踪
	traceShutdown, err := trace.InitTrace("comet", cfg.Trace)
	if err != nil {
		logger.CometLogger.Warnf("init trace (disabled): %v", err)
	}

	// 4. gRPC client pool（挂载弹性拦截器）
	logicPool := client.NewPoolWithConfig(ns, wire.SNService, cfg.Resilience, cfg.GRPC)
	gwPool := client.NewPoolWithConfig(ns, wire.SNWGateway, cfg.Resilience, cfg.GRPC)

	// 5. service clients（LogicClient 接入 ResilientClient：重试 + fallback 换实例）
	logicCli := service.NewLogicClient(logicPool, cfg.Resilience)
	pusher := service.NewGatewayPusher(gwPool)

	// 6. Router + handlers
	r := kim.NewRouter()
	r.Use(middleware.Recover())
	// login
	loginHandler := handler.NewLoginHandler(logicCli)
	r.Handle(wire.CommandLoginSignIn, loginHandler.DoSysLogin)
	r.Handle(wire.CommandLoginSignOut, loginHandler.DoSysLogout)
	// talk
	chatHandler := handler.NewChatHandler(logicCli, logicCli)
	r.Handle(wire.CommandChatUserTalk, chatHandler.DoUserTalk)
	r.Handle(wire.CommandChatGroupTalk, chatHandler.DoGroupTalk)
	r.Handle(wire.CommandChatTalkAck, chatHandler.DoTalkAck)
	// group
	groupHandler := handler.NewGroupHandler(logicCli)
	r.Handle(wire.CommandGroupCreate, groupHandler.DoCreate)
	r.Handle(wire.CommandGroupJoin, groupHandler.DoJoin)
	r.Handle(wire.CommandGroupQuit, groupHandler.DoQuit)
	r.Handle(wire.CommandGroupDetail, groupHandler.DoDetail)
	// offline
	offlineHandler := handler.NewOfflineHandler(logicCli)
	r.Handle(wire.CommandOfflineIndex, offlineHandler.DoSyncIndex)
	r.Handle(wire.CommandOfflineContent, offlineHandler.DoSyncContent)

	// 7. gRPC server（挂载服务端限流）
	grpcSrv, err := server.NewGRPCServer(cfg.Listen,
		server.WithServiceName("comet"),
		server.WithLimiter(cfg.Resilience.Limiter),
		server.WithGRPCConfig(cfg.GRPC),
		server.WithAuthSecret(cfg.AppSecret),
	)
	if err != nil {
		return nil, err
	}
	impl := &CometServiceImpl{
		router: r,
		pusher: pusher,
		cache:  cache,
	}
	rpc.RegisterCometServiceServer(grpcSrv, impl)

	// 8. Consul 注册
	if err := ns.Register(&naming.DefaultService{
		Id:       cfg.ServiceID,
		Name:     wire.SNChat,
		Address:  cfg.PublicAddress,
		Port:     cfg.PublicPort,
		Protocol: "grpc",
		Tags:     cfg.Tags,
		Meta: map[string]string{
			naming.KeyHealthURL: fmt.Sprintf("http://%s:%d/health", cfg.PublicAddress, cfg.MonitorPort),
			"zone":              cfg.Zone,
		},
	}); err != nil {
		return nil, fmt.Errorf("register comet service: %w", err)
	}
	grpcSrv.SetReady()
	logClosed = true

	return &Server{
		config:        cfg,
		grpcSrv:       grpcSrv,
		naming:        ns,
		logicPool:     logicPool,
		gwPool:        gwPool,
		log:           log,
		traceShutdown: traceShutdown,
	}, nil
}

// Start 启动 gRPC 服务（阻塞）
func (s *Server) Start(ctx context.Context) error {
	monitorAddr := fmt.Sprintf(":%d", s.config.MonitorPort)
	go func() {
		if err := server.StartMonitorHTTPWithReady(monitorAddr, s.grpcSrv); err != nil {
			logger.CometLogger.Errorf("monitor http error: %v", err)
		}
	}()
	logger.CometLogger.Infof("comet monitor listening on %s", monitorAddr)
	logger.CometLogger.Infof("comet service starting on %s", s.config.Listen)
	return s.grpcSrv.Start()
}

// Stop 反注册 Consul 并优雅关闭 gRPC 服务
func (s *Server) Stop(ctx context.Context) error {
	if s.naming != nil {
		if err := s.naming.Deregister(s.config.ServiceID); err != nil {
			logger.CometLogger.Warnf("deregister comet: %v", err)
		}
	}
	s.gwPool.Close()
	s.logicPool.Close()
	s.grpcSrv.GracefulStop()
	if s.traceShutdown != nil {
		s.traceShutdown()
	}
	if s.log != nil {
		_ = s.log.Close()
	}
	return nil
}

// initRedis 初始化单机 Redis 客户端
func initRedis(ctx context.Context, addr string, password string) (*redis.Client, error) {
	if addr == "" {
		return nil, nil
	}
	redisdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
	})
	_, err := redisdb.Ping(ctx).Result()
	if err != nil {
		return nil, err
	}
	return redisdb, nil
}
