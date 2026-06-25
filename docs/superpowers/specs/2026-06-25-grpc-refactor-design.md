# kim 项目 gRPC 重构设计方案

> **版本：** v2.0.0
> **日期：** 2026-06-25
> **状态：** 已批准（待规格自检）

---

## 1. 背景与目标

### 1.1 当前架构问题

kim (King IM Cloud) 是一个 Go 实现的高性能分布式即时通信系统。当前架构存在以下问题：

1. **服务间通信用裸 TCP + 自定义 LogicPkt 协议**：Gateway ↔ Comet 通过 `container.Forward` 走 TCP，无服务发现、无负载均衡、无重试/熔断、无链路追踪。
2. **Comet ↔ Logic 用 HTTP + protobuf**：resty HTTP 客户端 + 手动 `proto.Marshal/Unmarshal`，样板代码多，无类型安全。
3. **`container` 包深度耦合 TCP**：维护全局 channel 表、TCP client 池、服务状态机，代码复杂且难维护。
4. **目录结构混乱**：`wire/` 混放 proto 定义、生成代码、协议常量、辅助工具；`services/*/serv/` 职责不清。
5. **go.mod 引入了 grpc v1.81.1 但只用了 `status`/`codes`**，未发挥 gRPC 通信能力。

### 1.2 改造目标

- **服务间通信统一为 gRPC**：Gateway ↔ Comet ↔ Logic 全部用 gRPC，删除裸 TCP 和 HTTP 通信。
- **保留客户端接入层不变**：Client ↔ Gateway 仍用 WebSocket/TCP + LogicPkt 协议。
- **优化目录结构**：按 Go 社区主流布局重组（`cmd/` + `internal/` + `api/` + `pkg/` + `services/`）。
- **建立可观测性基线**：gRPC 拦截器（日志 + 指标 + recovery）+ Prometheus + Zap 日志。

### 1.3 关键决策

| 决策点 | 选择 | 理由 |
|---|---|---|
| 迁移策略 | 一次性重写 | 用户明确选择 |
| 目录结构 | 单二进制 + cmd/internal/api/pkg/services | 当下流行 Go 布局 |
| Gateway↔Comet 通信 | 一元调用（Forward + Push） | 比 stream 简单，IM 推送场景够用 |
| Logic 协议 | 纯 gRPC，删除 HTTP | 内部服务无需 HTTP |
| 配置 | YAML + Cobra，统一结构 | 迁移成本低 |
| 可观测性 | 日志 + 指标 + 拦截器 | 80% 排障需求，无 OTel |

---

## 2. 总体架构

### 2.1 改造前后对比

**改造前：**

```
Client ──WS/TCP──► Gateway ──TCP+LogicPkt──► Comet ──HTTP+protobuf──► Logic
                     ▲                          │
                     └──TCP Push────────────────┘
```

**改造后：**

```
Client ──WS/TCP──► Gateway ──gRPC (Forward)──► Comet ──gRPC──────────► Logic
                     ▲                          │
                     └──gRPC (Push)─────────────┘
```

### 2.2 服务职责

| 服务 | 职责 | 对外协议 |
|---|---|---|
| **Gateway** | 客户端接入、鉴权、消息转发 | WS/TCP（客户端）+ gRPC server（接收 Comet Push）+ gRPC client（调用 Comet） |
| **Comet** | 聊天/登录/群组业务 | gRPC server（接收 Gateway 调用）+ gRPC client（调用 Logic + 调用 Gateway Push） |
| **Logic** | 持久化、消息存储 | gRPC server only |
| **Router** | IP 区域路由 | HTTP（保持不变，纯无状态查询） |

### 2.3 gRPC 服务定义总览

```
┌──────────┐  Forward()         ┌──────────┐  InsertMessage()  ┌──────────┐
│ Gateway  │ ──────────────────►│  Comet   │ ─────────────────►│  Logic   │
│ (client) │                    │ (server) │                   │ (server) │
│          │  Push()            │ (client) │  GetMessage()     │          │
│ (server) │ ◄──────────────────│          │ ─────────────────►│          │
└──────────┘                    └──────────┘                   └──────────┘
```

- **Gateway → Comet**：`CometService.Forward()` 一元调用，转发客户端消息
- **Comet → Gateway**：`GatewayService.Push()` 一元调用，推送消息给客户端（Comet 通过 Consul 查到目标 Gateway，直接 gRPC 调用）
- **Comet → Logic**：`LogicService.InsertMessage()` 等一元调用，替代原 HTTP API
- **Router**：保持 HTTP 不变

### 2.4 核心改造点

1. **删除 `container` 包的 TCP client 逻辑**，改为 gRPC client pool
2. **删除 `dialer.go`**（TCP 握手包不再需要）
3. **删除 Comet 的 `service/message.go` 等 HTTP 客户端**，改为 gRPC client
4. **Logic 删除 Iris HTTP 路由**，改为 gRPC server
5. **保留 `pkt.LogicPkt`** 作为客户端协议（Gateway 入口/出口不变）
6. **保留 `tcp/` `websocket/` 包**（客户端接入层不变）

---

## 3. 目录结构

### 3.1 完整目录树

