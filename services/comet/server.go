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

	"github.com/go-redis/redis/v7"
	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/internal/naming"
	"github.com/klintcheng/kim/internal/server"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/middleware"
	"github.com/klintcheng/kim/services/comet/handler"
	"github.com/klintcheng/kim/services/comet/service"
	"github.com/klintcheng/kim/storage"
	"github.com/klintcheng/kim/wire"
)

// Server Comet gRPC 服务
type Server struct {
	config    *Config
	grpcSrv   *server.GRPCServer
	naming    naming.Naming
	logicPool *client.Pool
	gwPool    *client.Pool
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
	defer log.Close()

	// 2. 初始化 Redis
	rdb, err := initRedis(cfg.RedisAddrs)
	if err != nil {
		return nil, err
	}
	cache := storage.NewRedisStorage(rdb)

	// 3. Consul naming
	ns, err := naming.NewNaming(cfg.ConsulURL)
	if err != nil {
		return nil, err
	}

	// 4. gRPC client pool
	logicPool := client.NewPool(ns, wire.SNService) // "royal"
	gwPool := client.NewPool(ns, wire.SNWGateway)   // "wgateway"

	// 5. service clients
	logicCli := service.NewLogicClient(logicPool)
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

	// 7. gRPC server
	grpcSrv, err := server.NewGRPCServer(cfg.Listen, server.WithServiceName("comet"))
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
	_ = ns.Register(&naming.DefaultService{
		Id:       cfg.ServiceID,
		Name:     wire.SNChat,
		Address:  cfg.PublicAddress,
		Port:     cfg.PublicPort,
		Protocol: "grpc",
		Tags:     cfg.Tags,
		Meta: map[string]string{
			naming.KeyHealthURL: fmt.Sprintf("http://%s:%d/health", cfg.PublicAddress, cfg.PublicPort),
			"zone":              cfg.Zone,
		},
	})

	return &Server{
		config:    cfg,
		grpcSrv:   grpcSrv,
		naming:    ns,
		logicPool: logicPool,
		gwPool:    gwPool,
	}, nil
}

// Start 启动 gRPC 服务（阻塞）
func (s *Server) Start(ctx context.Context) error {
	logger.CometLogger.Infof("comet service starting on %s", s.config.Listen)
	return s.grpcSrv.Start()
}

// Stop 反注册 Consul 并优雅关闭 gRPC 服务
func (s *Server) Stop(ctx context.Context) error {
	if s.naming != nil {
		_ = s.naming.Deregister(s.config.ServiceID)
	}
	s.gwPool.Close()
	s.logicPool.Close()
	s.grpcSrv.GracefulStop()
	return nil
}

// initRedis 初始化单机 Redis 客户端
func initRedis(addr string) (*redis.Client, error) {
	if addr == "" {
		return nil, nil
	}
	redisdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	_, err := redisdb.Ping().Result()
	if err != nil {
		return nil, err
	}
	return redisdb, nil
}
