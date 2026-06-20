# KIM (King IM Cloud) 技术栈与项目架构

## 项目简介

KIM 是一个基于 Go 语言开发的高性能分布式即时通信系统，采用微服务架构，支持 WebSocket 和 TCP 双协议接入，使用 Protocol Buffers 进行消息序列化，通过 Consul 实现服务注册与发现，Redis 做会话存储，MySQL 做业务数据持久化。

---

## 项目架构

### 整体架构图

```
客户端 (Web SDK / Flutter SDK)
  │
  ├── HTTP GET /api/lookup/:token ──→ [Router 路由查询服务] ── 返回最优 Gateway 域名
  │
  └── WS/TCP 长连接 ────────────────→ [Gateway 网关服务] ── Token 验证、路由选择、消息转发
                                        │
                                        └── TCP 内部长连接 → [Server 业务服务 (login/chat)] ── 业务逻辑处理
                                                                │
                                                                └── HTTP + Protobuf → [Service/Royal 数据服务] ── 数据持久化
                                                                                        ├── MySQL (baseDb + messageDb)
                                                                                        └── Redis (已读索引 + AccessToken)
```

### 四大微服务

| 服务名 | 内部代号 | 端口 | 职责 |
|--------|----------|------|------|
| Gateway | gateway | 可配置 | 面向客户端的连接接入层，管理 WS/TCP 长连接、JWT 认证、消息转发与路由选择 |
| Server | chat / login | 可配置 | IM 核心业务处理，包括登录/登出、单聊、群聊、消息确认、离线消息同步 |
| Service | royal | 可配置 | 数据持久化层，以 HTTP REST API 提供用户管理、群组管理、消息存储（写扩散模型） |
| Router | router | 8100 | HTTP 路由查询，基于 IP 地理位置为用户分配就近接入的 Gateway |

### 消息转发全链路

```
[入站]
Client (WS/TCP)
  → Gateway.Channel.Readloop          帧读取 + 协程池分发
    → Gateway.Handler.Receive         反序列化、注入 Meta(app/account)
      → container.Forward             路由选择 + TCP 发送
        → RouteSelector.Lookup        CRC32 哈希 → Zone → 选定 Server 节点

[Server 处理]
Server.Channel.Readloop               从 Gateway 连接读帧
  → ServHandler.Receive               查询 Redis Session
    → Router.Serve                    按 Command 路由到 Handler
      → DoUserTalk / DoGroupTalk      业务处理: 保存消息 → 查位置 → 推送

[出站]
ctx.Resp() / ctx.Dispatch()           构造响应/推送包
  → ServerDispatcher.Push             注入 MetaDestChannels
    → container.Push                  注入 MetaDestServer → TCP 发送到 Gateway
      → Gateway.readLoop              读取回传消息
        → pushMessage                 解析目标 ChannelId → 清除路由 Meta
          → Gateway.Channel.writeloop WriteFrame + Flush → 送达 Client
```

### 目录结构