```
kim/
├── cmd/
│   └── kim/
│       └── main.go                    # 单一入口，Cobra root + 4 子命令
│
├── api/                               # 对外协议定义（单一真相源）
│   └── proto/
│       ├── pkt/                       # 客户端协议（保留现有）
│       │   ├── protocol.proto         # LoginReq/Resp, Message 等
│       │   └── common.proto           # Header, Meta, LogicPkt 字段
│       └── rpc/                       # 服务间 gRPC 协议（新增 service 定义）
│           ├── comet.proto            # CometService
│           ├── gateway.proto          # GatewayService
│           └── logic.proto            # LogicService
│
├── gen/                               # protoc 生成代码（勿手改）
│   ├── pkt/                           # 原 wire/pkt 内容迁移
│   │   ├── protocol.pb.go
│   │   └── common.pb.go
│   └── rpc/                           # 原 wire/rpc + 新增 _grpc.pb.go
│       ├── comet.pb.go
│       ├── comet_grpc.pb.go
│       ├── gateway.pb.go
│       ├── gateway_grpc.pb.go
│       ├── logic.pb.go
│       └── logic_grpc.pb.go
│
├── internal/                          # 内部共享代码（不可外部 import）
│   ├── config/                        # 统一配置加载
│   │   ├── config.go                  # Viper 封装，YAML + env（KIM_ 前缀）
│   │   └── config_test.go
│   ├── logger/                        # Zap 日志统一封装
│   │   ├── logger.go                  # Init/Get/Sugar，按服务名区分
│   │   └── logger_test.go
│   ├── naming/                        # Consul 服务发现
│   │   ├── consul.go                  # 原 naming/consul 迁入
│   │   └── resolver.go                # gRPC Consul resolver（新增）
│   ├── server/                        # gRPC server bootstrap helpers
│   │   ├── grpc.go                    # NewGRPCServer（拦截器、keepalive、tls）
│   │   ├── interceptor.go             # logging/metrics/recovery 拦截器
│   │   └── health.go                  # gRPC health protocol + HTTP /health
│   ├── client/                        # gRPC client pool
│   │   ├── pool.go                    # 连接池 + 负载均衡（round-robin）
│   │   └── pool_test.go
│   └── metrics/                       # Prometheus 指标
│       └── metrics.go                 # 原 serv/metrics.go 迁入
│
├── pkg/                               # 可复用公共库（理论可被外部引用）
│   ├── tcp/                           # 客户端接入层（保留）
│   │   └── ...
│   ├── websocket/                     # 客户端接入层（保留）
│   │   └── ...
│   ├── pkt/                           # LogicPkt 编解码（保留）
│   │   ├── packet.go                  # 原 wire/pkt/packet.go
│   │   └── endian/                    # 原 wire/endian
│   ├── token/                         # JWT（保留）
│   │   └── ...
│   └── kim/                           # 核心接口（Server/Channel/Client）
│       ├── server.go                  # 原 kim 包核心接口
│       ├── channel.go
│       └── client.go
│
├── services/                          # 各服务实现
│   ├── gateway/
│   │   ├── cmd/                       # Cobra 子命令
│   │   │   └── start.go
│   │   ├── conf.yaml
│   │   ├── config.go                  # Gateway 专属配置结构
│   │   ├── handler.go                 # 原 serv/handler.go（Acceptor/Listener）
│   │   ├── forwarder.go               # gRPC client：调用 CometService.Forward
│   │   ├── pusher.go                  # gRPC server：实现 GatewayService.Push
│   │   ├── selector.go                # 原 serv/selector.go（路由选择）
│   │   └── server.go                  # 组装 WS/TCP server + gRPC server
│   │
│   ├── comet/
│   │   ├── cmd/
│   │   │   └── start.go
│   │   ├── conf.yaml
│   │   ├── config.go
│   │   ├── server.go                  # gRPC server：实现 CometService
│   │   ├── handler/                   # 业务 handler（原 comet/handler）
│   │   │   ├── login.go
│   │   │   ├── chat.go
│   │   │   ├── group.go
│   │   │   └── offline.go
│   │   ├── service/                   # Logic gRPC client（原 service/*.go 改造）
│   │   │   ├── logic_client.go        # 封装 LogicService 调用
│   │   │   └── pusher.go              # 调用 GatewayService.Push
│   │   └── router.go                  # 原 kim.Router 迁入
│   │
│   ├── logic/
│   │   ├── cmd/
│   │   │   └── start.go
│   │   ├── conf.yaml
│   │   ├── config.go
│   │   ├── server.go                  # gRPC server：实现 LogicService
│   │   ├── handler/                   # 原 logic/handler 改造为 gRPC handler
│   │   │   ├── message.go
│   │   │   ├── group.go
│   │   │   └── user.go
│   │   └── database/                  # 原 logic/database（保留）
│   │       ├── mysql.go
│   │       ├── redis.go
│   │       ├── model.go
│   │       └── id_generator.go
│   │
│   └── router/                        # 保持 HTTP 不变
│       ├── cmd/
│       │   └── start.go
│       ├── conf.yaml
│       ├── config.go
│       ├── server.go
│       ├── apis/
│       ├── conf/
│       └── ipregion/
│
├── deployments/
│   ├── docker/
│   │   └── Dockerfile                 # 多阶段构建单二进制
│   └── compose/
│       └── docker-compose.yaml        # MySQL/Redis/Consul
│
├── scripts/
│   ├── proto.sh                       # protoc 生成脚本（原 wire/build.sh）
│   └── lint.sh
│
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

### 3.2 关键设计决策

**1. `api/proto` + `gen/` 分离**
- `api/proto/` 是协议的单一真相源（source of truth），只放 `.proto` 文件
- `gen/` 是 protoc 生成代码，禁止手改，CI 校验 `make proto && git diff --exit-code`

**2. `internal/` 强制封装**
- `config`/`logger`/`naming`/`server`/`client`/`metrics` 都是内部共享，不可被外部项目 import
- 服务间不能直接 import 对方的 `services/<svc>/`，只能通过 `gen/rpc/` 的 gRPC client 调用

**3. `pkg/` vs `internal/`**
- `pkg/`：接入层协议（`tcp`/`websocket`/`pkt`/`token`）+ 核心接口（`kim`），理论可复用
- `internal/`：项目内部基础设施，不对外

**4. 服务内部分层**
每个服务统一为：
```
services/<svc>/
├── cmd/start.go       # Cobra 子命令入口
├── conf.yaml          # 配置
├── config.go          # 配置结构 + 加载
├── server.go          # 服务组装（gRPC/WS/TCP server）
├── handler/           # 业务逻辑
└── service/           # 调用其他服务的 client（仅 comet 有）
```

### 3.3 迁移映射表

| 原路径 | 新路径 | 说明 |
|---|---|---|
| `services/main.go` | `cmd/kim/main.go` | 入口 |
| `wire/proto/*.proto` | `api/proto/pkt/*.proto` | 客户端协议 |
| `wire/pkt/*.pb.go` | `gen/pkt/*.pb.go` | 生成代码 |
| `wire/rpc/*.pb.go` | `gen/rpc/*.pb.go` | 生成代码 |
| `wire/pkt/packet.go` | `pkg/pkt/packet.go` | LogicPkt 编解码 |
| `wire/endian/` | `pkg/pkt/endian/` | 字节序辅助 |
| `wire/definitions.go` | `pkg/pkt/commands.go` | 命令字常量 |
| `wire/grpc_helper.go` | 删除 | 用 `internal/server/interceptor.go` 替代 |
| `container/` | 删除 | gRPC client pool 替代 |
| `naming/consul/` | `internal/naming/consul.go` | 迁入 |
| `logger/` | `internal/logger/` | 迁入 |
| `tcp/` `websocket/` | `pkg/tcp/` `pkg/websocket/` | 迁入 |
| `services/gateway/serv/` | `services/gateway/`（拆分） | handler/forwarder/pusher |
| `services/comet/service/*.go` | `services/comet/service/logic_client.go` | HTTP → gRPC |
| `services/logic/handler/*.go` | `services/logic/handler/*.go`（改造） | HTTP → gRPC |

---

## 4. gRPC 协议定义

### 4.1 `api/proto/rpc/gateway.proto` — Gateway 服务

```proto
syntax = "proto3";

package rpc;

option go_package = "github.com/klintcheng/kim/gen/rpc;rpc";

// GatewayService 由 Gateway 实现，Comet 调用
// 用于 Comet 把消息推回 Gateway，再由 Gateway 转发给客户端
service GatewayService {
  // Push 推送消息给指定 channel
  rpc Push(PushReq) returns (PushResp);
}

message PushReq {
  string channel_ids = 1;  // 目标 channel 列表（逗号分隔）
  bytes  packet = 2;       // 客户端协议包（LogicPkt 序列化后的字节）
}

message PushResp {
  int32  code    = 1;  // 0=成功，非 0=失败
  string message = 2;
}
```

**设计要点：**
- `packet` 用 `bytes` 透传 `LogicPkt` 序列化字节，Gateway 不需要反序列化再序列化，零拷贝
- `channel_ids` 逗号分隔，兼容现有 `MetaDestChannels` 协议

### 4.2 `api/proto/rpc/comet.proto` — Comet 服务

```proto
syntax = "proto3";

package rpc;

option go_package = "github.com/klintcheng/kim/gen/rpc;rpc";

// CometService 由 Comet 实现，Gateway 调用
service CometService {
  // Forward 转发客户端消息
  rpc Forward(ForwardReq) returns (ForwardResp);
}

message ForwardReq {
  bytes packet = 1;  // 客户端协议包（LogicPkt 序列化后的字节）
}

message ForwardResp {
  int32  code    = 1;  // 0=成功，非 0=失败
  string message = 2;
  bytes  packet = 3;   // 需要回传给客户端的响应包（如登录响应），为空则无需响应
}
```

### 4.3 `api/proto/rpc/logic.proto` — Logic 服务

```proto
syntax = "proto3";

package rpc;

option go_package = "github.com/klintcheng/kim/gen/rpc;rpc";

// LogicService 由 Logic 实现，Comet 调用
// 替代原 HTTP API，提供消息持久化、群组管理、离线同步
service LogicService {
  // === 消息相关 ===
  rpc InsertUserMessage(InsertMessageReq)   returns (InsertMessageResp);
  rpc InsertGroupMessage(InsertMessageReq)  returns (InsertMessageResp);
  rpc AckMessage(AckMessageReq)             returns (AckMessageResp);

  // === 离线消息 ===
  rpc GetOfflineMessageIndex(GetOfflineMessageIndexReq)     returns (GetOfflineMessageIndexResp);
  rpc GetOfflineMessageContent(GetOfflineMessageContentReq) returns (GetOfflineMessageContentResp);

  // === 群组相关 ===
  rpc GroupCreate(GroupCreateReq)   returns (GroupCreateResp);
  rpc GroupGet(GroupGetReq)         returns (GroupGetResp);
  rpc GroupJoin(GroupJoinReq)       returns (GroupJoinResp);
  rpc GroupQuit(GroupQuitReq)       returns (GroupQuitResp);
  rpc GroupMembers(GroupMembersReq) returns (GroupMembersResp);

  // === 用户相关 ===
  rpc Login(LoginReq) returns (LoginResp);
}

// 复用现有 wire/rpc 中的 message 定义（InsertMessageReq 等）
// 迁移时直接把 wire/rpc/*.proto 的 message 部分搬过来
```

### 4.4 完整 RPC 方法清单

| 调用方 | 被调方 | RPC 方法 | 原实现 | 用途 |
|---|---|---|---|---|
| Gateway | Comet | `CometService.Forward` | `container.Forward` | 转发客户端消息 |
| Comet | Gateway | `GatewayService.Push` | `container.Push` | 推送消息给客户端 |
| Comet | Logic | `LogicService.InsertUserMessage` | HTTP POST `/message/user` | 存单聊消息 |
| Comet | Logic | `LogicService.InsertGroupMessage` | HTTP POST `/message/group` | 存群聊消息 |
| Comet | Logic | `LogicService.AckMessage` | HTTP POST `/message/ack` | 消息已读 |
| Comet | Logic | `LogicService.GetOfflineMessageIndex` | HTTP POST `/offline/index` | 离线索引 |
| Comet | Logic | `LogicService.GetOfflineMessageContent` | HTTP POST `/offline/content` | 离线内容 |
| Comet | Logic | `LogicService.GroupCreate` | HTTP POST `/group` | 建群 |
| Comet | Logic | `LogicService.GroupGet` | HTTP GET `/group/:id` | 查群 |
| Comet | Logic | `LogicService.GroupJoin` | HTTP POST `/group/member` | 加群 |
| Comet | Logic | `LogicService.GroupQuit` | HTTP DELETE `/group/member` | 退群 |
| Comet | Logic | `LogicService.GroupMembers` | HTTP GET `/group/members/:id` | 群成员 |
| Comet | Logic | `LogicService.Login` | HTTP POST `/user/login` | 用户登录 |

### 4.5 protoc 生成脚本 `scripts/proto.sh`

```bash
#!/bin/bash
set -e

PROTO_ROOT="api/proto"
GEN_ROOT="gen"

# 清空旧生成代码
rm -rf ${GEN_ROOT}/pkt ${GEN_ROOT}/rpc
mkdir -p ${GEN_ROOT}/pkt ${GEN_ROOT}/rpc

# 生成客户端协议（pkt）
protoc \
  --proto_path=${PROTO_ROOT}/pkt \
  --go_out=${GEN_ROOT}/pkt \
  --go_opt=paths=source_relative \
  ${PROTO_ROOT}/pkt/*.proto

# 生成服务间协议（rpc，含 gRPC service）
protoc \
  --proto_path=${PROTO_ROOT}/rpc \
  --proto_path=${PROTO_ROOT}/pkt \
  --go_out=${GEN_ROOT}/rpc \
  --go_opt=paths=source_relative \
  --go-grpc_out=${GEN_ROOT}/rpc \
  --go-grpc_opt=paths=source_relative \
  ${PROTO_ROOT}/rpc/*.proto

echo "proto generated to ${GEN_ROOT}/"
```

### 4.6 包路径变更

```go
// 改造前
import "github.com/klintcheng/kim/wire/rpc"

// 改造后
import "github.com/klintcheng/kim/gen/rpc"
```

---

## 5. 各服务实现细节

### 5.1 Gateway 服务

#### 5.1.1 `services/gateway/server.go` — 服务组装

```go
package gateway

import (
    "context"
    "fmt"

    "github.com/klintcheng/kim/internal/config"
    "github.com/klintcheng/kim/internal/logger"
    "github.com/klintcheng/kim/internal/naming"
    "github.com/klintcheng/kim/internal/server"
    "github.com/klintcheng/kim/pkg/kim"
    "github.com/klintcheng/kim/pkg/tcp"
    "github.com/klintcheng/kim/pkg/websocket"
    "github.com/klintcheng/kim/gen/rpc"
)

type Server struct {
    config     *Config
    wsSrv      kim.Server         // 客户端接入（WS/TCP）
    grpcSrv    *server.GRPCServer // 接收 Comet Push
    cometCli   *CometForwarder    // gRPC client 调用 Comet
    consul     naming.Naming
}

func New(ctx context.Context, cfg *Config) (*Server, error) {
    s := &Server{config: cfg}

    // 1. 客户端接入层（保留 WS/TCP）
    service := buildNamingService(cfg)
    var wsSrv kim.Server
    if cfg.Protocol == "tcp" {
        wsSrv = tcp.NewServer(cfg.Listen, service, buildSrvOpts(cfg)...)
    } else {
        wsSrv = websocket.NewServer(cfg.Listen, service, buildSrvOpts(cfg)...)
    }
    handler := NewHandler(cfg.ServiceID, cfg.AppSecret)
    wsSrv.SetAcceptor(handler)
    wsSrv.SetMessageListener(handler)
    wsSrv.SetStateListener(handler)
    s.wsSrv = wsSrv

    // 2. gRPC server：接收 Comet 的 Push 调用
    grpcSrv, err := server.NewGRPCServer(cfg.GRPCListen, server.WithServiceName("gateway"))
    if err != nil {
        return nil, err
    }
    rpc.RegisterGatewayServiceServer(grpcSrv, NewPusher(handler))
    s.grpcSrv = grpcSrv

    // 3. gRPC client：调用 Comet 的 Forward
    s.consul, err = naming.NewConsul(cfg.ConsulURL)
    if err != nil {
        return nil, err
    }
    s.cometCli = NewCometForwarder(cfg.ServiceID, s.consul)

    return s, nil
}

func (s *Server) Start(ctx context.Context) error {
    if err := s.registerConsul(); err != nil {
        return err
    }
    go s.grpcSrv.Start()
    return s.wsSrv.Start()
}

func (s *Server) Stop(ctx context.Context) error {
    s.grpcSrv.GracefulStop()
    return s.wsSrv.Shutdown(ctx)
}
```

#### 5.1.2 `services/gateway/forwarder.go` — gRPC client 调用 Comet

```go
package gateway

import (
    "context"
    "fmt"

    "github.com/klintcheng/kim/gen/rpc"
    "github.com/klintcheng/kim/internal/client"
    "github.com/klintcheng/kim/internal/naming"
    "github.com/klintcheng/kim/pkg/pkt"
)

// CometForwarder 封装对 CometService 的调用，替代原 container.Forward
type CometForwarder struct {
    serviceID string
    pool      *client.Pool
    selector  Selector
}

func NewCometForwarder(serviceID string, ns naming.Naming) *CometForwarder {
    return &CometForwarder{
        serviceID: serviceID,
        pool:      client.NewPool(ns, "chat"),
        selector:  NewRouteSelector(),
    }
}

func (f *CometForwarder) Forward(ctx context.Context, p *pkt.LogicPkt) error {
    targetID, err := f.selector.Lookup(&p.Header)
    if err != nil {
        return err
    }
    conn, err := f.pool.Get(targetID)
    if err != nil {
        return err
    }
    cli := rpc.NewCometServiceClient(conn)
    _, err = cli.Forward(ctx, &rpc.ForwardReq{
        Packet: pkt.Marshal(p),
    })
    return err
}
```

#### 5.1.3 `services/gateway/pusher.go` — gRPC server 实现 Push

```go
package gateway

import (
    "context"
    "strings"

    "github.com/klintcheng/kim/gen/rpc"
    "github.com/klintcheng/kim/pkg/kim"
)

// Pusher 实现 rpc.GatewayServiceServer
type Pusher struct {
    rpc.UnimplementedGatewayServiceServer
    handler *Handler
}

func (p *Pusher) Push(ctx context.Context, req *rpc.PushReq) (*rpc.PushResp, error) {
    err := p.handler.PushToChannels(req.ChannelIds, req.Packet)
    if err != nil {
        return &rpc.PushResp{Code: 1, Message: err.Error()}, nil
    }
    return &rpc.PushResp{Code: 0}, nil
}
```

#### 5.1.4 Handler 改造要点

原 `handler.go` 的 `Accept`/`Receive`/`Disconnect` 逻辑基本不变，改动：

```go
// 改造前
err = container.Forward(wire.SNLogin, req)

// 改造后
err = h.forwarder.Forward(ctx, req)
```

新增 `PushToChannels` 方法（替代原 `container.pushMessage`）：

```go
func (h *Handler) PushToChannels(channelIDs string, packet []byte) error {
    ids := strings.Split(channelIDs, ",")
    for _, id := range ids {
        ch := h.channels.Get(id)
        if ch == nil {
            continue
        }
        _ = ch.WriteFrame(kim.OpBinary, packet)
    }
    return nil
}
```

关键变化：原 `container` 维护全局 channel 表，现在 channel 表下沉到 Gateway 的 `Handler`。

### 5.2 Comet 服务

#### 5.2.1 `services/comet/server.go` — gRPC server

```go
package comet

import (
    "context"

    "github.com/klintcheng/kim/gen/rpc"
    "github.com/klintcheng/kim/internal/client"
    "github.com/klintcheng/kim/internal/naming"
    "github.com/klintcheng/kim/internal/server"
    "github.com/klintcheng/kim/services/comet/handler"
    "github.com/klintcheng/kim/services/comet/service"
)

type Server struct {
    config   *Config
    grpcSrv  *server.GRPCServer
    router   *Router
    logicCli *service.LogicClient
    gwCli    *service.GatewayPusher
}

func New(ctx context.Context, cfg *Config) (*Server, error) {
    s := &Server{config: cfg}

    // 1. gRPC client：调用 Logic
    logicPool := client.NewPool(nil, "royal")
    s.logicCli = service.NewLogicClient(logicPool)

    // 2. gRPC client：调用 Gateway Push
    gwPool := client.NewPool(nil, "wgateway")
    s.gwCli = service.NewGatewayPusher(gwPool)

    // 3. 业务路由
    s.router = buildRouter(s.logicCli)

    // 4. gRPC server：实现 CometService
    grpcSrv, err := server.NewGRPCServer(cfg.Listen, server.WithServiceName("comet"))
    if err != nil {
        return nil, err
    }
    rpc.RegisterCometServiceServer(grpcSrv, NewCometServiceImpl(s.router))
    s.grpcSrv = grpcSrv

    return s, nil
}
```

#### 5.2.2 `services/comet/service/logic_client.go` — 替代原 HTTP client

```go
package service

import (
    "context"

    "github.com/klintcheng/kim/gen/rpc"
    "github.com/klintcheng/kim/internal/client"
)

// LogicClient 封装对 LogicService 的 gRPC 调用，替代原 MessageHttp/GroupHttp/UserHttp
type LogicClient struct {
    pool *client.Pool
}

func NewLogicClient(pool *client.Pool) *LogicClient {
    return &LogicClient{pool: pool}
}

// 实现 service.Message 接口（保持原接口签名）
func (c *LogicClient) InsertUser(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
    conn, err := c.pool.GetAny()
    if err != nil {
        return nil, err
    }
    cli := rpc.NewLogicServiceClient(conn)
    return cli.InsertUserMessage(context.Background(), req)
}

// InsertGroup / SetAck / GetMessageIndex / GetMessageContent 同理
```

关键收益：删除 `MessageHttp`/`GroupHttp`/`UserHttp` 三个 HTTP client 类，删除所有 `proto.Marshal` + `resty.Post` + `proto.Unmarshal` 样板。

#### 5.2.3 `services/comet/service/pusher.go` — 调用 Gateway Push

```go
package service

import (
    "context"

    "github.com/klintcheng/kim/gen/rpc"
    "github.com/klintcheng/kim/internal/client"
)

// GatewayPusher 封装对 GatewayService.Push 的调用，替代原 container.Push
type GatewayPusher struct {
    pool *client.Pool
}

func (p *GatewayPusher) Push(gatewayServiceID, channelIDs string, packet []byte) error {
    conn, err := p.pool.Get(gatewayServiceID)
    if err != nil {
        return err
    }
    cli := rpc.NewGatewayServiceClient(conn)
    _, err = cli.Push(context.Background(), &rpc.PushReq{
        ChannelIds: channelIDs,
        Packet:     packet,
    })
    return err
}
```

#### 5.2.4 CometServiceImpl — gRPC handler 入口

```go
package comet

import (
    "context"

    "github.com/klintcheng/kim/gen/rpc"
    "github.com/klintcheng/kim/pkg/pkt"
)

type CometServiceImpl struct {
    rpc.UnimplementedCometServiceServer
    router   *Router
    pusher   *service.GatewayPusher
}

func (s *CometServiceImpl) Forward(ctx context.Context, req *rpc.ForwardReq) (*rpc.ForwardResp, error) {
    p, err := pkt.Unmarshal(req.Packet)
    if err != nil {
        return &rpc.ForwardResp{Code: 1, Message: err.Error()}, nil
    }
    resp := s.router.Handle(ctx, p, s.pusher)
    if resp != nil {
        return &rpc.ForwardResp{Code: 0, Packet: pkt.Marshal(resp)}, nil
    }
    return &rpc.ForwardResp{Code: 0}, nil
}
```

#### 5.2.5 业务 handler 改造

原 `comet/handler/*.go` 的 handler 签名保持不变：

```go
func (h *ChatHandler) DoUserTalk(ctx context.Context, packet *pkt.LogicPkt) (*pkt.LogicPkt, error)
```

内部调用从 `h.messageService.InsertUser(...)`（HTTP）变为 `h.logicCli.InsertUser(...)`（gRPC），handler 代码几乎不动。

### 5.3 Logic 服务

#### 5.3.1 `services/logic/server.go` — gRPC server

```go
package logic

import (
    "context"

    "github.com/klintcheng/kim/gen/rpc"
    "github.com/klintcheng/kim/internal/server"
    "github.com/klintcheng/kim/services/logic/database"
    "github.com/klintcheng/kim/services/logic/handler"
)

type Server struct {
    config  *Config
    grpcSrv *server.GRPCServer
}

func New(ctx context.Context, cfg *Config) (*Server, error) {
    s := &Server{config: cfg}

    // 初始化 DB/Redis/IDGen（原逻辑保留）
    db, err := database.InitDb(cfg.Driver, cfg.BaseDb)
    // ... 原有初始化逻辑

    // gRPC server：实现 LogicService
    grpcSrv, err := server.NewGRPCServer(cfg.Listen, server.WithServiceName("logic"))
    if err != nil {
        return nil, err
    }
    h := &handler.ServiceHandler{
        BaseDb:    db,
        MessageDb: messageDb,
        Idgen:     idgen,
        Cache:     rdb,
    }
    rpc.RegisterLogicServiceServer(grpcSrv, h)
    s.grpcSrv = grpcSrv
    return s, nil
}
```

#### 5.3.2 handler 改造

改造前（Iris HTTP）：

```go
func (h *ServiceHandler) InsertUserMessage(ctx iris.Context) {
    req := &rpc.InsertMessageReq{}
    body, _ := ctx.GetBody()
    proto.Unmarshal(body, req)
    resp, _ := h.insertUserMessage(ctx, req)
    body, _ = proto.Marshal(resp)
    ctx.Write(body)
}
```

改造后（gRPC）：

```go
func (h *ServiceHandler) InsertUserMessage(ctx context.Context, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
    return h.insertUserMessage(ctx, req)
}
```

关键收益：删除所有 HTTP 样板，handler 方法签名直接由 proto 生成，类型安全。

#### 5.3.3 删除的内容

- `services/logic/server.go` 中的 `newApp()`、`setAllowedResponses()`
- `iris` 依赖（go.mod 移除）
- `resty` 依赖（go.mod 移除）
- `/health` HTTP 端点改由 `internal/server/health.go` 统一提供

### 5.4 Router 服务

完全不变。Router 是纯 HTTP 查询服务，无状态，不参与消息流转，保持 Iris HTTP。

### 5.5 `internal/server/grpc.go` — gRPC server 公共封装

```go
package server

import (
    "net"
    "time"

    "google.golang.org/grpc"
    "google.golang.org/grpc/health"
    healthpb "google.golang.org/grpc/health/grpc_health_v1"
    "google.golang.org/grpc/keepalive"
    "google.golang.org/grpc/reflection"
)

type GRPCServer struct {
    *grpc.Server
    addr string
}

type Option func(*options)

type options struct {
    serviceName string
}

func WithServiceName(name string) Option {
    return func(o *options) { o.serviceName = name }
}

func NewGRPCServer(addr string, opts ...Option) (*GRPCServer, error) {
    o := &options{}
    for _, opt := range opts { opt(o) }

    s := grpc.NewServer(
        grpc.UnaryInterceptor(UnaryChain(
            RecoveryInterceptor,
            LoggingInterceptor(o.serviceName),
            MetricsInterceptor,
        )),
        grpc.KeepaliveParams(keepalive.ServerParameters{
            Time:    30 * time.Second,
            Timeout: 10 * time.Second,
        }),
    )

    hs := health.NewServer()
    hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
    healthpb.RegisterHealthServer(s, hs)
    reflection.Register(s)

    return &GRPCServer{Server: s, addr: addr}, nil
}

func (s *GRPCServer) Start() error {
    lis, err := net.Listen("tcp", s.addr)
    if err != nil {
        return err
    }
    return s.Serve(lis)
}
```

### 5.6 `internal/client/pool.go` — gRPC client 连接池

```go
package client

import (
    "fmt"
    "sync"

    "github.com/klintcheng/kim/internal/naming"
    "google.golang.org/grpc"
)

// Pool 管理对某类服务的 gRPC 连接，替代原 container 的 TCP client 管理
type Pool struct {
    naming      naming.Naming
    serviceName string
    mu          sync.RWMutex
    conns       map[string]*grpc.ClientConn
    rr          *roundRobin
}

func NewPool(ns naming.Naming, serviceName string) *Pool {
    p := &Pool{
        naming:      ns,
        serviceName: serviceName,
        conns:       make(map[string]*grpc.ClientConn),
        rr:          newRoundRobin(),
    }
    go p.watch()
    return p
}

// Get 按 serviceID 精确获取连接
func (p *Pool) Get(serviceID string) (*grpc.ClientConn, error) {
    p.mu.RLock()
    conn, ok := p.conns[serviceID]
    p.mu.RUnlock()
    if !ok {
        return nil, fmt.Errorf("service %s not found", serviceID)
    }
    return conn, nil
}

// GetAny round-robin 选一个连接
func (p *Pool) GetAny() (*grpc.ClientConn, error) {
    p.mu.RLock()
    defer p.mu.RUnlock()
    if len(p.conns) == 0 {
        return nil, fmt.Errorf("no available %s instance", p.serviceName)
    }
    id := p.rr.Next(p.allIDs())
    return p.conns[id], nil
}
```

### 5.7 改造前后代码量对比

| 模块 | 改造前 | 改造后 | 变化 |
|---|---|---|---|
| `container/` | ~500 行 | 0（删除） | -500 |
| `services/comet/service/*.go`（HTTP client） | ~450 行 | ~150 行（gRPC client） | -300 |
| `services/logic/server.go` + handler | ~400 行 | ~250 行 | -150 |
| `services/gateway/serv/dialer.go` | ~50 行 | 0（删除） | -50 |
| `internal/server/` + `internal/client/` | 0 | ~300 行（新增） | +300 |
| gRPC 拦截器 | 0 | ~100 行 | +100 |
| **总计** | | | **-600 行** |

---

## 6. 启动方式与配置

### 6.1 单二进制入口 `cmd/kim/main.go`

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "syscall"

    "github.com/klintcheng/kim/services/comet"
    "github.com/klintcheng/kim/services/gateway"
    "github.com/klintcheng/kim/services/logic"
    "github.com/klintcheng/kim/services/router"
    "github.com/spf13/cobra"
)

const version = "v2.0.0"

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(),
        syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    root := &cobra.Command{
        Use:     "kim",
        Version: version,
        Short:   "King IM Cloud - Distributed IM System",
        CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
    }
    root.PersistentFlags().StringP("config", "c", "", "global config file")

    root.AddCommand(gateway.NewStartCmd(ctx, version))
    root.AddCommand(comet.NewStartCmd(ctx, version))
    root.AddCommand(logic.NewStartCmd(ctx, version))
    root.AddCommand(router.NewStartCmd(ctx, version))

    if err := root.ExecuteContext(ctx); err != nil {
        fmt.Fprintf(os.Stderr, "kim: %v\n", err)
        os.Exit(1)
    }
}
```

### 6.2 各服务子命令统一模板

以 `services/gateway/cmd/start.go` 为例：

```go
package cmd

import (
    "context"
    "time"

    "github.com/klintcheng/kim/services/gateway"
    "github.com/spf13/cobra"
)

func NewStartCmd(ctx context.Context, version string) *cobra.Command {
    var (
        configPath string
        protocol   string
        routePath  string
    )

    cmd := &cobra.Command{
        Use:   "gateway",
        Short: "Start the gateway service (client access + gRPC server)",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg, err := gateway.LoadConfig(configPath)
            if err != nil {
                return err
            }
            cfg.Protocol = protocol
            cfg.RoutePath = routePath

            srv, err := gateway.New(ctx, cfg)
            if err != nil {
                return err
            }

            go func() {
                <-ctx.Done()
                shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
                defer cancel()
                _ = srv.Stop(shutdownCtx)
            }()

            return srv.Start(ctx)
        },
    }

    cmd.Flags().StringVarP(&configPath, "config", "c",
        "services/gateway/conf.yaml", "config file")
    cmd.Flags().StringVarP(&protocol, "protocol", "p",
        "ws", "client protocol: ws or tcp")
    cmd.Flags().StringVarP(&routePath, "route", "r",
        "services/gateway/route.json", "route table file")

    return cmd
}
```

### 6.3 配置结构设计

#### 6.3.1 `internal/config/config.go` — 统一加载器

```go
package config

import (
    "fmt"
    "strings"

    "github.com/spf13/viper"
)

// Load 加载配置文件 + 环境变量覆盖
// 环境变量格式：KIM_<FIELD>，如 KIM_CONSUL_URL
func Load(path string, out interface{}) error {
    v := viper.New()
    v.SetConfigFile(path)
    v.SetEnvPrefix("KIM")
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
    v.AutomaticEnv()

    if err := v.ReadInConfig(); err != nil {
        return fmt.Errorf("load config %s: %w", path, err)
    }
    if err := v.Unmarshal(out); err != nil {
        return fmt.Errorf("unmarshal config: %w", err)
    }
    return nil
}
```

#### 6.3.2 `services/gateway/config.go` — Gateway 配置

```go
package gateway

type Config struct {
    // === 服务标识 ===
    ServiceID    string `mapstructure:"service_id"`
    ServiceName  string `mapstructure:"service_name"`
    PublicAddress string `mapstructure:"public_address"`
    PublicPort   int    `mapstructure:"public_port"`

    // === 客户端接入 ===
    Listen    string `mapstructure:"listen"`
    Protocol  string `mapstructure:"protocol"`
    RoutePath string `mapstructure:"route_path"`

    // === gRPC ===
    GRPCListen string `mapstructure:"grpc_listen"`

    // === 业务 ===
    AppSecret string `mapstructure:"app_secret"`
    Domain    string `mapstructure:"domain"`
    Tags      []string `mapstructure:"tags"`

    // === 基础设施 ===
    ConsulURL       string `mapstructure:"consul_url"`
    LogLevel        string `mapstructure:"log_level"`
    Kafka           KafkaConfig `mapstructure:"kafka"`
    MonitorPort     int    `mapstructure:"monitor_port"`
    ConnectionGPool int    `mapstructure:"connection_gpool"`
    MessageGPool    int    `mapstructure:"message_gpool"`
}

type KafkaConfig struct {
    Brokers []string `mapstructure:"brokers"`
    Topic   string   `mapstructure:"topic"`
}

func LoadConfig(path string) (*Config, error) {
    var cfg Config
    if err := config.Load(path, &cfg); err != nil {
        return nil, err
    }
    if cfg.ServiceName == "" {
        cfg.ServiceName = "wgateway"
    }
    return &cfg, nil
}
```

#### 6.3.3 `services/gateway/conf.yaml` — 配置文件示例

```yaml
service_id: "gateway-1"
service_name: "wgateway"
public_address: "127.0.0.1"
public_port: 8000

listen: ":8000"
protocol: "ws"
route_path: "services/gateway/route.json"

grpc_listen: ":9000"

app_secret: ""
domain: "im.example.com"
tags: ["zone-hz"]

consul_url: "http://127.0.0.1:8500"
log_level: "info"
monitor_port: 8001
connection_gpool: 1000
message_gpool: 1000

kafka:
  brokers: ["127.0.0.1:9092"]
  topic: "kim-logs"
```

#### 6.3.4 各服务配置对比

| 配置项 | gateway | comet | logic | router |
|---|---|---|---|---|
| `listen` | WS/TCP :8000 | gRPC :9001 | gRPC :9002 | HTTP :8100 |
| `grpc_listen` | :9000（接收 Push） | —（listen 即 gRPC） | —（listen 即 gRPC） | — |
| `consul_url` | 是 | 是 | 是 | 是 |
| `redis_addrs` | — | 是 | 是 | — |
| `base_db` / `message_db` | — | — | 是 | — |
| `route_path` | 是 | — | — | — |
| `data_path` | — | — | — | 是 |

端口规划：
- 8000-8004：客户端接入（WS/TCP）
- 9000-9004：gRPC 服务间通信
- 8001-8004 +1：Prometheus 监控

### 6.4 Makefile

```makefile
.PHONY: all build proto test lint run-all stop-all docker-up docker-down

VERSION ?= v2.0.0
BIN_DIR := bin
SERVICES := gateway comet logic router

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN_DIR)/kim ./cmd/kim

proto:
	@bash scripts/proto.sh

proto-check: proto
	@git diff --exit-code gen/ || (echo "gen/ is out of date, run 'make proto'" && exit 1)

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

docker-up:
	docker compose -f deployments/compose/docker-compose.yaml up -d

docker-down:
	docker compose -f deployments/compose/docker-compose.yaml down

run-all: build
	@for svc in $(SERVICES); do \
		$(BIN_DIR)/kim $$svc -c services/$$svc/conf.yaml > /tmp/kim-$$svc.log 2>&1 & \
		echo "Started $$svc (pid $$!)"; \
	done

run-%: build
	$(BIN_DIR)/kim $* -c services/$*/conf.yaml

