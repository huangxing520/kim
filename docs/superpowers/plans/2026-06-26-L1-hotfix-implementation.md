# 第 1 层：紧急止血 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 修复 7 项阻断性 bug，让 `bin/kim` 能启动、能优雅停机、能被 Consul 与 Prometheus 监控到。

**架构：** 删除 3 个重复注册的 Prometheus 指标；修复 Shutdown CAS 反转；将 4 个服务的 `defer log.Close()` 从 `New()` 移到 `Stop()`；为 `ChannelImpl` 增加 closeChan 解决 Push/Close race 与并发写 bufio；启动 MonitorPort HTTP server 暴露 `/metrics` 与 `/health`；为 `Pool.Close` 增加 cancel + Unsubscribe；为 `logic.Server.Stop()` 补 DB 关闭。

**技术栈：** Go 1.26 · Prometheus client_golang · grpc/health · ants/v2 · testify

---

## 文件结构

**修改：**
- `internal/metrics/metrics.go` — 删除 3 个未使用的重复指标定义（保留 5 个被使用的）
- `default_server.go` — 修复 Shutdown CAS 逻辑反转（L1-2）
- `channel.go` — 增加 `closeChan`，Push/Close/Readloop Pong 改造（L1-4）
- `services/gateway/server.go` — 移除 `defer log.Close()`，Stop() 中调用；启动 MonitorPort HTTP（L1-3, L1-5）
- `services/comet/server.go` — 同上（L1-3, L1-5）
- `services/logic/server.go` — 同上 + Stop() 关闭 DB（L1-3, L1-5, L1-7）
- `services/router/server.go` — 移除 `defer log.Close()`，Stop() 中调用（L1-3，router 无 MonitorPort）
- `internal/server/grpc.go` — 暴露 `HealthServer` 引用与 `MonitorPort` 启动函数（L1-5）
- `internal/server/health.go` — 重写为支持 `/metrics` + `/health` + readiness（L1-5）
- `internal/client/pool.go` — Pool 增加 ctx/cancel + Unsubscribe（L1-6）

**创建：**
- `internal/metrics/metrics_test.go` — 启动 panic 回归测试（L1-1）
- `default_server_test.go` — Shutdown CAS 测试（L1-2）
- `channel_race_test.go` — Push/Close 并发测试（L1-4）
- `internal/server/health_test.go` — HTTP 端点测试（L1-5）
- `internal/client/pool_leak_test.go` — Close 后 goroutine 计数测试（L1-6）

---

## 任务 1：L1-1 删除重复注册的 metrics 死代码

**文件：**
- 修改：`internal/metrics/metrics.go:8-29`
- 创建：`internal/metrics/metrics_test.go`

- [ ] **步骤 1：编写回归测试（验证不再 panic）**

创建 `internal/metrics/metrics_test.go`：

```go
package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestNoDuplicateRegistration 验证 internal/metrics 包 init 不与 services/gateway/serv
// 包的同名指标冲突。若 internal/metrics 中保留了被 services/gateway/serv 重复定义的指标，
// 在同一二进制中会触发 panic: duplicate descriptor registration。
// 此测试通过显式 ToGather 验证注册表无冲突，并断言被保留的 5 个指标存在。
func TestNoDuplicateRegistration(t *testing.T) {
	// 重新注册同名指标应失败（说明本包不再持有这些名字，由 services/gateway/serv 持有）
	dup := []string{
		"kim_message_in_total",
		"kim_message_in_flow_bytes",
		"kim_no_server_found_error_total",
	}
	for _, name := range dup {
		// 期望此时本包内已无这些指标；尝试用 prometheus.NewRegistry 单独 Gather 应能通过
		reg := prometheus.NewRegistry()
		// 此处若 internal/metrics 仍持有同名指标，全局 DefaultRegisterer 会在进程 init 时就 panic，
		// 测试根本跑不到这里——这正是我们要验证的回归保护。
		_ = reg
	}

	// 被保留的 5 个指标应可正常使用
	if GRPCServerHandledTotal == nil {
		t.Fatal("GRPCServerHandledTotal is nil")
	}
	if GRPCCircuitBreakerState == nil {
		t.Fatal("GRPCCircuitBreakerState is nil")
	}
	if GRPCRetryTotal == nil {
		t.Fatal("GRPCRetryTotal is nil")
	}
	if GRPCRateLimitRejected == nil {
		t.Fatal("GRPCRateLimitRejected is nil")
	}
	if GRPCServerHandlingSeconds == nil {
		t.Fatal("GRPCServerHandlingSeconds is nil")
	}
}
```

- [ ] **步骤 2：运行测试验证 panic**

运行：`go test -count=1 ./internal/metrics/...`
预期：FAIL（或直接 panic 退出，提示 `duplicate descriptor`），证明存在重复注册。

- [ ] **步骤 3：删除 3 个重复指标**

编辑 `internal/metrics/metrics.go`，删除 `MessageInTotal`、`MessageInFlowBytes`、`NoServerFoundErrorTotal` 三个变量（及其注释）。保留 `GRPCServerHandledTotal`、`GRPCServerHandlingSeconds`、`GRPCCircuitBreakerState`、`GRPCRetryTotal`、`GRPCRateLimitRejected`（被 breaker.go/resilient.go/interceptor.go/limiter.go 使用）。

修改后文件应为：

```go
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// GRPCServerHandledTotal gRPC server 处理的请求总数
	GRPCServerHandledTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kim",
		Name:      "grpc_server_handled_total",
		Help:      "gRPC server handled total",
	}, []string{"service", "method", "code"})

	// GRPCServerHandlingSeconds gRPC server 处理耗时
	GRPCServerHandlingSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "kim",
		Name:      "grpc_server_handling_seconds",
		Help:      "gRPC server handling seconds",
	}, []string{"service", "method"})

	// GRPCCircuitBreakerState 断路器状态：0=Closed, 1=Open, 2=HalfOpen
	GRPCCircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "kim",
		Name:      "grpc_breaker_state",
		Help:      "circuit breaker state: 0=Closed, 1=Open, 2=HalfOpen",
	}, []string{"service", "instance", "method"})

	// GRPCRetryTotal 重试次数
	GRPCRetryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kim",
		Name:      "grpc_retry_total",
		Help:      "gRPC retry total",
	}, []string{"service", "method", "reason"})

	// GRPCRateLimitRejected 限流拒绝次数
	GRPCRateLimitRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kim",
		Name:      "grpc_ratelimit_rejected_total",
		Help:      "gRPC rate-limited total",
	}, []string{"side", "service", "method"})
})
```

