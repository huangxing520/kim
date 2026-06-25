# kim 项目改进路线图设计方案（5 层混合推进）

> **版本：** v1.0
> **日期：** 2026-06-26
> **状态：** 已批准（待用户审查）
> **依赖：** [2026-06-25-resilience-design.md](./2026-06-25-resilience-design.md)（弹性套件基线）
> **范围：** P0 + P1 + P2 全量改进；不含生产数据兼容（可直接重置 DB）

---

## 1. 背景与目标

### 1.1 当前问题

弹性套件（[2026-06-25-resilience-design.md](./2026-06-25-resilience-design.md)）落地后，对全项目做了三方并行扫描（代码质量 / 测试可靠性 / 安全部署），共识别 40+ 改进点。核心阻断性问题集中在：

1. **二进制无法启动**：[internal/metrics/metrics.go:10-21](../../internal/metrics/metrics.go#L10-L21) 与 [services/gateway/serv/metrics.go:17-33](../../services/gateway/serv/metrics.go#L17-L33) 重复注册同名指标，`bin/kim` 任何子命令直接 panic（已实测）
2. **优雅停机形同虚设**：[default_server.go:219-221](../../default_server.go#L219-L221) CAS 成功后 `return`，关闭连接代码永不执行
3. **logger use-after-close**：4 个服务 `New()` 中 `defer log.Close()` 在返回时即触发，Kafka producer 关闭后全局 logger 写入会 panic/丢日志
4. **channel.go 并发 race**：[channel.go:115-131](../../channel.go#L115-L131) Push/Close 之间无锁 → send on closed channel panic；[:184-186](../../channel.go#L184-L186) Readloop 与 writeloop 并发写 bufio.Writer
5. **群聊 >1000 人消息索引静默截断**：[message_handler.go:107-110](../../services/logic/handler/message_handler.go#L107-L110) 剩余成员永久收不到消息索引（数据丢失）
6. **JWT 硬编码密钥 `"secret"` + 废弃库 CVE-2020-26160**：可伪造任意账号 token
7. **明文密码存储 + 字符串比较**：[model.go:51](../../services/logic/database/model.go#L51) `Password string gorm:"size:30"` 连 bcrypt 60 字符都放不下
8. **gRPC 全链路无 TLS + 无 auth interceptor + reflection 开放**：任何人可远程调用 `LogicService.Login` 伪造登录
9. **MonitorPort HTTP server 从未启动**：Consul 健康检查连接拒绝 → 服务被标 critical；`/metrics` 也无法 scrape，弹性指标全盲区
10. **Pool.Close goroutine 泄漏**：[pool.go:155-162](../../internal/client/pool.go#L155-L162) 不 cancel watch、不 Unsubscribe，反复重启累积泄漏
11. **Logic `Stop()` 未关 DB 连接池**

P1/P2 还涉及 context 未贯穿、错误系统性吞掉、4 个 Dockerfile 与代码端口脱节、README 过时、测试稀疏、根目录 `package kim` 分层混乱、依赖陈旧（jwt-go/redis v7/iris 重）等。

### 1.2 设计目标

- **第 1 优先**：让系统能跑起来、能优雅停机、能被监控
- **第 2 优先**：消除远程可利用安全漏洞
- **第 3 优先**：补齐可靠性短板（context 贯穿、错误处理、健康检查）
- **第 4 优先**：部署/文档与实际架构对齐
- **第 5 优先**：在测试基线保护下做架构重构与依赖升级

### 1.3 关键决策

| 决策点 | 选择 | 理由 |
|---|---|---|
| 范围 | P0 + P1 + P2 全量 | 用户选定 |
| wire/token 是否可改 | 可改（仅保留 wire/pkt 二进制协议不动） | JWT 安全修复必须 |
| 生产数据兼容 | 无生产数据，可直接重置 | JWT 密钥轮换+密码 bcrypt 重哈不需要兼容期 |
| 推进策略 | 方案 C：分层混合 | 止血优先；2/3 层可并行；重构放最后有测试保护 |
| 子项目粒度 | 5 个独立子项目，各自走规格→计划→实现 | 单规格无法覆盖 40+ 项；每层可独立交付 |
| 并行调度 | 第 2/3 层可并行（文件少有交集） | 用 dispatching-parallel-agents 加速 |
| 工作区隔离 | 每子项目用 git worktree | 隔离变更、便于回退 |

---

## 2. 总体路线图

### 2.1 五层依赖图

```
                 ┌──────────────────────────────┐
                 │  第1层 紧急止血（必须最先）       │
                 │  让系统能跑起来、能停、能被监控   │
                 └──────────────┬───────────────┘
                                │
                 ┌──────────────┴───────────────┐
                 │                              │
       ┌─────────▼─────────┐         ┌──────────▼──────────┐
       │  第2层 安全加固     │         │  第3层 可靠性补强     │
       │  消除远程可利用漏洞  │ 并行    │  context/错误/健康检查│
       └─────────┬─────────┘         └──────────┬──────────┘
                 │                              │
                 └──────────────┬───────────────┘
                                │
                 ┌──────────────▼───────────────┐
                 │  第4层 部署与文档对齐           │
                 │  Dockerfile/README/CI/conf    │
                 └──────────────┬───────────────┘
                                │
                 ┌──────────────▼───────────────┐
                 │  第5层 测试与架构重构           │
                 │  在测试基线保护下重构          │
                 └──────────────────────────────┘
```

### 2.2 各层范围一览

| 层 | 名称 | 子项目数 | 依赖 | 预计周期 |
|---|---|---|---|---|
| 1 | 紧急止血 | 7 项 | 无 | 1-2 天 |
| 2 | 安全加固 | 5 项 | 第 1 层 | 2-3 天 |
| 3 | 可靠性补强 | 6 项 | 第 1 层 | 2-3 天 |
| 4 | 部署与文档 | 5 项 | 第 2/3 层 | 1-2 天 |
| 5 | 测试与重构 | 6 项 | 第 4 层 | 3-5 天 |

---

## 3. 各层子项目细节

### 3.1 第 1 层：紧急止血

**目标**：让系统真正能跑起来、能优雅停机、能被 Consul 与 Prometheus 监控到。完成后系统才是后续工作的可验证基线。

| # | 项 | 位置 | 修复策略 |
|---|---|---|---|
| L1-1 | metrics 重复注册 panic | [internal/metrics/metrics.go:10-21](../../internal/metrics/metrics.go#L10-L21) | 删除 `internal/metrics` 中三个未使用的重复定义（`MessageInTotal`/`MessageInFlowBytes`/`NoServerFoundErrorTotal`），保留 `services/gateway/serv/metrics.go` 的实际使用版本 |
| L1-2 | Shutdown CAS 逻辑反转 | [default_server.go:219-221](../../default_server.go#L219-L221) | CAS 成功后应继续执行关闭逻辑；CAS 失败（已 quit）才 return |
| L1-3 | defer log.Close() 提前关闭 | [gateway/server.go:57](../../services/gateway/server.go#L57)、[comet/server.go:56](../../services/comet/server.go#L56)、[logic/server.go:54](../../services/logic/server.go#L54)、[router/server.go:46](../../services/router/server.go#L46) | 移除 `New()` 中的 `defer log.Close()`，改在 `Server.Stop()` 末尾调用 `log.Close()`；为 `Server` 增加 `logger *zap.Logger` 字段保存引用 |
| L1-4 | channel.go Push/Close race + 并发写 bufio | [channel.go:115-131](../../channel.go#L115-L131)、[:184-186](../../channel.go#L184-L186) | Push 改用 `select { case ch.writechan <- payload: case <-ch.closeChan: }`；Close 先 CAS 再 `close(ch.closeChan)` 后 `close(ch.writechan)`；Pong 走 writechan 不直接写 bufio |
| L1-5 | MonitorPort HTTP server 未启动 | [services/gateway/server.go:139](../../services/gateway/server.go#L139) | 在 `Start()` 中启动 `http.ListenAndServe(monitorPort, mux)`，注册 `/metrics`（`promhttp.Handler()`）+ `/health`；删除 `internal/server/health.go` 死代码或激活它 |
| L1-6 | Pool.Close goroutine 泄漏 | [internal/client/pool.go:155-162](../../internal/client/pool.go#L155-L162) | `Pool` 增加 `ctx`/`cancel`；`Close()` 先 `cancel()` 让 `watch()` 退出，再 `naming.Unsubscribe`，最后关 gRPC 连接 |
| L1-7 | Logic Stop() 未关 DB | [services/logic/server.go:147-156](../../services/logic/server.go#L147-L156) | `Stop()` 增加 `sqlDB, _ := s.baseDb.DB(); sqlDB.Close()` 与 messageDb 同 |

**验证标准**：`make run-all` 4 个服务正常启动且无 panic；`curl localhost:8001/metrics` 返回 Prometheus 文本；`curl localhost:8001/health` 返回 200；`make stop-all` 后 Consul 中服务状态变 critical、进程退出干净（`pgrep kim` 无残留）；重启 3 次后 goroutine 数无累积（pprof 抓取）。

---

### 3.2 第 2 层：安全加固

**目标**：消除所有远程可利用安全漏洞。可与第 3 层并行。

| # | 项 | 位置 | 修复策略 |
|---|---|---|---|
| L2-1 | 群聊消息索引截断丢数据 | [message_handler.go:107-110](../../services/logic/handler/message_handler.go#L107-L110) | 改为分批写入：按 `maxBatchSize=1000` 分多个事务循环 `InsertGroupMessage`，全部成员都收到索引；注释保留防超时本意 |
| L2-2 | JWT 库迁移 + 密钥配置化 + Parse Valid() | [wire/token/jwt.go](../../wire/token/jwt.go)、[user_handler.go:26](../../services/logic/handler/user_handler.go#L26)、[gateway/serv/handler.go:83-85](../../services/gateway/serv/handler.go#L83-L85) | (1) `dgrijalva/jwt-go` → `golang-jwt/jwt/v5`；(2) `Parse` 中校验 `token.Method`（防 alg=none）并调用 `Valid()`；(3) 删除硬编码 `DefaultSecret`，从配置读取 `app_secret`，缺失则 fail-fast；(4) 不再把 Password/AccessToken 当 JWT claim（仅放 Account+Exp） |
| L2-3 | 密码 bcrypt + 常量时间比较 | [model.go:51](../../services/logic/database/model.go#L51)、[user_handler.go:25](../../services/logic/handler/user_handler.go#L25) | (1) `Password` 字段 `size:60`+bcrypt 哈希存储；(2) 注册时 `bcrypt.GenerateFromPassword`；(3) 登录时 `bcrypt.CompareHashAndPassword`（内部已是常量时间）；(4) 迁移脚本：清空现有 user 表（无生产数据） |
| L2-4 | gRPC TLS + auth interceptor + 关 reflection | [internal/server/grpc.go:51-74](../../internal/server/grpc.go#L51-L74)、[internal/client/pool.go:133](../../internal/client/pool.go#L133) | (1) `internal/server/grpc.go` 支持 `grpc.Creds(credentials.NewTLS(tlsConfig))`，配置开关 `grpc.tls.enable`（默认 false 开发模式）；(2) logic 服务端启用 mTLS（加载 `grpc.tls.server_cert`/`server_key`，要求客户端提供 `grpc.tls.ca_cert` 签发的证书）；(3) comet/gateway/router 服务端启用 token auth 拦截器（新增 `internal/server/auth.go`，从 metadata `authorization` 取 bearer token 并 `token.Parse` 校验）；(4) reflection 仅在 `grpc.reflection.enable=true`（默认 false）时注册；(5) 客户端 `pool.go` 按 `grpc.tls.enable` 选择 `credentials.NewTLS(...)` 或 `insecure.NewCredentials()` |
| L2-5 | 敏感信息日志脱敏 | [gateway/serv/handler.go:107](../../services/gateway/serv/handler.go#L107)、[comet/handler/login_handler.go:44](../../services/comet/handler/login_handler.go#L44)、[internal/server/interceptor.go:46-47](../../internal/server/interceptor.go#L46-L47) | (1) `*token.Token` 的 `String()` 方法脱敏（Password/AccessToken 用 `***`）；(2) Recovery 拦截器把 `debug.Stack()` 改为只回客户端 `Internal Server Error`，堆栈仅写日志；(3) `context.go:100` Debug 日志对消息 body 长度限制+哈希后输出 |

**验证标准**：用 `grpcurl` 在未持 token 时调用任意 RPC 返回 `Unauthenticated`；构造 `alg=none` token 被拒；伪造的 `secret` 密钥 token 被拒（密钥从配置读后）；注册用户密码以 `$2a$` 开头存库；`grep -r "secret" services/*/conf.yaml` 无硬编码；日志中无 Password/AccessToken 明文。

---

### 3.3 第 3 层：可靠性补强

**目标**：补齐 context 贯穿、错误处理、健康检查、panic recovery。可与第 2 层并行。

| # | 项 | 位置 | 修复策略 |
|---|---|---|---|
| L3-1 | context 贯穿调用链 | [context.go:41-51](../../context.go#L41-L51)、[forwarder.go:76](../../services/gateway/forwarder.go#L76)、[logic_client.go](../../services/comet/service/logic_client.go)、[pusher.go:40](../../services/comet/service/pusher.go#L40) | (1) `kim.Context` 新增 `StdContext() context.Context` 方法（返回内部 `context.Context`，避免修改接口签名破坏调用方）；(2) forwarder/logic_client/pusher 全部从 `context.Background()` 改为 `ctx.StdContext()`；(3) message_handler 的 `insertUserMessage/insertGroupMessage` 接收并传 ctx 到 GORM（`db.WithContext(ctx)`） |
| L3-2 | 错误处理审计 | [gateway/server.go:143](../../services/gateway/server.go#L143)、[comet/server.go:130](../../services/comet/server.go#L130)、[logic/server.go:118](../../services/logic/server.go#L118)、[login_handler.go:94](../../services/comet/handler/login_handler.go#L94)、[user_handler.go:29](../../services/logic/handler/user_handler.go#L29)、[limiter.go:51-53](../../internal/server/limiter.go#L51-L53) | (1) `_ = ns.Register` 改为检查并 log.Fatal/return；(2) RedisGet 错误传播给客户端；(3) `Cache.Set` 失败返回错误让登录失败（一致性优先）；(4) `limiter.go:51-53` 空块改为 logger 记录被拒 RPC |
| L3-3 | 健康检查 readiness/liveness 区分 | [internal/server/grpc.go:70](../../internal/server/grpc.go#L70)、[internal/server/health.go](../../internal/server/health.go) | (1) 激活 `HealthChecker`，区分 `liveness`（进程活着）与 `readiness`（DB/Redis/Naming 都就绪）；(2) 启动时 `SetServingStatus("", NOT_SERVING)`，依赖就绪后切 `SERVING`；(3) `/healthz/live` 与 `/healthz/ready` 两个 HTTP 端点 |
| L3-4 | Gateway WS panic recovery | [gateway/serv/handler.go:52](../../services/gateway/serv/handler.go#L52)、[:138](../../services/gateway/serv/handler.go#L138) | 在 `gpool.Submit` 的 func 内 `defer func(){ if r := recover(); r != nil { logger.GatewayLogger.Error(...) } }()`；或抽 `internal/util/recover.go` 公共 helper |
| L3-5 | isNoRetryError 识别 gRPC 状态码 | [internal/client/resilient.go:151-159](../../internal/client/resilient.go#L151-L159) | 增加 `codes.InvalidArgument`/`PermissionDenied`/`NotFound`/`AlreadyExists`/`FailedPrecondition`/`OutOfRange`/`Unimplemented` 判断为不可重试 |
| L3-6 | grpc.Dial → grpc.NewClient | [internal/client/pool.go:132](../../internal/client/pool.go#L132) | `grpc.Dial` → `grpc.NewClient`（阻塞式、可立即返回错误）；`:138-140` 的 `continue` 改为日志记录 |

**验证标准**：客户端取消请求时下游 gRPC 也取消（看 trace）；Consul 中服务在 DB 未就绪时为 NOT_SERVING；故意 panic 一个 WS handler 后服务仍正常；`grpcurl InvalidArgument` 不触发重试（看 metrics `retry_total` 不增加）。

---

### 3.4 第 4 层：部署与文档对齐

**目标**：让部署流程与"单一二进制 + Cobra"架构一致，让新人按文档能跑起来。

| # | 项 | 位置 | 修复策略 |
|---|---|---|---|
| L4-1 | 4 个 Dockerfile 统一为单镜像 | [Dockerfile_server](../../Dockerfile_server)、[Dockerfile_royal](../../Dockerfile_royal)、[Dockerfile_router](../../Dockerfile_router)、[Dockerfile_gateway](../../Dockerfile_gateway) | 删除 4 个旧 Dockerfile；新建单一 `Dockerfile`：多阶段构建 `bin/kim`，运行时镜像 `FROM scratch` + non-root + HEALTHCHECK + LABEL；`docker-compose-kim.yml` 用同一镜像不同 `command: [kim, gateway/comet/logic/router, start]` |
| L4-2 | docker-compose 端口对齐 | [docker-compose-kim.yml](../../docker-compose-kim.yml) | gateway 暴露 8000/9001/8001；comet 8005/9001；logic 9002；router 8100；与 conf.yaml 一致 |
| L4-3 | README 重写 | [README.md:53-59](../../README.md#L53-L59) | 删除 `go run main.go gateway/server/royal` 旧说明；写 `make docker-up` / `make run-all` / `make status` / 单服务调试 / proto 重新生成 / 环境变量前缀 KIM_ |
| L4-4 | 环境变量 slice 覆盖修复 | [internal/config/config.go:12-25](../../internal/config/config.go#L12-L25) | 在 `Load` 后增加 `decodeSliceEnv(v, "kafka.brokers")` 等 helper：检测环境变量 `KIM_KAFKA_BROKERS`，若为非空字符串则按空格 split 覆盖 `Unmarshal` 结果（不引入新依赖，最小改动）；后续可在 L5-5 评估是否迁移到 koanf |
| L4-5 | conf.yaml 4 份统一 + Redis 密码可配 | [services/comet/server.go:174-185](../../services/comet/server.go#L174-L185)、[services/logic/server.go:166-177](../../services/logic/server.go#L166-L177) | (1) router/conf.yaml 补 `service_id/public_address/resilience/trace`；(2) 抽 `internal/config.MergeDefaults(cfg)` 公共 helper，4 服务调用；(3) comet/logic 的 `initRedis(addr)` 改用 `database/redis.go:InitRedis(addr, pass)`，conf.yaml 加 `redis.password` |

**验证标准**：`docker compose up` 单镜像跑 4 个服务全绿；`docker compose exec gateway wget -qO- localhost:8001/health` 200；新人按 README 5 分钟内跑通；`KIM_KAFKA_BROKERS="a:9092 b:9092" ./bin/kim comet start` 能解析为 slice。

---

### 3.5 第 5 层：测试与架构重构

**目标**：在测试基线保护下做架构重构与依赖升级。

| # | 项 | 位置 | 修复策略 |
|---|---|---|---|
| L5-1 | 核心模块补测试 | channel.go / forwarder.go / comet_service_impl.go / logic_client.go / context.go / pool.go | TDD 风格，先写表驱动测试覆盖：channel 并发场景、Push/Close race、forwarder 路由、logic_client RPC mock（gomock）、context 链路 |
| L5-2 | 集成测试 build tag 隔离 | [services/logic/handler/message_handler_test.go](../../services/logic/handler/message_handler_test.go)、[mysql_test.go](../../services/logic/database/mysql_test.go)、[storage/redis_test.go](../../storage/redis_test.go) | 加 `//go:build integration`；`make test` 默认跑单元测试；新增 `make test-integration` 跑集成测试；CI 矩阵补 logic/router 包 |
| L5-3 | 根目录 package kim → internal/kim | [channel.go](../../channel.go)、[context.go](../../context.go)、[default_server.go](../../default_server.go)、[dispatcher.go](../../dispatcher.go) 等 15 个根 .go | 整体迁移到 `internal/kim/`；`services/*` 改 import 路径；保持 `package kim` 名字不变，仅改位置；通过 import 别名保持调用点可读 |
| L5-4 | 4 服务目录结构统一 | services/{gateway,comet,logic,router} | 统一为 `handler/`（业务）+ `service/`（gRPC service impl）+ `data/`（持久化）+ `conf/`（配置）；comet 加 `conf/`；gateway `serv/` → `handler/`；logic `database/` → `data/`；router `apis/` → `handler/` |
| L5-5 | 依赖升级 | [go.mod:8](../../go.mod#L8)、[:9](../../go.mod#L9) | (1) jwt-go 已在 L2-2 迁移；(2) go-redis v7 → v9（API 变化：`redis.NewClient` 签名一致，pipeline/tx API 调整）；(3) 评估 iris → 标准库 `net/http` + `chi`/`gorilla/mux`（router 只有一个路由，瘦身） |
| L5-6 | CI 增强 | [.github/workflows/ci.yml](../../.github/workflows/ci.yml)、[.golangci.yml](../../.golangci.yml) | (1) 新增 `govulncheck` job；(2) `.golangci.yml` 启用 `gosec`；(3) 测试矩阵补 `./services/logic/...` 与 `./services/router/...`；(4) 新增 release job（git tag 触发 + changelog 生成） |

**验证标准**：`make test` 在无外部依赖下全绿；`go test -race ./...` 无 race 报告；`govulncheck ./...` 无 vuln；`golangci-lint run` 无 error；根目录无 `*.go`（除 main.go）；4 服务目录结构相同。

---

## 4. 实施顺序与并行策略

### 4.1 串行/并行调度

```
阶段 1（串行）：  [第 1 层 紧急止血]                          持续 1-2 天
                       │
阶段 2（并行）：  [第 2 层 安全加固] ∥ [第 3 层 可靠性补强]    持续 2-3 天
                       │              │
                       └──────┬───────┘
                              │
阶段 3（串行）：  [第 4 层 部署与文档对齐]                     持续 1-2 天
                              │
阶段 4（串行）：  [第 5 层 测试与架构重构]                     持续 3-5 天
```

### 4.2 工作区隔离

每层用一个独立 git worktree（`using-git-worktrees` skill）：
- `../kim-L1-hotfix`、`../kim-L2-security`、`../kim-L3-reliability`、`../kim-L4-deploy`、`../kim-L5-refactor`
- 完成后通过 PR 合回 `feature/grpc-refactor` 分支

### 4.3 验证关卡

每层完成后必须通过（`verification-before-completion` skill）：
1. `go build ./...` 无错误
2. `go test ./...` 已有测试全绿
3. `go vet ./...` 无警告
4. 运行 `make run-all` + 烟测 4 个服务正常
5. 在 commit message 中给出验证命令的输出摘要

---

## 5. 错误处理与回退

### 5.1 单层失败回退

每层是独立子项目，失败时仅回退该层。L1 失败会阻塞 L2-L5（系统跑不起来无法验证后续），但 L1 内部 7 项可独立 commit。

### 5.2 风险与缓解

| 风险 | 缓解 |
|---|---|
| L1-4 channel race 修复引入新死锁 | TDD 先写并发测试（多个 goroutine 并发 Push+Close） |
| L2-4 gRPC TLS 引入开发环境复杂度 | 配置开关 `grpc.tls.enable=false` 默认开发模式不启用 TLS；CI 用 insecure 模式跑测试 |
| L2-2 JWT 库迁移破坏现有 token 格式 | 无生产数据，直接重置；测试用例覆盖签发-解析往返 |
| L5-3 package kim 迁移引发大面积 import 改动 | 用 `sed` 批量替换 + `goimports` + 一次 `go build` 验证；一个 commit 完成 |
| L5-5 go-redis v7→v9 API 不兼容 | 单独子 commit，先跑通编译，再修测试 |
| 第 2/3 层并行合并冲突 | 文件少有交集：L2 改 wire/token+handler/auth，L3 改 forwarder+logic_client+context+interceptor；如冲突在 L1 改过的 server.go，L2/L3 各自 rebase L1 |

---

## 6. 不在本次范围

- **wire/pkt 协议二进制格式**（magic/seq/字段编码）不动，仅 wire/token 可改
- **业务逻辑变更**（聊天/群组/离线消息的业务规则不动）
- **性能优化**（连接池调参、sync.Pool 等）— 留待后续专题
- **多区域/多 IDC 部署** — 留待后续专题
- **新增功能**（如文件消息、消息撤回）— 不在改进路线图范围

---

## 7. 风险与缓解（汇总）

| 风险等级 | 风险 | 缓解策略 |
|---|---|---|
| 高 | L1 修复不彻底导致后续工作基线不稳 | L1 必须有完整验证清单（5 项烟测），不通过不进入 L2 |
| 高 | L2/L3 并行合并冲突 | 严格文件分工：L2 = `wire/token/` + `services/logic/handler/` + `internal/server/auth.go`（新） + `services/gateway/serv/handler.go`；L3 = `internal/client/` + `services/comet/service/` + `services/gateway/forwarder.go` + `context.go` |
| 中 | L5 重构破坏现有功能 | L5 必须在 L1-L4 测试基线完备后开始；每个子项一个 commit，便于 git bisect |
| 中 | 总周期偏长（10-15 天） | 严格按层推进，不跨层；每层闭环后可独立交付价值 |
| 低 | 依赖升级引入新 CVE | `govulncheck` 在 CI 中作为门禁 |

---

## 8. 下一步

1. 用户审查本路线图规格
2. 审查通过后，调用 `writing-plans` 技能为**第 1 层（紧急止血）**编写实现计划
3. L1 实现并验证通过后，再为 L2/L3 各编写实现计划（可并行）
4. 依次推进 L4、L5

每层的实现计划文档保存到 `docs/superpowers/plans/2026-06-26-<layer-name>-implementation.md`。
