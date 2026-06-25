# Comet 服务 - 聊天/登录/群组业务

## 模块概述

Comet 是 kim 系统的核心业务处理服务，负责处理 IM 核心业务逻辑。它作为无状态的业务逻辑层，承担以下职责：

- **登录认证**：处理用户登录/登出，Session 管理，异地登录踢下线
- **消息收发**：单聊、群聊消息的路由转发和离线处理
- **群组管理**：群组创建、加入、退出、详情查询
- **离线同步**：离线消息索引同步和内容拉取
- **消息推送**：通过 Gateway 将消息推送给在线用户

服务默认监听端口：gRPC `:8005`，Monitor HTTP `:8007`。

## 架构设计

Comet 服务是纯 gRPC 服务，采用 Router + Handler 架构：

```
┌─────────────────────────────────────────────────────────────┐
│                     Gateway 服务                              │
│              gRPC: CometService.Forward                      │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                     gRPC Server                              │
│               ┌─────────────────────┐                        │
│               │  CometServiceImpl   │                        │
│               │  - Forward()        │                        │
│               └─────────┬───────────┘                        │
└─────────────────────────┼────────────────────────────────────┘
                          │
┌─────────────────────────▼────────────────────────────────────┐
│                         kim.Router                           │
│         (命令字 → Handler 路由，中间件链)                      │
│                     ┌─────────┐                              │
│                     │ Recover │ 中间件                        │
│                     └────┬────┘                              │
└──────────────────────────┼───────────────────────────────────┘
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
┌───────▼───────┐  ┌───────▼───────┐  ┌──────▼────────┐
│ LoginHandler  │  │  ChatHandler  │  │ GroupHandler  │
│ - DoSysLogin  │  │ - DoUserTalk  │  │ - DoCreate    │
│ - DoSysLogout │  │ - DoGroupTalk │  │ - DoJoin      │
│               │  │ - DoTalkAck   │  │ - DoQuit      │
└───────┬───────┘  └───────┬───────┘  │ - DoDetail    │
        │                  │          └───────┬───────┘
        │                  │                  │
        │          ┌───────▼───────┐          │
        │          │OfflineHandler │          │
        │          │-DoSyncIndex   │          │
        │          │-DoSyncContent │          │
        │          └───────┬───────┘          │
        │                  │                  │
        └──────────────────┼──────────────────┘
                           │
        ┌──────────────────┴──────────────────┐
        │                                     │
┌───────▼────────┐                   ┌────────▼───────┐
│  LogicClient   │                   │ GatewayPusher  │
│ (gRPC Client)  │                   │ (gRPC Client)  │
│ - 弹性保护      │                   │ - 推送消息      │
└───────┬────────┘                   └────────┬───────┘
        │                                     │
┌───────▼────────┐                   ┌────────▼───────┐
│  Logic 服务     │                   │  Gateway 服务   │
│ (消息持久化)    │                   │ (消息下发)      │
└────────────────┘                   └────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                      Redis 缓存                               │
│         - Session 存储 (在线用户位置)                          │
│         - AccessToken 缓存                                   │
│         - 消息已读位置                                        │
└─────────────────────────────────────────────────────────────┘
```

## 关键组件

### 核心结构体

```go
// Server Comet gRPC 服务
type Server struct {
    config        *Config              // 服务配置
    grpcSrv       *server.GRPCServer   // gRPC Server
    naming        naming.Naming        // Consul 服务发现
    logicPool     *client.Pool         // Logic 服务连接池
    gwPool        *client.Pool         // Gateway 服务连接池
    log           *logger.Logger       // 日志实例
    traceShutdown func()               // 链路追踪关闭函数
}
```

### CometServiceImpl - gRPC 服务实现

`services/comet/comet_service_impl.go`

```go
type CometServiceImpl struct {
    rpc.UnimplementedCometServiceServer
    router *kim.Router         // 命令路由器
    pusher kim.Dispatcher      // 消息推送器（GatewayPusher）
    cache  kim.SessionStorage  // Session 存储（Redis）
}
```

主要方法：
- `Forward(ctx, req)` - 接收 Gateway 转发的消息：反序列化 LogicPkt → 获取/构建 Session → 路由到对应 Handler

### LoginHandler - 登录处理器

`services/comet/handler/login_handler.go`

```go
type LoginHandler struct {
    userService service.User  // Logic 服务 User 接口
}
```