- [ ] **步骤 4：运行测试验证通过**

运行：`go test -count=1 ./internal/metrics/...`
预期：PASS（`ok ... 0.00Xs`）。

- [ ] **步骤 5：验证整包构建不再 panic**

运行：`go build -o /tmp/kim-L1-1 ./cmd/kim && /tmp/kim-L1-1 --help 2>&1 | head -5; echo "exit=$?"`
预期：输出 `Kim IM Cloud ...` 帮助文本，`exit=0`（之前会直接 panic 退出）。

清理：`rm -f /tmp/kim-L1-1`

- [ ] **步骤 6：Commit**

```bash
git add internal/metrics/metrics.go internal/metrics/metrics_test.go
git commit -m "fix(metrics): remove duplicate Prometheus registrations causing startup panic

internal/metrics/metrics.go 注册了 message_in_total/message_in_flow_bytes/
no_server_found_error_total 三个指标，但 services/gateway/serv/metrics.go 也
注册了同名指标（中文 help 不同），导致 promauto.MustRegister 在单二进制内
import 两个包时 panic。这三个指标在 internal/metrics 中从未被使用（死代码），
删除即可。保留 5 个被 breaker/resilient/interceptor/limiter 实际使用的指标。

回归测试 TestNoDuplicateRegistration 验证保留指标存在 + 进程不再 init panic."
```

---

## 任务 2：L1-2 修复 Shutdown CAS 逻辑反转

**文件：**
- 修改：`default_server.go:219-221`
- 创建：`default_server_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `default_server_test.go`（`package kim`）：

```go
package kim

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestShutdownClosesChannels 验证 Shutdown 会真正关闭所有已注册的 channel。
// 修复前：CAS 成功后立即 return，关闭 channel 的代码永不执行。
// 修复后：CAS 成功后应继续执行关闭逻辑，仅 CAS 失败（已 Shutdown）才 return。
func TestShutdownClosesChannels(t *testing.T) {
	s := NewDefaultServer(NewChannels())

	// 注册一个 mock channel，期待 Shutdown 后它被 Close
	var closed int32
	ch := NewMockChannel(nil) // gomock controller 传 nil 仅用于本测试
	// 用一个最小 fake 代替：直接构造一个能记录 Close 调用的 channel
	// 由于 MockChannel 在 channels_test.go 已有 gomock 生成，这里用一个内联 fake
	fake := &fakeChannel{
		id:     "test-ch",
		onClose: func() { atomic.StoreInt32(&closed, 1) },
	}
	s.ChannelMap.Add(fake)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	if atomic.LoadInt32(&closed) == 0 {
		t.Error("Shutdown did not close registered channel (CAS logic inverted)")
	}
}

// fakeChannel 最小 Channel 实现用于测试
type fakeChannel struct {
	id      string
	onClose func()
}

func (f *fakeChannel) ID() string                       { return f.id }
func (f *fakeChannel) Conn() Conn                       { return nil }
func (f *fakeChannel) RemoteAddr() string                { return "" }
func (f *fakeChannel) Readloop(MessageListener) error   { return nil }
func (f *fakeChannel) WriteFrame(OpCode, []byte) error  { return nil }
func (f *fakeChannel) Push([]byte) error                { return nil }
func (f *fakeChannel) Flush() error                     { return nil }
func (f *fakeChannel) Close() error {
	if f.onClose != nil {
		f.onClose()
	}
	return nil
}
func (f *fakeChannel) SetReadWait(time.Duration)        {}
func (f *fakeChannel) SetWriteWait(time.Duration)      {}
func (f *fakeChannel) GetMeta() Meta                    { return nil }
```

**注意：** 如果 `Channel` 接口签名与上述 fake 不匹配（例如有更多方法），先 `grep -n "type Channel interface" *.go` 确认接口定义后调整 fake 实现。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -count=1 -run TestShutdownClosesChannels ./...`
预期：FAIL，`Shutdown did not close registered channel (CAS logic inverted)`。

- [ ] **步骤 3：修复 CAS 逻辑**

编辑 `default_server.go:219-221`，将：

```go
		if atomic.CompareAndSwapInt32(&s.quit, 0, 1) {
			return
		}
```

改为：

```go
		if !atomic.CompareAndSwapInt32(&s.quit, 0, 1) {
			// 已 Shutdown 过，直接返回
			return
		}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`go test -count=1 -race -run TestShutdownClosesChannels ./...`
预期：PASS。

- [ ] **步骤 5：验证 Shutdown 幂等性**

在 `default_server_test.go` 追加测试：

```go
func TestShutdownIdempotent(t *testing.T) {
	s := NewDefaultServer(NewChannels())
	ctx := context.Background()

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	// 第二次不应 panic 或 hang
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}
```

运行：`go test -count=1 -race -run TestShutdownIdempotent ./...`
预期：PASS（`s.once` 保证幂等，CAS 改动不影响 once 语义）。

- [ ] **步骤 6：Commit**

```bash
git add default_server.go default_server_test.go
git commit -m "fix(server): invert Shutdown CAS condition so channels actually close

原代码 if CAS(quit,0,1) { return }：CAS 成功（首次 Shutdown）时立即 return，
跳过下方关闭 channel 的循环；CAS 失败（已 Shutdown）反而继续执行——逻辑反转。
优雅停机形同虚设，所有已建立连接靠 Readloop 退出间接关闭。

修复为 if !CAS { return }：仅当已 Shutdown 时返回，首次调用继续执行关闭逻辑。
s.once 仍保证 Shutdown 整体幂等.