run-%-fg: build
	$(BIN_DIR)/kim $* -c services/$*/conf.yaml

stop-all:
	@for svc in $(SERVICES); do \
		pkill -f "kim $$svc" 2>/dev/null || true; \
	done

status:
	@ps aux | grep "kim " | grep -v grep || echo "No kim services running"

logs-%:
	tail -f /tmp/kim-$*.log

clean:
	rm -rf $(BIN_DIR) coverage.out coverage.html
```

### 6.5 Dockerfile

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /kim ./cmd/kim

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai
COPY --from=builder /kim /usr/local/bin/kim
ENTRYPOINT ["kim"]
CMD ["--help"]
```

### 6.6 docker-compose.yaml

```yaml
version: "3.8"

services:
  mysql:
    image: mysql:8.0
    ports: ["3306:3306"]
    environment:
      MYSQL_ROOT_PASSWORD: root
      MYSQL_DATABASE: kim
    volumes:
      - mysql_data:/var/lib/mysql

  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]

  consul:
    image: hashicorp/consul:1.16
    ports: ["8500:8500", "8600:8600/udp"]
    command: agent -dev -client=0.0.0.0

volumes:
  mysql_data:
```

### 6.7 环境变量覆盖

所有配置项都支持环境变量覆盖，规则：`KIM_<UPPER_FIELD>`

