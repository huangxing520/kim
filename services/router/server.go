// 文件：server.go
// 职责：Router 服务入口——Cobra 命令行启动区域路由服务，根据客户端 IP 返回最优网关域名列表。
//
// 定义的类型：
//   - ServerStartOptions 结构体：命令行启动参数（config / data 路径）
//
// 方法：
//   - NewServerStartCmd(ctx, version)   → 创建 router 子命令（Cobra）
//   - RunServerStart(ctx, opts, version) → 启动 Router：加载配置 → 加载 IP 区域映射/区域权重 → 创建 Ip2region →
//                                          创建 Consul Naming → 注册 /api/lookup/:token 路由 → Listen Iris

package router

import (
	"context"
	"path"

	"github.com/kataras/iris/v12"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/naming/consul"
	"github.com/klintcheng/kim/services/router/apis"
	"github.com/klintcheng/kim/services/router/conf"
	"github.com/klintcheng/kim/services/router/ipregion"
	"github.com/spf13/cobra"
)

// ServerStartOptions ServerStartOptions
type ServerStartOptions struct {
	config string
	data   string
}

// NewServerStartCmd creates a new http server command
func NewServerStartCmd(ctx context.Context, version string) *cobra.Command {
	opts := &ServerStartOptions{}

	cmd := &cobra.Command{
		Use:   "router",
		Short: "Start a router",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunServerStart(ctx, opts, version)
		},
	}
	cmd.PersistentFlags().StringVarP(&opts.config, "config", "c", "services/router/conf.yaml", "Config file")
	cmd.PersistentFlags().StringVarP(&opts.data, "data", "d", "services/router/data", "data path")
	return cmd
}

// RunServerStart run http server
func RunServerStart(ctx context.Context, opts *ServerStartOptions, version string) error {
	config, err := conf.Init(opts.config)
	if err != nil {
		return err
	}
	log, err := logger.Init(logger.Settings{
		Level:       "info",
		Filename:    "./data/router.log",
		ServiceName: "router",
		Kafka:       config.Kafka,
	})
	if err != nil {
		return err
	}
	logger.RouterLogger = log.Sugar()
	defer log.Close()
	logger.RouterLogger.Infow("ahah", "key", "value")
	mappings, err := conf.LoadMapping(path.Join(opts.data, "mapping.json"))
	if err != nil {
		return err
	}
	logger.RouterLogger.Infof("load mappings - %v", mappings)
	regions, err := conf.LoadRegions(path.Join(opts.data, "regions.json"))
	if err != nil {
		return err
	}
	logger.RouterLogger.Infof("load regions - %v", regions)

	region, err := ipregion.NewIp2region(path.Join(opts.data, "ip2region.db"))
	if err != nil {
		return err
	}

	ns, err := consul.NewNaming(config.ConsulURL)
	if err != nil {
		return err
	}

	router := apis.RouterApi{
		Naming:   ns,
		IpRegion: region,
		Config: conf.Router{
			Mapping: mappings,
			Regions: regions,
		},
	}

	app := iris.Default()

	app.Get("/health", func(ctx iris.Context) {
		_, _ = ctx.WriteString("ok")
	})
	routerAPI := app.Party("/api/lookup")
	{
		routerAPI.Get("/:token", router.Lookup)
	}

	// Start server
	return app.Listen(config.Listen, iris.WithOptimizations)
}
