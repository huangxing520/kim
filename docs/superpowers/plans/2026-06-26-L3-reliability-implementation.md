# L3 可靠性补强层实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 补齐 kim 项目可靠性短板——context 贯穿调用链、系统性错误处理审计、健康检查 readiness/liveness 区分、Gateway WS panic recovery、不可重试错误 gRPC 状态码识别、grpc.Dial 迁移到 grpc.NewClient。

**架构：** 6 个独立任务，按 L3-4（先建 recover 基础设施，供多处使用）→ L3-1（context 贯穿，影响多个 RPC 调用点）→ L3-5（不可重试错误识别）→ L3-6（grpc.Dial→NewClient）→ L3-2（错误处理审计收尾）→ L3-3（健康检查，最后整合所有依赖就绪状态）顺序推进；每项走 TDD。

**技术栈：** Go 1.26 · google.golang.org/grpc/codes · google.golang.org/grpc/status · google.golang.org/grpc/health/grpc_health_v1

**前置环境：** 运行 go 命令前先执行 `export PATH=$PATH:/root/.version-fox/sdks/golang/bin`

**注意：** L2 与 L3 可并行开发。L2 主要修改 `wire/token/` + `services/logic/handler/` + `internal/server/auth.go`；L3 主要修改 `internal/client/` + `services/comet/service/` + `services/gateway/forwarder.go` + `context.go` + `internal/server/health.go`，文件交集极少。若合并时遇到 `internal/server/grpc.go` 冲突，以 L2 的 TLS/auth/reflection 版本为基线，再合并 L3 的 HealthServer 导出修改。

---

## 文件结构一览

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `internal/util/recover.go` | L3-4（注：如 L2 已创建则扩展）：panic recovery helpers |
| 修改 | `context.go` | L3-1：Context 接口新增 StdContext() 方法 |
| 修改 | `services/gateway/forwarder.go` | L3-1：Forward 使用 ctx.StdContext() |
| 修改 | `services/comet/service/logic_client.go` | L3-1：所有 RPC 方法支持传入 ctx |
| 修改 | `services/comet/service/pusher.go` | L3-1：Push 使用传入 ctx |
| 修改 | `services/comet/service/service.go` | L3-1+L3-4：调用点适配 ctx 参数，gpool.Submit 加 recover |
| 修改 | `internal/client/resilient.go` | L3-5：isNoRetryError 识别 gRPC 不可重试状态码 |
| 修改 | `internal/client/pool.go` | L3-6：grpc.Dial → grpc.NewClient，dial 错误记录日志 |
| 修改 | `internal/server/health.go` | L3-3：扩展为 readiness/liveness 两个端点 |
| 修改 | `internal/server/grpc.go` | L3-3：导出 HealthServer，启动时设 NOT_SERVING |
| 修改 | `services/gateway/server.go` | L3-2+L3-3：错误处理、依赖就绪后切 SERVING |
| 修改 | `services/comet/server.go` | L3-2+L3-3：错误处理、依赖就绪后切 SERVING |
| 修改 | `services/logic/server.go` | L3-2+L3-3：错误处理、DB/Redis 就绪后切 SERVING |
| 修改 | `services/router/server.go` | L3-2：错误处理 |
| 修改 | `internal/server/limiter.go` | L3-2：空错误块改为日志记录 |
| 创建 | `internal/util/recover_test.go` | L3-4：recover helper 测试 |
| 创建 | `internal/client/resilient_test.go` | L3-5：isNoRetryError 测试 |
| 创建 | `internal/server/health_test.go` | L3-3：readiness/liveness 测试 |

---

### 任务 1：Gateway WS panic recovery + GoSafe helper（L3-4）

**文件：**
- 创建/修改：`internal/util/recover.go`
- 修改：`services/gateway/serv/handler.go`（Receive 方法中 gpool.Submit 的 panic）
- 测试：`internal/util/recover_test.go`

**问题分析：** Gateway 的 `Receive` 方法通过 `gpool.Submit` 执行业务逻辑（查看 default_server.go 或相关代码），如果 handler panic（如空指针、序列化错误），整个进程可能崩溃；`Accept`/`Disconnect` 中也无 recover。需要统一的 panic recovery helper，所有 goroutine 入口都用 `defer util.Recover(...)` 保护。

**注意：** 如果 L2-5 已创建 `internal/util/recover.go`，则在其基础上扩展；否则全新创建。

- [ ] **步骤 1：编写失败的测试**

创建 `internal/util/recover_test.go`（若已存在则扩展）：

```go
package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecover(t *testing.T) {
	assert.NotPanics(t, func() {
		defer Recover("test-location")
		panic("test panic")
	})
}

func TestRecoverNoPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		defer Recover("test-no-panic")
	})
}

func TestGoSafe(t *testing.T) {
	done := make(chan struct{})
	GoSafe("test-goroutine", func() {
		defer close(done)
		panic("test panic in goroutine")
	})
	<-done // 如果 recover 正常工作，不会死锁
}

func TestSafeRecover(t *testing.T) {
	called := make(chan interface{}, 1)
	go func() {
		defer SafeRecover("test-safe", func(r interface{}) {
			called <- r
		})
		panic("expected panic")
	}()
	r := <-called
	assert.Equal(t, "expected panic", r)
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -v -run TestRecover ./internal/util/...`
预期：FAIL（编译错误——recover.go 不存在或缺少函数）。

