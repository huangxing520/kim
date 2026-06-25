# kim gRPC 重构实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 将 kim 项目服务间通信从 TCP+HTTP 统一为 gRPC，重组目录结构为 Go 社区主流布局。

**架构：** Client ↔ Gateway 保持 WS/TCP；Gateway ↔ Comet ↔ Logic 改为 gRPC 一元调用（Forward/Push/业务 RPC）；删除 container 包，用 gRPC client pool 替代。

**技术栈：** Go 1.26 · gRPC + Protobuf · Consul · Viper · Zap · Cobra · Prometheus

**规格文档：** `docs/superpowers/specs/2026-06-25-grpc-refactor-design.md`

---

## 文件结构总览

### 新建文件

| 文件 | 职责 |
|---|---|
| `cmd/kim/main.go` | 单二进制入口 |
| `api/proto/pkt/protocol.proto` | 客户端协议（从 wire/proto 迁入） |
| `api/proto/pkt/common.proto` | 客户端协议（从 wire/proto 迁入） |
| `api/proto/rpc/gateway.proto` | GatewayService 定义 |
| `api/proto/rpc/comet.proto` | CometService 定义 |
| `api/proto/rpc/logic.proto` | LogicService 定义 |
| `scripts/proto.sh` | protoc 生成脚本 |
| `internal/config/config.go` | 统一配置加载 |
| `internal/logger/logger.go` | Zap 日志封装 |
| `internal/naming/consul.go` | Consul 服务发现 |
| `internal/naming/resolver.go` | gRPC Consul resolver |
| `internal/server/grpc.go` | gRPC server 封装 |
| `internal/server/interceptor.go` | gRPC 拦截器 |
| `internal/server/health.go` | 健康检查 |
| `internal/client/pool.go` | gRPC 连接池 |
| `internal/metrics/metrics.go` | Prometheus 指标 |
| `services/gateway/cmd/start.go` | Gateway Cobra 子命令 |
| `services/gateway/config.go` | Gateway 配置 |
| `services/gateway/forwarder.go` | gRPC client 调 Comet |
| `services/gateway/pusher.go` | gRPC server 接 Push |
| `services/comet/cmd/start.go` | Comet Cobra 子命令 |
| `services/comet/config.go` | Comet 配置 |
| `services/comet/service/logic_client.go` | gRPC client 调 Logic |
| `services/comet/service/pusher.go` | gRPC client 调 Gateway Push |
| `services/comet/comet_service_impl.go` | CometService 实现 |
| `services/logic/cmd/start.go` | Logic Cobra 子命令 |
| `services/logic/config.go` | Logic 配置 |
| `deployments/docker/Dockerfile` | 多阶段构建 |
| `deployments/compose/docker-compose.yaml` | 开发环境 |

### 修改文件

| 文件 | 变更 |
|---|---|
| `services/gateway/server.go` | 删除 container，改为 WS/TCP + gRPC |
| `services/gateway/handler.go` | container.Forward → forwarder.Forward |
| `services/comet/server.go` | 删除 TCP server，改为 gRPC server |
| `services/comet/handler/*.go` | 内部调用走 gRPC |
| `services/logic/server.go` | 删除 Iris，改为 gRPC server |
| `services/logic/handler/*.go` | HTTP handler → gRPC handler |
| `go.mod` | 移除 iris/resty，调整依赖 |
| `Makefile` | 新增 proto/proto-check 目标 |

### 删除文件/目录

| 路径 | 原因 |
|---|---|
| `wire/` | 拆分到 api/proto + gen + pkg/pkt |
| `container/` | gRPC client pool 替代 |
| `naming/` | 迁入 internal/naming |
| `logger/` | 迁入 internal/logger |
| `services/main.go` | 迁入 cmd/kim/main.go |
| `services/gateway/serv/dialer.go` | TCP 握手不再需要 |
| `services/comet/service/message.go` | HTTP client 被 gRPC 替代 |
| `services/comet/service/group.go` | HTTP client 被 gRPC 替代 |
| `services/comet/service/user.go` | HTTP client 被 gRPC 替代 |

---

## 任务列表

### 阶段 1：协议层重构