```
kim/
├── server.go                 # 核心接口定义 (Server, Client, Channel, Conn, Frame 等)
├── default_server.go         # Server 默认实现 (TCP 监听、协议升级、连接管理)
├── channel.go                # Channel 实现 (读写循环、异步写缓冲、心跳)
├── channels.go               # ChannelMap (sync.Map 实现的连接集合)
├── context.go                # 请求上下文 (中间件链、Resp/Dispatch)
├── router.go                 # 消息路由器 (命令路由 + 中间件 + sync.Pool)
├── dispatcher.go             # Dispatcher 接口定义
├── storage.go                # SessionStorage 接口定义
├── location.go               # Location 结构 (ChannelId + GateId)
├── event.go                  # 一次性事件通知
├── net.go                    # 网络工具 (IP 获取)
├── metrics.go                # Prometheus 监控指标
│
├── container/                # 服务编排引擎 (生命周期管理、服务发现、消息转发)
├── wire/                     # 协议层
│   ├── pkt/                  # 数据包结构 (LogicPkt, BasicPkt, 序列化/反序列化)
│   ├── proto/                # Protobuf 定义文件 (.proto)
│   ├── endian/               # 大端序二进制读写工具
│   ├── token/                # JWT 认证
│   ├── rpc/                  # RPC 定义
│   ├── definitions.go        # 全局命令常量与服务名常量
│   └── seq.go                # 序列号生成器
│
├── tcp/                      # TCP 传输层实现
├── websocket/                # WebSocket 传输层实现
├── naming/                   # 服务注册与发现接口
│   └── consul/               # Consul 实现
├── middleware/               # 中间件 (Recover)
├── logger/                   # 日志模块 (logrus + 按天滚动)
├── storage/                  # Redis 会话存储实现
├── report/                   # 压测报告统计
├── data/                     # 运行时日志输出目录
│
├── services/                 # 四大微服务
│   ├── main.go               # 统一入口 (cobra 命令)
│   ├── gateway/              # 网关服务
│   │   ├── server.go         # 启动入口
│   │   ├── conf/             # 配置与路由规则
│   │   └── serv/             # 业务处理 (Accept/Receive/Disconnect/Selector)
│   ├── server/               # 业务逻辑服务
│   │   ├── server.go         # 启动入口与路由注册
│   │   ├── conf/             # 配置
│   │   ├── serv/             # TCP 连接处理 (Accept/Receive/Dispatcher)
│   │   ├── handler/          # 业务 Handler (login/chat/group/offline)
│   │   └── service/          # 对 Royal 服务的 HTTP 客户端
│   ├── service/              # 数据持久化服务 (Royal)
│   │   ├── server.go         # 启动入口与 API 路由注册
│   │   ├── conf/             # 配置
│   │   ├── database/         # 数据模型 (GORM)、MySQL/Redis 初始化、ID 生成器
│   │   └── handler/          # API Handler (message/group/user)
│   └── router/               # 路由查询服务
│       ├── server.go         # 启动入口
│       ├── conf/             # 配置与国家-Region 映射
│       ├── apis/             # Lookup API
│       └── ipregion/         # IP 地理位置查询
│
└── examples/                 # 示例与测试
    ├── dialer/               # 客户端登录封装
    ├── echo/                 # Echo 客户端示例
    ├── mock/                 # Mock 服务端与客户端
    ├── kimbench/             # 基准测试工具 (单聊/群聊/登录压测)
    ├── unittest/             # 端到端集成测试
    └── benchmark/            # 性能测试
```

---

## 技术栈总览

| 类别 | 技术 | 版本 | 用途 |
|------|------|------|------|
| 语言 | Go | 1.16+ | 主开发语言 |
| 消息序列化 | Protocol Buffers | v2 (google.golang.org/protobuf v1.31.0) | 消息体序列化/反序列化 |
| 实时通信 | WebSocket (gobwas/ws) | v1.0.4 | 客户端 WebSocket 长连接 |
| 实时通信 | TCP | 标准库 net | 客户端 TCP 长连接 + 服务间内部通信 |
| 远程调用 | gRPC | v1.58.2 | gRPC 辅助工具 |
| HTTP 框架 | Iris | v12.2.0-alpha8 | Router 和 Service(Royal) 的 HTTP API 服务 |
| HTTP 客户端 | Resty | v2.6.0 | Server 调用 Service(Royal) 的 HTTP 请求 |
| 服务注册与发现 | Consul | api v1.8.1 | 服务注册、健康检查、服务发现 |
| 缓存/会话存储 | Redis (go-redis) | v7.4.0 | Session 存储、消息已读索引、AccessToken 缓存 |
| ORM | GORM | v1.21.15 | MySQL 数据库 ORM 操作 |
| 数据库 | MySQL | gorm.io/driver/mysql v1.1.1 | 用户/群组/消息持久化存储 |
| CLI 框架 | Cobra | v0.0.5 | 命令行参数解析与子命令管理 |
| 配置管理 | Viper | v1.7.1 | YAML/环境变量配置文件加载 |
| 配置管理 | envconfig | v1.4.0 | 环境变量映射到结构体 |
| 日志 | Logrus | v1.7.0 | 结构化日志输出 |
| 日志滚动 | file-rotatelogs | v2.4.0 | 日志文件按天滚动切割 |
| 日志 Hook | lfshook | — | 将日志输出到文件 |
| 监控 | Prometheus client_golang | v1.11.1 | 连接数、消息流量等指标采集 |
| 协程池 | ants | v2.4.6 | goroutine 复用，控制并发数量 |
| ID 生成 | Snowflake | v0.3.0 | 雪花算法生成全局唯一消息 ID |
| ID 生成 | KSUID | v1.0.3 | 生成连接 ChannelID |
| 认证 | JWT (dgrijalva/jwt-go) | v3.2.0 | 用户身份认证 Token 生成与解析 |
| IP 定位 | ip2region | v2.2.0 | 离线 IP 地理位置查询 (Router 就近接入) |
| 缓冲池 | gobwas/pool | v0.2.1 | bufio.Reader/Writer 对象池复用 |
| 测试断言 | testify | v1.8.3 | 单元测试与集成测试断言 |
| Mock 测试 | golang/mock | v1.6.0 | 接口 Mock 生成 (gomock) |
| 容器化 | Docker + docker-compose | — | 服务打包与编排部署 |