- [ ] **步骤 3：实现 recover helpers**

创建或修改 `internal/util/recover.go`：

```go
package util

import (
	"fmt"
	"runtime/debug"

	"github.com/klintcheng/kim/internal/logger"
)

// Recover 用于 goroutine 中的 panic 捕获
// 用法：go func() { defer util.Recover("goroutine-name"); ... }()
func Recover(location string) {
	if r := recover(); r != nil {
		logger.CommonLogger.Errorf("panic recovered in %s: %v\n%s", location, r, debug.Stack())
	}
}

// SafeRecover 带自定义回调的 panic 恢复
func SafeRecover(location string, onRecover func(r interface{})) {
	if r := recover(); r != nil {
		logger.CommonLogger.Errorf("panic recovered in %s: %v\n%s", location, r, debug.Stack())
		if onRecover != nil {
			onRecover(r)
		}
	}
}

// GoSafe 启动一个带 panic recovery 的 goroutine
func GoSafe(location string, fn func()) {
	go func() {
		defer Recover(location)
		fn()
	}()
}
```

- [ ] **步骤 4：在 Gateway handler 中应用 recover**

首先检查 `services/gateway/serv/handler.go` 中的 Receive 方法——如果 `default_server.go` 或上层已经用 gpool 执行 Receive，需要在 goroutine 入口处加 recover。查看 `default_server.go` 中消息读取循环，确认哪里启动 goroutine：

修改消息处理的 goroutine 入口（通常在 `default_server.go` 的读循环或 `channel.go` 中），在 `go func()` 第一行加 `defer util.Recover(...)`。

如果是在 `channel.go` 的 readloop/writeloop 中，修改对应的 goroutine 启动处；如果是在网关的消息分发处（如 `default_server.go` 中 `go agent.Dispatch()`），同样加上。

同时在 `Handler.Receive` 方法入口加 recover 作为双层保护：

在 `services/gateway/serv/handler.go` 的 Receive 方法开头添加：
```go
func (h *Handler) Receive(ag kim.Agent, payload []byte) {
	defer util.Recover(fmt.Sprintf("gateway.Receive channel=%s", ag.ID()))
	// ... 现有代码 ...
}
```

同理给 Accept 和 Disconnect 也加上（Accept 是在连接的 goroutine 中调用的）：
```go
func (h *Handler) Accept(conn kim.Conn, timeout time.Duration) (string, kim.Meta, error) {
	defer util.Recover("gateway.Accept")
	// ... 现有代码 ...
}

func (h *Handler) Disconnect(id string) error {
	defer util.Recover(fmt.Sprintf("gateway.Disconnect id=%s", id))
	// ... 现有代码 ...
}
```

需要 import "github.com/klintcheng/kim/internal/util"。

- [ ] **步骤 5：在 comet 服务的 gpool.Submit 处加 recover**

查看 comet 服务中是否使用 gpool 启动 goroutine（`comet` 目录下搜索 `gpool.Submit`），同样在提交的 func 第一行加 `defer util.Recover(...)`。

- [ ] **步骤 6：运行测试验证通过**

运行：`go build ./... && go vet ./...`
预期：BUILD OK，vet 无警告。
运行：`go test -v -run TestRecover ./internal/util/...`
预期：所有 4 个测试 PASS。

- [ ] **步骤 7：Commit**

```bash
git add internal/util/recover.go internal/util/recover_test.go services/gateway/serv/handler.go
git commit -m "feat(reliability): add panic recovery helpers, protect WS handlers from crashes

- Add internal/util/recover.go with Recover/SafeRecover/GoSafe helpers
- All WS handler methods (Accept/Receive/Disconnect) now have defer-recover
- Panic stack trace logged server-side, goroutine continues serving other connections
- A single bad message/connection can no longer crash the entire gateway process"
```

---

### 任务 2：context 贯穿调用链（L3-1）

**文件：**
- 修改：`context.go`（kim.Context 接口新增 StdContext）
- 修改：`services/gateway/forwarder.go`
- 修改：`services/comet/service/logic_client.go`
- 修改：`services/comet/service/pusher.go`
- 修改：`services/comet/service/service.go`（comet service impl，需要适配 ctx 传递）
- 修改：调用 logic_client / pusher 的所有 handler（comet/handler/ 目录）

**问题分析：** 当前所有 gRPC 调用都使用 `context.Background()`，导致：(1) 客户端断开连接时下游 RPC 不会取消，浪费资源；(2) 无法透传 trace metadata；(3) 超时/截止时间无法从上游传递到下游。需要让 `kim.Context` 携带标准 `context.Context`，并在所有跨服务 RPC 调用中使用它。

- [ ] **步骤 1：编写失败的测试**

首先理解 `kim.ContextImpl` 的生命周期：它在每次消息处理时创建，由 `BuildContext()` 构建。需要给它加一个 `stdCtx context.Context` 字段。

创建 `context_test.go`（如果根目录已有类似测试则扩展）：

