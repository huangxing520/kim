# CLAUDE.md

## 这是什么
kim (King IM Cloud) 是一个 Go 实现的高性能分布式即时通信系统，支持 TCP/WebSocket 接入、单聊/群聊、离线消息同步。服务间通信统一使用 gRPC。

## 技术栈
- Go 1.26 · gobwas/ws (WebSocket) · gRPC + Protobuf · Consul (服务发现)
- MySQL (GORM) · Redis (go-redis v7) · Kafka (日志)
- Cobra (CLI) · Viper (配置) · Zap (日志) · Prometheus (指标)

## 架构
单一二进制 `bin/kim`，通过 Cobra 子命令启动 4 个服务（入口 [cmd/kim/main.go](cmd/kim/main.go)，版本 `v2.0.0`）：
- `gateway` — WS/TCP 接入网关 (:8000) + gRPC 服务端 (:9001)，按 zone 路由消息到 comet
- `comet` — 聊天/登录/群组业务，gRPC 服务端 (:8005)，作为 gRPC client 调用 logic 与 gateway
- `logic` — gRPC 服务端 (:9002，服务名 `royal`) + MySQL 持久化
- `router` — IP 区域路由 (Iris HTTP :8100)

**服务间通信**（全部 gRPC unary）：
- Gateway → Comet：`CometService.Forward`（[services/gateway/forwarder.go](services/gateway/forwarder.go)）
- Comet → Gateway：`GatewayService.Push`（[services/comet/service/pusher.go](services/comet/service/pusher.go)）
- Comet → Logic：`LogicService.*`（[services/comet/service/logic_client.go](services/comet/service/logic_client.go)）

**客户端协议保持不变**：消息包 `pkt.LogicPkt`，魔数 `MagicLogicPkt`，命令常量 `Command*`。

## 目录结构
```
cmd/kim/              # 单二进制入口（Cobra root + 4 个子命令）
api/proto/            # proto 源文件
  ├─ pkt/             # 客户端协议（common.proto / protocol.proto）
  └─ rpc/             # 服务间 gRPC（gateway.proto / comet.proto / logic.proto）
gen/                  # protoc 生成代码
  ├─ pkt/             # 客户端协议 Go 代码
  └─ rpc/             # gRPC service + message Go 代码
internal/             # 共享基础设施（不可被外部引用）
  ├─ config/          # Viper 配置加载（KIM_ 前缀环境变量覆盖）
  ├─ logger/          # Zap 日志封装（含 init() 初始化 CommonLogger）
  ├─ naming/          # Consul 服务发现 + gRPC resolver
  ├─ server/          # gRPC server 封装（拦截器/健康检查/reflection）
  ├─ client/          # gRPC 连接池（round-robin + Consul watch）
  └─ metrics/         # Prometheus 指标
services/             # 各服务实现
  ├─ <svc>/cmd/       # Cobra 子命令（start.go）
  ├─ <svc>/config.go  # 服务配置结构 + LoadConfig
  ├─ <svc>/server.go  # 服务启动/停止逻辑
  └─ <svc>/...        # 业务 handler / service
wire/                 # 客户端协议层（保留，勿删）
```

## 关键约定
- **服务名常量**在 [wire/definitions.go](wire/definitions.go)（`SNWGateway`/`SNChat`/`SNService` 等），**勿改名**——Consul 服务发现依赖它们。
- **协议**：消息包 `pkt.LogicPkt`，魔数 `MagicLogicPkt`；命令常量 `Command*`（如 `chat.user.talk`）；Meta key 用 `dest.server`/`dest.channels`。
- **配置**：YAML + 环境变量（前缀 `KIM_`），文件在 `services/<svc>/conf.yaml`，snake_case 键名，通过 `internal/config.Load` 加载。
- **日志**：用 `logger.<Svc>Logger`（如 `GatewayLogger`），**仅在服务初始化后使用**——`internal/logger` 包的 `init()` 会用默认配置初始化 `CommonLogger`，但各服务专用 logger 需在 Start 阶段赋值。
- **gRPC 类型统一**：所有服务间 RPC 类型使用 `gen/rpc` 包（从 `api/proto/rpc/*.proto` 生成）。`wire/rpc` 已删除，避免 protobuf 全局注册冲突。
- **Channel 表归属**：`DefaultServer.ChannelMap` 管理所有在线 channel；`Pusher.Push` 通过 `wsSrv.Push(channelId, data)` 下发。
- **循环依赖解耦**：`serv.Handler` 通过 `Forwarder` 接口调用 gateway 的 `CometForwarder`（见 [services/gateway/serv/handler.go](services/gateway/serv/handler.go)）。

## 不要动
- `wire/proto/{common,protocol}.proto`（需 `wire/build.sh` 重新生成）
- `wire/pkt/*.pb.go`（`rpc.proto` 已删除，`wire/rpc/` 已删除）
- `api/proto/**/*.proto`（需 `scripts/proto.sh` 重新生成 `gen/`）
- `gen/**/*.pb.go`（自动生成）
- 服务名常量（`wire/definitions.go` 中的 `SN*`）

## 怎么运行
```bash
make docker-up        # 启动 MySQL/Redis/Consul
make run-all          # 后台启动 4 个服务
make status           # 查看状态
make logs-gateway     # 看日志
make stop-all && make docker-down
```
单服务调试：`make run-<svc>-fg`。测试：`make test`。

## 重新生成 proto
```bash
bash scripts/proto.sh   # 重新生成 gen/ 下的 gRPC 代码
cd wire && bash build.sh # 重新生成 wire/ 下的客户端协议代码
```