```bash
KIM_CONSUL_URL=http://consul:8500 ./bin/kim gateway
KIM_BASE_DB="user:pass@tcp(mysql:3306)/kim?parseTime=true" ./bin/kim logic
```

---

## 7. 迁移步骤与风险控制

### 7.1 迁移阶段总览

```
阶段 1: 协议层重构 (api/gen)         ──┐
阶段 2: 基础设施层 (internal/)         │  每阶段结束
阶段 3: Logic 服务 gRPC 化             │  都能 go build
阶段 4: Comet 服务 gRPC 化             │  + go test
阶段 5: Gateway 服务 gRPC 化           │
阶段 6: 删除旧代码 + 目录清理          ──┘
阶段 7: 集成测试 + 文档
```

### 7.2 阶段 1：协议层重构

**目标：** 建立 `api/proto` + `gen/` 目录，定义所有 gRPC service

**步骤：**
1. 创建 `api/proto/pkt/`，把 `wire/proto/*.proto` 迁入
2. 创建 `api/proto/rpc/`，把 `wire/rpc/rpc.proto` 的 message 部分迁入 `logic.proto`
3. 新增 `api/proto/rpc/logic.proto` 的 `service LogicService` 定义
4. 新增 `api/proto/rpc/comet.proto` 的 `service CometService`
5. 新增 `api/proto/rpc/gateway.proto` 的 `service GatewayService`
6. 编写 `scripts/proto.sh`
7. 运行生成，得到 `gen/pkt/` + `gen/rpc/`（含 `_grpc.pb.go`）