```go
package kim

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestContextImpl_StdContext(t *testing.T) {
	c := BuildContext().(*ContextImpl)

	// 默认 context 不应为 nil
	ctx := c.StdContext()
	assert.NotNil(t, ctx)

	// WithValue 能正确传递
	type ctxKey struct{}
	c2 := c.WithStdContext(context.WithValue(ctx, ctxKey{}, "test"))
	assert.Equal(t, "test", c2.StdContext().Value(ctxKey{}))
}

func TestContextImpl_StdContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := BuildContext().(*ContextImpl).WithStdContext(ctx)

	cancel()
	select {
	case <-c.StdContext().Done():
		// 预期：cancel 后 Done() 关闭
	case <-time.After(time.Second):
		t.Fatal("context should be canceled")
	}
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -v -run TestContextImpl_StdContext ./.`
预期：FAIL（编译错误——StdContext/WithStdContext 方法未定义）。

- [ ] **步骤 3：修改 Context 接口和实现**

修改 `context.go`：

1. 在 `Context` 接口中添加方法：
```go
type Context interface {
	Dispatcher
	SessionStorage
	Header() *pkt.Header
	ReadBody(val proto.Message) error
	Session() Session
	RespWithError(status pkt.Status, err error) error
	Resp(status pkt.Status, body proto.Message) error
	Dispatch(body proto.Message, recvs ...*Location) error
	Next()
	StdContext() context.Context          // 新增：获取标准 context
	WithStdContext(ctx context.Context) Context  // 新增：设置标准 context（链式调用）
}
```

2. 在 `ContextImpl` 结构体中添加字段：
```go
type ContextImpl struct {
	Dispatcher
	SessionStorage

	handlers HandlersChain
	index    int
	request  *pkt.LogicPkt
	session  Session
	stdCtx   context.Context  // 新增
}
```

3. 实现方法：
```go
func (c *ContextImpl) StdContext() context.Context {
	if c.stdCtx == nil {
		return context.Background()
	}
	return c.stdCtx
}

func (c *ContextImpl) WithStdContext(ctx context.Context) Context {
	c.stdCtx = ctx
	return c
}
```

4. 修改 `reset()` 方法清理 stdCtx：
```go
func (c *ContextImpl) reset() {
	c.request = nil
	c.index = 0
	c.handlers = nil
	c.session = nil
	c.stdCtx = nil
}
```

5. 修改 `BuildContext()` 初始化 stdCtx：
```go
func BuildContext() Context {
	return &ContextImpl{
		stdCtx: context.Background(),
	}
}
```

6. 添加 `"context"` import。

- [ ] **步骤 4：修改 CometForwarder.Forward 使用 ctx**

问题：Forward 方法当前不接收 `kim.Context` 参数，但它是由 `serv.Handler.Receive` 等调用的，调用方有 ctx。

查看 `Forwarder` 接口定义（在 `services/gateway/serv/handler.go` 第 40-42 行）：
```go
type Forwarder interface {
	Forward(p *pkt.LogicPkt) error
}
```

需要修改接口签名，传入 context：

修改 `services/gateway/serv/handler.go` 中的 Forwarder 接口：
```go
type Forwarder interface {
	Forward(ctx context.Context, p *pkt.LogicPkt) error
}
```

修改 `services/gateway/forwarder.go`：

1. 修改 Forward 方法签名和实现，使用传入 ctx：
```go
func (f *CometForwarder) Forward(ctx context.Context, p *pkt.LogicPkt) error {
	if p == nil || p.Command == "" || p.ChannelId == "" {
		return fmt.Errorf("invalid packet")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// ... 中间查找服务的逻辑不变 ...

	// 第 76 行：将 context.Background() 替换为 ctx
	cli := rpc.NewCometServiceClient(conn)
	_, err = cli.Forward(ctx, &rpc.ForwardReq{
		Packet: pkt.Marshal(p),
	})
	return err
}
```

2. 修改所有调用 Forward 的地方传入 ctx：
   - `services/gateway/serv/handler.go` 的 Accept 方法（第 123 行）：`err = h.Forwarder.Forward(context.Background(), req)` — Accept 是在新连接时调用的，此时没有 kim.Context，使用 `context.Background()` 即可
   - `services/gateway/serv/handler.go` 的 Receive 方法（第 161 行）：此处是消息处理，如果有 kim.Context 则使用，否则 `context.Background()` — 注意 Receive 的签名是 `(ag kim.Agent, payload []byte)`，没有直接的 kim.Context。这里使用 `context.Background()` 作为第一阶段改进，后续 L5 重构时再从 Agent 获取 ctx。
   - `services/gateway/serv/handler.go` 的 Disconnect 方法（第 182 行）：使用 `context.Background()`

- [ ] **步骤 5：修改 LogicClient 支持传入 ctx**

当前 `LogicClient` 的所有方法第一个参数是 `app string`，但没有 ctx。需要调整方法签名加入 ctx，或者让调用方通过参数传递。最简洁的方式是修改方法签名，将 ctx 作为第一个参数：

修改 `services/comet/service/logic_client.go`：