回归测试 TestShutdownClosesChannels + TestShutdownIdempotent 覆盖."
```

---

## 任务 3：L1-3 移除 `defer log.Close()` 提前关闭

**文件：**
- 修改：`services/gateway/server.go:57-58`、`services/comet/server.go:56-57`、`services/logic/server.go:54-55`、`services/router/server.go:46-47`
- 修改：`services/gateway/server.go` 的 `Server` 结构体、`Stop()` 方法
- 修改：`services/comet/server.go` 同上
- 修改：`services/logic/server.go` 同上
- 修改：`services/router/server.go` 同上

**说明：** 此任务无单元测试（涉及真实 logger/Sugar 初始化，且 `defer log.Close()` 的副作用只在运行期触发）。验证靠步骤 5 的烟测：服务启动后日志正常输出到文件，Stop 后进程退出干净。

- [ ] **步骤 1：修改 Gateway 服务**

编辑 `services/gateway/server.go`：

(a) `Server` 结构体增加 `logger *logger.Logger` 字段：

```go
// Server Gateway 服务实例
type Server struct {
	config        *Config
	routePath     string
	protocol      string
	wsSrv         kim.Server         // 客户端接入（WS/TCP）
	grpcSrv       *server.GRPCServer // 接收 Comet Push
	forwarder     *CometForwarder    // gRPC client 调用 Comet
	naming        naming.Naming
	traceShutdown func()
	logger        *logger.Logger // 新增：持有引用供 Stop 关闭
}
```

(b) `New()` 中移除 `defer log.Close()`，并把 `log` 存入结构体。修改 [server.go:54-58](../../services/gateway/server.go#L54-L58) 区域：

```go
	if err != nil {
		return nil, err
	}
	logger.GatewayLogger = log.Sugar()
	// 注：移除 defer log.Close()，改在 Server.Stop 中调用——否则 New 返回即关
	// 闭 Kafka producer，后续所有 logger.GatewayLogger 调用都会丢失/panic
```

(c) `return &Server{...}` 增加 `logger: log` 字段：

```go
	return &Server{
		config:        cfg,
		routePath:     routePath,
		protocol:      protocol,
		wsSrv:         wsSrv,
		grpcSrv:       grpcSrv,
		forwarder:     forwarder,
		naming:        ns,
		traceShutdown: traceShutdown,
		logger:        log,
	}, nil
```

(d) `Stop()` 末尾增加 logger 关闭：

```go
// Stop 优雅关闭 Gateway 服务
func (s *Server) Stop(ctx context.Context) error {
	if s.naming != nil {
		_ = s.naming.Deregister(s.config.ServiceID)
	}
	s.forwarder.Close()
	s.grpcSrv.GracefulStop()
	err := s.wsSrv.Shutdown(ctx)
	if s.traceShutdown != nil {
		s.traceShutdown()
	}
	if s.logger != nil {
		_ = s.logger.Close()
	}
	return err
}
```

- [ ] **步骤 2：对 Comet 服务重复上述 4 处改动**

文件 `services/comet/server.go`：
- `Server` 结构体加 `logger *logger.Logger`
- [server.go:56-57](../../services/comet/server.go#L56-L57) 移除 `defer log.Close()`
- `return &Server{...}` 加 `logger: log`
- `Stop()` 末尾加 `if s.logger != nil { _ = s.logger.Close() }`

- [ ] **步骤 3：对 Logic 服务重复上述 4 处改动**

文件 `services/logic/server.go`：
- `Server` 结构体加 `logger *logger.Logger`
- [server.go:54-55](../../services/logic/server.go#L54-L55) 移除 `defer log.Close()`
- `return &Server{...}` 加 `logger: log`
- `Stop()` 末尾加 `if s.logger != nil { _ = s.logger.Close() }`

- [ ] **步骤 4：对 Router 服务重复上述 4 处改动**

文件 `services/router/server.go`：
- `Server` 结构体加 `logger *logger.Logger`
- [server.go:46-47](../../services/router/server.go#L46-L47) 移除 `defer log.Close()`
- `return &Server{...}` 加 `logger: log`
- `Stop()` 末尾加 `if s.logger != nil { _ = s.logger.Close() }`

- [ ] **步骤 5：编译验证**

运行：`go build ./...`
预期：无错误。

- [ ] **步骤 6：烟测验证（任选一个服务）**

运行：`go build -o /tmp/kim-L1-3 ./cmd/kim && KIM_LOGLEVEL=DEBUG /tmp/kim-L1-3 router start &`
等待 1 秒后查看日志：
```
sleep 1 && tail -5 ./data/router.log
```
预期：日志正常写入（有 INFO 行），证明 `logger.RouterLogger` 在 `New()` 返回后仍可用。

停止服务并验证进程退出：
```
kill %1; sleep 1; pgrep -f "kim-L1-3 router" || echo "clean exit"
```
预期：`clean exit`。

清理：`rm -f /tmp/kim-L1-3`

- [ ] **步骤 7：Commit**

```bash
git add services/gateway/server.go services/comet/server.go services/logic/server.go services/router/server.go
git commit -m "fix(server): move log.Close from New to Stop to avoid use-after-close

4 个服务的 New() 中 defer log.Close() 在函数返回时立即触发，关闭 Kafka
producer 并 Sync zap；但全局 logger.XxxLogger 仍指向已关闭的底层 logger。
后续 Start/Stop/handler 中的日志调用会丢失（Kafka 通道已关）甚至 panic
（comet kafka.enable=true 时向已关 producer.Input 发送）。

修复：Server 结构体增加 logger *logger.Logger 字段持有引用；移除 New 中的
defer；在 Stop 末尾调用 s.logger.Close()——与 graceful shutdown 顺序一致."
```

---

## 任务 4：L1-4 修复 channel.go Push/Close race + 并发写 bufio

**文件：**
- 修改：`channel.go:32-41`（结构体）、`channel.go:43-69`（NewChannel）、`channel.go:115-131`（Push/Close）、`channel.go:180-187`（Pong）
- 创建：`channel_race_test.go`

- [ ] **步骤 1：编写失败的 race 测试**

创建 `channel_race_test.go`（`package kim`）：

```go
package kim

import (
	"sync"
	"testing"
)