#### 任务 1：创建 proto 文件和生成脚本

**文件：**
- 创建：`api/proto/pkt/protocol.proto`
- 创建：`api/proto/pkt/common.proto`
- 创建：`api/proto/rpc/gateway.proto`
- 创建：`api/proto/rpc/comet.proto`
- 创建：`api/proto/rpc/logic.proto`
- 创建：`scripts/proto.sh`

- [ ] 步骤 1：把 `wire/proto/*.proto` 复制到 `api/proto/pkt/`，修改 `go_package`
- [ ] 步骤 2：创建 `api/proto/rpc/gateway.proto`（GatewayService + PushReq/PushResp）
- [ ] 步骤 3：创建 `api/proto/rpc/comet.proto`（CometService + ForwardReq/ForwardResp）
- [ ] 步骤 4：创建 `api/proto/rpc/logic.proto`（LogicService + 所有 message 从 wire/rpc 迁入）
- [ ] 步骤 5：创建 `scripts/proto.sh`
- [ ] 步骤 6：运行 `bash scripts/proto.sh` 生成代码到 `gen/`
- [ ] 步骤 7：验证 `go build ./gen/...`
- [ ] 步骤 8：Commit

---

### 阶段 2：基础设施层

#### 任务 2：创建 internal/config

**文件：**
- 创建：`internal/config/config.go`
- 创建：`internal/config/config_test.go`

- [ ] 步骤 1：编写 `config.Load` 函数（Viper + env）
- [ ] 步骤 2：编写测试（加载 YAML + 环境变量覆盖）
- [ ] 步骤 3：验证 `go test ./internal/config/...`
- [ ] 步骤 4：Commit

#### 任务 3：创建 internal/logger

**文件：**
- 创建：`internal/logger/logger.go`

- [ ] 步骤 1：迁移 `logger/` 包代码到 `internal/logger/`
- [ ] 步骤 2：保持 `logger.GatewayLogger` 等全局变量兼容
- [ ] 步骤 3：验证 `go build ./internal/logger/...`
- [ ] 步骤 4：Commit

#### 任务 4：创建 internal/naming

**文件：**
- 创建：`internal/naming/consul.go`
- 创建：`internal/naming/resolver.go`

- [ ] 步骤 1：迁移 `naming/consul/` 代码到 `internal/naming/consul.go`
- [ ] 步骤 2：实现 gRPC Consul resolver
- [ ] 步骤 3：验证 `go build ./internal/naming/...`
- [ ] 步骤 4：Commit

#### 任务 5：创建 internal/server

**文件：**
- 创建：`internal/server/grpc.go`
- 创建：`internal/server/interceptor.go`
- 创建：`internal/server/health.go`

- [ ] 步骤 1：实现 `GRPCServer` 封装（拦截器、keepalive、health、reflection）
- [ ] 步骤 2：实现 logging/metrics/recovery 拦截器
- [ ] 步骤 3：实现 gRPC Health + HTTP /health
- [ ] 步骤 4：验证 `go build ./internal/server/...`
- [ ] 步骤 5：Commit

#### 任务 6：创建 internal/client

**文件：**
- 创建：`internal/client/pool.go`
- 创建：`internal/client/pool_test.go`

- [ ] 步骤 1：实现 `Pool` 结构（连接池 + round-robin + Consul watch）
- [ ] 步骤 2：编写测试（Get/GetAny/并发安全）
- [ ] 步骤 3：验证 `go test ./internal/client/...`
- [ ] 步骤 4：Commit

#### 任务 7：创建 internal/metrics

**文件：**
- 创建：`internal/metrics/metrics.go`

- [ ] 步骤 1：迁移 `services/gateway/serv/metrics.go` 到 `internal/metrics/`
- [ ] 步骤 2：验证 `go build ./internal/metrics/...`
- [ ] 步骤 3：Commit

---

### 阶段 3：Logic 服务 gRPC 化

#### 任务 8：Logic 配置和入口

**文件：**
- 创建：`services/logic/config.go`
- 创建：`services/logic/cmd/start.go`
- 修改：`services/logic/server.go`

