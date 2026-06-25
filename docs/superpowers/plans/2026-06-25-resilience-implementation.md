# 弹性套件实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 在 gRPC 重构基线上引入 Sentinel-Go 弹性套件，覆盖熔断 + 重试 + 超时 + 限流 + Fallback。

**架构：** 分层混合方案——熔断/限流用 gRPC 拦截器（客户端 + 服务端），重试/fallback 用 ResilientClient（封装 Pool），超时用客户端拦截器。调用方零改动（除构造函数签名）。

**技术栈：** Sentinel-Go（`github.com/alibaba/sentinel-golang`）+ gRPC 拦截器 + Prometheus 指标

**规格：** [docs/superpowers/specs/2026-06-25-resilience-design.md](../specs/2026-06-25-resilience-design.md)

---

## 文件结构

**新增文件（7 个）：**

| 文件 | 职责 |
|---|---|
| `internal/config/resilience.go` | ResilienceConfig 结构 + 默认值 + Parse 辅助方法 |
| `internal/config/resilience_test.go` | 配置默认值和解析测试 |
| `internal/client/breaker.go` | Sentinel 初始化 + ensureBreaker + 状态监听器 |
| `internal/client/limiter.go` | ensureLimiter（客户端限流规则注册） |
| `internal/client/interceptor.go` | 客户端拦截器链：timeout/breaker/limiter |
| `internal/client/resilient.go` | ResilientClient：重试 + fallback + 超时编排 |
| `internal/client/resilient_test.go` | ResilientClient 单元测试 |
| `internal/server/limiter.go` | 服务端限流拦截器 |

**修改文件（7 个）：**

| 文件 | 改动 |
|---|---|
| `internal/metrics/metrics.go` | 新增 3 个指标（breaker state/retry/ratelimit） |
| `internal/client/pool.go` | `grpc.Dial` 挂载拦截器链；新增 `GetAnyExcluding`；Pool 持有 `cfg` |
| `internal/server/grpc.go` | 服务端拦截器链加入 `LimiterInterceptor` |
| `services/comet/service/logic_client.go` | `pool.GetAny()` → `resilient.Call()` |
| `services/comet/service/pusher.go` | `pool.Get()` → `resilient.Call()`（按 gateway 精确实例） |
| `services/gateway/forwarder.go` | Pool 构造传入 cfg；Forward 只挂拦截器，不用 ResilientClient |
| `services/comet/server.go` + `services/gateway/server.go` | 传递 ResilienceConfig 到 Pool/Forwarder |
| `services/comet/config.go` + `services/gateway/config.go` | Config 结构加 `Resilience` 字段 |
| `go.mod` / `go.sum` | 引入 `github.com/alibaba/sentinel-golang` |

---

## 任务 1：引入 Sentinel-Go 依赖

**文件：**
- 修改：`go.mod`
- 修改：`go.sum`

- [ ] **步骤 1：添加 Sentinel-Go 依赖**

运行：
```bash
cd /root/program/go/kim && go get github.com/alibaba/sentinel-golang/api@latest
```

预期：`go.mod` 新增 `github.com/alibaba/sentinel-golang` 依赖，`go.sum` 更新。

- [ ] **步骤 2：验证依赖可用**

运行：
```bash
go build ./...
```

预期：编译通过（Sentinel 尚未使用，但依赖已就绪）。

- [ ] **步骤 3：Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add sentinel-golang dependency for resilience"
```

---

## 任务 2：ResilienceConfig 配置结构

**文件：**
- 创建：`internal/config/resilience.go`
- 创建：`internal/config/resilience_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `internal/config/resilience_test.go`：

```go
package config

import (
	"testing"
	"time"
)

func TestDefaultResilienceConfig(t *testing.T) {
	cfg := DefaultResilienceConfig()

	// Breaker
	if !cfg.Breaker.Enable {
		t.Error("Breaker.Enable should default to true")
	}
	if cfg.Breaker.Strategy != "both" {
		t.Errorf("Breaker.Strategy = %q, want %q", cfg.Breaker.Strategy, "both")
	}
	if cfg.Breaker.Threshold != 0.5 {
		t.Errorf("Breaker.Threshold = %v, want 0.5", cfg.Breaker.Threshold)
	}
	if cfg.Breaker.MinRequestAmount != 10 {
		t.Errorf("Breaker.MinRequestAmount = %v, want 10", cfg.Breaker.MinRequestAmount)
	}

	// Retry
	if !cfg.Retry.Enable {
		t.Error("Retry.Enable should default to true")
	}
	if cfg.Retry.MaxAttempts != 3 {
		t.Errorf("Retry.MaxAttempts = %v, want 3", cfg.Retry.MaxAttempts)
	}
	if cfg.Retry.Multiplier != 2.0 {
		t.Errorf("Retry.Multiplier = %v, want 2.0", cfg.Retry.Multiplier)
	}

	// Timeout
	if !cfg.Timeout.Enable {
		t.Error("Timeout.Enable should default to true")
	}

	// Limiter
	if !cfg.Limiter.Enable {
		t.Error("Limiter.Enable should default to true")
	}
	if cfg.Limiter.ClientQPS != 100 {
		t.Errorf("Limiter.ClientQPS = %v, want 100", cfg.Limiter.ClientQPS)
	}
	if cfg.Limiter.ServerQPS != 200 {
		t.Errorf("Limiter.ServerQPS = %v, want 200", cfg.Limiter.ServerQPS)
	}
}

func TestBreakerConfigSlowCallRTTDuration(t *testing.T) {
	cfg := DefaultResilienceConfig()
	got := cfg.Breaker.SlowCallRTTDuration()
	want := 500 * time.Millisecond
	if got != want {
		t.Errorf("SlowCallRTTDuration() = %v, want %v", got, want)
	}
}

func TestRetryConfigBackoffDurations(t *testing.T) {
	cfg := DefaultResilienceConfig()
	if cfg.Retry.InitialBackoffDuration() != 50*time.Millisecond {
		t.Errorf("InitialBackoffDuration = %v, want 50ms", cfg.Retry.InitialBackoffDuration())
	}
	if cfg.Retry.MaxBackoffDuration() != 500*time.Millisecond {
		t.Errorf("MaxBackoffDuration = %v, want 500ms", cfg.Retry.MaxBackoffDuration())
	}
}

func TestTimeoutConfigDefaultDuration(t *testing.T) {
	cfg := DefaultResilienceConfig()
	if cfg.Timeout.DefaultDuration() != 3*time.Second {
		t.Errorf("DefaultDuration = %v, want 3s", cfg.Timeout.DefaultDuration())
	}
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/config/ -run TestDefault -v`
预期：FAIL，报错 `DefaultResilienceConfig undefined`。