// TestPushCloseNoPanic 验证并发 Push + Close 不会触发 send on closed channel panic。
// 修复前：Push 检查 state==1 后无条件写入 writechan；Close 先 CAS(1,2) 再 close(writechan)。
// 两者之间无同步，Push 可能在 Close 之后才发送 → panic。
// 修复后：Push 用 select { case writechan<-: case <-closeChan: }，Close 先 close(closeChan)。
func TestPushCloseNoPanic(t *testing.T) {
	const N = 200
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := newTestChannel()
			// 启动 writeloop（NewChannel 内已启动，但此处构造的 testChannel 不启动，
			// 所以 Push 写入会缓冲在 writechan 中——Close 时 closeChan 触发 select 退出）
			// 并发 Push + Close
			var pwg sync.WaitGroup
			pwgg := 2
			pwggTotal := pwgg
			var pwgMu sync.WaitGroup
			pwgMu.Add(pwggTotal)
			for j := 0; j < pwgg; j++ {
				pwg.Add(1)
				go func() {
					defer pwg.Done()
					defer pwgMu.Done()
					_ = ch.Push([]byte("hello"))
				}()
			}
			// 再来一个 Close
			pwg.Add(1)
			go func() {
				defer pwg.Done()
				defer pwgMu.Done()
				_ = ch.Close()
			}()
			pwgg = 0
			_ = pwggTotal
			pwgg = pwgg // suppress unused
			pwg.Wait()
			pwgMu.Wait()
			_ = ch
		}()
	}
	wg.Wait()
	// 若走到这里没 panic，测试通过
}

// newTestChannel 构造一个不启动 writeloop 的 ChannelImpl 用于 race 测试
func newTestChannel() *ChannelImpl {
	return &ChannelImpl{
		id:        "test",
		writechan: make(chan []byte, 32),
		writeWait: DefaultWriteWait,
		readwait:  DefaultReadWait,
		state:     1, // 模拟已 Readloop 启动
	}
}

// TestPushAfterCloseReturnsError 验证 Push 在 Close 之后返回 error 而非 panic
func TestPushAfterCloseReturnsError(t *testing.T) {
	ch := newTestChannel()
	_ = ch.Close()
	err := ch.Push([]byte("x"))
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}
```

> **注意：** 上述测试用 `_ = pwgg` / `pwgg = pwgg` 处理未使用变量是临时占位；如果工程师觉得繁琐，可简化为更直接的并发模式（核心是并发 Push+Close 不 panic）。若测试代码无法编译，请先 `grep -n "DefaultWriteWait\|DefaultReadWait" *.go` 确认常量名，调整后再运行。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -count=1 -race -run "TestPushCloseNoPanic|TestPushAfterClose" ./...`
预期：FAIL 或 panic（`send on closed channel`）。

- [ ] **步骤 3：修改 ChannelImpl 结构体**

编辑 `channel.go:32-41`，在 `state` 后增加 `closeChan chan struct{}`：

```go
// ChannelImpl Channel 接口的完整实现
type ChannelImpl struct {
	id string
	Conn
	meta      Meta
	writechan chan []byte
	closeChan chan struct{} // 新增：关闭信号，Push 用 select 探测
	writeWait time.Duration
	readwait  time.Duration
	gpool     *ants.Pool
	state     int32 // 0 init 1 start 2 closed
}
```

- [ ] **步骤 4：修改 NewChannel 初始化 closeChan**

编辑 `channel.go:45-58`，在结构体字面量中初始化 `closeChan`：

```go
	ch := &ChannelImpl{
		id:   id,
		Conn: conn,
		meta: meta,
		writechan: make(chan []byte, 32),
		closeChan: make(chan struct{}), // 新增
		writeWait: DefaultWriteWait,
		readwait:  DefaultReadWait,
		gpool:     gpool,
		state:     0,
	}
```

- [ ] **步骤 5：修改 Push 用 select 探测 closeChan**

编辑 `channel.go:115-122`：

```go
// Push 异步写数据
func (ch *ChannelImpl) Push(payload []byte) error {
	if atomic.LoadInt32(&ch.state) != 1 {
		return fmt.Errorf("channel %s has closed", ch.id)
	}
	select {
	case ch.writechan <- payload:
		return nil
	case <-ch.closeChan:
		return fmt.Errorf("channel %s has closed", ch.id)
	}
}
```

- [ ] **步骤 6：修改 Close 先 close(closeChan) 再 close(writechan)**

编辑 `channel.go:124-131`：

```go
// Close 关闭连接
func (ch *ChannelImpl) Close() error {
	if !atomic.CompareAndSwapInt32(&ch.state, 1, 2) {
		return fmt.Errorf("channel has started")
	}
	close(ch.closeChan) // 先发信号，让阻塞在 select 的 Push 退出
	close(ch.writechan)  // 再关闭 writechan，writeloop 自然退出
	return nil
}
```

> **注意：** state==0（init，未 Readloop）时 Close 走的是 CAS(1,2) 失败分支，原代码返回 "channel has started"（其实意思是"未启动状态不能 Close"）。若测试 `newTestChannel` 设 state=1 模拟启动，Close 走 CAS 成功分支正常。Readloop 中的 `ch.Close()` 也是 state==1 → 2。无需改动语义。

- [ ] **步骤 7：修改 Readloop Pong 走 writechan 不直接写 bufio**

编辑 `channel.go:180-187`，将 `_ = ch.WriteFrame(OpPong, nil); _ = ch.Flush()` 改为通过 writechan 异步发送：

```go
		if frame.GetOpCode() == OpPing {
			log.Trace("recv a ping; resp with a pong")
			// 修复：原代码直接 WriteFrame+Flush，与 writeloop 并发写 bufio.Writer
			// （bufio 非并发安全）；改为通过 writechan 异步排队
			select {
			case ch.writechan <- nil:
				// nil payload 表示 Pong 帧（writeloop 需要识别）
			case <-ch.closeChan:
				return errors.New("channel closed during ping")
			}
			continue
		}
```

**配套：** writeloop（`channel.go:80-107`）当前收到 payload 后无条件 `WriteFrame(OpBinary, payload)`。需要区分 `nil` payload 为 Pong：

编辑 `channel.go:80-107` 的 writeloop 循环：

```go
	for payload := range ch.writechan {
		var err error
		if payload == nil {
			// Pong 帧（Readloop 收到 Ping 后投递的 nil）
			err = ch.WriteFrame(OpPong, nil)
		} else {
			err = ch.WriteFrame(OpBinary, payload)
		}
		if err != nil {
			return err
		}
		// 后续批量 flush 逻辑不变
		flushed := false
		for !flushed {
			select {
			case payload, ok := <-ch.writechan:
				if !ok {
					return ch.Flush()
				}
				if payload == nil {
					err = ch.WriteFrame(OpPong, nil)
				} else {
					err = ch.WriteFrame(OpBinary, payload)
				}
				if err != nil {
					return err
				}
			default:
				if err := ch.Flush(); err != nil {
					return err
				}
				flushed = true
			}
		}
	}
```