1. 修改 Message/Group/User 接口：
```go
type Message interface {
	InsertUser(ctx context.Context, app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error)
	InsertGroup(ctx context.Context, app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error)
	SetAck(ctx context.Context, app string, req *rpc.AckMessageReq) error
	GetMessageIndex(ctx context.Context, app string, req *rpc.GetOfflineMessageIndexReq) (*rpc.GetOfflineMessageIndexResp, error)
	GetMessageContent(ctx context.Context, app string, req *rpc.GetOfflineMessageContentReq) (*rpc.GetOfflineMessageContentResp, error)
}

type Group interface {
	Create(ctx context.Context, app string, req *rpc.CreateGroupReq) (*rpc.CreateGroupResp, error)
	Members(ctx context.Context, app string, req *rpc.GroupMembersReq) (*rpc.GroupMembersResp, error)
	Join(ctx context.Context, app string, req *rpc.JoinGroupReq) error
	Quit(ctx context.Context, app string, req *rpc.QuitGroupReq) error
	Detail(ctx context.Context, app string, req *rpc.GetGroupReq) (*rpc.GetGroupResp, error)
}

type User interface {
	Login(ctx context.Context, app string, req *rpc.LoginReq) error
}
```

2. 修改所有实现方法，将 `context.Background()` 替换为传入的 ctx：

```go
func (c *LogicClient) InsertUser(ctx context.Context, app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := c.resilient.Call(ctx, "InsertUserMessage",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).InsertUserMessage(ctx, req)
		})
	// ... 其余不变 ...
}
```

对所有方法（InsertGroup/SetAck/GetMessageIndex/GetMessageContent/Create/Members/Join/Quit/Detail/Login）做同样修改：将 `context.Background()` 替换为传入的 ctx 参数。

- [ ] **步骤 6：修改 GatewayPusher.Push 支持 ctx**

修改 `services/comet/service/pusher.go`：

```go
type GatewayPusher struct {
	pool *client.Pool
}

func NewGatewayPusher(pool *client.Pool) *GatewayPusher {
	return &GatewayPusher{pool: pool}
}

// Push 将消息推送到指定 gateway 的多个 Channel
func (p *GatewayPusher) Push(ctx context.Context, gateway string, channels []string, packet *pkt.LogicPkt) error {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := p.pool.Get(gateway)
	if err != nil {
		return err
	}
	packetBytes := pkt.Marshal(packet)
	cli := rpc.NewGatewayServiceClient(conn)
	_, err = cli.Push(ctx, &rpc.PushReq{
		ChannelIds: strings.Join(channels, ","),
		Packet:     packetBytes,
	})
	return err
}
```

注意：`kim.Dispatcher` 接口定义在根目录，需要检查签名。查看 `dispatcher.go`：

需要修改 `kim.Dispatcher` 接口的 Push 方法签名加入 ctx。读取 `dispatcher.go` 确认当前签名：

当前 Dispatcher 接口（查看代码确认）：
```go
type Dispatcher interface {
	Push(gateway string, channels []string, p *pkt.LogicPkt) error
}
```

需要改为：
```go
type Dispatcher interface {
	Push(ctx context.Context, gateway string, channels []string, p *pkt.LogicPkt) error
}
```

这会影响 `ContextImpl.Dispatch` 方法和 `ContextImpl.Resp` 方法中的 `c.Push(...)` 调用：
- 修改 `context.go` 的 Resp 方法中 `c.Push(c.Session().GetGateId(), []string{c.Session().GetChannelId()}, packet)` → `c.Push(c.StdContext(), c.Session().GetGateId(), []string{c.Session().GetChannelId()}, packet)`
- 修改 `context.go` 的 Dispatch 方法中 `c.Push(gateway, ids, packet)` → `c.Push(c.StdContext(), gateway, ids, packet)`

同时检查所有实现 Dispatcher 接口的地方（GatewayPusher、可能还有其他 mock），统一修改签名。

- [ ] **步骤 7：修改 comet 中所有调用 logic_client / pusher 的 handler**

搜索 comet handler 目录下所有调用 `.InsertUser`、`.Push`、`.Login` 等方法的地方，将 `ctx context.Context` 作为第一个参数传入。这些 handler 方法应该已经有 `kim.Context` 参数，使用 `c.StdContext()` 获取标准 ctx 传入。

例如，在 `services/comet/handler/message_handler.go` 中（或类似文件）：
- `h.Message.InsertUser(...)` → `h.Message.InsertUser(c.StdContext(), ...)`
- `h.Pusher.Push(...)` → `h.Pusher.Push(c.StdContext(), ...)`

逐一修复所有编译错误。

- [ ] **步骤 8：运行测试验证通过**

运行：`go build ./... && go vet ./...`
预期：BUILD OK，vet 无警告。
运行：`go test -v -run TestContextImpl_StdContext ./.`
预期：PASS。

- [ ] **步骤 9：Commit**

```bash
git add context.go context_test.go dispatcher.go services/gateway/serv/handler.go services/gateway/forwarder.go services/comet/service/logic_client.go services/comet/service/pusher.go services/comet/service/service.go services/comet/handler/*.go
git commit -m "feat(reliability): thread context.Context through entire RPC call chain

- Add Context.StdContext()/WithStdContext() to kim.Context interface
- Change Dispatcher.Push signature to accept context.Context
- Change Forwarder.Forward signature to accept context.Context
- Change LogicClient methods to accept context.Context as first parameter
- Change GatewayPusher.Push to accept context.Context
- Replace all context.Background() with upstream ctx in inter-service calls
- Client cancellation now propagates downstream to cancel in-flight RPCs
- Trace metadata can now flow across service boundaries"
```