- [ ] **步骤 3：编写实现**

创建 `internal/config/resilience.go`：

```go
package config

import "time"

// ResilienceConfig 弹性套件配置（代码默认 + YAML 覆盖）
type ResilienceConfig struct {
	Breaker BreakerConfig `yaml:"breaker" mapstructure:"breaker"`
	Retry   RetryConfig   `yaml:"retry" mapstructure:"retry"`
	Timeout TimeoutConfig `yaml:"timeout" mapstructure:"timeout"`
	Limiter LimiterConfig `yaml:"limiter" mapstructure:"limiter"`
}

// BreakerConfig 断路器配置
type BreakerConfig struct {
	Enable           bool    `yaml:"enable" mapstructure:"enable"`
	Strategy         string  `yaml:"strategy" mapstructure:"strategy"` // "error_rate" | "slow_call" | "both"
	Threshold        float64 `yaml:"threshold" mapstructure:"threshold"`
	SlowCallRTT      string  `yaml:"slow_call_rtt" mapstructure:"slow_call_rtt"`
	SlowCallRatio    float64 `yaml:"slow_call_ratio" mapstructure:"slow_call_ratio"`
	RetryTimeoutMs   int     `yaml:"retry_timeout_ms" mapstructure:"retry_timeout_ms"`
	MinRequestAmount int64   `yaml:"min_request_amount" mapstructure:"min_request_amount"`
	StatIntervalMs   int     `yaml:"stat_interval_ms" mapstructure:"stat_interval_ms"`
}

// RetryConfig 重试配置
type RetryConfig struct {
	Enable         bool    `yaml:"enable" mapstructure:"enable"`
	MaxAttempts    int     `yaml:"max_attempts" mapstructure:"max_attempts"`
	InitialBackoff string  `yaml:"initial_backoff" mapstructure:"initial_backoff"`
	MaxBackoff     string  `yaml:"max_backoff" mapstructure:"max_backoff"`
	Multiplier     float64 `yaml:"multiplier" mapstructure:"multiplier"`
	Jitter         float64 `yaml:"jitter" mapstructure:"jitter"`
}

// TimeoutConfig 超时配置
type TimeoutConfig struct {
	Enable  bool   `yaml:"enable" mapstructure:"enable"`
	Default string `yaml:"default" mapstructure:"default"`
}

// LimiterConfig 限流配置
type LimiterConfig struct {
	Enable    bool    `yaml:"enable" mapstructure:"enable"`
	ClientQPS float64 `yaml:"client_qps" mapstructure:"client_qps"`
	ServerQPS float64 `yaml:"server_qps" mapstructure:"server_qps"`
}

// DefaultResilienceConfig 返回内置默认值
func DefaultResilienceConfig() ResilienceConfig {
	return ResilienceConfig{
		Breaker: BreakerConfig{
			Enable:           true,
			Strategy:         "both",
			Threshold:        0.5,
			SlowCallRTT:      "500ms",
			SlowCallRatio:    0.5,
			RetryTimeoutMs:   5000,
			MinRequestAmount: 10,
			StatIntervalMs:   1000,
		},
		Retry: RetryConfig{
			Enable:         true,
			MaxAttempts:    3,
			InitialBackoff: "50ms",
			MaxBackoff:     "500ms",
			Multiplier:     2.0,
			Jitter:         0.1,
		},
		Timeout: TimeoutConfig{
			Enable:  true,
			Default: "3s",
		},
		Limiter: LimiterConfig{
			Enable:    true,
			ClientQPS: 100,
			ServerQPS: 200,
		},
	}
}

// SlowCallRTTDuration 解析慢调用阈值
func (c *BreakerConfig) SlowCallRTTDuration() time.Duration {
	d, _ := time.ParseDuration(c.SlowCallRTT)
	return d
}

// InitialBackoffDuration 解析初始退避
func (c *RetryConfig) InitialBackoffDuration() time.Duration {
	d, _ := time.ParseDuration(c.InitialBackoff)
	return d
}

// MaxBackoffDuration 解析最大退避
func (c *RetryConfig) MaxBackoffDuration() time.Duration {
	d, _ := time.ParseDuration(c.MaxBackoff)
	return d
}

// DefaultDuration 解析默认超时
func (c *TimeoutConfig) DefaultDuration() time.Duration {
	d, _ := time.ParseDuration(c.Default)
	return d
}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./internal/config/ -v`
预期：PASS。

- [ ] **步骤 5：Commit**

```bash
git add internal/config/resilience.go internal/config/resilience_test.go
git commit -m "feat(config): add ResilienceConfig with defaults"
```

---

## 任务 3：新增 Prometheus 指标

**文件：**
- 修改：`internal/metrics/metrics.go`

- [ ] **步骤 1：在 metrics.go 末尾追加新指标**

在 `internal/metrics/metrics.go` 的 `var (...)` 块末尾追加：

```go
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
```

- [ ] **步骤 2：验证编译**

运行：`go build ./internal/metrics/`
预期：编译通过。

- [ ] **步骤 3：Commit**

```bash
git add internal/metrics/metrics.go
git commit -m "feat(metrics): add breaker/retry/ratelimit metrics"
```

---

## 任务 4：Sentinel 初始化与断路器（breaker.go）

**文件：**
- 创建：`internal/client/breaker.go`

- [ ] **步骤 1：编写 breaker.go**

创建 `internal/client/breaker.go`：

