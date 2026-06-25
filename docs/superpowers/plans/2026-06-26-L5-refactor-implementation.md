# L5 测试与架构重构 — TDD 实施计划

> **日期**：2026-06-26  
> **依赖**：L1+L2+L3+L4 已完成  
> **范围**：6 个子任务  
> **策略**：先测试基线（L5-1/L5-2），后重构（L5-3/L5-4），再依赖升级（L5-5），最后 CI（L5-6）  
> **⚠️ 风险提示**：L5-3（package kim→internal/kim）和 L5-4（目录结构统一）是大面积 import 改动，每个独立 commit，便于回退

---

## 任务总览

| # | 任务 | 类型 | 风险 | 依赖 |
|---|------|------|------|------|
| L5-1 | 核心模块补单元测试 | 测试 | 低 | 无 |
| L5-2 | 集成测试 build tag 隔离 | 测试 | 低 | L5-1 |
| L5-6 | CI/lint 增强（先做，保护后续重构） | CI | 低 | L5-1 |
| L5-3 | 根目录 package kim → internal/kim | 重构 | **高** | L5-1/L5-2/L5-6 |
| L5-4 | 4 服务目录结构统一 | 重构 | 中 | L5-3 |
| L5-5 | 依赖升级（go-redis v7→v9、iris 评估） | 升级 | 中 | L5-3/L5-4 |

**执行顺序调整**：CI 配置提前到 L5-3 之前，以便重构过程中有 CI 兜底。

---

## L5-1：核心模块补单元测试

### 目标
为核心模块补充表驱动单元测试，建立测试基线保护后续重构。

### 覆盖范围
1. **channel.go**：Channel Push/Close 并发安全（已有 channel_race_test.go 基础上扩展）
2. **context.go**：StdContext 传播、Dispatch 多 gateway 错误聚合、Resp 错误处理
3. **internal/client/pool.go**：roundRobin 轮询逻辑
4. **internal/client/resilient.go**：isNoRetryError 对各 gRPC 状态码判断
5. **internal/server/interceptor.go**：LimiterInterceptor、RecoveryInterceptor
6. **services/gateway/serv**：RouteSelector（已有 selector_test.go）

### TDD 流程

对每个模块：
1. 先读现有代码理解边界条件
2. 写表驱动测试用例
3. 运行测试确认通过
4. 不修改生产代码（纯加测试）

### 新增/扩展测试文件
- `context_test.go`（已有基础，扩展 Dispatch 多 gateway 测试）
- `internal/client/pool_test.go`（roundRobin 测试）
- `internal/server/interceptor_test.go`（Recovery 不泄露堆栈测试）

### Commit message
```
test: add unit tests for core modules to establish baseline before refactoring

- context_test.go: Dispatch multi-gateway error aggregation, StdContext propagation
- internal/client/pool_test.go: roundRobin rotation logic
- internal/server/interceptor_test.go: RecoveryInterceptor does not leak stack
- Extend channel_race_test.go with additional concurrent scenarios
```

---

## L5-2：集成测试 build tag 隔离

### 问题
`storage/redis_test.go`、`services/logic/database/mysql_test.go`、`services/router/api_test.go` 需要外部依赖（Redis/MySQL/HTTP 服务），`make test` 或 `go test ./...` 在无 Docker 环境下会失败。

### 实施步骤

1. **在需要外部依赖的测试文件顶部加 build tag**：
   ```go
   //go:build integration
   // +build integration
   ```
   影响文件：
   - `storage/redis_test.go`
   - `services/logic/database/mysql_test.go`（以及其他 database 目录下的测试）
   - `services/router/api_test.go`
   - `examples/unittest/` 下的测试

2. **Makefile 新增 test-integration 目标**：
   ```makefile
   test:
       $(GO) test -v -race ./...
   
   test-integration:
       $(GO) test -v -race -tags=integration ./...
   
   test-unit:
       $(GO) test -v -race $(shell go list ./... | grep -v '/integration')
   ```
   注意：实际实现时用 `go test -v -race ./...` 默认会跳过 build tag 不匹配的文件，不需要 grep 过滤。但要确认无 tag 时 `go test ./...` 全绿。

3. **验证**：
   - 无 Docker 环境下 `go test ./...` 全绿
   - `go test -tags=integration ./...` 会运行集成测试（需 Docker 环境）

### Commit message
```
test: isolate integration tests with build tags

- Add //go:build integration to tests requiring Redis/MySQL/live services
- Add make test-integration target for running integration tests
- Default `make test` now runs only unit tests (no external deps required)
- Affected: storage/redis_test.go, services/logic/database/*_test.go, services/router/api_test.go
```

---

## L5-6：CI 增强（提前执行）