处理命令：
| 命令 | 方法 | 说明 |
|------|------|------|
| `login.sign_in` | `DoSysLogin` | 用户登录：验证身份 → 检查异地登录 → 踢下线 → 添加 Session |
| `login.sign_out` | `DoSysLogout` | 用户登出：删除 Session |

登录验证支持两种方式：
1. **AccessToken 验证**：从 Redis 读取 Token 比对
2. **密码验证**：调用 Logic 服务验证密码，成功后生成新 Token 存入 Redis

### ChatHandler - 聊天处理器

`services/comet/handler/chat_handler.go`

```go
type ChatHandler struct {
    msgService   service.Message  // Logic 服务 Message 接口
    groupService service.Group    // Logic 服务 Group 接口
}
```

处理命令：
| 命令 | 方法 | 说明 |
|------|------|------|
| `chat.user.talk` | `DoUserTalk` | 单聊：保存离线消息 → 对方在线则推送 |
| `chat.group.talk` | `DoGroupTalk` | 群聊：保存离线消息 → 获取群成员 → 批量推送 |
| `chat.talk.ack` | `DoTalkAck` | 消息已读确认 |

单聊流程：
1. 校验目标 Dest 不为空
2. 解析消息体
3. 查询接收方位置（是否在线）
4. 调用 Logic 保存离线消息（扩散写）
5. 接收方在线则推送 MessagePush
6. 返回消息 ID 和发送时间

群聊流程：
1. 校验群 ID 不为空
2. 解析消息体
3. 调用 Logic 保存群消息（扩散写到所有成员收件箱）
4. 查询群成员列表
5. 批量寻址在线成员
6. 批量推送 MessagePush

### GroupHandler - 群组处理器

`services/comet/handler/group_handler.go`

```go
type GroupHandler struct {
    groupService service.Group  // Logic 服务 Group 接口
}
```

处理命令：
| 命令 | 方法 | 说明 |
|------|------|------|
| `group.create` | `DoCreate` | 创建群组：调用 Logic 建群 → 通知初始成员 |
| `group.join` | `DoJoin` | 加入群组 |
| `group.quit` | `DoQuit` | 退出群组 |
| `group.detail` | `DoDetail` | 查询群详情：基本信息 + 成员列表 |

### OfflineHandler - 离线消息处理器

`services/comet/handler/offline_handler.go`

```go
type OfflineHandler struct {
    msgService service.Message  // Logic 服务 Message 接口
}
```

处理命令：
| 命令 | 方法 | 说明 |
|------|------|------|
| `offline.index` | `DoSyncIndex` | 同步离线消息索引列表 |
| `offline.content` | `DoSyncContent` | 按 messageId 列表批量拉取消息内容 |

同步流程：
1. 客户端传入本地最新消息 ID（或 0 表示全量同步）
2. 从 Redis 读取已读位置
3. 查询该时间点之后的消息索引
4. 更新已读位置到 Redis
5. 客户端根据索引列表拉取具体消息内容

### LogicClient - Logic 服务客户端

`services/comet/service/logic_client.go`

封装对 Logic 服务的 gRPC 调用，通过 `ResilientClient` 提供弹性保护：

```go
type LogicClient struct {
    resilient *client.ResilientClient  // 带熔断/重试/降级的客户端
}
```

封装接口：
- **Message 接口**：`InsertUser`、`InsertGroup`、`SetAck`、`GetMessageIndex`、`GetMessageContent`
- **Group 接口**：`Create`、`Members`、`Join`、`Quit`、`Detail`
- **User 接口**：`Login`

### GatewayPusher - Gateway 推送客户端

`services/comet/service/pusher.go`

```go
type GatewayPusher struct {
    pool *client.Pool  // Gateway 服务连接池
}
```

实现 `kim.Dispatcher` 接口：
- `Push(ctx, gateway, channels, packet)` - 通过 Gateway 的 `GatewayService.Push` 接口推送消息

## gRPC 接口

### 提供的 RPC 服务

Comet 作为 gRPC Server，提供以下服务供 Gateway 调用：

| 服务 | 方法 | 说明 |
|------|------|------|
| `CometService` | `Forward` | 接收 Gateway 转发的客户端消息 |

Forward 请求结构：
```go
type ForwardReq struct {
    Packet []byte  // 序列化后的 LogicPkt
}
```

### 调用的下游服务

Comet 作为 gRPC Client，调用以下服务：

#### 调用 Logic 服务（服务名 `royal`/`SNService`）