**验证：** `make proto && go build ./gen/...`

**风险：** proto 语法错误、go_package 配置不当
**应对：** 先在隔离分支验证生成，确认无 import cycle

### 7.3 阶段 2：基础设施层

**目标：** 建立 `internal/` 共享代码

**步骤：**
1. `internal/config/`：迁移 Viper 封装
2. `internal/logger/`：迁移 Zap 封装
3. `internal/naming/`：迁移 Consul 集成，新增 gRPC resolver
4. `internal/server/grpc.go`：实现 `GRPCServer` 封装
5. `internal/server/interceptor.go`：实现 logging/metrics/recovery 拦截器
6. `internal/server/health.go`：实现 gRPC Health + HTTP /health
7. `internal/client/pool.go`：实现 gRPC 连接池 + round-robin
8. `internal/metrics/`：迁移 Prometheus 指标

**验证：** `go build ./internal/... && go test ./internal/...`

**风险：** gRPC resolver 与 Consul 集成复杂
**应对：** 先用静态地址验证，再接 Consul

### 7.4 阶段 3：Logic 服务 gRPC 化

**目标：** Logic 从 HTTP 改为 gRPC server

**步骤：**
1. 创建 `services/logic/config.go`、`services/logic/cmd/start.go`
2. 改造 `services/logic/server.go`：删除 Iris，改为 gRPC server
3. 改造 `services/logic/handler/*.go`：方法签名从 `func(ctx iris.Context)` 改为 `func(ctx context.Context, req *rpc.XxxReq) (*rpc.XxxResp, error)`
4. 更新 `services/logic/conf.yaml`
5. 从 go.mod 移除 `iris` 依赖

