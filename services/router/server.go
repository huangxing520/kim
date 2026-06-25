// 文件：server.go
// 职责：Router 服务入口——创建 Iris HTTP 服务，根据客户端 IP 返回最优网关域名列表。
//
// 定义的类型：
//   - Server 结构体：Router 服务实例（持有 config / dataPath / app / naming）
//
// 方法：
//   - New(ctx, cfg, dataPath) → 创建 Router 服务：初始化 logger → 加载 mapping/regions/ip2region → 创建 Naming → 注册路由
//   - (Server).Start(ctx)     → 启动 Iris HTTP 服务（阻塞）
//   - (Server).Stop(ctx)      → 关闭 Iris HTTP 服务

package router

import (
	"context"
	"path"

	"github.com/kataras/iris/v12"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/naming"
	"github.com/klintcheng/kim/services/router/handler"
	"github.com/klintcheng/kim/services/router/conf"
	"github.com/klintcheng/kim/services/router/ipregion"
)

// Server Router 服务实例
type Server struct {
	config   *Config
	dataPath string
	app      *iris.Application
	naming   naming.Naming
	log      *logger.Logger
}

// New 创建 Router 服务实例
func New(ctx context.Context, cfg *Config, dataPath string) (*Server, error) {
	// 1. 初始化 logger
	log, err := logger.Init(logger.Settings{
		Level:       cfg.LogLevel,
		Filename:    "./data/router.log",
		ServiceName: "router",
		Kafka:       cfg.Kafka,
	})
	if err != nil {
		return nil, err
	}
	logger.RouterLogger = log.Sugar()
	logClosed := false
	defer func() {
		if !logClosed {
			_ = log.Close()
		}
	}()

	// 2. 加载路由配置
	mappings, err := conf.LoadMapping(path.Join(dataPath, "mapping.json"))
	if err != nil {
		return nil, err
	}
	logger.RouterLogger.Infof("load mappings - %v", mappings)
	regions, err := conf.LoadRegions(path.Join(dataPath, "regions.json"))
	if err != nil {
		return nil, err
	}
	logger.RouterLogger.Infof("load regions - %v", regions)

	// 3. 加载 IP 区域查询
	region, err := ipregion.NewIp2region(path.Join(dataPath, "ip2region.db"))
	if err != nil {
		return nil, err
	}

	// 4. Consul naming
	ns, err := naming.NewNaming(cfg.ConsulURL)
	if err != nil {
		return nil, err
	}

	// 5. 组装 RouterApi
	routerAPI := handler.RouterApi{
		Naming:   ns,
		IpRegion: region,
		Config: conf.Router{
			Mapping: mappings,
			Regions: regions,
		},
	}

	// 6. 创建 Iris app 并注册路由
	app := iris.Default()
	app.Get("/health", func(ctx iris.Context) {
		_, _ = ctx.WriteString("ok")
	})
	routerAPIGroup := app.Party("/api/lookup")
	{
		routerAPIGroup.Get("/:token", routerAPI.Lookup)
	}
	logClosed = true

	return &Server{
		config:   cfg,
		dataPath: dataPath,
		app:      app,
		naming:   ns,
		log:      log,
	}, nil
}

// Start 启动 Router HTTP 服务（阻塞）
func (s *Server) Start(ctx context.Context) error {
	logger.RouterLogger.Infof("router service starting on %s", s.config.Listen)
	return s.app.Listen(s.config.Listen, iris.WithOptimizations)
}

// Stop 关闭 Router HTTP 服务
func (s *Server) Stop(ctx context.Context) error {
	err := s.app.Shutdown(ctx)
	if s.log != nil {
		_ = s.log.Close()
	}
	return err
}