```go
package client

import (
	"strings"
	"sync"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/metrics"
)

var (
	sentinelOnce sync.Once
	sentinelErr  error
	breakerRules sync.Map // resource -> bool，防止重复注册
)

// InitSentinel 初始化 Sentinel 全局配置（进程级一次）
func InitSentinel() error {
	sentinelOnce.Do(func() {
		sentinelErr = sentinel.InitWithDefaultConfig()
		if sentinelErr == nil {
			circuitbreaker.RegisterStateChangeListeners(&breakerStateListener{})
		}
	})
	return sentinelErr
}

// ensureBreaker 为 resource=<svc>:<inst>:<method> 注册断路器规则（幂等）
func ensureBreaker(resource string, cfg config.BreakerConfig) {
	if !cfg.Enable {
		return
	}
	if _, loaded := breakerRules.LoadOrStore(resource, true); loaded {
		return
	}
	slowRTT := cfg.SlowCallRTTDuration()
	rules := make([]*circuitbreaker.Rule, 0, 2)
	if cfg.Strategy == "error_rate" || cfg.Strategy == "both" {
		rules = append(rules, &circuitbreaker.Rule{
			Resource:         resource,
			Strategy:         circuitbreaker.ErrorRatio,
			Threshold:        cfg.Threshold,
			RetryTimeoutMs:   uint32(cfg.RetryTimeoutMs),
			MinRequestAmount: cfg.MinRequestAmount,
			StatIntervalMs:   uint32(cfg.StatIntervalMs),
		})
	}
	if cfg.Strategy == "slow_call" || cfg.Strategy == "both" {
		rules = append(rules, &circuitbreaker.Rule{
			Resource:         resource,
			Strategy:         circuitbreaker.SlowRequestRatio,
			SlowCallRt:       uint64(slowRTT.Milliseconds()),
			Threshold:        cfg.SlowCallRatio,
			RetryTimeoutMs:   uint32(cfg.RetryTimeoutMs),
			MinRequestAmount: cfg.MinRequestAmount,
			StatIntervalMs:   uint32(cfg.StatIntervalMs),
		})
	}
	if _, err := circuitbreaker.LoadRules(rules); err != nil {
		logger.CommonLogger.Errorf("load breaker rules for %s: %v", resource, err)
	}
}

// parseResource 从 "logic:logic-1:InsertUserMessage" 解析出 service/instance/method
func parseResource(resource string) (service, instance, method string) {
	parts := strings.SplitN(resource, ":", 3)
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

// breakerStateListener 推送断路器状态变更到日志 + Prometheus
type breakerStateListener struct{}

func (l *breakerStateListener) OnTransformToClosed(prev circuitbreaker.State, rule *circuitbreaker.Rule) {
	svc, inst, mtd := parseResource(rule.Resource)
	logger.CommonLogger.Infof("circuit breaker CLOSED: %s", rule.Resource)
	metrics.GRPCCircuitBreakerState.WithLabelValues(svc, inst, mtd).Set(0)
}

func (l *breakerStateListener) OnTransformToOpen(prev circuitbreaker.State, rule *circuitbreaker.Rule, snapshot interface{}) {
	svc, inst, mtd := parseResource(rule.Resource)
	logger.CommonLogger.Warnf("circuit breaker OPEN: %s, snapshot: %v", rule.Resource, snapshot)
	metrics.GRPCCircuitBreakerState.WithLabelValues(svc, inst, mtd).Set(1)
}

func (l *breakerStateListener) OnTransformToHalfOpen(prev circuitbreaker.State, rule *circuitbreaker.Rule) {
	svc, inst, mtd := parseResource(rule.Resource)
	logger.CommonLogger.Infof("circuit breaker HALF_OPEN: %s", rule.Resource)
	metrics.GRPCCircuitBreakerState.WithLabelValues(svc, inst, mtd).Set(2)
}
```

- [ ] **步骤 2：验证编译**

运行：`go build ./internal/client/`
预期：编译通过。

- [ ] **步骤 3：Commit**

```bash
git add internal/client/breaker.go
git commit -m "feat(client): add sentinel breaker init and state listener"
```

---

## 任务 5：客户端限流器（limiter.go）

**文件：**
- 创建：`internal/client/limiter.go`

- [ ] **步骤 1：编写 limiter.go**

创建 `internal/client/limiter.go`：

```go
package client

import (
	"sync"

	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/klintcheng/kim/internal/logger"
)

var limiterRules sync.Map // resource -> bool，防止重复注册

// ensureLimiter 为 resource 注册限流规则（幂等）
// 被 limiterInterceptor 调用
func ensureLimiter(resource string, qps float64) {
	if _, loaded := limiterRules.LoadOrStore(resource, true); loaded {
		return
	}
	_, err := flow.LoadRules([]*flow.Rule{
		{
			Resource:               resource,
			Threshold:              qps,
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject,
		},
	})
	if err != nil {
		logger.CommonLogger.Errorf("load limiter rules for %s: %v", resource, err)
	}
}
```

- [ ] **步骤 2：验证编译**

运行：`go build ./internal/client/`
预期：编译通过。

- [ ] **步骤 3：Commit**

```bash
git add internal/client/limiter.go
git commit -m "feat(client): add ensureLimiter for client-side rate limiting"
```

---

## 任务 6：客户端拦截器（interceptor.go）

**文件：**
- 创建：`internal/client/interceptor.go`

- [ ] **步骤 1：编写 interceptor.go**

创建 `internal/client/interceptor.go`：

```go
package client

import (
	"context"
	"time"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// timeoutInterceptor per-RPC 超时
// 若 ctx 已有 deadline，不覆盖（让重试的总 deadline 生效）
func timeoutInterceptor(cfg config.TimeoutConfig) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if !cfg.Enable {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		if _, ok := ctx.Deadline(); ok {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		ctx, cancel := context.WithTimeout(ctx, cfg.DefaultDuration())
		defer cancel()
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// breakerInterceptor 断路器
// resource = serviceName:serviceID:method
func breakerInterceptor(cfg config.BreakerConfig, serviceName, serviceID string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if !cfg.Enable {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		resource := serviceName + ":" + serviceID + ":" + method
		ensureBreaker(resource, cfg)
		entry, err := sentinel.Entry(resource)
		if err != nil {
			// 断路器打开，返回 Unavailable 让上层 ResilientClient 换实例
			return status.Error(codes.Unavailable, "circuit breaker open: "+resource)
		}
		defer entry.Exit()
		// Sentinel 在 entry.Exit() 时自动统计 RTT 和错误
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// limiterInterceptor 客户端限流
// resource = client:serviceName:method（不区分实例，控制对下游的总 QPS）
func limiterInterceptor(cfg config.LimiterConfig, serviceName string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if !cfg.Enable {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		resource := "client:" + serviceName + ":" + method
		ensureLimiter(resource, cfg.ClientQPS)
		entry, err := sentinel.Entry(resource)
		if err != nil {
			metrics.GRPCRateLimitRejected.WithLabelValues("client", serviceName, method).Inc()
			return status.Error(codes.ResourceExhausted, "client rate limited: "+resource)
		}
		defer entry.Exit()
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// clientInterceptors 组装客户端拦截器链（供 Pool.refresh 使用）
// 顺序：timeout → breaker → limiter
func clientInterceptors(cfg config.ResilienceConfig, serviceName, serviceID string) []grpc.UnaryClientInterceptor {
	return []grpc.UnaryClientInterceptor{
		timeoutInterceptor(cfg.Timeout),
		breakerInterceptor(cfg.Breaker, serviceName, serviceID),
		limiterInterceptor(cfg.Limiter, serviceName),
	}
}

// 防止 time 包未使用警告（当 Timeout.Enable=false 时 time 仍被引用）
var _ = time.Second
```

