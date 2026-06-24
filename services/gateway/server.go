// 文件：server.go
// 职责：Gateway 服务入口——Cobra 命令行启动 Gateway 服务，组装 Server、配置、Handler、路由选择器、服务发现等组件。
//
// 定义的类型：
//   - ServerStartOptions 结构体：命令行启动参数（config / protocol / route）
//
// 方法：
//   - NewServerStartCmd(ctx, version)       → 创建 gateway 子命令（Cobra）
//   - RunServerStart(ctx, opts, version)     → 启动 Gateway：加载配置 → 初始化 logger → 创建 Handler → 创建 Server →
//                                             设置 Acceptor/MessageListener/StateListener → Init container →
//                                             注册 Consul Naming → 设置 Dialer/Selector → Start

package gateway

import (
	"context"
	"fmt"
	_ "net/http/pprof"
	"time"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/container"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/naming"
	"github.com/klintcheng/kim/naming/consul"
	"github.com/klintcheng/kim/services/gateway/conf"
	"github.com/klintcheng/kim/services/gateway/serv"
	"github.com/klintcheng/kim/tcp"
	"github.com/klintcheng/kim/websocket"
	"github.com/klintcheng/kim/wire"
	"github.com/spf13/cobra"
)

// const logName = "logs/gateway"

// ServerStartOptions ServerStartOptions
type ServerStartOptions struct {
	config   string
	protocol string
	route    string
}

// NewServerStartCmd creates a new http server command
func NewServerStartCmd(ctx context.Context, version string) *cobra.Command {
	opts := &ServerStartOptions{}

	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Start a gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunServerStart(ctx, opts, version)
		},
	}
	cmd.PersistentFlags().StringVarP(&opts.config, "config", "c", "services/gateway/conf.yaml", "Config file")
	cmd.PersistentFlags().StringVarP(&opts.route, "route", "r", "services/gateway/route.json", "route file")
	cmd.PersistentFlags().StringVarP(&opts.protocol, "protocol", "p", "ws", "protocol of ws or tcp")
	return cmd
}

// RunServerStart run http server
func RunServerStart(ctx context.Context, opts *ServerStartOptions, version string) error {
	config, err := conf.Init(opts.config)
	if err != nil {
		return err
	}
	log, err := logger.Init(logger.Settings{
		Level:       config.LogLevel,
		Filename:    "./data/gateway.log",
		ServiceName: "gateway",
		Kafka:       config.Kafka,
	})
	if err != nil {
		return err
	}
	logger.GatewayLogger = log.Sugar()

	defer log.Close()

	handler := &serv.Handler{
		ServiceID: config.ServiceID,
		AppSecret: config.AppSecret,
	}
	meta := make(map[string]string)
	meta[consul.KeyHealthURL] = fmt.Sprintf("http://%s:%d/health", config.PublicAddress, config.MonitorPort)
	meta["domain"] = config.Domain

	var srv kim.Server
	service := &naming.DefaultService{
		Id:       config.ServiceID,
		Name:     config.ServiceName,
		Address:  config.PublicAddress,
		Port:     config.PublicPort,
		Protocol: opts.protocol,
		Tags:     config.Tags,
		Meta:     meta,
	}
	srvOpts := []kim.ServerOption{
		kim.WithConnectionGPool(config.ConnectionGPool), kim.WithMessageGPool(config.MessageGPool),
	}
	switch opts.protocol {
	case "ws":
		srv = websocket.NewServer(config.Listen, service, srvOpts...)
	case "tcp":
		srv = tcp.NewServer(config.Listen, service, srvOpts...)
	default:
		srv = websocket.NewServer(config.Listen, service, srvOpts...)
	}

	srv.SetReadWait(time.Minute * 2)
	srv.SetAcceptor(handler)
	srv.SetMessageListener(handler)
	srv.SetStateListener(handler)

	_ = container.Init(srv, wire.SNChat, wire.SNLogin)
	container.EnableMonitor(fmt.Sprintf(":%d", config.MonitorPort))

	ns, err := consul.NewNaming(config.ConsulURL)
	if err != nil {
		return err
	}
	container.SetServiceNaming(ns)
	// set a dialer
	container.SetDialer(serv.NewDialer(config.ServiceID))
	// use routeSelector
	selector, err := serv.NewRouteSelector(opts.route)
	if err != nil {
		return err
	}
	container.SetSelector(selector)
	return container.Start()
}
