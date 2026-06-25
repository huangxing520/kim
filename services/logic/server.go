// 文件：server.go
// 职责：Logic 服务入口——gRPC 服务器，包含数据库初始化、Handler 注册、Consul 服务注册。
//
// 定义的类型：
//   - Server 结构体：gRPC 服务实例（持有 config / grpcSrv / naming）
//
// 方法：
//   - New(ctx, cfg)              → 创建 Logic 服务：初始化 logger/DB/Redis/IDGenerator → 注册 gRPC Handler → 注册 Consul
//   - (Server).Start(ctx)        → 启动 gRPC 服务（阻塞）
//   - (Server).Stop(ctx)         → 反注册 Consul + GracefulStop
//   - HashCode(key)              → CRC32 哈希（用于生成默认 NodeID）
//   - initRedis(addr)            → 初始化单机 Redis 客户端

package logic

import (
	"context"
	"fmt"
	"hash/crc32"

	"github.com/go-redis/redis/v7"
	"gorm.io/gorm"

	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/naming"
	"github.com/klintcheng/kim/internal/server"
	"github.com/klintcheng/kim/internal/trace"
	"github.com/klintcheng/kim/services/logic/database"
	"github.com/klintcheng/kim/services/logic/handler"
	"github.com/klintcheng/kim/wire"
)

// Server Logic gRPC 服务
type Server struct {
	config        *Config
	grpcSrv       *server.GRPCServer
	naming        naming.Naming
	baseDb        *gorm.DB
	messageDb     *gorm.DB
	log           *logger.Logger
	traceShutdown func()
}

// New 创建 Logic 服务实例
func New(ctx context.Context, cfg *Config) (*Server, error) {
	// 初始化 logger
	log, err := logger.Init(logger.Settings{
		Level:       cfg.LogLevel,
		Filename:    "./data/logic.log",
		ServiceName: "logic",
		Kafka:       cfg.Kafka,
	})
	if err != nil {
		return nil, err
	}
	logger.LogicLogger = log.Sugar()
	logClosed := false
	defer func() {
		if !logClosed {
			_ = log.Close()
		}
	}()

	// 初始化 DB
	baseDb, err := database.InitDb(cfg.Driver, cfg.BaseDb)
	if err != nil {
		return nil, err
	}
	messageDb, err := database.InitDb(cfg.Driver, cfg.MessageDb)
	if err != nil {
		return nil, err
	}

	if err := baseDb.AutoMigrate(&database.Group{}, &database.GroupMember{}, &database.User{}); err != nil {
		return nil, fmt.Errorf("auto migrate base db: %w", err)
	}
	if err := messageDb.AutoMigrate(&database.MessageIndex{}, &database.MessageContent{}); err != nil {
		return nil, fmt.Errorf("auto migrate message db: %w", err)
	}

	if cfg.NodeID == 0 {
		cfg.NodeID = int64(HashCode(cfg.ServiceID))
	}
	idgen, err := database.NewIDGenerator(cfg.NodeID)
	if err != nil {
		return nil, err
	}

	// 初始化 Redis
	rdb, err := initRedis(cfg.RedisAddrs, cfg.RedisPassword)
	if err != nil {
		return nil, err
	}

	// 创建 handler
	handler.AppSecret = cfg.AppSecret
	h := &handler.ServiceHandler{
		BaseDb:    baseDb,
		MessageDb: messageDb,
		Idgen:     idgen,
		Cache:     rdb,
	}

	// 初始化 Sentinel（断路器 + 限流器）
	if err := client.InitSentinel(); err != nil {
		logger.LogicLogger.Warnf("init sentinel (resilience disabled): %v", err)
	}

	// 初始化链路追踪
	traceShutdown, err := trace.InitTrace("logic", cfg.Trace)
	if err != nil {
		logger.LogicLogger.Warnf("init trace (disabled): %v", err)
	}

	// 创建 gRPC server（挂载服务端限流）
	grpcSrv, err := server.NewGRPCServer(cfg.Listen,
		server.WithServiceName("logic"),
		server.WithLimiter(cfg.Resilience.Limiter),
		server.WithGRPCConfig(cfg.GRPC),
		server.WithAuthSecret(cfg.AppSecret),
	)
	if err != nil {
		return nil, err
	}
	rpc.RegisterLogicServiceServer(grpcSrv, h)

	// Consul 注册
	ns, err := naming.NewNaming(cfg.ConsulURL)
	if err != nil {
		return nil, err
	}
	if err := ns.Register(&naming.DefaultService{
		Id:       cfg.ServiceID,
		Name:     wire.SNService,
		Address:  cfg.PublicAddress,
		Port:     cfg.PublicPort,
		Protocol: "grpc",
		Tags:     cfg.Tags,
		Meta: map[string]string{
			naming.KeyHealthURL: fmt.Sprintf("http://%s:%d/health", cfg.PublicAddress, cfg.MonitorPort),
		},
	}); err != nil {
		return nil, fmt.Errorf("register logic service: %w", err)
	}
	grpcSrv.SetReady()

	s := &Server{
		config:        cfg,
		grpcSrv:       grpcSrv,
		naming:        ns,
		baseDb:        baseDb,
		messageDb:     messageDb,
		log:           log,
		traceShutdown: traceShutdown,
	}
	logClosed = true

	return s, nil
}

// Start 启动 gRPC 服务（阻塞）
func (s *Server) Start(ctx context.Context) error {
	monitorAddr := fmt.Sprintf(":%d", s.config.MonitorPort)
	go func() {
		if err := server.StartMonitorHTTPWithReady(monitorAddr, s.grpcSrv); err != nil {
			logger.LogicLogger.Errorf("monitor http error: %v", err)
		}
	}()
	logger.LogicLogger.Infof("logic monitor listening on %s", monitorAddr)
	logger.LogicLogger.Infof("logic service starting on %s", s.config.Listen)
	return s.grpcSrv.Start()
}

// Stop 反注册 Consul 并优雅关闭 gRPC 服务
func (s *Server) Stop(ctx context.Context) error {
	if s.naming != nil {
		if err := s.naming.Deregister(s.config.ServiceID); err != nil {
			logger.LogicLogger.Warnf("deregister logic: %v", err)
		}
	}
	s.grpcSrv.GracefulStop()
	if s.baseDb != nil {
		if sqlDB, err := s.baseDb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	if s.messageDb != nil {
		if sqlDB, err := s.messageDb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	if s.traceShutdown != nil {
		s.traceShutdown()
	}
	if s.log != nil {
		_ = s.log.Close()
	}
	return nil
}

// HashCode CRC32 哈希（用于生成默认 NodeID）
func HashCode(key string) uint32 {
	hash32 := crc32.NewIEEE()
	hash32.Write([]byte(key))
	return hash32.Sum32() % 1000
}

// initRedis 初始化单机 Redis 客户端
func initRedis(addr string, password string) (*redis.Client, error) {
	if addr == "" {
		return nil, nil
	}
	redisdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
	})
	_, err := redisdb.Ping().Result()
	if err != nil {
		return nil, err
	}
	return redisdb, nil
}