- [ ] **步骤 2：验证编译**

运行：`go build ./internal/client/`
预期：编译通过。

- [ ] **步骤 3：Commit**

```bash
git add internal/client/interceptor.go
git commit -m "feat(client): add timeout/breaker/limiter interceptors"
```

---

## 任务 7：ResilientClient（resilient.go）

**文件：**
- 创建：`internal/client/resilient.go`
- 创建：`internal/client/resilient_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `internal/client/resilient_test.go`：

```go
package client

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeConn 构造一个无用的 *grpc.ClientConn（仅用于 InvokeFunc 签名）
func fakeConn() *grpc.ClientConn {
	return nil
}

func TestCall_Success(t *testing.T) {
	pool := &Pool{
		conns: map[string]*grpc.ClientConn{"inst-1": fakeConn()},
		rr:    newRoundRobin(),
	}
	cfg := config.DefaultResilienceConfig()
	cfg.Retry.InitialBackoff = "1ms"
	cfg.Retry.MaxBackoff = "1ms"
	rc := NewResilientClient(pool, "svc", cfg)

	calls := int32(0)
	invoke := func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return "ok", nil
	}

	resp, err := rc.Call(context.Background(), "Method", invoke)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "should not retry on success")
}

func TestCall_RetryableThenSuccess(t *testing.T) {
	pool := &Pool{
		conns: map[string]*grpc.ClientConn{"inst-1": fakeConn(), "inst-2": fakeConn()},
		rr:    newRoundRobin(),
	}
	cfg := config.DefaultResilienceConfig()
	cfg.Retry.InitialBackoff = "1ms"
	cfg.Retry.MaxBackoff = "1ms"
	rc := NewResilientClient(pool, "svc", cfg)

	calls := int32(0)
	invoke := func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return nil, status.Error(codes.Unavailable, "first fail")
		}
		return "ok", nil
	}

	resp, err := rc.Call(context.Background(), "Method", invoke)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "should retry once")
}

func TestCall_AllInstancesFail(t *testing.T) {
	pool := &Pool{
		conns: map[string]*grpc.ClientConn{"inst-1": fakeConn(), "inst-2": fakeConn()},
		rr:    newRoundRobin(),
	}
	cfg := config.DefaultResilienceConfig()
	cfg.Retry.InitialBackoff = "1ms"
	cfg.Retry.MaxBackoff = "1ms"
	cfg.Retry.MaxAttempts = 3
	rc := NewResilientClient(pool, "svc", cfg)

	calls := int32(0)
	invoke := func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return nil, status.Error(codes.Unavailable, "always fail")
	}

	_, err := rc.Call(context.Background(), "Method", invoke)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())
	// 2 个实例，MaxAttempts=3，所以最多调用 2 次（第 3 次无实例可选）
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestCall_NonRetryableError(t *testing.T) {
	pool := &Pool{
		conns: map[string]*grpc.ClientConn{"inst-1": fakeConn(), "inst-2": fakeConn()},
		rr:    newRoundRobin(),
	}
	cfg := config.DefaultResilienceConfig()
	rc := NewResilientClient(pool, "svc", cfg)

	calls := int32(0)
	invoke := func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return nil, status.Error(codes.InvalidArgument, "bad request")
	}

	_, err := rc.Call(context.Background(), "Method", invoke)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "should not retry on non-retryable error")
}

func TestCall_ContextCanceled(t *testing.T) {
	pool := &Pool{
		conns: map[string]*grpc.ClientConn{"inst-1": fakeConn()},
		rr:    newRoundRobin(),
	}
	cfg := config.DefaultResilienceConfig()
	rc := NewResilientClient(pool, "svc", cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	calls := int32(0)
	invoke := func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return nil, ctx.Err()
	}

	_, err := rc.Call(ctx, "Method", invoke)
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "should not retry on canceled ctx")
}

func TestCall_RetryDisabled(t *testing.T) {
	pool := &Pool{
		conns: map[string]*grpc.ClientConn{"inst-1": fakeConn()},
		rr:    newRoundRobin(),
	}
	cfg := config.DefaultResilienceConfig()
	cfg.Retry.Enable = false
	rc := NewResilientClient(pool, "svc", cfg)

	calls := int32(0)
	invoke := func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return nil, status.Error(codes.Unavailable, "fail")
	}

	_, err := rc.Call(context.Background(), "Method", invoke)
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "should not retry when disabled")
}

func TestCall_ExcludeTriedInstances(t *testing.T) {
	pool := &Pool{
		conns: map[string]*grpc.ClientConn{"inst-1": fakeConn(), "inst-2": fakeConn()},
		rr:    newRoundRobin(),
	}
	cfg := config.DefaultResilienceConfig()
	cfg.Retry.InitialBackoff = "1ms"
	cfg.Retry.MaxBackoff = "1ms"
	rc := NewResilientClient(pool, "svc", cfg)

	triedInstances := []string{}
	calls := int32(0)
	invoke := func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			triedInstances = append(triedInstances, "first")
			return nil, status.Error(codes.Unavailable, "fail")
		}
		triedInstances = append(triedInstances, "second")
		return "ok", nil
	}

	resp, err := rc.Call(context.Background(), "Method", invoke)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Len(t, triedInstances, 2, "should try 2 different instances")
}