---

### 任务 3：isNoRetryError 识别 gRPC 状态码（L3-5）

**文件：**
- 修改：`internal/client/resilient.go`
- 测试：`internal/client/resilient_test.go`

**问题分析：** 当前 `isNoRetryError` 只识别 `context.Canceled`/`context.DeadlineExceeded`，但参数错误（InvalidArgument）、权限错误（PermissionDenied）、未找到（NotFound）等明确的客户端错误不应重试——重试只会浪费资源、增加延迟。需要使用 `status.FromError()` 提取 gRPC 状态码，对明确不可恢复的错误直接返回，不触发重试。

- [ ] **步骤 1：编写失败的测试**

创建 `internal/client/resilient_test.go`：

```go
package client

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsNoRetryError_ContextCanceled(t *testing.T) {
	assert.True(t, isNoRetryError(context.Canceled))
	assert.True(t, isNoRetryError(context.DeadlineExceeded))
}

func TestIsNoRetryError_GrpcStatusCodes(t *testing.T) {
	noRetryCodes := []codes.Code{
		codes.InvalidArgument,
		codes.PermissionDenied,
		codes.NotFound,
		codes.AlreadyExists,
		codes.FailedPrecondition,
		codes.OutOfRange,
		codes.Unimplemented,
		codes.Unauthenticated,
	}
	for _, c := range noRetryCodes {
		err := status.Error(c, "test error")
		assert.True(t, isNoRetryError(err), "code %s should not be retried", c)
	}
}

func TestIsNoRetryError_RetriableCodes(t *testing.T) {
	retryCodes := []codes.Code{
		codes.Unavailable,
		codes.DeadlineExceeded,
		codes.ResourceExhausted,
		codes.Internal,
	}
	for _, c := range retryCodes {
		err := status.Error(c, "test error")
		assert.False(t, isNoRetryError(err), "code %s should be retried", c)
	}
}

func TestIsNoRetryError_NilError(t *testing.T) {
	assert.False(t, isNoRetryError(nil))
}

func TestIsNoRetryError_GenericError(t *testing.T) {
	assert.False(t, isNoRetryError(errors.New("generic error")))
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -v -run TestIsNoRetryError ./internal/client/...`
预期：FAIL（gRPC 状态码未被识别，测试断言失败）。

- [ ] **步骤 3：增强 isNoRetryError**

修改 `internal/client/resilient.go`，添加 gRPC status 导入和状态码判断：

```go
package client

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ... 现有代码不变 ...

// isNoRetryError 判断是否不可重试的错误
func isNoRetryError(err error) bool {
	if err == nil {
		return false
	}
	// context 取消/超时
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	// gRPC 状态码判断
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.InvalidArgument,
			codes.PermissionDenied,
			codes.NotFound,
			codes.AlreadyExists,
			codes.FailedPrecondition,
			codes.OutOfRange,
			codes.Unimplemented,
			codes.Unauthenticated:
			// 明确的客户端错误/不可恢复错误，不重试
			return true
		}
	}
	return false
}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`go build ./internal/client/... && go vet ./internal/client/...`
预期：BUILD OK。
运行：`go test -v -run TestIsNoRetryError ./internal/client/...`
预期：所有测试 PASS。

- [ ] **步骤 5：Commit**

```bash
git add internal/client/resilient.go internal/client/resilient_test.go
git commit -m "feat(reliability): recognize non-retriable gRPC status codes

- isNoRetryError now uses status.FromError to extract gRPC codes
- Non-retriable: InvalidArgument, PermissionDenied, NotFound, AlreadyExists,
  FailedPrecondition, OutOfRange, Unimplemented, Unauthenticated
- Retriable (default): Unavailable, DeadlineExceeded, ResourceExhausted, Internal
- Context cancellation/deadline still short-circuits immediately
- Prevents wasted retry attempts on permanent client errors"
```

---

### 任务 4：grpc.Dial → grpc.NewClient + 错误日志（L3-6）

**文件：**
- 修改：`internal/client/pool.go`

**问题分析：** `grpc.Dial` 已废弃，推荐使用 `grpc.NewClient`（阻塞式、懒连接，可立即返回配置错误）。同时当前 `refresh()` 中 `grpc.Dial` 失败时直接 `continue` 没有日志，导致连接失败时难以排查。需要迁移并增加错误日志。

**注意：** 如果 L2-4 已修改 pool.go 添加 TLS 支持，则在此基础上做 Dial→NewClient 迁移；否则在当前版本上修改。

- [ ] **步骤 1：编写失败的测试**

这一改动主要是 API 迁移和日志增强，逻辑不变，单元测试验证连接池基本功能即可。创建简单的连通性测试（如果不存在）：

```go
package client

import (
	"testing"

	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/naming"
	"github.com/stretchr/testify/assert"
)

// mockNaming 实现 naming.Naming 接口用于测试
type mockNaming struct{}

