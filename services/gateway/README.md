# Gateway 服务 - WS/TCP 接入网关

## 模块概述

Gateway 是 kim 系统的客户端接入网关服务，负责处理客户端的 WebSocket 和 TCP 连接。它作为系统的第一道门户，承担以下职责：

- **连接管理**：接收客户端 WS/TCP 连接，维护在线 Channel
- **登录鉴权**：验证客户端 JWT Token，处理登录/登出流程
- **消息转发**：将客户端消息按区域路由转发到对应的 Comet 服务
- **消息推送**：接收 Comet 服务的推送请求，将消息下发到客户端连接
- **指标监控**：收集 Prometheus 指标，暴露监控端点

服务默认监听端口：WS/TCP `:8000`，gRPC `:9001`，Monitor HTTP `:8001`。

## 架构设计

Gateway 服务采用分层设计：

```
┌─────────────────────────────────────────────────────────────┐
│                      客户端 (WS/TCP)                         │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                  WS/TCP Server (websocket/tcp)              │
│               - Acceptor (鉴权/登录)                         │
│               - MessageListener (消息接收)                    │
│               - StateListener (连接状态)                      │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                        Handler 层                            │
│         ┌─────────────┐    ┌─────────────┐                  │
│         │  Handler     │    │  Selector   │                  │
│         │ (业务处理)    │    │ (区域路由)   │                  │
│         └──────┬───────┘    └──────┬──────┘                  │
└────────────────┼───────────────────┼─────────────────────────┘
                 │                   │
┌────────────────▼───────────────────▼─────────────────────────┐
│                    CometForwarder (gRPC Client)              │
│                - 连接池管理 (client.Pool)                     │
│                - 弹性保护 (熔断/重试/限流/超时)                 │
└──────────────────────────┬──────────────────────────────────┘
                           │ gRPC Forward
┌──────────────────────────▼──────────────────────────────────┐
│                       Comet 服务                             │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│               gRPC Server (接收 Comet Push)                  │
│                    ┌─────────────┐                          │
│                    │   Pusher    │                          │
│                    │ (消息下发)   │                          │
│                    └──────┬──────┘                          │
└───────────────────────────┼─────────────────────────────────┘
                            │ Push
┌───────────────────────────▼─────────────────────────────────┐
│                    WS/TCP Server                            │
│                 - 写入客户端 Channel                         │
└─────────────────────────────────────────────────────────────┘
```

## 关键组件

### 核心结构体

```go
// Server Gateway 服务实例
type Server struct {
    config        *Config              // 服务配置
    routePath     string               // 路由配置文件路径
    protocol      string               // 接入协议 (ws/tcp)
    wsSrv         kim.Server           // 客户端接入（WS/TCP）
    grpcSrv       *server.GRPCServer   // 接收 Comet Push
    forwarder     *CometForwarder      // gRPC client 调用 Comet
    naming        naming.Naming        // Consul 服务发现
    log           *logger.Logger       // 日志实例
    traceShutdown func()               // 链路追踪关闭函数
}
```

### Handler - 业务处理器

`services/gateway/handler/handler.go`

```go
// Handler Gateway 业务处理器，实现 kim.Acceptor / kim.MessageListener / kim.StateListener 接口
type Handler struct {
    ServiceID string
    AppSecret string
    Forwarder Forwarder  // 接口解耦，便于测试
}
```

主要方法：
- `Accept(conn, timeout)` - 连接接收：读取登录包 → JWT 验证 → 生成 ChannelID → Forward 到 Comet 登录服务
- `Receive(ag, payload)` - 消息接收：Ping→Pong 处理 / LogicPkt Forward 到对应 Comet 服务
- `Disconnect(id)` - 连接断开：Forward SignOut 登出消息到 Comet

### RouteSelector - 区域路由选择器

`services/gateway/handler/selector.go`

```go
// RouteSelector 区域路由选择器
type RouteSelector struct {
    route *conf.Route
}
```

路由策略：
1. 白名单优先：App 命中白名单则固定到指定 Zone
2. 权重分片：按 App/Account 哈希计算 Slot，映射到对应 Zone
3. 区域内哈希：Zone 内多个 Comet 节点，按 Account 哈希选择具体节点
4. 降级策略：Zone 内无可用服务时随机选择全局节点

### CometForwarder - 消息转发器

`services/gateway/forwarder.go`

```go
type CometForwarder struct {
    ns        naming.Naming        // Consul 服务发现
    pool      *client.Pool         // gRPC 连接池
    selector  *handler.RouteSelector  // 路由选择器
    gatewayID string               // 当前 Gateway ID
    cfg       config.ResilienceConfig  // 弹性配置
}
```

通过 gRPC 连接池调用 Comet 服务的 `CometService.Forward` 接口，内置弹性保护（熔断、重试、限流、超时）。

### Pusher - 消息推送器

`services/gateway/pusher.go`

```go
// Pusher 实现 rpc.GatewayServiceServer 接口，接收 Comet 推送的消息
type Pusher struct {
    rpc.UnimplementedGatewayServiceServer
    pushFn func(channelID string, data []byte) error  // wsSrv.Push
}
```

实现 `GatewayService.Push` 方法，将 Comet 推送的消息拆分 ChannelIds，逐个调用 pushFn 写入客户端 Channel。

### 监控指标