func TestIsRetryable(t *testing.T) {
	ctx := context.Background()
	assert.True(t, isRetryable(status.Error(codes.Unavailable, ""), ctx))
	assert.True(t, isRetryable(status.Error(codes.DeadlineExceeded, ""), ctx))
	assert.False(t, isRetryable(status.Error(codes.InvalidArgument, ""), ctx))
	assert.False(t, isRetryable(status.Error(codes.ResourceExhausted, ""), ctx))
	assert.False(t, isRetryable(errors.New("non-grpc error"), ctx))

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, isRetryable(status.Error(codes.Unavailable, ""), canceledCtx))
}

func TestApplyJitter(t *testing.T) {
	base := 100 * time.Millisecond
	jittered := applyJitter(base, 0.1)
	// ±10% 范围内
	assert.True(t, jittered >= 90*time.Millisecond && jittered <= 110*time.Millisecond)

	// jitter=0 不变
	assert.Equal(t, base, applyJitter(base, 0))
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/client/ -run TestCall -v`
预期：FAIL，报错 `NewResilientClient undefined`。

- [ ] **步骤 3：编写实现**

创建 `internal/client/resilient.go`：

```go
package client

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// InvokeFunc 由调用方提供，执行实际的 gRPC 调用
type InvokeFunc func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error)

// ResilientClient 封装 Pool，提供带重试+fallback 的调用
type ResilientClient struct {
	pool        *Pool
	cfg         config.ResilienceConfig
	serviceName string
}

// NewResilientClient 创建 ResilientClient
func NewResilientClient(pool *Pool, serviceName string, cfg config.ResilienceConfig) *ResilientClient {
	return &ResilientClient{pool: pool, cfg: cfg, serviceName: serviceName}
}

// Call 执行一次带弹性的 gRPC 调用
func (c *ResilientClient) Call(ctx context.Context, method string, invoke InvokeFunc) (interface{}, error) {
	// 不启用重试：单次调用
	if !c.cfg.Retry.Enable {
		conn, err := c.pool.GetAny()
		if err != nil {
			return nil, err
		}
		return invoke(ctx, conn)
	}

	// 重试总 deadline（不超过 ctx 原有 deadline）
	retryCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		perCall := c.cfg.Timeout.DefaultDuration()
		total := perCall * time.Duration(c.cfg.Retry.MaxAttempts)
		var cancel context.CancelFunc
		retryCtx, cancel = context.WithTimeout(ctx, total)
		defer cancel()
	}

	initial := c.cfg.Retry.InitialBackoffDuration()
	maxBackoff := c.cfg.Retry.MaxBackoffDuration()

	var lastErr error
	tried := make(map[string]bool)
	for attempt := 1; attempt <= c.cfg.Retry.MaxAttempts; attempt++ {
		conn, serviceID, err := c.pool.GetAnyExcluding(tried)
		if err != nil {
			break // 所有实例都试过了
		}

		resp, err := invoke(retryCtx, conn)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		if !isRetryable(err, retryCtx) {
			break
		}
		tried[serviceID] = true

		if attempt < c.cfg.Retry.MaxAttempts {
			backoff := time.Duration(float64(initial) * math.Pow(c.cfg.Retry.Multiplier, float64(attempt-1)))
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			backoff = applyJitter(backoff, c.cfg.Retry.Jitter)
			select {
			case <-retryCtx.Done():
				return nil, retryCtx.Err()
			case <-time.After(backoff):
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available %s instance after retries", c.serviceName)
	}
	return nil, lastErr
}

// isRetryable 判断错误是否可重试
// 可重试：Unavailable（含熔断）、DeadlineExceeded（超时但 ctx 未取消）
// 不可重试：业务错误、Canceled、ResourceExhausted（限流不重试）
func isRetryable(err error, ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	}
	return false
}

// applyJitter 给 backoff 加 ±jitter% 的抖动
func applyJitter(d time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return d
	}
	delta := float64(d) * jitter
	offset := (rand.Float64()*2 - 1) * delta
	return time.Duration(float64(d) + offset)
}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./internal/client/ -run "TestCall|TestIsRetryable|TestApplyJitter" -v`
预期：PASS。

- [ ] **步骤 5：Commit**

```bash
git add internal/client/resilient.go internal/client/resilient_test.go
git commit -m "feat(client): add ResilientClient with retry and fallback"
```

---

## 任务 8：Pool 改造（挂载拦截器 + GetAnyExcluding）

**文件：**
- 修改：`internal/client/pool.go`

- [ ] **步骤 1：修改 Pool 结构体和 NewPool 签名**

在 `internal/client/pool.go` 中，修改 `Pool` 结构体：

```go
// Pool 管理对某类服务的 gRPC 连接，替代原 container 的 TCP client 管理
type Pool struct {
	naming      naming.Naming
	serviceName string
	cfg         config.ResilienceConfig // 新增
	mu          sync.RWMutex
	conns       map[string]*grpc.ClientConn
	rr          *roundRobin
}
```

修改 `NewPool`：

```go
// NewPool 创建连接池，监听指定服务的变更
func NewPool(ns naming.Naming, serviceName string, cfg config.ResilienceConfig) *Pool {
	p := &Pool{
		naming:      ns,
		serviceName: serviceName,
		cfg:         cfg,
		conns:       make(map[string]*grpc.ClientConn),
		rr:          newRoundRobin(),
	}
	// 初始化 Sentinel（进程级一次）
	_ = InitSentinel()
	if ns != nil {
		go p.watch()
	}
	return p
}
```

- [ ] **步骤 2：新增 GetAnyExcluding 方法**

在 `pool.go` 中 `GetAny` 方法后追加：

```go
// GetAnyExcluding 排除已试过的实例（供 ResilientClient 使用）
func (p *Pool) GetAnyExcluding(exclude map[string]bool) (*grpc.ClientConn, string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for id, conn := range p.conns {
		if exclude[id] {
			continue
		}
		return conn, id, nil
	}
	return nil, "", fmt.Errorf("no available %s instance (all excluded)", p.serviceName)
}
```

- [ ] **步骤 3：修改 refresh 方法挂载拦截器**

在 `refresh` 方法中，修改 `grpc.Dial` 调用：

```go
			conn, err := grpc.Dial(addr,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(10*1024*1024)),
				grpc.WithChainUnaryInterceptor(
					clientInterceptors(p.cfg, p.serviceName, id)...,
				),
			)
