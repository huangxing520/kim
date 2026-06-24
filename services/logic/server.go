// 文件：server.go
// 职责：Logic 服务入口——Cobra 命令行启动 Royal（HTTP API）服务，包含数据库初始化、路由注册、服务注册。
//
// 定义的类型：
//   - ServerStartOptions 结构体：命令行启动参数（config）
//
// 方法：
//   - NewServerStartCmd(ctx, version)       → 创建 logic 子命令（Cobra）
//   - RunServerStart(ctx, opts, version)     → 启动 Logic：加载配置 → 初始化 DB/Redis/IDGenerator → 注册 Handler →
//                                             注册 Consul 服务 → 启动 Iris HTTP 服务
//   - HashCode(key)                          → CRC32 哈希（用于生成 NodeID）
//   - HashCode(key)                          → 同 container.HashCode，用于计算默认 NodeID

package logic

import (
	"context"
	"fmt"
	"hash/crc32"

	"gorm.io/gorm"

	"github.com/kataras/iris/v12"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/naming"
	"github.com/klintcheng/kim/naming/consul"
	"github.com/klintcheng/kim/services/logic/conf"
	"github.com/klintcheng/kim/services/logic/database"
	"github.com/klintcheng/kim/services/logic/handler"
	"github.com/klintcheng/kim/wire"
	"github.com/spf13/cobra"
)

// ServerStartOptions ServerStartOptions
type ServerStartOptions struct {
	config string
}

// NewServerStartCmd creates a new http server command
func NewServerStartCmd(ctx context.Context, version string) *cobra.Command {
	opts := &ServerStartOptions{}

	cmd := &cobra.Command{
		Use:   "logic",
		Short: "Start a rpc service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunServerStart(ctx, opts, version)
		},
	}
	cmd.PersistentFlags().StringVarP(&opts.config, "config", "c", "services/logic/conf.yaml", "Config file")
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
		Filename:    "./data/logic.log",
		ServiceName: "logic",
		Kafka:       config.Kafka,
	})
	if err != nil {
		return err
	}
	logger.LogicLogger = log.Sugar()
	defer log.Close()

	// database.Init
	var (
		baseDb    *gorm.DB
		messageDb *gorm.DB
	)
	baseDb, err = database.InitDb(config.Driver, config.BaseDb)
	if err != nil {
		return err
	}
	messageDb, err = database.InitDb(config.Driver, config.MessageDb)
	if err != nil {
		return err
	}

	_ = baseDb.AutoMigrate(&database.Group{}, &database.GroupMember{}, &database.User{})
	_ = messageDb.AutoMigrate(&database.MessageIndex{}, &database.MessageContent{})

	if config.NodeID == 0 {
		config.NodeID = int64(HashCode(config.ServiceID))
	}
	idgen, err := database.NewIDGenerator(config.NodeID)
	if err != nil {
		return err
	}

	rdb, err := conf.InitRedis(config.RedisAddrs, "")
	if err != nil {
		return err
	}
	//cache:= storage.NewRedisStorage(rdb)
	ns, err := consul.NewNaming(config.ConsulURL)
	if err != nil {
		return err
	}
	_ = ns.Register(&naming.DefaultService{
		Id:       config.ServiceID,
		Name:     wire.SNService, // service name
		Address:  config.PublicAddress,
		Port:     config.PublicPort,
		Protocol: "http",
		Tags:     config.Tags,
		Meta: map[string]string{
			consul.KeyHealthURL: fmt.Sprintf("http://%s:%d/health", config.PublicAddress, config.PublicPort),
		},
	})
	defer func() {
		_ = ns.Deregister(config.ServiceID)
	}()
	serviceHandler := handler.ServiceHandler{
		BaseDb:    baseDb,
		MessageDb: messageDb,
		Idgen:     idgen,
		Cache:     rdb,
	}

	ac := conf.MakeAccessLog()
	defer ac.Close()

	app := newApp(&serviceHandler)
	app.UseRouter(ac.Handler)
	app.UseRouter(setAllowedResponses)

	// Start server
	return app.Listen(config.Listen, iris.WithOptimizations)
}

func newApp(serviceHandler *handler.ServiceHandler) *iris.Application {
	app := iris.Default()

	app.Get("/health", func(ctx iris.Context) {
		_, _ = ctx.WriteString("ok")
	})
	messageAPI := app.Party("/api/:app/message")
	{
		messageAPI.Post("/user", serviceHandler.InsertUserMessage)
		messageAPI.Post("/group", serviceHandler.InsertGroupMessage)
		messageAPI.Post("/ack", serviceHandler.MessageAck)
	}

	groupAPI := app.Party("/api/:app/group")
	{
		groupAPI.Get("/:id", serviceHandler.GroupGet)
		groupAPI.Post("", serviceHandler.GroupCreate)
		groupAPI.Post("/member", serviceHandler.GroupJoin)
		groupAPI.Delete("/member", serviceHandler.GroupQuit)
		groupAPI.Get("/members/:id", serviceHandler.GroupMembers)
	}
	userAPI := app.Party("/api/:app/user")
	{
		userAPI.Post("/login", serviceHandler.Login)
	}
	offlineAPI := app.Party("/api/:app/offline")
	{
		offlineAPI.Use(iris.Compression)
		offlineAPI.Post("/index", serviceHandler.GetOfflineMessageIndex)
		offlineAPI.Post("/content", serviceHandler.GetOfflineMessageContent)
	}
	return app
}

func setAllowedResponses(ctx iris.Context) {
	// Indicate that the Server can send JSON, XML, YAML and MessagePack for this request.
	ctx.Negotiation().JSON().Protobuf().MsgPack()
	// Add more, allowed by the server format of responses, mime types here...

	// If client is missing an "Accept: " header then default it to JSON.
	ctx.Negotiation().Accept.JSON()

	ctx.Next()
}

func HashCode(key string) uint32 {
	hash32 := crc32.NewIEEE()
	hash32.Write([]byte(key))
	return hash32.Sum32() % 1000
}