### 目标
在大重构前建立 CI 门禁，让每次改动都有自动化验证。

### 实施步骤

1. **创建 .github/workflows/ci.yml**：
   ```yaml
   name: CI
   on: [push, pull_request]
   jobs:
     test:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v4
         - uses: actions/setup-go@v5
           with: { go-version: '1.26' }
         - run: go mod download
         - run: go build ./...
         - run: go vet ./...
         - run: go test -race -count=1 ./...
     vulncheck:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v4
         - uses: actions/setup-go@v5
           with: { go-version: '1.26' }
         - run: go install golang.org/x/vuln/cmd/govulncheck@latest
         - run: govulncheck ./...
   ```

2. **更新 .golangci.yml 启用 gosec**：
   - 在 linters.enable 中添加 `gosec`
   - 添加 gosec 排除规则（如有误报）

3. **不创建 release job**（留待真正需要发布时）

### Commit message
```
ci: add GitHub Actions workflow with test and govulncheck jobs

- ci.yml: build + vet + race test on every push/PR
- ci.yml: govulncheck job for vulnerability scanning
- .golangci.yml: enable gosec security linter
```

---

## L5-3：根目录 package kim → internal/kim（⚠️ 高风险）

### 目标
将根目录 15 个 `package kim` 文件迁移到 `internal/kim/` 目录，遵循 Go 项目布局惯例（根目录只放 main.go 和 README 等元文件）。

### 迁移文件列表
```
channel.go          → internal/kim/channel.go
channel_race_test.go→ internal/kim/channel_race_test.go
channels.go         → internal/kim/channels.go
channels_test.go    → internal/kim/channels_test.go
context.go          → internal/kim/context.go
context_test.go     → internal/kim/context_test.go
default_server.go   → internal/kim/default_server.go
default_server_test.go → internal/kim/default_server_test.go
dispatcher.go       → internal/kim/dispatcher.go
dispatcher_mock.go  → internal/kim/dispatcher_mock.go
event.go            → internal/kim/event.go
location.go         → internal/kim/location.go
metrics.go          → internal/kim/metrics.go
net.go              → internal/kim/net.go
router.go           → internal/kim/router.go
server.go           → internal/kim/server.go
server_mock.go      → internal/kim/server_mock.go
storage.go          → internal/kim/storage.go
storage_mock.go     → internal/kim/storage_mock.go
```

### 执行步骤

1. **创建 internal/kim/ 目录**
2. **移动所有 .go 文件**到 internal/kim/（保持 package kim 不变，只改位置）
3. **批量更新 import 路径**：将所有 `github.com/klintcheng/kim`（作为 kim 包导入的地方）替换？
   - **注意**：根目录的 kim 包 import path 仍然是 `github.com/klintcheng/kim`
   - 移到 internal/kim/ 后，import path 变为 `github.com/klintcheng/kim/internal/kim`
   - 需要把所有文件中的 import 从 `github.com/klintcheng/kim`（引用根 kim 包的地方）改为 `github.com/klintcheng/kim/internal/kim`
   - **关键**：`cmd/kim/main.go`、`services/*`、`internal/*`、`wire/*`、`storage/*`、`examples/*`、`gen/*` 都需要检查
4. **保留 cmd/kim/main.go 在根目录的 cmd/kim/ 下**（已经是正确的位置）
5. **根目录不再有非 main 的 .go 文件**：
   - 检查 `examples/` 下是否有引用根包的
6. **执行 build + vet + test 验证**
7. **一个 commit 完成**，便于 `git revert` 回退

### ⚠️ 风险缓解
- **不修改任何文件中的代码逻辑**，只做 `mv` + import 路径替换
- `package kim` 名字不变
- 移动后立即运行 `go build ./...`，有错误一次性修复

### Commit message
```
refactor: move root package kim to internal/kim/

Follow Go project layout conventions: root package (channel.go, context.go,
default_server.go, etc.) moved to internal/kim/. This prevents external
projects from depending on kim's internal implementation details.

- Move 18 .go files from root to internal/kim/
- Update all import paths from "github.com/klintcheng/kim" to "github.com/klintcheng/kim/internal/kim"
- Package name remains `kim`; only location changes
- No logic changes, pure directory restructuring + import fixes
```

---

## L5-4：4 服务目录结构统一

### 目标
统一 4 个服务的目录结构：
```
services/<svc>/
├── cmd/           # Cobra 子命令（已有）
├── conf.yaml      # 配置文件
├── config.go      # 配置结构定义
├── server.go      # 服务启动/停止逻辑
├── handler/       # 业务 handler（消息处理）
├── service/       # gRPC service 实现
└── data/          # 持久化/存储（如果需要）
```

### 需要的调整