```

- [ ] **步骤 4：更新 pool_test.go**

修改 `internal/client/pool_test.go` 中的 `TestPoolGetEmpty`：

```go
func TestPoolGetEmpty(t *testing.T) {
	p := &Pool{
		conns: make(map[string]*grpc.ClientConn),
		rr:    newRoundRobin(),
	}
	_, err := p.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent service")
	}

	_, err = p.GetAny()
	if err == nil {
		t.Error("expected error for empty pool")
	}

	_, _, err = p.GetAnyExcluding(nil)
	if err == nil {
		t.Error("expected error for empty pool with GetAnyExcluding")
	}
}
```

- [ ] **步骤 5：验证编译**

运行：`go build ./internal/client/`
预期：编译通过。

- [ ] **步骤 6：运行测试**

运行：`go test ./internal/client/ -v`
预期：PASS（包括 resilient_test.go 和 pool_test.go）。

- [ ] **步骤 7：Commit**

```bash
git add internal/client/pool.go internal/client/pool_test.go
git commit -m "feat(client): mount interceptors on Pool and add GetAnyExcluding"
```

---

## 任务 9：服务端限流拦截器（limiter.go）

**文件：**
- 创建：`internal/server/limiter.go`

- [ ] **步骤 1：编写 limiter.go**

创建 `internal/server/limiter.go`：

```go
package server

import (
	"context"
	"sync"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var serverLimiterRules sync.Map

// ensureServerLimiter 为 resource 注册限流规则（幂等）
func ensureServerLimiter(resource string, qps float64) {
	if _, loaded := serverLimiterRules.LoadOrStore(resource, true); loaded {
		return
	}
	flow.LoadRules([]*flow.Rule{
		{
			Resource:               resource,
			Threshold:              qps,
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject,
		},
	})
}

// LimiterInterceptor 服务端入口限流
// resource = server:serviceName:method
func LimiterInterceptor(serviceName string, cfg config.LimiterConfig) UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (interface{}, error) {
		if !cfg.Enable {
			return handler(ctx, req)
		}
		resource := "server:" + serviceName + ":" + info.FullMethod
		ensureServerLimiter(resource, cfg.ServerQPS)
		entry, err := sentinel.Entry(resource)
		if err != nil {
			metrics.GRPCRateLimitRejected.WithLabelValues("server", serviceName, info.FullMethod).Inc()
			return nil, status.Error(codes.ResourceExhausted, "server rate limited: "+resource)
		}
		defer entry.Exit()
		return handler(ctx, req)
	}
}
```

- [ ] **步骤 2：验证编译**

运行：`go build ./internal/server/`
预期：编译通过。

- [ ] **步骤 3：Commit**

```bash
git add internal/server/limiter.go
git commit -m "feat(server): add LimiterInterceptor for server-side rate limiting"
```

---

## 任务 10：服务端拦截器链改造（grpc.go）

**文件：**
- 修改：`internal/server/grpc.go`

- [ ] **步骤 1：修改 options 结构体**

在 `internal/server/grpc.go` 中，修改 `options` 结构体：

```go
type options struct {
	serviceName   string
	resilienceCfg config.ResilienceConfig
}
```

新增 `WithResilience` 选项函数：

```go
// WithResilience 设置弹性配置（用于服务端限流）
func WithResilience(cfg config.ResilienceConfig) Option {
	return func(o *options) { o.resilienceCfg = cfg }
}
```

- [ ] **步骤 2：修改 NewGRPCServer**

修改 `NewGRPCServer` 函数：

```go
// NewGRPCServer 创建 gRPC server
func NewGRPCServer(addr string, opts ...Option) (*GRPCServer, error) {
	o := &options{resilienceCfg: config.DefaultResilienceConfig()}
	for _, opt := range opts {
		opt(o)
	}

	// 初始化 Sentinel（与 client 共用进程级实例）
	_ = client.InitSentinel()

	chain := UnaryChain(
		RecoveryInterceptor,
		LoggingInterceptor(o.serviceName),
		MetricsInterceptor(o.serviceName),
		LimiterInterceptor(o.serviceName, o.resilienceCfg.Limiter),
	)

	s := grpc.NewServer(
		grpc.UnaryInterceptor(grpc.UnaryServerInterceptor(chain)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
	)

	// gRPC Health Protocol
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s, hs)

	// 反射（grpcurl 调试用）
	reflection.Register(s)

	return &GRPCServer{Server: s, addr: addr}, nil
}
```

- [ ] **步骤 3：添加 import**

在 `internal/server/grpc.go` 的 import 块中添加：

```go
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/internal/config"
```

- [ ] **步骤 4：验证编译**

运行：`go build ./internal/server/`
预期：编译通过。

- [ ] **步骤 5：Commit**

```bash
git add internal/server/grpc.go
git commit -m "feat(server): integrate LimiterInterceptor into server chain"
```

---

## 任务 11：Comet 服务接入弹性套件

**文件：**
- 修改：`services/comet/config.go`
- 修改：`services/comet/server.go`
- 修改：`services/comet/service/logic_client.go`
- 修改：`services/comet/service/pusher.go`

- [ ] **步骤 1：修改 Comet Config**

在 `services/comet/config.go` 的 `Config` 结构体中添加字段：

```go
	Resilience config.ResilienceConfig `mapstructure:"resilience"`
```

在 `LoadConfig` 中添加默认值：

```go
	// 应用弹性默认配置（未配置的字段用默认值填充）
	defaults := config.DefaultResilienceConfig()
	if !cfg.Resilience.Breaker.Enable && cfg.Resilience.Breaker.Strategy == "" {
		cfg.Resilience = defaults
	}
```

- [ ] **步骤 2：修改 Comet Server**

在 `services/comet/server.go` 中：

1. 修改 `New` 函数中的 Pool 创建：

```go
	// 4. gRPC client pool
	logicPool := client.NewPool(ns, wire.SNService, cfg.Resilience) // "royal"
	gwPool := client.NewPool(ns, wire.SNWGateway, cfg.Resilience)   // "wgateway"

	// 5. service clients
	logicCli := service.NewLogicClient(logicPool, cfg.Resilience)
	pusher := service.NewGatewayPusher(gwPool, cfg.Resilience)