---

## 技术栈详细使用说明

### Protocol Buffers — 消息序列化

Protocol Buffers 是项目的核心序列化方案，所有业务消息（登录、聊天、群组、Session 等）均通过 Protobuf 定义并编解码。

**使用文件：**

- `wire/pkt/protocol.pb.go` — Protobuf 生成的协议消息（Header、Meta、Status、LoginReq/Resp、MessageReq/Resp/Push、GroupCreateReq/Resp、Session、MessageIndex 等）
- `wire/pkt/common.pb.go` — Protobuf 生成的公共消息结构
- `wire/rpc/rpc.pb.go` — Protobuf 生成的 RPC 消息（Server 与 Service 间的 HTTP+Protobuf 通信）
- `wire/pkt/packet.go` — LogicPkt 和 BasicPkt 的 Body 使用 `proto.Marshal` / `proto.Unmarshal` 进行编解码
- `context.go` — Context 的 `Resp()`、`Dispatch()`、`ReadBody()` 方法中使用 Protobuf 消息
- `services/server/serv/handler.go` — Server 端消息反序列化
- `services/server/service/user.go` — 用户服务 RPC 请求/响应
- `services/server/service/message.go` — 消息服务 RPC 请求/响应
- `services/server/service/group.go` — 群组服务 RPC 请求/响应
- `services/gateway/serv/dialer.go` — Gateway 拨号器内部握手
- `storage/redis_impl.go` — Session 序列化存储到 Redis

### WebSocket (gobwas/ws) — 客户端长连接

高性能零分配的 WebSocket 库，用于客户端与 Gateway 之间的实时通信。

**使用文件：**

- `default_server.go` — 引入 `ws.DefaultServerReadBufferSize` / `WriteBufferSize` 常量
- `websocket/server.go` — Upgrader 实现，调用 `ws.Upgrade()` 完成 HTTP 到 WebSocket 协议升级
- `websocket/connection.go` — WsConn 封装，使用 `ws.ReadFrame()` 读帧、`wsutil.WriteServerMessage()` 写帧
- `websocket/client.go` — WebSocket 客户端，使用 `wsutil.WriteClientMessage()` 发送带 Mask 的客户端消息
- `examples/dialer/client_dialer.go` — 示例客户端拨号器
- `examples/mock/client.go` — Mock 客户端实现

### TCP — 传输层通信

标准库 `net` 包提供的 TCP 能力，用于客户端 TCP 接入和服务间内部通信。

**使用文件：**

- `tcp/server.go` — TCP Upgrader，将原始 `net.Conn` 包装为 `TcpConn`
- `tcp/connection.go` — TcpConn 实现，帧格式为 1 字节 OpCode + 4 字节长度前缀 + Payload
- `tcp/client.go` — TCP 客户端实现，支持心跳和连接状态管理
- `default_server.go` — `net.Listen("tcp", ...)` 监听端口
- `container/container.go` — 服务间 TCP 拨号与连接管理

### Consul — 服务注册与发现

