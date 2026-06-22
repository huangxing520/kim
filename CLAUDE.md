# CLAUDE.md

## 这是什么
kim (King IM Cloud) 是一个 Go 实现的高性能分布式即时通信系统，支持 TCP/WebSocket 接入、单聊/群聊、离线消息同步。

## 技术栈
- Go 1.26 · Iris (HTTP) · gobwas/ws (WebSocket) · gRPC + Protobuf
- MySQL (GORM) · Redis (go-redis v7) · Consul (服务发现) · Kafka (日志)
- Cobra (CLI) · Viper + envconfig (配置) · Zap (日志) · Prometheus (指标)

## 架构
单一二进制 `bin/kim`，通过子命令启动 4 个服务（见 [services/main.go](services/main.go)）：
- `gateway` (:8000) — WS/TCP 接入网关，按 zone 路由
- `comet` (:8005) — 聊天/登录/群组业务，连接 gateway
- `logic` (:8080) — HTTP API + MySQL 持久化 (服务名 `royal`)
- `router` (:8100) — IP 区域路由

服务名常量在 [wire/definitions.go](wire/definitions.go)（`SNWGateway`/`SNChat`/`SNService` 等），**勿改名**——服务间发现依赖它们。

## 关键约定
- **包结构**：根包 `kim` 定义核心接口（`Server`/`Channel`/`Dispatcher`），`container` 是运行时容器，`wire` 是协议层，`services/<svc>/` 是各服务实现。
- **协议**：消息包 `pkt.LogicPkt`，魔数 `MagicLogicPkt`；命令常量 `Command*`（如 `chat.user.talk`）；Meta key 用 `dest.server`/`dest.channels`。
- **配置**：YAML + 环境变量（前缀 `KIM_`），文件在 `services/<svc>/conf.yaml`。
- **日志**：用 `logger.<Svc>Logger`（如 `GatewayLogger`），**仅在服务初始化后使用**——`conf.Init` 阶段未初始化，改用标准 `log` 包（已踩坑）。
- **服务状态**：`container` 用 `StateYoung`→`StateAdult` 两阶段，新发现服务延迟 10s 才可路由。
- **不要动**：`wire/proto/*.proto`（需 `wire/build.sh` 重新生成）、`wire/pkt/*.pb.go`、服务名常量。

## 怎么运行
```bash
make docker-up        # 启动 MySQL/Redis/Consul
make run-all          # 后台启动 4 个服务
make status           # 查看状态
make logs-gateway     # 看日志
make stop-all && make docker-down
```
单服务调试：`make run-<svc>-fg`。测试：`make test`。