```

2. 修改 gRPC server 创建：

```go
	// 7. gRPC server
	grpcSrv, err := server.NewGRPCServer(cfg.Listen,
		server.WithServiceName("comet"),
		server.WithResilience(cfg.Resilience),
	)
```

- [ ] **步骤 3：修改 LogicClient**

在 `services/comet/service/logic_client.go` 中：

1. 修改 `LogicClient` 结构体：

```go
// LogicClient Logic 服务 gRPC 客户端，实现 Message / Group / User 接口
type LogicClient struct {
	resilient *client.ResilientClient
}

// NewLogicClient 创建 LogicClient
func NewLogicClient(pool *client.Pool, cfg config.ResilienceConfig) *LogicClient {
	return &LogicClient{resilient: client.NewResilientClient(pool, "logic", cfg)}
}
```

2. 添加 import：

```go
	"github.com/klintcheng/kim/internal/config"
```

3. 修改所有 13 个方法，以 `InsertUser` 为例（其余 12 个同样改造）：

```go
func (c *LogicClient) InsertUser(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	resp, err := c.resilient.Call(context.Background(), "InsertUserMessage",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).InsertUserMessage(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.InsertMessageResp), nil
}
```

完整方法清单（每个都按此模式改造）：
- `InsertUser` → method `"InsertUserMessage"`
- `InsertGroup` → method `"InsertGroupMessage"`
- `SetAck` → method `"AckMessage"`
- `GetMessageIndex` → method `"GetOfflineMessageIndex"`
- `GetMessageContent` → method `"GetOfflineMessageContent"`
- `Create` → method `"GroupCreate"`
- `Members` → method `"GroupMembers"`
- `Join` → method `"GroupJoin"`
- `Quit` → method `"GroupQuit"`
- `Detail` → method `"GroupGet"`
- `Login` → method `"Login"`

- [ ] **步骤 4：修改 GatewayPusher**

在 `services/comet/service/pusher.go` 中：

1. 修改 `GatewayPusher` 结构体：

```go
// GatewayPusher Gateway 消息推送器，实现 kim.Dispatcher 接口
type GatewayPusher struct {
	resilient *client.ResilientClient
}

// NewGatewayPusher 创建 GatewayPusher
func NewGatewayPusher(pool *client.Pool, cfg config.ResilienceConfig) *GatewayPusher {
	return &GatewayPusher{resilient: client.NewResilientClient(pool, "gateway", cfg)}
}
```

2. 修改 `Push` 方法（注意：原 Push 是按 gateway serviceID 精确调用，不是 round-robin。这里改为用 ResilientClient 的 Call 方法，但传入精确的 gateway ID 作为"method"的一部分，让熔断器按 gateway 实例区分。由于 Pusher 的语义是"推送到指定 gateway"，fallback 到其他 gateway 没有意义，所以这里不使用 ResilientClient 的 fallback，而是直接用 pool.Get + 拦截器）：

```go
// Push 将消息推送到指定 gateway 的多个 Channel
// 注意：Pusher 按精确 gateway serviceID 调用，不使用 ResilientClient 的 fallback
// 熔断/限流/超时由 Pool 的客户端拦截器提供
func (p *GatewayPusher) Push(gateway string, channels []string, packet *pkt.LogicPkt) error {
	conn, err := p.pool.Get(gateway)
	if err != nil {
		return err
	}
	packetBytes := pkt.Marshal(packet)
	cli := rpc.NewGatewayServiceClient(conn)
	_, err = cli.Push(context.Background(), &rpc.PushReq{
		ChannelIds: strings.Join(channels, ","),
		Packet:     packetBytes,
	})
	return err
}
```

保留 `pool` 字段（不用 resilient）：

```go
type GatewayPusher struct {
	pool *client.Pool
}

func NewGatewayPusher(pool *client.Pool, cfg config.ResilienceConfig) *GatewayPusher {
	_ = cfg // 配置通过 Pool 的拦截器生效
	return &GatewayPusher{pool: pool}
}
```

- [ ] **步骤 5：验证编译**

运行：`go build ./services/comet/...`
预期：编译通过。

- [ ] **步骤 6：Commit**

```bash
git add services/comet/
git commit -m "feat(comet): integrate resilience into LogicClient and Pusher"
```

---

## 任务 12：Gateway 服务接入弹性套件

**文件：**
- 修改：`services/gateway/config.go`
- 修改：`services/gateway/server.go`
- 修改：`services/gateway/forwarder.go`

- [ ] **步骤 1：修改 Gateway Config**

在 `services/gateway/config.go` 的 `Config` 结构体中添加字段：

```go
	Resilience config.ResilienceConfig `mapstructure:"resilience"`
```

在 `LoadConfig` 中添加默认值：

```go
	// 应用弹性默认配置
	defaults := config.DefaultResilienceConfig()
	if !cfg.Resilience.Breaker.Enable && cfg.Resilience.Breaker.Strategy == "" {
		cfg.Resilience = defaults
	}