func (m *mockNaming) Find(serviceName string) ([]naming.ServiceRegistration, error) {
	return nil, nil
}
func (m *mockNaming) Subscribe(serviceName string, callback func([]naming.ServiceRegistration)) error {
	return nil
}
func (m *mockNaming) Unsubscribe(serviceName string) error {
	return nil
}
func (m *mockNaming) Register(service naming.ServiceRegistration) error {
	return nil
}
func (m *mockNaming) Deregister(serviceID string) error {
	return nil
}

func TestNewPool(t *testing.T) {
	p := NewPoolWithConfig(&mockNaming{}, "test-service", config.DefaultResilienceConfig())
	assert.NotNil(t, p)
	p.Close()
}
```

运行测试确认编译通过。实际上主要验证点是 `go build ./...` 不报错。

- [ ] **步骤 2：迁移 grpc.Dial → grpc.NewClient**

修改 `internal/client/pool.go` 的 `refresh` 方法：

1. 将 `grpc.Dial(addr, ...)` 改为 `grpc.NewClient(addr, ...)`
2. 捕获错误并日志记录（当前只 `continue`，改为 logger.Errorf）
3. 注意：`grpc.NewClient` 返回的 `*grpc.ClientConn` 是懒连接的，不会立即拨号；但错误（如配置错误）会立即返回

```go
func (p *Pool) refresh() {
	services, err := p.naming.Find(p.serviceName)
	if err != nil {
		logger.CommonLogger.Warnf("pool: find %s failed: %v", p.serviceName, err)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	currentIDs := make(map[string]bool)
	for _, svc := range services {
		id := svc.ServiceID()
		currentIDs[id] = true
		if _, exists := p.conns[id]; !exists {
			addr := fmt.Sprintf("%s:%d", svc.PublicAddress(), svc.PublicPort())
			interceptors := InterceptorChain(p.serviceName, id, p.cfg)

			dialOpts := []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(10*1024*1024)),
				grpc.WithChainUnaryInterceptor(interceptors...),
				grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			}

			// 如 L2 已加 TLS 支持，这里保留 TLS 选项逻辑
			// if p.grpcCfg.TLSEnable { ... }

			conn, err := grpc.NewClient(addr, dialOpts...)
			if err != nil {
				logger.CommonLogger.Errorf("pool: new client for %s/%s at %s: %v", p.serviceName, id, addr, err)
				continue
			}
			p.conns[id] = conn
		}
	}

	for id, conn := range p.conns {
		if !currentIDs[id] {
			if err := conn.Close(); err != nil {
				logger.CommonLogger.Warnf("pool: close connection to %s/%s: %v", p.serviceName, id, err)
			}
			delete(p.conns, id)
		}
	}
}
```

同时需要检查 `watch()` 方法中 `p.naming.Subscribe` 的错误处理：当前是 `_ = p.naming.Subscribe(...)`，改为日志记录错误：

```go
func (p *Pool) watch() {
	p.refresh()

	if err := p.naming.Subscribe(p.serviceName, func(services []kim.ServiceRegistration) {
		p.refresh()
	}); err != nil {
		logger.CommonLogger.Errorf("pool: subscribe to %s failed: %v", p.serviceName, err)
	}

	<-p.done
	if err := p.naming.Unsubscribe(p.serviceName); err != nil {
		logger.CommonLogger.Warnf("pool: unsubscribe from %s: %v", p.serviceName, err)
	}
}
```

- [ ] **步骤 3：运行测试验证通过**

运行：`go build ./... && go vet ./...`
预期：BUILD OK，vet 无警告。
运行：`go test -v ./internal/client/...`
预期：PASS。

- [ ] **步骤 4：Commit**

```bash
git add internal/client/pool.go internal/client/pool_test.go
git commit -m "feat(reliability): migrate grpc.Dial to grpc.NewClient, log dial errors