**验证：** `go build ./services/logic/...` + grpcurl 测试

**风险：** handler 方法签名变更可能遗漏
**应对：** 编译器强制检查接口实现

### 7.5 阶段 4：Comet 服务 gRPC 化

**目标：** Comet 双向 gRPC（client 调 Logic + server 接 Gateway + client 调 Gateway Push）

**步骤：**
1. 创建 `services/comet/config.go`、`services/comet/cmd/start.go`
2. 改造 `services/comet/server.go`：删除 TCP server，改为 gRPC server
3. 创建 `services/comet/service/logic_client.go`：实现 `service.Message`/`Group`/`User` 接口
4. 创建 `services/comet/service/pusher.go`：实现 `GatewayPusher`
5. 创建 `services/comet/comet_service_impl.go`：实现 `CometService.Forward`
6. 改造 `services/comet/handler/*.go`：handler 签名不变，内部调用走 gRPC
7. 从 go.mod 移除 `resty` 依赖

**验证：** `go build ./services/comet/...` + grpcurl 测试

**风险：** Comet 的 `kim.Router` 依赖 `container`，需解耦
**应对：** 把 `kim.Router` 迁入 `services/comet/router.go`

### 7.6 阶段 5：Gateway 服务 gRPC 化

**目标：** Gateway 保留 WS/TCP 接入 + 新增 gRPC