基于 HashiCorp Consul 实现微服务间的注册、健康检查与动态发现。

**使用文件：**

- `naming/consul/naming.go` — Consul Naming 接口实现：`Register()` 注册服务（含 HTTP 健康检查），`Find()` 查询服务实例，`Subscribe()` 基于阻塞查询的长轮询变更监听，`Deregister()` 注销服务
- `naming/naming.go` — 定义 `Naming` 抽象接口
- `naming/service.go` — `DefaultService` 结构体实现 `ServiceRegistration` 接口

### Redis (go-redis) — 缓存与会话存储

Redis 在项目中承担三重角色：Session 会话存储、消息已读索引维护、AccessToken 缓存。

**使用文件：**

- `storage/redis_impl.go` — `RedisStorage` 实现 `SessionStorage` 接口（Add/Delete/Get/GetLocations）
- `storage/redis_test.go` — Redis 存储基准测试
- `services/service/database/redis.go` — Redis 初始化与键名约定（`chat:ack:{account}`）
- `services/service/handler/message_handler.go` — 消息已读确认写入 Redis
- `services/service/conf/config.go` — Redis 连接配置
- `services/server/conf/config.go` — Server 的 Redis 配置

### GORM + MySQL — 数据持久化

GORM 作为 ORM 框架，配合 MySQL 驱动，用于 Service(Royal) 服务的数据持久化操作。

**使用文件：**

- `services/service/database/mysql.go` — MySQL 数据库初始化，配置表名前缀 `t_`、慢查询阈值
- `services/service/database/model.go` — GORM 数据模型定义：`User`、`Group`、`GroupMember`、`MessageIndex`、`MessageContent`
- `services/service/server.go` — 双数据库初始化（baseDb + messageDb）
- `services/service/handler/message_handler.go` — 消息写入（写扩散模型）、离线消息查询
- `services/service/handler/group_handler.go` — 群组 CRUD（事务保证一致性）

### Iris — HTTP 框架

Iris 高性能 HTTP 框架用于 Router 服务和 Service(Royal) 服务的 REST API。

**使用文件：**

- `services/router/server.go` — Router 服务启动 Iris HTTP 服务（端口 8100）
- `services/router/apis/router.go` — Lookup API（IP 查询 → Region → IDC → Gateway 域名列表）
- `services/service/server.go` — Service(Royal) 启动 Iris HTTP 服务，注册 API 路由
- `services/service/handler/user_handler.go` — 用户登录验证 API
- `services/service/handler/message_handler.go` — 消息存储 API
- `services/service/handler/group_handler.go` — 群组管理 API
- `services/service/conf/config.go` — Iris AccessLog 中间件配置

### Cobra — CLI 框架

Cobra 命令行框架用于所有微服务的统一启动入口和示例程序的子命令管理。

**使用文件：**

- `services/main.go` — 微服务统一入口，注册 gateway / server / royal 子命令
- `services/gateway/server.go` — Gateway 服务启动命令
- `services/server/server.go` — Server 服务启动命令
- `services/service/server.go` — Service(Royal) 启动命令
- `services/router/server.go` — Router 服务启动命令
- `examples/main.go` — 示例程序入口，注册 echo / mock_cli / mock_srv / benchmark 子命令
- `examples/mock/cmd.go` — Mock 客户端与服务端子命令
- `examples/kimbench/cmd.go` — 压测工具子命令（user / group / login）
- `examples/echo/echo.go` — Echo 示例子命令

### Viper — 配置管理

Viper 用于加载各微服务的 YAML 配置文件。

**使用文件：**

- `services/gateway/conf/config.go` — Gateway 配置加载
- `services/server/conf/config.go` — Server 配置加载
- `services/service/conf/config.go` — Service(Royal) 配置加载
- `services/router/conf/config.go` — Router 配置加载

### envconfig — 环境变量配置

envconfig 用于将环境变量映射到 Go 结构体，与 Viper 配合完成配置管理。

**使用文件：**

- `services/gateway/conf/config.go`
- `services/server/conf/config.go`
- `services/service/conf/config.go`
- `services/router/conf/config.go`