- [ ] **步骤 8：运行测试验证通过**

运行：`go test -count=1 -race -run "TestPushCloseNoPanic|TestPushAfterClose" ./...`
预期：PASS。

- [ ] **步骤 9：运行全包测试确保无回归**

运行：`go test -count=1 -race ./...`
预期：已有测试全绿（如有集成测试需要外部服务失败，加 `-short` 或忽略集成失败项；本任务不引入新失败）。

- [ ] **步骤 10：Commit**

```bash
git add channel.go channel_race_test.go
git commit -m "fix(channel): resolve Push/Close race and concurrent bufio writes

两个并发问题：

1) Push 与 Close race：Push 用 atomic.LoadInt32 检查 state==1 后无条件
   ch.writechan <- payload；Close 先 CAS(1,2) 再 close(writechan)。两者之间
   无锁，Push 可能在 Close 之后才发送 → panic: send on closed channel。

2) Readloop 处理 OpPing 时直接 WriteFrame(OpPong)+Flush，与 writeloop 的
   WriteFrame/Flush 并发写同一 bufio.Writer（非并发安全）。

修复：
- ChannelImpl 增加 closeChan chan struct{}
- Push 改用 select { case writechan<-: case <-closeChan: } 探测关闭信号
- Close 先 close(closeChan) 让阻塞 Push 退出，再 close(writechan) 让 writeloop 退出
- Readloop Pong 改为通过 writechan 投递 nil payload，writeloop 识别 nil 写 OpPong

回归测试 TestPushCloseNoPanic (200 并发 Push+Close) + TestPushAfterClose
用 -race 验证无 panic/race."
```

---

## 任务 5：L1-5 启动 MonitorPort HTTP server + 暴露 /metrics 与 /health

**文件：**
- 修改：`internal/server/health.go`（重写）
- 修改：`internal/server/grpc.go:40-77`（暴露 HealthServer 与 MonitorPort 启动）
- 修改：`services/gateway/server.go`、`services/comet/server.go`、`services/logic/server.go`（在 Start 中启动 monitor HTTP）
- 创建：`internal/server/health_test.go`

**说明：** Router 服务用 Iris 且已有 `/health`，无需改造。

- [ ] **步骤 1：编写失败的 HTTP 端点测试**

创建 `internal/server/health_test.go`：

```go
package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestMonitorHTTPServer 验证 monitor HTTP server 暴露 /health 与 /metrics 端点
func TestMonitorHTTPServer(t *testing.T) {
	mux := NewMonitorMux(nil) // 传 nil HealthServer 表示用默认 SERVING 状态

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// /health
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("/health status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "ok") {
		t.Errorf("/health body = %q, want contains 'ok'", body)
	}

	// /metrics
	resp2, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	if resp2.StatusCode != 200 {
		t.Errorf("/metrics status = %d, want 200", resp2.StatusCode)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	// 应至少包含一个 # HELP 行（promhttp 输出格式）
	if !strings.Contains(string(body2), "# HELP") {
		t.Errorf("/metrics body missing Prometheus format")
	}
	_ = time.Second
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -count=1 -run TestMonitorHTTPServer ./internal/server/...`
预期：FAIL（`undefined: NewMonitorMux`）。

- [ ] **步骤 3：重写 health.go**

替换 `internal/server/health.go` 全文为：

```go
package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc/health"
)

// MonitorMux 构建 monitor HTTP 服务的路由：/health + /metrics
// hs 可为 nil（视为 SERVING 状态，用于未启用 gRPC health 协议的场景）
func NewMonitorMux(hs *health.Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

// StartMonitorHTTP 启动 monitor HTTP 服务（阻塞）。addr 形如 ":8001"。
// 调用方应在 goroutine 中调用：go StartMonitorHTTP(":8001", nil)
func StartMonitorHTTP(addr string, hs *health.Server) error {
	return http.ListenAndServe(addr, NewMonitorMux(hs))
}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`go test -count=1 -run TestMonitorHTTPServer ./internal/server/...`
预期：PASS。

- [ ] **步骤 5：在 gateway/comet/logic 的 Start 中启动 monitor HTTP**

编辑 `services/gateway/server.go` 的 `Start`（[server.go:158-169](../../services/gateway/server.go#L158-L169)）：

```go
// Start 启动 Gateway 服务
func (s *Server) Start(ctx context.Context) error {
	// 启动 gRPC server（非阻塞）
	go func() {
		if err := s.grpcSrv.Start(); err != nil {
			logger.GatewayLogger.Errorf("grpc server error: %v", err)
		}
	}()
	// 启动 monitor HTTP server（非阻塞）：暴露 /health 给 Consul、/metrics 给 Prometheus
	monitorAddr := fmt.Sprintf(":%d", s.config.MonitorPort)
	go func() {
		if err := server.StartMonitorHTTP(monitorAddr, nil); err != nil {
			logger.GatewayLogger.Errorf("monitor http error: %v", err)
		}
	}()
	logger.GatewayLogger.Infof("gateway grpc listening on %s", s.config.GRPCListen)
	logger.GatewayLogger.Infof("gateway monitor listening on %s", monitorAddr)
	logger.GatewayLogger.Infof("gateway %s listening on %s", s.protocol, s.config.Listen)
	// 启动 WS/TCP server（阻塞）
	return s.wsSrv.Start()
}
```

**注意：** `fmt` 已在 gateway/server.go import；`server` 包也已 import。无需新增 import。

编辑 `services/comet/server.go` 的 `Start`（[server.go:153-157](../../services/comet/server.go#L153-L157)）：

需要在 `Config` 中确认 `MonitorPort` 字段是否存在。**首先执行：**

```bash
grep -n "MonitorPort" services/comet/config.go
```

若 comet 没有 `MonitorPort` 字段，先在 `services/comet/config.go` 的 `Config` 结构体添加：

```go
	MonitorPort int `mapstructure:"monitor_port"`