**步骤：**
1. 创建 `services/gateway/config.go`、`services/gateway/cmd/start.go`
2. 改造 `services/gateway/server.go`：保留 WS/TCP，新增 gRPC server
3. 创建 `services/gateway/forwarder.go`：实现 `CometForwarder`
4. 创建 `services/gateway/pusher.go`：实现 `GatewayService.Push`
5. 改造 `services/gateway/handler.go`：`container.Forward` → `h.forwarder.Forward`
6. 迁移 `services/gateway/serv/selector.go` → `services/gateway/selector.go`
7. 删除 `services/gateway/serv/dialer.go`

**验证：** `go build ./services/gateway/...` + 端到端测试

**风险：** channel 表从 `container` 全局下沉到 Gateway 本地
**应对：** channel 表用 `sync.Map`，单元测试覆盖并发场景

### 7.7 阶段 6：删除旧代码 + 目录清理

**目标：** 删除所有废弃代码，确保目录干净

**步骤：**
1. 删除 `wire/` 目录
2. 删除 `container/` 目录
3. 删除 `naming/` 目录
4. 删除 `logger/` 目录
5. 删除 `services/*/serv/` 目录
6. 删除 `services/main.go`
7. 更新所有 import 路径
8. 运行 `go mod tidy`
9. 更新 `CLAUDE.md`

