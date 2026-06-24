// 文件：server.go
// 职责：Comet 服务入口——Cobra 命令行启动 Chat/Login 服务，组装 Router、Handler、Redis 缓存、服务发现等组件。
//
// 定义的类型：
//   - ServerStartOptions 结构体：命令行启动参数（config / serviceName）
//
// 方法：
//   - NewServerStartCmd(ctx, version)   → 创建 comet 子命令（Cobra）
//   - RunServerStart(ctx, opts, version) → 启动 Comet：加载配置 → 初始化 logger → 创建 Router 并注册 Handler →
//                                          初始化 Redis 缓存 → 创建 Server → Init container →
//                                          注册 Consul Naming → Start

package comet

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/container"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/middleware"
	"github.com/klintcheng/kim/naming"
	"github.com/klintcheng/kim/naming/consul"
	"github.com/klintcheng/kim/services/comet/conf"
	"github.com/klintcheng/kim/services/comet/handler"
	"github.com/klintcheng/kim/services/comet/serv"
	"github.com/klintcheng/kim/services/comet/service"
	"github.com/klintcheng/kim/storage"
	"github.com/klintcheng/kim/tcp"
	"github.com/klintcheng/kim/wire"
	"github.com/spf13/cobra"
)

// ServerStartOptions ServerStartOptions
type ServerStartOptions struct {
	config      string
	serviceName string
}

// NewServerStartCmd creates a new http server command
func NewServerStartCmd(ctx context.Context, version string) *cobra.Command {
	opts := &ServerStartOptions{}

	cmd := &cobra.Command{
		Use:   "comet",
		Short: "Start a server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunServerStart(ctx, opts, version)
		},
	}
	cmd.PersistentFlags().StringVarP(&opts.config, "config", "c", "services/comet/conf.yaml", "Config file")
	cmd.PersistentFlags().StringVarP(&opts.serviceName, "serviceName", "s", "chat", "defined a service name,option is login or chat")
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
		Filename:    "./data/comet.log",
		ServiceName: opts.serviceName,
		Kafka:       config.Kafka,
	})
	if err != nil {
		return err
	}
	logger.CometLogger = log.Sugar()
	defer log.Close()

	var groupService service.Group
	var messageService service.Message
	var userMessage service.User
	if strings.TrimSpace(config.RoyalURL) != "" {
		groupService = service.NewGroupService(config.RoyalURL)
		messageService = service.NewMessageService(config.RoyalURL)
		userMessage = service.NewUserService(config.RoyalURL)
	} else {
		srvRecord := &resty.SRVRecord{
			Domain:  "consul",
			Service: wire.SNService,
		}
		groupService = service.NewGroupServiceWithSRV("http", srvRecord)
		messageService = service.NewMessageServiceWithSRV("http", srvRecord)
		userMessage = service.NewUserServiceWithSRV("http", srvRecord)
	}

	r := kim.NewRouter()
	r.Use(middleware.Recover())
	r.Use(middleware.Recover())
	// login
	loginHandler := handler.NewLoginHandler(userMessage)
	r.Handle(wire.CommandLoginSignIn, loginHandler.DoSysLogin)
	r.Handle(wire.CommandLoginSignOut, loginHandler.DoSysLogout)
	// talk
	chatHandler := handler.NewChatHandler(messageService, groupService)
	r.Handle(wire.CommandChatUserTalk, chatHandler.DoUserTalk)
	r.Handle(wire.CommandChatGroupTalk, chatHandler.DoGroupTalk)
	r.Handle(wire.CommandChatTalkAck, chatHandler.DoTalkAck)
	// group
	groupHandler := handler.NewGroupHandler(groupService)
	r.Handle(wire.CommandGroupCreate, groupHandler.DoCreate)
	r.Handle(wire.CommandGroupJoin, groupHandler.DoJoin)
	r.Handle(wire.CommandGroupQuit, groupHandler.DoQuit)
	r.Handle(wire.CommandGroupDetail, groupHandler.DoDetail)

	// offline
	offlineHandler := handler.NewOfflineHandler(messageService)
	r.Handle(wire.CommandOfflineIndex, offlineHandler.DoSyncIndex)
	r.Handle(wire.CommandOfflineContent, offlineHandler.DoSyncContent)

	rdb, err := conf.InitRedis(config.RedisAddrs, "")
	if err != nil {
		return err
	}
	cache := storage.NewRedisStorage(rdb)
	servhandler := serv.NewServHandler(r, cache)

	meta := make(map[string]string)
	meta[consul.KeyHealthURL] = fmt.Sprintf("http://%s:%d/health", config.PublicAddress, config.MonitorPort)
	meta["zone"] = config.Zone

	service := &naming.DefaultService{
		Id:       config.ServiceID,
		Name:     opts.serviceName,
		Address:  config.PublicAddress,
		Port:     config.PublicPort,
		Protocol: string(wire.ProtocolTCP),
		Tags:     config.Tags,
		Meta:     meta,
	}
	srvOpts := []kim.ServerOption{
		kim.WithConnectionGPool(config.ConnectionGPool), kim.WithMessageGPool(config.MessageGPool),
	}
	srv := tcp.NewServer(config.Listen, service, srvOpts...)

	srv.SetReadWait(kim.DefaultReadWait)
	srv.SetAcceptor(servhandler)
	srv.SetMessageListener(servhandler)
	srv.SetStateListener(servhandler)

	if err := container.Init(srv); err != nil {
		return err
	}
	container.EnableMonitor(fmt.Sprintf(":%d", config.MonitorPort))

	ns, err := consul.NewNaming(config.ConsulURL)
	if err != nil {
		return err
	}
	container.SetServiceNaming(ns)

	return container.Start()
}