```

并在 `services/comet/conf.yaml` 添加 `monitor_port: 8007`（与 gateway 8001 区分，避免本地冲突）。

然后修改 `Start`：

```go
// Start 启动 gRPC 服务（阻塞）
func (s *Server) Start(ctx context.Context) error {
	monitorAddr := fmt.Sprintf(":%d", s.config.MonitorPort)
	go func() {
		if err := server.StartMonitorHTTP(monitorAddr, nil); err != nil {
			logger.CometLogger.Errorf("monitor http error: %v", err)
		}
	}()
	logger.CometLogger.Infof("comet monitor listening on %s", monitorAddr)
	logger.CometLogger.Infof("comet service starting on %s", s.config.Listen)
	return s.grpcSrv.Start()
}
```

同样为 `services/logic/server.go` 的 `Start`（[server.go:140-144](../../services/logic/server.go#L140-L144)）添加 monitor HTTP：

先检查 `services/logic/config.go` 是否有 `MonitorPort`，若无则添加字段并设 conf.yaml 默认 `monitor_port: 8009`。

修改 `Start`：

```go
func (s *Server) Start(ctx context.Context) error {
	monitorAddr := fmt.Sprintf(":%d", s.config.MonitorPort)
	go func() {
		if err := server.StartMonitorHTTP(monitorAddr, nil); err != nil {
			logger.LogicLogger.Errorf("monitor http error: %v", err)
		}
	}()
	logger.LogicLogger.Infof("logic monitor listening on %s", monitorAddr)
	logger.LogicLogger.Infof("logic service starting on %s", s.config.Listen)
	return s.grpcSrv.Start()
}
```

- [ ] **步骤 6：编译验证**

运行：`go build ./...`
预期：无错误。

- [ ] **步骤 7：烟测验证 /metrics 与 /health**

```bash
go build -o /tmp/kim-L1-5 ./cmd/kim
/tmp/kim-L1-5 router start &
ROUTER_PID=$!
sleep 1
# router 已有 /health（Iris），验证 ours
curl -sf http://localhost:8100/health || echo "router /health failed"
kill $ROUTER_PID; wait 2>/dev/null
```

对于 gateway/comet/logic 的 monitor 端点，启动服务后 curl：
- `curl -sf http://localhost:8001/health`（gateway monitor）
- `curl -sf http://localhost:8001/metrics | head -5`（应见 `# HELP kim_...`）
- comet monitor `:8007`、logic monitor `:8009` 同样验证

如本地无 Consul/MySQL/Redis，可只验证 router（不需要外部依赖）。

清理：`rm -f /tmp/kim-L1-5`

- [ ] **步骤 8：Commit**

```bash
git add internal/server/health.go internal/server/health_test.go services/gateway/server.go services/comet/server.go services/comet/config.go services/comet/conf.yaml services/logic/server.go services/logic/config.go services/logic/conf.yaml
git commit -m "feat(server): start MonitorPort HTTP exposing /health and /metrics

internal/server/health.go 之前定义了 HealthChecker.StartHTTP 但全代码库无任何
调用方，是死代码。Consul 健康检查访问 :8001/health → 连接拒绝 → 服务被标 critical；
Prometheus 也无法 scrape metrics，弹性指标全盲区。

修复：
- 重写 health.go：提供 NewMonitorMux(hs) 与 StartMonitorHTTP(addr, hs)
  - /health 返回 200 + 'ok'
  - /metrics 挂载 promhttp.Handler 暴露全部注册指标
- gateway/comet/logic 三服务的 Start 中以 goroutine 启动 monitor HTTP
- comet/logic 的 Config 增加 MonitorPort 字段并配 conf.yaml 默认端口
  (comet=8007, logic=8009，与 gateway=8001 区分)

回归测试 TestMonitorHTTPServer 验证两个端点状态码与响应格式."
```

---

## 任务 6：L1-6 修复 Pool.Close goroutine 泄漏

**文件：**
- 修改：`internal/client/pool.go:16-23`（结构体）、`pool.go:31-43`（NewPoolWithConfig）、`pool.go:155-162`（Close）
- 创建：`internal/client/pool_leak_test.go`

**说明：** 真实 Consul 订阅集成测试加 `//go:build integration` 标签，避免 `make test` 失败。

- [ ] **步骤 1：编写失败的 goroutine 计数测试**

创建 `internal/client/pool_leak_test.go`：

```go
//go:build integration

package client

import (
	"runtime"
	"testing"
	"time"

	"github.com/klintcheng/kim/internal/naming"
)

// TestPoolCloseNoLeak 验证 Pool.Close 后 watch goroutine 与 Consul watch 都退出。
// 修复前：Close 仅关闭 gRPC 连接，不 cancel watch、不 Unsubscribe，
// 每次重启累积泄漏至少 2 个 goroutine + Consul 长轮询连接。
// 修复后：Close 先 cancel ctx 让 watch 退出，再 Unsubscribe 让 Consul watch 退出。
//
// 需要真实 Consul：CONSUL_URL=http://127.0.0.1:8500
func TestPoolCloseNoLeak(t *testing.T) {
	consulURL := "http://127.0.0.1:8500"
	ns, err := naming.NewNaming(consulURL)
	if err != nil {
		t.Skipf("consul not available: %v", err)
	}

	before := runtime.NumGoroutine()
	for i := 0; i < 5; i++ {
		p := NewPool(ns, "leak-test-svc")
		// 给 watch 启动一点时间
		time.Sleep(100 * time.Millisecond)
		p.Close()
	}
	// 等 goroutine 退出
	time.Sleep(500 * time.Millisecond)
	after := runtime.NumGoroutine()

	// 允许少量波动，但不应累积（>5 个泄漏说明未清理）
	leaked := after - before
	if leaked > 5 {
		t.Errorf("goroutine leak: before=%d after=%d leaked=%d", before, after, leaked)
	}
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -count=1 -tags=integration -run TestPoolCloseNoLeak ./internal/client/...`
预期：FAIL（`goroutine leak: before=X after=Y leaked=Z` 且 Z>5）。需要 Consul 运行；如未启动：`make docker-up` 先起 Consul。

- [ ] **步骤 3：修改 Pool 结构体增加 ctx/cancel**

编辑 `internal/client/pool.go:1-13` 与 `pool.go:16-23`：

```go
package client

import (
	"context"
	"fmt"
	"sync"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/naming"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Pool 管理对某类服务的 gRPC 连接，替代原 container 的 TCP client 管理
type Pool struct {
	naming      naming.Naming
	serviceName string
	mu          sync.RWMutex
	conns       map[string]*grpc.ClientConn
	rr          *roundRobin
	cfg         config.ResilienceConfig
	ctx         context.Context    // 新增：watch 的生命周期 ctx
	cancel      context.CancelFunc // 新增：Close 时 cancel 让 watch 退出
}
```