- [ ] 步骤 1：创建 `Config` 结构 + `LoadConfig`
- [ ] 步骤 2：创建 Cobra 子命令 `cmd/start.go`
- [ ] 步骤 3：改造 `server.go`：删除 Iris，改为 gRPC server
- [ ] 步骤 4：验证 `go build ./services/logic/...`
- [ ] 步骤 5：Commit

#### 任务 9：Logic handler 改造

**文件：**
- 修改：`services/logic/handler/message_handler.go`
- 修改：`services/logic/handler/group_handler.go`
- 修改：`services/logic/handler/user_handler.go`

- [ ] 步骤 1：改造 message_handler（HTTP → gRPC 签名）
- [ ] 步骤 2：改造 group_handler
- [ ] 步骤 3：改造 user_handler
- [ ] 步骤 4：注册 `LogicServiceServer`
- [ ] 步骤 5：验证 `go build ./services/logic/...`
- [ ] 步骤 6：Commit

---

### 阶段 4：Comet 服务 gRPC 化

#### 任务 10：Comet 配置和入口

**文件：**
- 创建：`services/comet/config.go`
- 创建：`services/comet/cmd/start.go`
- 修改：`services/comet/server.go`

- [ ] 步骤 1：创建 `Config` + `LoadConfig`
- [ ] 步骤 2：创建 Cobra 子命令
- [ ] 步骤 3：改造 `server.go`：删除 TCP server，改为 gRPC server
- [ ] 步骤 4：验证 `go build ./services/comet/...`
- [ ] 步骤 5：Commit

#### 任务 11：Comet gRPC client

**文件：**
- 创建：`services/comet/service/logic_client.go`
- 创建：`services/comet/service/pusher.go`

- [ ] 步骤 1：实现 `LogicClient`（调 LogicService）
- [ ] 步骤 2：实现 `GatewayPusher`（调 GatewayService.Push）
- [ ] 步骤 3：验证 `go build ./services/comet/...`
- [ ] 步骤 4：Commit

#### 任务 12：CometService 实现和 handler 改造

**文件：**
- 创建：`services/comet/comet_service_impl.go`
- 创建：`services/comet/router.go`
- 修改：`services/comet/handler/*.go`

- [ ] 步骤 1：实现 `CometServiceImpl.Forward`
- [ ] 步骤 2：迁移 `kim.Router` 到 `services/comet/router.go`
- [ ] 步骤 3：改造 handler 内部调用（container.Push → pusher.Push）
- [ ] 步骤 4：验证 `go build ./services/comet/...`
- [ ] 步骤 5：Commit

---

### 阶段 5：Gateway 服务 gRPC 化

#### 任务 13：Gateway 配置和入口

**文件：**
- 创建：`services/gateway/config.go`
- 创建：`services/gateway/cmd/start.go`
- 修改：`services/gateway/server.go`

- [ ] 步骤 1：创建 `Config` + `LoadConfig`
- [ ] 步骤 2：创建 Cobra 子命令
- [ ] 步骤 3：改造 `server.go`：保留 WS/TCP + 新增 gRPC server
- [ ] 步骤 4：验证 `go build ./services/gateway/...`
- [ ] 步骤 5：Commit

#### 任务 14：Gateway gRPC client/server

**文件：**
- 创建：`services/gateway/forwarder.go`
- 创建：`services/gateway/pusher.go`
- 修改：`services/gateway/handler.go`
- 修改：`services/gateway/selector.go`（从 serv/ 迁入）

- [ ] 步骤 1：实现 `CometForwarder`
- [ ] 步骤 2：实现 `Pusher`（GatewayService.Push）
- [ ] 步骤 3：改造 handler（container.Forward → forwarder.Forward + channel 表下沉）
- [ ] 步骤 4：迁移 selector.go
- [ ] 步骤 5：验证 `go build ./services/gateway/...`
- [ ] 步骤 6：Commit

---

### 阶段 6：入口和清理

#### 任务 15：创建 cmd/kim/main.go

**文件：**
- 创建：`cmd/kim/main.go`

- [ ] 步骤 1：创建单二进制入口
- [ ] 步骤 2：验证 `go build ./cmd/kim/...`
- [ ] 步骤 3：Commit

#### 任务 16：Router 服务迁移