- Replace deprecated grpc.Dial with grpc.NewClient (lazy, non-blocking)
- Log error when NewClient fails instead of silently continuing
- Log error when Subscribe fails instead of discarding it
- Log warning when Unsubscribe or connection Close fails
- Improves debuggability of connection pool issues"
```

---

### 任务 5：错误处理审计（L3-2）

**文件：**
- 修改：`internal/server/limiter.go`
- 修改：`services/gateway/server.go`
- 修改：`services/comet/server.go`
- 修改：`services/logic/server.go`
- 修改：`services/router/server.go`
- 修改：`services/comet/handler/login_handler.go`（如有错误吞掉）

**问题分析：** 多处使用 `_ = err` 丢弃关键错误：
1. `_ = ns.Register(...)` 服务注册失败直接忽略，服务启动后 Consul 找不到
2. `limiter.go:51-53` `flow.LoadRules` 错误空块，限流规则加载失败不可见
3. Redis/Cache 操作失败未传播，导致登录成功但缓存没写入（后续请求从缓存取 token 失败）
4. 各服务 server.go 中 `f(http.ListenAndServe(...))` 的错误未检查（L1 已启动 MonitorPort 但错误未处理）

- [ ] **步骤 1：编写失败的测试**

错误处理修复主要靠代码审计和编译验证，为关键可测试点编写测试：

由于 limiter 是 Sentinel 全局状态，测试较复杂，此处以 `go build + go vet + 手动代码审查` 为主要验证方式。

创建简单的编译验证测试即可。

- [ ] **步骤 2：修复 limiter.go 空错误块**

修改 `internal/server/limiter.go` 第 51-53 行：

```go
if _, loaded := serverLimiterRules.LoadOrStore(resource, true); !loaded {
	_, err := flow.LoadRules([]*flow.Rule{
		{
			Resource:               resource,
			Threshold:              cfg.ServerQPS,
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject,
		},
	})
	if err != nil {
		logger.CommonLogger.Errorf("limiter: load rules for %s failed: %v", resource, err)
	}
}
```

需要导入 `"github.com/klintcheng/kim/internal/logger"`。

- [ ] **步骤 3：修复各服务 server.go 中 ns.Register 错误处理**

检查每个服务的 server.go（gateway/comet/logic/router），找到类似 `_ = naming.Register(ns, svc)` 的地方，改为：

```go
if err := naming.Register(ns, svc); err != nil {
	return nil, fmt.Errorf("register service: %w", err)
}
```

- [ ] **步骤 4：修复 Monitor HTTP server 错误处理**

在各服务 `Start()` 方法中，`go server.StartMonitorHTTP(monitorPort)` 改为错误记录：

```go
go func() {
	if err := server.StartMonitorHTTP(cfg.MonitorPort); err != nil {
		logger.<Service>Logger.Errorf("monitor http server: %v", err)
	}
}()
```

或者使用 `util.GoSafe` 启动更安全。

- [ ] **步骤 5：修复 comet/login_handler.go 中 Cache/Redis 错误**

检查 `services/comet/handler/login_handler.go`（或对应位置），找到 Redis Get/Set 错误被吞掉的地方，改为错误传播或日志记录。根据设计文档：
- RedisGet 错误应传播给客户端（或至少记录）
- Cache.Set 失败应返回错误让登录失败（一致性优先）

- [ ] **步骤 6：修复 logic 中 DB 操作错误处理**

检查 `services/logic/handler/` 下的 `_ = db.Create(...)` 或类似丢弃错误的写法，改为返回错误。

- [ ] **步骤 7：运行测试验证通过**

运行：`go build ./... && go vet ./...`
预期：BUILD OK，vet 无警告。
运行：`go test ./...`
预期：所有已有测试 PASS。

- [ ] **步骤 8：Commit**

```bash
git add internal/server/limiter.go services/*/server.go services/comet/handler/login_handler.go services/logic/handler/*.go
git commit -m "feat(reliability): audit and fix ignored errors across services

- limiter.go: log error when flow.LoadRules fails instead of silent discard
- server.go: check naming.Register error and fail-fast instead of _
- server.go: log Monitor HTTP server errors instead of silently dying
- login_handler: propagate Cache.Set/Redis errors instead of ignoring
- All discarded errors now either logged or returned to caller
- Services fail-fast on startup errors instead of running in degraded state"
```

---

### 任务 6：健康检查 readiness/liveness 区分（L3-3）

**文件：**
- 修改：`internal/server/health.go`
- 修改：`internal/server/grpc.go`（导出 HealthServer 供外部设置状态）
- 修改：`services/gateway/server.go`、`services/comet/server.go`、`services/logic/server.go`

**问题分析：** 当前 `/health` 端点永远返回 200，gRPC health 也永远是 SERVING，无法反映真实依赖状态：DB 未连接、Redis 不可用、Naming 服务未就绪时服务不应接收流量。需要区分：
- **liveness**（`/healthz/live`）：进程活着 → 总是 200
- **readiness**（`/healthz/ready`）：DB/Redis/Naming 都就绪 → 200，否则 503；对应 gRPC health 设为 SERVING/NOT_SERVING

- [ ] **步骤 1：编写失败的测试**

创建 `internal/server/health_test.go`：

```go
package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLivenessHandler(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz/live", livenessHandler)

	req := httptest.NewRequest("GET", "/healthz/live", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestReadinessHandler_NotReady(t *testing.T) {
	ready := false
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz/ready", readinessHandler(func() bool { return ready }))

	req := httptest.NewRequest("GET", "/healthz/ready", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestReadinessHandler_Ready(t *testing.T) {
	ready := true
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz/ready", readinessHandler(func() bool { return ready }))

	req := httptest.NewRequest("GET", "/healthz/ready", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -v -run TestReadiness ./internal/server/...`
预期：FAIL（编译错误——livenessHandler/readinessHandler 未定义）。

- [ ] **步骤 3：修改 internal/server/health.go**

```go
package server

import (
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ready 是原子操作的 readiness 标志：1=就绪，0=未就绪
var ready int32

// SetReady 设置 readiness 状态
func SetReady(isReady bool) {
	if isReady {
		atomic.StoreInt32(&ready, 1)
	} else {
		atomic.StoreInt32(&ready, 0)
	}
}

// IsReady 检查 readiness 状态
func IsReady() bool {
	return atomic.LoadInt32(&ready) == 1
}

// livenessHandler 存活探针：进程在运行就返回 200
func livenessHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// readinessHandler 就绪探针：依赖都可用才返回 200，否则 503
func readinessHandler(check func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if check != nil && !check() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
			return
		}
		if !IsReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}
}

// NewMonitorMux 创建监控 HTTP mux，支持 liveness/readiness/metrics
func NewMonitorMux(customReadyCheck ...func() bool) *http.ServeMux {
	mux := http.NewServeMux()

	// 旧版 /health 端点保持兼容（等同于 live + ready）
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !IsReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/healthz/live", livenessHandler)

	var readyCheck func() bool
	if len(customReadyCheck) > 0 {
		readyCheck = customReadyCheck[0]
	}
	mux.HandleFunc("/healthz/ready", readinessHandler(readyCheck))

	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

// StartMonitorHTTP 启动监控 HTTP 服务
func StartMonitorHTTP(addr string, customReadyCheck ...func() bool) error {
	mux := NewMonitorMux(customReadyCheck...)
	return http.ListenAndServe(addr, mux)
}
```

- [ ] **步骤 4：修改 internal/server/grpc.go 导出 HealthServer**

**注意：** 如果 L2-4 已重构 grpc.go（添加 TLS/auth/reflection 选项），则在 L2 版本基础上做以下修改；否则修改当前版本。

1. GRPCServer 结构体需要持有 hs（health server）：
```go
type GRPCServer struct {
	*grpc.Server
	addr string
	hs   *health.Server // 如果 L2 版本已有此字段则保留
}
```

2. 启动时设置 NOT_SERVING 而非 SERVING：
```go
hs := health.NewServer()
hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
healthpb.RegisterHealthServer(s, hs)
```

3. 添加 HealthServer() 方法供外部访问（如果 L2 版本已有则保留）：
```go
func (s *GRPCServer) HealthServer() *health.Server {
	return s.hs
}
```

4. 注意：reflection.Register 改为仅在配置启用时注册（如果 L2 已做则无需重复）。

- [ ] **步骤 5：在各服务中集成 readiness 状态切换**

**gateway/server.go：**
1. 启动 gRPC 服务和 Monitor HTTP 时，初始设 NOT_SERVING
2. 所有依赖初始化完成（naming 注册成功、pool 建立连接等）后，调用 `server.SetReady(true)` 和 `grpcSrv.HealthServer().SetServingStatus("", healthpb.HealthCheckResponse_SERVING)`

**comet/server.go：**
- 同理，依赖初始化完成后切 SERVING

**logic/server.go：**
- DB 和 Redis 连接成功后切 SERVING
- 提供 customReadyCheck 函数：`ping` DB 和 Redis 检查连通性

```go
// 示例：logic server 的 readiness 检查
readyCheck := func() bool {
	if s.baseDb != nil {
		sqlDB, err := s.baseDb.DB()
		if err != nil || sqlDB.Ping() != nil {
			return false
		}
	}
	if s.messageDb != nil {
		sqlDB, err := s.messageDb.DB()
		if err != nil || sqlDB.Ping() != nil {
			return false
		}
	}
	if s.Cache != nil {
		if err := s.Cache.Ping(context.Background()).Err(); err != nil {
			return false
		}
	}
	return true
}
```

Monitor HTTP 启动时传入 readyCheck：
```go
util.GoSafe("monitor-http", func() {
	if err := server.StartMonitorHTTP(cfg.MonitorPort, readyCheck); err != nil {
		logger.LogicLogger.Errorf("monitor http: %v", err)
	}
})
```

- [ ] **步骤 6：运行测试验证通过**

运行：`go build ./... && go vet ./...`
预期：BUILD OK，vet 无警告。
运行：`go test -v -run TestReadiness ./internal/server/...`
预期：所有 3 个测试 PASS。

- [ ] **步骤 7：Commit**

```bash
git add internal/server/health.go internal/server/health_test.go internal/server/grpc.go services/gateway/server.go services/comet/server.go services/logic/server.go
git commit -m "feat(reliability): distinguish liveness and readiness health checks

- Add /healthz/live (always 200 if process is up)
- Add /healthz/ready (200 only when DB/Redis/Naming ready, 503 otherwise)
- Add /healthz/ready custom check function for DB/Redis ping
- gRPC health starts as NOT_SERVING, switches to SERVING after deps ready
- Atomic ready flag for thread-safe state toggling
- Old /health endpoint kept for backward compatibility (returns ready status)
- Consul/K8s can now properly route traffic away from unready instances"
```

---

## 端到端验证清单（L3 完成后执行）

- [ ] `go build ./...` 无错误
- [ ] `go vet ./...` 无警告
- [ ] `go test ./...` 所有测试通过
- [ ] `grep -rn "context.Background()" services/ internal/` 仅在 main/初始化位置出现，RPC 调用点均使用传入 ctx
- [ ] `grep -rn "_ = " services/ internal/ --include="*.go" | grep -v "_ = err" | grep -v "_ = w.Write" | grep -v "_ = conn.Close"` 无关键错误丢弃
- [ ] `grep -n "grpc.Dial(" internal/client/pool.go` 无匹配（已全部迁移到 NewClient）
- [ ] 启动服务后 `curl localhost:8001/healthz/ready` 返回 503（未就绪），等几秒后返回 200
- [ ] 故意 panic 一个 WS handler（构造异常消息），gateway 进程仍在运行、日志记录堆栈
- [ ] 客户端取消请求后，下游 gRPC 调用也被取消（可通过日志或 trace 验证）