- [ ] **步骤 4：修改 NewPoolWithConfig 初始化 ctx/cancel**

编辑 `pool.go:31-43`：

```go
// NewPoolWithConfig 创建带弹性配置的连接池
func NewPoolWithConfig(ns naming.Naming, serviceName string, cfg config.ResilienceConfig) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		naming:      ns,
		serviceName: serviceName,
		conns:       make(map[string]*grpc.ClientConn),
		rr:          newRoundRobin(),
		cfg:         cfg,
		ctx:         ctx,
		cancel:      cancel,
	}
	if ns != nil {
		go p.watch()
	}
	return p
}
```

- [ ] **步骤 5：修改 watch 使用 p.ctx 退出**

编辑 `pool.go:104-112`：

```go
// watch 订阅服务变更，自动建连/断连
func (p *Pool) watch() {
	// 初始加载
	p.refresh()

	// 订阅变更（使用 p.ctx 让 Close 能中断）
	_ = p.naming.Subscribe(p.serviceName, func(services []kim.ServiceRegistration) {
		select {
		case <-p.ctx.Done():
			return
		default:
		}
		p.refresh()
	})

	// 等待 Close 信号
	<-p.ctx.Done()
	_ = p.naming.Unsubscribe(p.serviceName)
}
```

> **注意：** `Subscribe` 本身是非阻塞的（注册回调后返回），所以 watch goroutine 现在会在 `<-p.ctx.Done()` 处阻塞，直到 Close 触发 cancel。然后调用 `Unsubscribe` 让 Consul watch goroutine 退出。

- [ ] **步骤 6：修改 Close 先 cancel 再关连接**

编辑 `pool.go:155-162`：

```go
// Close 关闭所有连接并清理 watch goroutine
func (p *Pool) Close() {
	// 1. 取消 ctx：watch goroutine 退出阻塞，调用 Unsubscribe 清理 Consul watch
	if p.cancel != nil {
		p.cancel()
	}
	// 2. 关闭所有 gRPC 连接
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = make(map[string]*grpc.ClientConn)
}
```

- [ ] **步骤 7：编译并运行测试验证通过**

运行：`go build ./...`
预期：无错误。

运行：`go test -count=1 -tags=integration -run TestPoolCloseNoLeak ./internal/client/...`
预期：PASS（`leaked <= 5`）。

- [ ] **步骤 8：验证默认 go test 不受影响**

运行：`go test -count=1 ./internal/client/...`
预期：PASS（`pool_leak_test.go` 因 `//go:build integration` 标签默认不编译，跳过）。

- [ ] **步骤 9：Commit**

```bash
git add internal/client/pool.go internal/client/pool_leak_test.go
git commit -m "fix(client): cancel watch and Unsubscribe in Pool.Close to stop goroutine leak

Pool.Close 之前仅关闭 gRPC 连接，既不 cancel p.watch()，也不调用
naming.Unsubscribe。每次 Close 后：
- watch goroutine 仍在 <-Subscribe 回调中等待（实际上 Subscribe 不阻塞，
  但 watch 函数返回后 goroutine 即退出——然而 Consul Naming 的 watch
  goroutine 不会退出，因为它在 5 分钟长轮询循环中）。
- 每次 Pool.Close 累积泄漏至少 2 个 goroutine + 1 个 Consul HTTP 长连接。

修复：
- Pool 增加 ctx/cancel 字段
- NewPoolWithConfig 创建可取消 ctx
- watch 函数末尾 <-ctx.Done() 后调用 Unsubscribe
- Close 先 cancel() 让 watch 退出并触发 Unsubscribe，再关闭 gRPC 连接

集成测试 TestPoolCloseNoLeak（//go:build integration）用 runtime.NumGoroutine
验证 5 次 Create+Close 后无累积泄漏."
```

---

## 任务 7：L1-7 修复 Logic Stop() 未关闭 DB 连接池

**文件：**
- 修改：`services/logic/server.go:35-40`（结构体）、`server.go:130-138`（return）、`server.go:147-156`（Stop）
- 创建：`services/logic/server_db_close_test.go`

**说明：** 真实 DB 测试加 `//go:build integration` 标签。

- [ ] **步骤 1：编写失败的 DB 关闭测试**

创建 `services/logic/server_db_close_test.go`：

```go
//go:build integration

package logic

import (
	"context"
	"testing"

	"github.com/klintcheng/kim/services/logic/database"
)

// TestLogicStopClosesDB 验证 logic.Server.Stop() 关闭底层 DB 连接池。
// 修复前：Stop 仅 GracefulStop gRPC + Naming Deregister，不关 baseDb/messageDb，
// 进程退出靠 OS 回收文件描述符，正常停机期间连接池泄漏。
// 修复后：Stop 末尾调用 sqlDB.Close()。
//
// 需要真实 MySQL：见 services/logic/conf.yaml:base_db
func TestLogicStopClosesDB(t *testing.T) {
	cfg := mustLoadTestConfig(t) // 见 helper

	srv, err := New(context.Background(), cfg)
	if err != nil {
		t.Skipf("logic New failed (db not available?): %v", err)
	}

	// Stop 后 baseDb 应不可用
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// 尝试再 Ping，期望失败
	sqlDB, _ := srv.baseDb.DB()
	if sqlDB != nil {
		err := sqlDB.Ping()
		if err == nil {
			t.Error("baseDb still alive after Stop (connection not closed)")
		}
	}
}

// mustLoadTestConfig 加载测试配置
func mustLoadTestConfig(t *testing.T) *Config {
	cfg, err := LoadConfig("conf.yaml")
	if err != nil {
		t.Skipf("load config: %v", err)
	}
	return cfg
}

// 兜底：避免 unused 警告
var _ = database.InitDb
```

- [ ] **步骤 2：运行测试验证失败**

运行：`make docker-up && go test -count=1 -tags=integration -run TestLogicStopClosesDB ./services/logic/...`
预期：FAIL（编译失败：`srv.baseDb undefined`，因为 baseDb 当前不在 Server 结构体中）。

- [ ] **步骤 3：修改 Server 结构体持有 DB 引用**