**验证：** `go build ./... && go test -race ./... && make proto-check && make lint`

### 7.8 阶段 7：集成测试 + 文档

**目标：** 端到端验证 + 文档更新

**步骤：**
1. 编写集成测试：启动 4 个服务，WebSocket 客户端登录+聊天
2. 验证 Consul 服务发现
3. 验证优雅退出
4. 更新 `CLAUDE.md` 和 `README.md`

### 7.9 风险矩阵

| 风险 | 概率 | 影响 | 应对措施 |
|---|---|---|---|
| proto 生成失败/import cycle | 中 | 高 | 阶段 1 隔离验证 |
| gRPC resolver 与 Consul 不兼容 | 中 | 高 | 先用静态地址，再接 Consul |
| Comet handler 签名变更遗漏 | 低 | 中 | 编译器强制检查 |
| Gateway channel 表下沉并发问题 | 中 | 高 | 用 `sync.Map`，并发测试 |
| 优雅退出时连接泄漏 | 中 | 中 | 集成测试验证 |
| 性能回退 | 低 | 中 | 压测对比，keepalive 优化 |

### 7.10 回滚策略

**Git 层面：**
- 在 `feature/grpc-refactor` 分支进行所有工作
- 每个阶段一个 commit，便于 bisect
- 主分支保持 `v1` 可用状态

**部署层面：**
- 保留 `v1` 镜像 tag
- 灰度发布：先 logic，再 comet，最后 gateway

### 7.11 验收标准

**功能验收：**
- [ ] WebSocket 客户端能登录、发消息、收消息
- [ ] TCP 客户端能登录、发消息、收消息
- [ ] 单聊消息正确送达
- [ ] 群聊消息正确送达所有群成员
- [ ] 离线消息同步正常
- [ ] 群组创建/加入/退出正常
- [ ] 消息已读 ACK 正常

**非功能验收：**
- [ ] `go build ./...` 无错误
- [ ] `go test -race -cover ./...` 覆盖率不低于改造前
- [ ] `make proto-check` 通过
- [ ] `make lint` 无 error
- [ ] Consul 服务注册/健康检查正常
- [ ] `kill -TERM` 优雅退出
- [ ] gRPC 拦截器日志/指标正常输出
- [ ] grpcurl 能反射调用所有 RPC 方法

**性能验收：**
- [ ] 单聊消息端到端延迟 < 50ms（本地环境）
- [ ] 1000 并发连接稳定运行 10 分钟
- [ ] gRPC 调用 P99 延迟 < 10ms

### 7.12 工作量估算

| 阶段 | 预估工时 |
|---|---|
| 阶段 1：协议层 | 1-2 天 |
| 阶段 2：基础设施 | 2-3 天 |
| 阶段 3：Logic | 1-2 天 |
| 阶段 4：Comet | 2-3 天 |
| 阶段 5：Gateway | 2-3 天 |
| 阶段 6：清理 | 1 天 |
| 阶段 7：测试 | 1-2 天 |
| **总计** | **10-16 天**（单人全职） |

---

## 8. 不在本次范围内

以下内容明确排除在本次重构之外：

- **OpenTelemetry 链路追踪**：可观测性仅做到日志+指标+拦截器，不引入 OTel
- **数据库 schema 变更**：不涉及 DB 迁移
- **客户端协议变更**：LogicPkt 协议保持不变，客户端无感
- **Router 服务改造**：保持 HTTP 不变
- **K8s 部署配置**：仅提供 Dockerfile + docker-compose，不写 K8s manifests
- **grpc-gateway**：不为 Logic 保留 HTTP API