| 服务 | 当前 | 调整 |
|------|------|------|
| gateway | `serv/` 目录 | `serv/` → `handler/` |
| gateway | `conf2.yaml` 多余文件 | 删除 conf2.yaml |
| logic | `database/` 目录 | `database/` → `data/` |
| router | `apis/` 目录 | `apis/` → `handler/` |
| router | `ipregion/` 数据 | 保留在 `data/ipregion/`（已在 data/ 下） |
| comet | `handler/` 已有 | 无需改动 |
| comet | `service/` 已有 | 无需改动 |

### 执行步骤

1. **gateway**: `mv services/gateway/serv services/gateway/handler`，更新包名和 import
2. **logic**: `mv services/logic/database services/logic/data`，更新包名和 import
3. **router**: `mv services/router/apis services/router/handler`，更新包名和 import
4. 删除 `services/gateway/conf2.yaml`（多余的配置备份）
5. 每个步骤一个 commit，或者合并为一个 commit（改动都是目录重命名）
6. 验证 build + vet + test

### 注意
- 包名从 `serv` → `handler`、`database` → `data`、`apis` → `handler`
- router 的 handler 包名改为 `handler`（原为 `apis`）
- 注意 `services/gateway/pusher.go` 和 `services/gateway/forwarder.go` 在 gateway 根目录，是 service 层文件，保留位置不动（它们是辅助结构，不是 handler）

### Commit message
```
refactor: unify 4 service directory structures

Standardize on handler/ + service/ + data/ layout:
- gateway: serv/ → handler/
- logic:   database/ → data/
- router:  apis/ → handler/
- Remove stray services/gateway/conf2.yaml
- Package names updated accordingly; no logic changes
```

---

## L5-5：依赖升级

### 升级项

| 依赖 | 从 | 到 | 风险 | 说明 |
|------|----|----|------|------|
| go-redis | `github.com/go-redis/redis/v7` | `github.com/redis/go-redis/v9` | 中 | v7→v9 有 API 变化 |
| iris | `github.com/kataras/iris/v12` | 评估→标准库 net/http | 低 | router 只有一个 HTTP 路由，可以直接用标准库 |

### 5a. go-redis v7 → v9

1. **更新 go.mod**：
   ```
   go get github.com/redis/go-redis/v9@latest
   go mod tidy
   ```

2. **API 变化适配**：
   - import path: `github.com/go-redis/redis/v7` → `github.com/redis/go-redis/v9`
   - `redis.NewClient(&redis.Options{...})` 签名基本一致
   - 大部分命令 API（Set/Get/MGet/HSet/Pipeline）一致
   - v9 要求 `context.Context` 作为第一个参数传递给命令（如 `cli.Get(ctx, key)`）
   - 需要修改 `storage/redis_impl.go` 和 comet/logic 中的 redis 调用，传入 context

3. **验证**：build + vet + test

### 5b. iris → 标准库 net/http

检查 router 的 HTTP 代码，确认是否只有简单路由。如果是，替换为标准库 `net/http`：
- 删除 iris 依赖
- 使用 `http.HandleFunc` 注册路由
- 如果 router 使用了 iris 的复杂中间件/路由参数，评估是否保留（但当前看只是 IP 区域查询）

### Commit messages
```
feat(deps): upgrade go-redis from v7 to v9

- Module path: github.com/go-redis/redis/v7 → github.com/redis/go-redis/v9
- Add context.Context parameter to all Redis command calls
- Pipeline/Exec API unchanged
- Update storage/redis_impl.go and comet/logic redis initialization
```

```
refactor(router): replace iris with standard library net/http

Router only has a single HTTP endpoint for IP region lookup.
Remove heavy iris dependency, use net/http directly.
```

---

## 验证清单

L5 全部完成后执行：

```bash
# 构建
go build ./...

# 静态检查
go vet ./...

# 单元测试（无外部依赖）
go test -race -count=1 ./...

# golangci-lint
golangci-lint run ./...

# govulncheck
govulncheck ./...

# 确认根目录无非 main 的 .go 文件
ls *.go  # 应该只有 cmd/kim/main.go（如果在根目录），或者为空（main.go 在 cmd/kim/）
# 根目录应该没有 .go 文件（cmd/kim/ 下有 main.go 是正确的）

# 目录结构验证
ls services/gateway/  # 应有 handler/ 而非 serv/
ls services/logic/    # 应有 data/ 而非 database/
ls services/router/   # 应有 handler/ 而非 apis/
```

## 不在 L5 范围

- 业务逻辑变更（聊天/群组/离线消息规则）
- 性能优化（连接池参数调优、sync.Pool 等）
- 生产环境 Docker Compose（TLS/mTLS 配置）
- 新增功能（文件消息、消息撤回等）
- wire/pkt 协议格式变更（二进制兼容性）