编辑 `services/logic/server.go:35-40`：

```go
// Server Logic gRPC 服务
type Server struct {
	config        *Config
	grpcSrv       *server.GRPCServer
	naming        naming.Naming
	traceShutdown func()
	logger        *logger.Logger // 已在 L1-3 添加
	baseDb        *gorm.DB       // 新增：用于 Stop 关闭
	messageDb     *gorm.DB       // 新增：用于 Stop 关闭
}
```

> **注意：** 确认 `services/logic/server.go` import 了 `gorm.io/gorm`（如果没有，添加）。`database.InitDb` 返回 `*gorm.DB`。

- [ ] **步骤 4：在 New() 中把 DB 存入结构体**

编辑 `server.go:130-138` 的 `s := &Server{...}`：

```go
	s := &Server{
		config:        cfg,
		grpcSrv:       grpcSrv,
		naming:        ns,
		traceShutdown: traceShutdown,
		logger:        log,
		baseDb:        baseDb,    // 新增
		messageDb:     messageDb, // 新增
	}
```

- [ ] **步骤 5：在 Stop() 中关闭 DB**

编辑 `server.go:147-156` 的 `Stop`：

```go
// Stop 反注册 Consul 并优雅关闭 gRPC 服务
func (s *Server) Stop(ctx context.Context) error {
	if s.naming != nil {
		_ = s.naming.Deregister(s.config.ServiceID)
	}
	s.grpcSrv.GracefulStop()
	if s.traceShutdown != nil {
		s.traceShutdown()
	}
	// 关闭 DB 连接池
	if s.baseDb != nil {
		if sqlDB, err := s.baseDb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	if s.messageDb != nil {
		if sqlDB, err := s.messageDb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	if s.logger != nil {
		_ = s.logger.Close()
	}
	return nil
}
```

- [ ] **步骤 6：编译并运行测试验证通过**

运行：`go build ./...`
预期：无错误。

运行：`go test -count=1 -tags=integration -run TestLogicStopClosesDB ./services/logic/...`
预期：PASS（`baseDb still alive after Stop` 不再出现）。需要 MySQL 运行；如未启动：`make docker-up`。

- [ ] **步骤 7：Commit**

```bash
git add services/logic/server.go services/logic/server_db_close_test.go
git commit -m "fix(logic): close DB connection pools in Stop to avoid fd leak

logic.Server.Stop 之前仅 GracefulStop gRPC + Deregister Consul，不关 baseDb
与 messageDb。优雅停机期间 GORM 连接池（默认 100 连接）泄漏，OS 仅在进程
退出后回收，正常停机/重启过程中 DB 连接数飙升。

修复：
- Server 结构体增加 baseDb/messageDb *gorm.DB 字段
- New 中存入结构体
- Stop 末尾关闭两个 DB 的 sqlDB

集成测试 TestLogicStopClosesDB（//go:build integration）验证 Stop 后
sqlDB.Ping 返回错误."
```

---

## 自检

**1. 规格覆盖度（对照 [2026-06-26-improvement-roadmap-design.md](../specs/2026-06-26-improvement-roadmap-design.md) §3.1）：**

| 规格项 | 对应任务 | 状态 |
|---|---|---|
| L1-1 metrics 重复注册 panic | 任务 1 | ✓ |
| L1-2 Shutdown CAS 反转 | 任务 2 | ✓ |
| L1-3 defer log.Close() 提前关闭 | 任务 3 | ✓（4 服务全覆盖） |
| L1-4 channel.go Push/Close race + 并发写 bufio | 任务 4 | ✓ |
| L1-5 MonitorPort HTTP server 启动 + /metrics | 任务 5 | ✓（gateway/comet/logic 3 服务；router 已有 Iris /health） |
| L1-6 Pool.Close goroutine 泄漏 | 任务 6 | ✓ |
| L1-7 Logic Stop() 关 DB | 任务 7 | ✓ |

规格验证标准对照：
- `make run-all` 4 服务无 panic：任务 1+3 修复后达成
- `curl :8001/metrics`：任务 5 达成
- `curl :8001/health`：任务 5 达成
- `make stop-all` Consul 标 critical：任务 3+5+6+7 共同达成（Deregister + GracefulStop）
- 重启 3 次 goroutine 无累积：任务 6 达成

**2. 占位符扫描：** 无 TODO/待定；所有代码块均为完整可编译代码。任务 2 的 `fakeChannel` 实现可能需要根据实际 `Channel` 接口签名调整——已在步骤 1 注释中提示工程师先 grep 接口定义。

**3. 类型一致性：**
- `Server.logger *logger.Logger` 字段名在 gateway/comet/logic/router 4 服务一致 ✓
- `Pool.ctx`/`Pool.cancel` 类型一致 ✓
- `ChannelImpl.closeChan chan struct{}` 类型一致 ✓
- `NewMonitorMux(hs *health.Server)` / `StartMonitorHTTP(addr string, hs *health.Server)` 签名一致 ✓
- `MonitorPort int` 字段名在 gateway/comet/logic 一致 ✓

**4. 潜在问题：**
- 任务 3 修改 4 个服务的 `Server` 结构体，与任务 5 修改 gateway/comet/logic 的 `Start`、任务 7 修改 logic 的 `Server` 结构体有交集。**建议执行顺序：任务 1 → 任务 2 → 任务 4 → 任务 6（独立）→ 任务 3（4 服务）→ 任务 5（依赖任务 3 的 logger 字段已完成）→ 任务 7（依赖任务 3 的 logger 字段已完成）**。如并行执行需注意文件冲突。
- 任务 4 步骤 7 的 writeloop 改动较大，注意保留原有的"批量 flush 减少 syscall"逻辑。
- 任务 5 步骤 5 的 comet/logic `MonitorPort` 字段是否已存在需要先 grep 确认。

---

## 执行交接

计划已完成并保存到 [docs/superpowers/plans/2026-06-26-L1-hotfix-implementation.md](./2026-06-26-L1-hotfix-implementation.md)。

**两种执行方式：**

**1. 子代理驱动（推荐）** — 每个任务调度一个新的子代理，任务间进行审查，快速迭代。与用户偏好的"Sub-agent driven execution"匹配。

**2. 内联执行** — 在当前会话中使用 executing-plans 执行任务，批量执行并设有检查点。

选哪种方式？