| 服务 | 方法 | 说明 |
|------|------|------|
| `LogicService` | `Login` | 用户登录验证 |
| `LogicService` | `InsertUserMessage` | 插入单聊消息 |
| `LogicService` | `InsertGroupMessage` | 插入群聊消息 |
| `LogicService` | `AckMessage` | 消息已读确认 |
| `LogicService` | `GetOfflineMessageIndex` | 查询离线消息索引 |
| `LogicService` | `GetOfflineMessageContent` | 查询离线消息内容 |
| `LogicService` | `GroupCreate` | 创建群组 |
| `LogicService` | `GroupJoin` | 加入群组 |
| `LogicService` | `GroupQuit` | 退出群组 |
| `LogicService` | `GroupMembers` | 查询群成员 |
| `LogicService` | `GroupGet` | 查询群详情 |

#### 调用 Gateway 服务（服务名 `wgateway`/`SNWGateway`）

| 服务 | 方法 | 说明 |
|------|------|------|
| `GatewayService` | `Push` | 推送消息到客户端 Channel |

## 配置说明

配置文件：`services/comet/conf.yaml`

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `service_id` | string | `comet-1` | 服务唯一 ID |
| `listen` | string | `:8005` | gRPC 监听地址 |
| `public_address` | string | `127.0.0.1` | 服务对外公布地址 |
| `public_port` | int | `8005` | gRPC 对外端口 |
| `monitor_port` | int | `8007` | Prometheus/Health 监控端口 |
| `tags` | []string | `["server"]` | Consul 服务标签 |
| `zone` | string | - | 所属可用区（用于 Gateway 区域路由） |
| `consul_url` | string | `http://127.0.0.1:8500` | Consul 地址 |
| `redis_addrs` | string | `127.0.0.1:6379` | Redis 地址 |
| `redis_password` | string | - | Redis 密码 |
| `log_level` | string | `info` | 日志级别 |
| `message_g_pool` | int | `5000` | 消息处理协程池大小 |
| `connection_g_pool` | int | `500` | 连接处理协程池大小 |
| `app_secret` | string | - | JWT 签名密钥（用于 gRPC 认证） |
| `kafka.enable` | bool | `true` | 是否启用 Kafka 日志 |
| `kafka.brokers` | []string | - | Kafka broker 列表 |
| `kafka.topic` | string | `kim_logs` | Kafka 日志主题 |
| `resilience.*` | object | - | 弹性保护配置 |
| `trace.*` | object | - | 链路追踪配置 |
| `grpc.*` | object | - | gRPC TLS/认证配置 |

## 启动方式

### 命令行启动

```bash
# 默认配置启动
./bin/kim comet

# 指定配置文件
./bin/kim comet -c services/comet/conf.yaml
```

### 命令行参数

| 参数 | 短参 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `services/comet/conf.yaml` | 配置文件路径 |

### Make 命令

```bash
# 前台启动调试
make run-comet-fg

# 后台启动
make run-all
```

## 依赖关系

### internal 模块依赖

| 模块 | 用途 |
|------|------|
| `internal/config` | Viper 配置加载 |
| `internal/logger` | Zap 日志封装 |
| `internal/naming` | Consul 服务发现与注册 |
| `internal/server` | gRPC Server 封装 |
| `internal/client` | gRPC 连接池 + 弹性客户端（ResilientClient） |
| `internal/trace` | OpenTelemetry 链路追踪 |
| `internal/kim` | 核心 Router/Context/Dispatcher 抽象 |

### 中间件依赖

| 模块 | 用途 |
|------|------|
| `middleware` | Recover 中间件（panic 恢复） |

### 存储依赖

| 模块 | 用途 |
|------|------|
| `storage` | Redis Session 存储实现 |
| Redis | 在线 Session、AccessToken、消息已读位置缓存 |

### 其他服务依赖

| 服务 | 用途 | 调用方式 |
|------|------|----------|
| Logic | 消息持久化、用户/群组数据 | gRPC `LogicService.*` |
| Gateway | 消息推送到客户端 | gRPC `GatewayService.Push` |
| Consul | 服务发现与注册 | HTTP API |

### 协议层依赖

| 模块 | 用途 |
|------|------|
| `wire/pkt` | 客户端协议包（LogicPkt 及各种请求/响应结构） |
| `wire` | 常量定义（命令字/服务名/Meta Key） |
| `gen/rpc` | gRPC Protobuf 生成代码 |