### Logrus — 结构化日志

Logrus 作为底层日志库，通过 `logger` 包封装为全局统一接口。

**使用文件：**

- `logger/logger.go` — 全局日志实例，暴露 Trace/Debug/Info/Warn/Error/Fatal 各级别函数
- `logger/setting.go` — 日志初始化（按天滚动、保留天数、text/json 格式）
- `services/router/server.go` — Router 服务日志
- `services/router/apis/router.go` — Lookup API 日志
- `services/service/database/redis.go` — Redis 连接日志
- `services/service/conf/config.go` — 配置加载日志
- `services/server/conf/config.go` — 配置加载日志
- `services/gateway/conf/route.go` — 路由规则加载日志

### file-rotatelogs + lfshook — 日志滚动

file-rotatelogs 实现日志文件按天滚动切割，lfshook 将 Logrus 日志输出到文件。

**使用文件：**

- `logger/logger.go` — 初始化 rotatelogs 和 lfshook
- `logger/setting.go` — 配置滚动策略（按天切割、保留 7 天）

### Prometheus — 监控指标

Prometheus client_golang 用于采集和暴露系统监控指标。

**使用文件：**

- `metrics.go` — 定义 `kim_channel_total` GaugeVec（网关并发连接数，按 serviceId/serviceName 标签）
- `container/container.go` — 暴露 `/metrics` 端点
- `container/metrics.go` — 定义 `kim_message_out_flow_bytes` Counter（网关下发消息字节数）
- `services/gateway/serv/metrics.go` — 定义消息接收总数、消息流量、Zone 查找失败次数等指标

### ants — 协程池

ants 协程池用于控制并发 goroutine 数量并复用 goroutine，避免高并发场景下的资源耗尽。

**使用文件：**

- `default_server.go` — 创建消息处理协程池 `ants.NewPool()`
- `channel.go` — `Readloop` 中通过 `gpool.Submit()` 异步提交消息处理任务
- `examples/benchmark/server_test.go` — 性能测试并发连接
- `examples/kimbench/usertalk.go` — 单聊压测并发控制
- `examples/kimbench/login.go` — 登录压测并发控制
- `examples/kimbench/grouptalk.go` — 群聊压测并发控制

### Snowflake — 雪花 ID 生成器

基于 Twitter Snowflake 算法生成全局唯一的 int64 消息 ID。

**使用文件：**

- `services/service/database/id_generator.go` — ID 生成器初始化与接口封装
- `services/service/handler/group_handler.go` — 群组创建时使用雪花 ID

### KSUID — 连接 ID 生成

KSUID (K-Sortable Unique Identifier) 用于生成连接 ChannelID。

**使用文件：**

- `default_server.go` — 默认 Acceptor 使用 `ksuid.New()` 生成 Channel ID
- `channels_test.go` — Channel 测试
- `storage/redis_test.go` — 存储测试
- `services/service/handler/message_handler_test.go` — 消息处理测试
- `services/router/api_test.go` — Router API 测试
- `services/gateway/serv/selector_test.go` — 路由选择器测试
- `examples/mock/cmd.go` — Mock 服务

### JWT — 身份认证

基于 JWT (JSON Web Token) 实现用户身份认证，HS256 签名算法。

**使用文件：**

- `wire/token/jwt.go` — `Token` 结构（Account、App、Exp、Password、AccessToken），`Generate()` 生成 Token，`Parse()` 解析验证

### Resty — HTTP 客户端

Resty HTTP 客户端用于 Server 服务调用 Service(Royal) 的 REST API。

**使用文件：**

- `services/server/server.go` — 初始化 Resty 客户端
- `services/server/service/user.go` — 用户登录验证 HTTP 请求
- `services/server/service/message.go` — 消息存储 HTTP 请求（InsertUser/InsertGroup/SetAck/GetIndex/GetContent）
- `services/server/service/group.go` — 群组管理 HTTP 请求（Create/Members/Join/Quit/Detail）
- `services/server/service/message_test.go` — 消息服务测试
- `services/router/api_test.go` — Router API 测试

### ip2region — IP 地理定位