**文件：**
- 创建：`services/router/cmd/start.go`
- 创建：`services/router/config.go`
- 修改：`services/router/server.go`

- [ ] 步骤 1：创建 Cobra 子命令 + Config
- [ ] 步骤 2：改造 server.go 适配新入口
- [ ] 步骤 3：验证 `go build ./services/router/...`
- [ ] 步骤 4：Commit

#### 任务 17：删除旧代码 + import 修复

**文件：**
- 删除：`wire/`、`container/`、`naming/`、`logger/`、`services/main.go`、`services/*/serv/`
- 修改：所有 import 路径

- [ ] 步骤 1：删除 `wire/` 目录
- [ ] 步骤 2：删除 `container/` 目录
- [ ] 步骤 3：删除 `naming/` 目录
- [ ] 步骤 4：删除 `logger/` 目录
- [ ] 步骤 5：删除 `services/main.go`
- [ ] 步骤 6：删除 `services/gateway/serv/dialer.go`
- [ ] 步骤 7：更新所有 import 路径
- [ ] 步骤 8：运行 `go mod tidy`
- [ ] 步骤 9：验证 `go build ./...`
- [ ] 步骤 10：Commit

#### 任务 18：pkg 目录迁移

**文件：**
- 迁移：`tcp/` → `pkg/tcp/`
- 迁移：`websocket/` → `pkg/websocket/`
- 迁移：`wire/pkt/packet.go` → `pkg/pkt/packet.go`
- 迁移：`wire/endian/` → `pkg/pkt/endian/`
- 迁移：`wire/definitions.go` → `pkg/pkt/commands.go`
- 迁移：`wire/token/` → `pkg/token/`
- 迁移：`kim` 核心接口 → `pkg/kim/`

- [ ] 步骤 1：迁移 tcp/websocket 到 pkg/
- [ ] 步骤 2：迁移 pkt/endian/definitions 到 pkg/pkt/
- [ ] 步骤 3：迁移 token 到 pkg/
- [ ] 步骤 4：迁移 kim 核心接口到 pkg/kim/
- [ ] 步骤 5：更新所有 import 路径
- [ ] 步骤 6：验证 `go build ./...`
- [ ] 步骤 7：Commit

---

### 阶段 7：部署文件和收尾

#### 任务 19：创建 Makefile 和部署文件

**文件：**
- 修改：`Makefile`
- 创建：`deployments/docker/Dockerfile`
- 创建：`deployments/compose/docker-compose.yaml`

- [ ] 步骤 1：更新 Makefile
- [ ] 步骤 2：创建 Dockerfile
- [ ] 步骤 3：创建 docker-compose.yaml
- [ ] 步骤 4：Commit

#### 任务 20：更新 CLAUDE.md 和最终验证

**文件：**
- 修改：`CLAUDE.md`

- [ ] 步骤 1：更新 CLAUDE.md 反映新架构
- [ ] 步骤 2：运行 `go build ./...`
- [ ] 步骤 3：运行 `go test -race ./...`
- [ ] 步骤 4：运行 `make proto-check`
- [ ] 步骤 5：Commit

---

## 自检

**1. 规格覆盖度：**
- 第 1 节（总体架构）→ 任务 1-18 全覆盖
- 第 2 节（目录结构）→ 任务 1-18 全覆盖
- 第 3 节（gRPC 协议）→ 任务 1 全覆盖
- 第 4 节（各服务实现）→ 任务 8-14 全覆盖
- 第 5 节（启动方式与配置）→ 任务 8-16 + 19 全覆盖
- 第 6 节（迁移步骤）→ 任务 1-20 按阶段对应
- 第 7 节（风险控制）→ 每个任务有验证步骤

**2. 占位符扫描：** 无 TODO/待定/类似任务 N

**3. 类型一致性：**
- `Config` 结构在任务 8/10/13 中各服务独立定义，字段一致
- `Pool.Get`/`Pool.GetAny` 在任务 6 定义，任务 11/14 使用
- `CometForwarder.Forward` 在任务 14 定义，任务 14 步骤 3 使用
- `GatewayPusher.Push` 在任务 11 定义，任务 12 使用