`services/gateway/handler/metrics.go`

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `kim_message_in_total` | CounterVec | serviceId, serviceName, command | 网关接收消息总数 |
| `kim_message_in_flow_bytes` | CounterVec | serviceId, serviceName, command | 网关接收消息字节数 |
| `kim_no_server_found_error_total` | CounterVec | zone | Zone 服务查找失败次数 |

## gRPC 接口

### 提供的 RPC 服务

Gateway 作为 gRPC Server，提供以下服务供 Comet 调用：

| 服务 | 方法 | 说明 |
|------|------|------|
| `GatewayService` | `Push` | 接收 Comet 推送的消息，下发到客户端 Channel |

Push 请求结构：
```go
type PushReq struct {
    ChannelIds string  // 逗号分隔的 ChannelID 列表
    Packet     []byte  // 序列化后的 LogicPkt
}
```

### 调用的下游服务

Gateway 作为 gRPC Client，调用 Comet 服务：

| 下游服务 | 方法 | 说明 |
|----------|------|------|
| `CometService` | `Forward` | 转发客户端消息到 Comet 处理 |

Forward 请求结构：
```go
type ForwardReq struct {
    Packet []byte  // 序列化后的 LogicPkt
}
```

## 配置说明

配置文件：`services/gateway/conf.yaml`

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `service_id` | string | `gate01` | 服务唯一 ID，需保证全局唯一 |
| `service_name` | string | `wgateway` | Consul 注册服务名 |
| `listen` | string | `:8000` | WS/TCP 监听地址 |
| `grpc_listen` | string | `:9001` | gRPC 服务监听地址 |
| `grpc_port` | int | `9001` | gRPC 服务端口（Consul 注册用） |
| `public_address` | string | `127.0.0.1` | 服务对外公布地址 |
| `public_port` | int | `8000` | WS/TCP 对外端口 |
| `tags` | []string | - | Consul 服务标签（如 IDC 标识） |
| `domain` | string | - | 客户端接入域名 |
| `consul_url` | string | `http://127.0.0.1:8500` | Consul 地址 |
| `monitor_port` | int | `8001` | Prometheus/Health 监控端口 |
| `app_secret` | string | - | **必填** JWT 签名密钥 |
| `log_level` | string | `info` | 日志级别 |
| `message_g_pool` | int | `5000` | 消息处理协程池大小 |
| `connection_g_pool` | int | `15000` | 连接处理协程池大小 |
| `protocol` | string | `ws` | 接入协议：ws 或 tcp |
| `kafka.enable` | bool | `false` | 是否启用 Kafka 日志 |
| `kafka.brokers` | []string | - | Kafka broker 列表 |
| `kafka.topic` | string | `kim_logs` | Kafka 日志主题 |
| `resilience.*` | object | - | 弹性保护配置（熔断/重试/超时/限流） |
| `trace.enable` | bool | `false` | 是否启用链路追踪 |
| `trace.exporter` | string | `otlp` | 追踪导出器：otlp/stdout/noop |
| `trace.endpoint` | string | `127.0.0.1:4317` | OTLP Collector 地址 |
| `trace.sampling_ratio` | float | `0.1` | 采样率 (0.0~1.0) |
| `grpc.tls_enable` | bool | `false` | 是否启用 TLS |
| `grpc.auth_enable` | bool | `false` | 是否启用 gRPC 认证 |

### 路由配置

路由配置文件（JSON）通过 `--route/-r` 参数指定，默认 `services/gateway/route.json`，包含：

- `route_by`: 路由维度（account/app）
- `zones`: 区域列表（ID + 权重）
- `whitelist`: App 白名单（指定 App 固定路由到某个 Zone）

## 启动方式

### 命令行启动

通过 kim 主程序启动：

```bash
# 默认配置启动
./bin/kim gateway

# 指定配置和路由文件
./bin/kim gateway -c services/gateway/conf.yaml -r services/gateway/route.json

# 使用 TCP 协议
./bin/kim gateway -p tcp
```

### 命令行参数

| 参数 | 短参 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `services/gateway/conf.yaml` | 配置文件路径 |
| `--route` | `-r` | `services/gateway/route.json` | 路由配置文件路径 |
| `--protocol` | `-p` | `ws` | 接入协议：ws 或 tcp |

### Make 命令

```bash
# 前台启动调试
make run-gateway-fg

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
| `internal/server` | gRPC Server 封装（拦截器/健康检查） |
| `internal/client` | gRPC 连接池 + 弹性客户端 |
| `internal/trace` | OpenTelemetry 链路追踪 |
| `internal/kim` | 核心 Server/Agent/Router 抽象接口 |

### 其他服务依赖

| 服务 | 用途 | 调用方式 |
|------|------|----------|
| Comet | 业务逻辑处理 | gRPC `CometService.Forward` |
| Consul | 服务发现与注册 | HTTP API |

### 协议层依赖

| 模块 | 用途 |
|------|------|
| `wire/pkt` | 客户端协议包（LogicPkt） |
| `wire/token` | JWT Token 解析 |
| `wire` | 常量定义（命令字/服务名/Meta Key） |
| `gen/rpc` | gRPC Protobuf 生成代码 |
| `websocket` / `tcp` | WS/TCP 接入层实现 |
| `storage` | Redis 会话存储 |