```

添加 import：`"github.com/klintcheng/kim/internal/config"`

- [ ] **步骤 2：修改 CometForwarder**

在 `services/gateway/forwarder.go` 中：

1. 修改 `CometForwarder` 结构体，添加 `cfg` 字段：

```go
// CometForwarder Comet 转发器，封装对 CometService.Forward 的 gRPC 调用
type CometForwarder struct {
	ns        naming.Naming
	pool      *client.Pool
	selector  *serv.RouteSelection
	gatewayID string
	cfg       config.ResilienceConfig
}
```

2. 修改 `NewCometForwarder` 签名：

```go
// NewCometForwarder 创建 CometForwarder
func NewCometForwarder(ns naming.Naming, selector *serv.RouteSelection,
	gatewayID string, cfg config.ResilienceConfig) *CometForwarder {
	return &CometForwarder{
		ns:        ns,
		pool:      client.NewPool(ns, wire.SNChat, cfg), // "chat"
		selector:  selector,
		gatewayID: gatewayID,
		cfg:       cfg,
	}
}
```

3. `Forward` 方法保持不变（拦截器已通过 Pool 挂载，超时由 timeoutInterceptor 处理）：

```go
// Forward 转发消息到 Comet 服务（替代 container.Forward）
// 注意：Forwarder 不使用 ResilientClient（保留 selector 语义）
// 熔断/限流/超时由 Pool 的客户端拦截器提供
func (f *CometForwarder) Forward(p *pkt.LogicPkt) error {
	if p == nil || p.Command == "" || p.ChannelId == "" {
		return fmt.Errorf("invalid packet")
	}
	regs, err := f.ns.Find(wire.SNChat)
	if err != nil {
		return fmt.Errorf("find comet service: %w", err)
	}
	if len(regs) == 0 {
		return fmt.Errorf("no comet service found")
	}
	services := make([]kim.Service, len(regs))
	for i, r := range regs {
		services[i] = r
	}
	targetID := f.selector.Lookup(&p.Header, services)
	p.AddStringMeta(wire.MetaDestServer, f.gatewayID)
	conn, err := f.pool.Get(targetID)
	if err != nil {
		return err
	}
	cli := rpc.NewCometServiceClient(conn)
	_, err = cli.Forward(context.Background(), &rpc.ForwardReq{
		Packet: pkt.Marshal(p),
	})
	return err
}
```

添加 import：`"github.com/klintcheng/kim/internal/config"`

- [ ] **步骤 3：修改 Gateway Server**

在 `services/gateway/server.go` 中：

1. 修改 `NewCometForwarder` 调用：

```go
	// 4. gRPC forwarder（调 Comet）
	forwarder := NewCometForwarder(ns, selector, cfg.ServiceID, cfg.Resilience)
```

2. 修改 gRPC server 创建：

```go
	// 6. gRPC server（接收 Comet Push）
	grpcSrv, err := server.NewGRPCServer(cfg.GRPCListen,
		server.WithServiceName("gateway"),
		server.WithResilience(cfg.Resilience),
	)
```

- [ ] **步骤 4：验证编译**

运行：`go build ./services/gateway/...`
预期：编译通过。

- [ ] **步骤 5：Commit**

```bash
git add services/gateway/
git commit -m "feat(gateway): integrate resilience into Forwarder and server"
```

---

## 任务 13：配置文件接入

**文件：**
- 修改：`services/comet/conf.yaml`
- 修改：`services/gateway/conf.yaml`

- [ ] **步骤 1：Comet 配置文件追加 resilience 段**

在 `services/comet/conf.yaml` 末尾追加：

```yaml
# 弹性套件配置（不配置则使用代码默认值）
resilience:
  breaker:
    enable: true
    strategy: both
    threshold: 0.5
    slow_call_rtt: 500ms
    slow_call_ratio: 0.5
    retry_timeout_ms: 5000
    min_request_amount: 10
    stat_interval_ms: 1000
  retry:
    enable: true
    max_attempts: 3
    initial_backoff: 50ms
    max_backoff: 500ms
    multiplier: 2.0
    jitter: 0.1
  timeout:
    enable: true
    default: 3s
  limiter:
    enable: true
    client_qps: 100
    server_qps: 200
```

- [ ] **步骤 2：Gateway 配置文件追加 resilience 段**

在 `services/gateway/conf.yaml` 末尾追加相同的 `resilience` 段（同上）。

- [ ] **步骤 3：Commit**

```bash
git add services/comet/conf.yaml services/gateway/conf.yaml
git commit -m "chore: add resilience config to comet and gateway conf.yaml"
```

---

## 任务 14：全量编译与测试验证

**文件：** 无（验证任务）

- [ ] **步骤 1：全量编译**

运行：
```bash
cd /root/program/go/kim && go build ./...
```

预期：编译通过，无错误。

- [ ] **步骤 2：全量测试**

运行：
```bash
go test ./... -v -count=1
```

预期：所有测试 PASS。

- [ ] **步骤 3：验证 vet**

运行：
```bash
go vet ./...
```

预期：无警告。

- [ ] **步骤 4：启动服务验证（可选）**

运行：
```bash
make run-all && make status
```

预期：4 个服务正常启动。

- [ ] **步骤 5：Commit（如有修复）**

如果验证过程中发现并修复了问题：

```bash
git add -A
git commit -m "fix: resolve compilation/test issues from resilience integration"
```

---

## 自检清单

### 规格覆盖度

| 规格章节 | 对应任务 | 状态 |
|---|---|---|
| 3.1 配置结构 | 任务 2 | ✓ |
| 3.2 Sentinel 初始化 + breaker.go | 任务 4 | ✓ |
| 3.3 客户端拦截器 interceptor.go | 任务 6 | ✓ |
| 3.4 客户端限流器 limiter.go | 任务 5 | ✓ |
| 3.5 服务端限流拦截器 | 任务 9 | ✓ |
| 3.6 ResilientClient | 任务 7 | ✓ |
| 3.7 Pool 改造 | 任务 8 | ✓ |
| 3.8 服务端拦截器链改造 | 任务 10 | ✓ |
| 3.9 调用方改造（LogicClient/Pusher/Forwarder） | 任务 11、12 | ✓ |
| 7. 可观测性（Prometheus 指标） | 任务 3 | ✓ |
| 配置文件接入 | 任务 13 | ✓ |
| Sentinel-Go 依赖 | 任务 1 | ✓ |

### 占位符扫描

- 无 "TODO"、"待定"、"后续实现"
- 每个代码步骤都有完整代码块
- 每个测试步骤都有实际测试代码

### 类型一致性

- `NewResilientClient(pool, serviceName, cfg)` — 任务 7 定义，任务 11/12 使用 ✓
- `NewPool(ns, serviceName, cfg)` — 任务 8 定义，任务 11/12 使用 ✓
- `NewLogicClient(pool, cfg)` — 任务 11 定义 ✓
- `NewGatewayPusher(pool, cfg)` — 任务 11 定义 ✓
- `NewCometForwarder(ns, selector, gatewayID, cfg)` — 任务 12 定义 ✓
- `WithResilience(cfg)` — 任务 10 定义，任务 11/12 使用 ✓
- `GetAnyExcluding(exclude)` — 任务 8 定义，任务 7 使用 ✓
- `clientInterceptors(cfg, serviceName, serviceID)` — 任务 6 定义，任务 8 使用 ✓