离线 IP 地理位置查询库，用于 Router 服务判断用户所在区域以实现就近接入。

**使用文件：**

- `services/router/ipregion/ipregion.go` — 封装 ip2region 内存搜索，返回国家、省份、城市、ISP 信息

### gRPC — 辅助工具

gRPC 在项目中仅用于辅助错误处理。

**使用文件：**

- `wire/grpc_helper.go` — 提供 `IsGrpcError()` 函数，判断 error 是否为指定 gRPC status code

### gobwas/pool — 缓冲池

bufio Reader/Writer 对象池，减少内存分配。

**使用文件：**

- `default_server.go` — `pbufio.GetReader()` / `pbufio.GetWriter()` 获取缓冲读写器，连接关闭时归还

### testify — 测试断言

项目中广泛使用的测试断言库，覆盖单元测试与集成测试。

**使用文件：**

- `channels_test.go` — Channel 管理测试
- `wire/token/jwt_test.go` — JWT Token 测试
- `wire/pkt/read_write_test.go` — 数据包读写测试
- `wire/pkt/packet_test.go` — 数据包结构测试
- `storage/redis_test.go` — Redis 存储测试
- `services/service/handler/message_handler_test.go` — 消息处理测试
- `services/server/service/group_test.go` — 群组服务测试
- `services/server/service/message_test.go` — 消息服务测试
- `services/router/api_test.go` — Router API 测试
- `services/router/apis/router_test.go` — Router 逻辑测试
- `services/router/ipregion/ipregion_test.go` — IP 定位测试
- `services/gateway/serv/selector_test.go` — 路由选择器测试
- `naming/consul/naming_test.go` — Consul 服务发现测试
- `examples/unittest/login_test.go` — 登录流程集成测试
- `examples/unittest/offline_test.go` — 离线消息集成测试
- `examples/unittest/chat_test.go` — 聊天流程集成测试

### golang/mock — Mock 测试

gomock 用于生成接口 Mock 实现，支持单元测试中隔离依赖。

**使用文件：**

- `storage_mock.go` — `SessionStorage` 接口的 Mock 实现
- `server_mock.go` — `Server` 接口的 Mock 实现
- `dispatcher_mock.go` — `Dispatcher` 接口的 Mock 实现
- `channels_test.go` — 使用 Mock 进行 Channel 测试

### Docker + docker-compose — 容器化部署

项目提供完整的 Docker 支持，包括中间件环境和微服务本身的容器化。

**使用文件：**

- `docker-compose.yml` — 中间件环境编排（MySQL、Consul、Redis）
- `docker-compose-kim.yml` — KIM 微服务编排（Gateway、Server、Royal）
- `Dockerfile_gateway` — Gateway 服务镜像构建
- `Dockerfile_router` — Router 服务镜像构建
- `Dockerfile_royal` — Service(Royal) 服务镜像构建
- `Dockerfile_server` — Server 服务镜像构建
- `build_images.sh` — 批量构建镜像脚本

---

## 核心设计模式

**接口驱动：** `server.go` 定义全部基础抽象（Server、Channel、Client、Conn、Frame、Acceptor、MessageListener、StateListener、Dialer），具体实现通过接口注入。

**中间件链模式：** `Router` + `Context` + `HandlerFunc` 构成类似 Gin 的中间件链式处理模型，`ctx.Next()` 驱动链式调用。

**函数选项模式：** `ServerOption` 通过 `WithMessageGPool()`、`WithConnectionGPool()` 等选项函数灵活配置 Server。

**对象池复用：** `sync.Pool` 复用 Context 对象（Router）、`ants.Pool` 复用 goroutine（消息处理）、`gobwas/pool` 复用 bufio（连接缓冲）。

**协议透明切换：** `tcp/` 和 `websocket/` 共享框架接口（Server、Client、Conn），业务层无需感知底层传输协议差异。

**写扩散消息存储：** 每条消息为每个接收方各写一条 `MessageIndex`，离线消息通过 Redis 维护已读位点。

**一致性路由：** Gateway 通过 CRC32 哈希 + 权重 Slot 算法，保证同一用户的消息始终路由到同一后端 Server 节点。
