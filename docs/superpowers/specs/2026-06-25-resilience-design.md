# kim 项目弹性套件设计方案（熔断 + 重试 + 超时 + 限流 + Fallback）

> **版本：** v1.0
> **日期：** 2026-06-25
> **状态：** 已批准（待规格自检）
> **依赖：** [2026-06-25-grpc-refactor-design.md](./2026-06-25-grpc-refactor-design.md)（gRPC 重构基线）

---

## 1. 背景与目标

### 1.1 当前问题

gRPC 重构（[2026-06-25-grpc-refactor-design.md](./2026-06-25-grpc-refactor-design.md)）已建立 gRPC 通信基线，但服务间调用仍缺乏弹性能力：

1. **无熔断**：[services/comet/service/logic_client.go](../../services/comet/service/logic_client.go) 13 个方法都是裸 `cli.XXX(ctx, req)`，下游实例故障时调用方会持续重试，引发雪崩。
2. **无重试**：瞬时故障（网络抖动、实例重启）直接返回错误，IM 场景下用户体验差。
3. **无超时**：所有调用用 `context.Background()`，下游变慢时调用方 goroutine 堆积。
4. **无限流**：上游突发流量可能打挂下游服务。
5. **无 Fallback**：单实例故障即整体失败，未利用 pool 中其他健康实例。

### 1.2 设计目标

在 gRPC 重构基线上，引入 **Sentinel-Go** 弹性套件，覆盖：

- **熔断**：实例级，错误率 + 慢调用双策略，打开后隔离故障实例
- **重试**：对瞬时故障自动重试，指数退避 + 抖动
- **超时**：per-RPC 超时，重试总时长受 deadline 约束
- **限流**：客户端 + 服务端双向限流
- **Fallback**：熔断某实例后自动换 pool 中其他实例重试

### 1.3 关键决策

| 决策点 | 选择 | 理由 |
|---|---|---|
| 弹性库 | Sentinel-Go | 用户指定；熔断+限流+热点一体化 |
| 熔断粒度 | 实例级，resource=`<svc>:<serviceID>:<method>` | 故障实例隔离，与 pool round-robin 配合 |
| 熔断策略 | 错误率 + 慢调用双断路器 | 覆盖"挂了"和"变慢"两种故障 |
| 重试位置 | ResilientClient 层（拦截器外） | 需访问 pool 多实例做 fallback |
| 重试触发 | `codes.Unavailable` / `codes.DeadlineExceeded` | 瞬时故障，业务错误不重试 |
| 超时 | per-RPC，重试共享总 deadline | 避免重试雪崩 |
| 限流 | 客户端 + 服务端双向 | 客户端保护下游，服务端保护自己 |
| Fallback | 熔断/失败自动换实例 | 最大化可用性 |
| 配置 | 代码默认 + YAML 覆盖 | 不配也能跑 |
| 实现方案 | 方案 C：分层混合 | 熔断/限流用拦截器，重试/fallback 用 ResilientClient |

---

## 2. 总体架构

### 2.1 架构图

```
┌─────────────────────────────────────────────────────────────┐
│  调用方（LogicClient / CometForwarder / Pusher）              │
│    resilient.Call(ctx, "logic", "InsertUserMessage", req)   │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  internal/client/resilient.go  ← 新增                        │
│  ResilientClient（封装 Pool）                                 │
│    1. 选实例（round-robin）                                    │
│    2. 调用 gRPC（已挂客户端拦截器链）                           │
│    3. 失败/熔断 → 换下一个实例重试（受总 deadline 约束）         │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  gRPC 客户端拦截器链（在 Pool.grpc.Dial 时挂载）                │
│  顺序：timeout → breaker → limiter → call                    │
│    - timeout:  per-RPC context.WithTimeout                   │
│    - breaker:  sentinel.Entry(resource=svc:inst:method)      │
│    - limiter:  sentinel.Flow(resource=svc:method)            │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  gRPC 服务端（已挂服务端拦截器链）                              │
│  顺序：recovery → logging → metrics → limiter → handler      │
│    - limiter:  sentinel.Flow(resource=server:svc:method)     │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 方案选型对比

| 方案 | 描述 | 优点 | 缺点 | 结论 |
|---|---|---|---|---|
| A. 纯 gRPC 拦截器 | 熔断/限流/重试全塞进拦截器 | 横切集中，调用方零改动 | 拦截器拿不到 pool，fallback 必须外溢；重试要自己造轮子 | ✗ |
| B. LogicClient/Forwarder 手动封装 | 每个调用点手动 `sentinel.Entry()` | 逻辑集中 | 13+ 方法样板代码，违反 DRY；服务端限流无处安放 | ✗ |
| **C. 分层混合** | 熔断+限流用拦截器，重试+fallback 用 ResilientClient | 横切集中 + fallback 自然 + 调用方零改动 | 多一层抽象 | **✓ 采用** |

### 2.3 改动范围

**新增文件（6 个）：**

| 文件 | 职责 |
|---|---|
| `internal/client/resilient.go` | ResilientClient：重试 + fallback + 超时编排 |
| `internal/client/breaker.go` | Sentinel 断路器初始化 + 客户端断路器拦截器 |
| `internal/client/limiter.go` | Sentinel 限流器初始化 + 客户端限流拦截器 |
| `internal/client/interceptor.go` | 客户端拦截器链组装 + timeout 拦截器 |
| `internal/server/limiter.go` | 服务端限流拦截器 |
| `internal/config/resilience.go` | ResilienceConfig 配置结构 + 默认值 |

**修改文件（6 个）：**

| 文件 | 改动 |
|---|---|
| `internal/client/pool.go` | `grpc.Dial` 挂载客户端拦截器链；新增 `GetAnyExcluding`；Pool 持有 `cfg` |
| `internal/server/grpc.go` | 服务端拦截器链加入 `LimiterInterceptor` |
| `services/comet/service/logic_client.go` | `pool.GetAny()` → `resilient.Call()` |
| `services/gateway/forwarder.go` | 只挂拦截器，**不用 ResilientClient**（保留 selector 语义） |
| `services/comet/service/pusher.go` | `pool.GetAny()` → `resilient.Call()` |
| `go.mod` | 引入 `github.com/alibaba/sentinel-golang` |

**配置文件（4 个）：**

各服务 `conf.yaml` 新增 `resilience` 段：
- `services/gateway/conf.yaml`
- `services/comet/conf.yaml`
- `services/logic/conf.yaml`
- `services/router/conf.yaml`（仅服务端限流）

**不改动：**
- proto 定义、客户端协议、服务名常量、业务 handler 逻辑、`wire/` 包

---

## 3. 组件细节

### 3.1 配置结构 `internal/config/resilience.go`

```go
package config

import "time"

// ResilienceConfig 弹性套件配置（代码默认 + YAML 覆盖）
type ResilienceConfig struct {
    Breaker BreakerConfig `yaml:"breaker"`
    Retry   RetryConfig   `yaml:"retry"`
    Timeout TimeoutConfig `yaml:"timeout"`
    Limiter LimiterConfig `yaml:"limiter"`
}

type BreakerConfig struct {
    Enable           bool    `yaml:"enable"`             // 默认 true
    Strategy         string  `yaml:"strategy"`           // "error_rate" | "slow_call" | "both"，默认 "both"
    Threshold        float64 `yaml:"threshold"`          // 错误率阈值，默认 0.5
    SlowCallRTT      string  `yaml:"slow_call_rtt"`      // 慢调用阈值，默认 "500ms"
    SlowCallRatio    float64 `yaml:"slow_call_ratio"`    // 慢调用占比阈值，默认 0.5
    RetryTimeoutMs   int     `yaml:"retry_timeout_ms"`   // 打开后探测间隔，默认 5000
    MinRequestAmount int64   `yaml:"min_request_amount"` // 最小样本数，默认 10
    StatIntervalMs   int     `yaml:"stat_interval_ms"`   // 统计窗口，默认 1000
}

type RetryConfig struct {
    Enable         bool    `yaml:"enable"`          // 默认 true
    MaxAttempts    int     `yaml:"max_attempts"`    // 默认 3（含首次）
    InitialBackoff string  `yaml:"initial_backoff"` // 默认 "50ms"
    MaxBackoff     string  `yaml:"max_backoff"`     // 默认 "500ms"
    Multiplier     float64 `yaml:"multiplier"`      // 默认 2.0
    Jitter         float64 `yaml:"jitter"`          // 默认 0.1（±10%）
}

type TimeoutConfig struct {
    Enable  bool   `yaml:"enable"`   // 默认 true
    Default string `yaml:"default"`  // 默认 "3s"
}

type LimiterConfig struct {
    Enable    bool    `yaml:"enable"`      // 默认 true
    ClientQPS float64 `yaml:"client_qps"`  // 客户端每方法 QPS，默认 100
    ServerQPS float64 `yaml:"server_qps"`  // 服务端每方法 QPS，默认 200
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

// Parse 辅助方法
func (c *BreakerConfig) SlowCallRTTDuration() time.Duration {
    d, _ := time.ParseDuration(c.SlowCallRTT)
    return d
}

func (c *RetryConfig) InitialBackoffDuration() time.Duration {
    d, _ := time.ParseDuration(c.InitialBackoff)
    return d
}

func (c *RetryConfig) MaxBackoffDuration() time.Duration {
    d, _ := time.ParseDuration(c.MaxBackoff)
    return d
}

func (c *TimeoutConfig) DefaultDuration() time.Duration {
    d, _ := time.ParseDuration(c.Default)
    return d
}
```

**YAML 示例（`services/comet/conf.yaml`）：**

```yaml
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

### 3.2 Sentinel 初始化 `internal/client/breaker.go`

```go
package client

import (
    "sync"
    "time"

    sentinel "github.com/alibaba/sentinel-golang/api"
    "github.com/alibaba/sentinel-golang/core/circuitbreaker"
    "github.com/klintcheng/kim/internal/config"
    "github.com/klintcheng/kim/internal/logger"
)

var (
    sentinelOnce sync.Once
    breakerRules sync.Map // resource -> bool，防止重复注册
)

// InitSentinel 初始化 Sentinel 全局配置（进程级一次）
func InitSentinel() error {
    var err error
    sentinelOnce.Do(func() {
        err = sentinel.InitWithDefaultConfig()
        if err == nil {
            // 注册断路器状态变更监听器，推到日志 + Prometheus
            circuitbreaker.RegisterStateChangeListeners(&breakerStateListener{})
        }
    })
    return err
}

// ensureBreaker 为 resource=<svc>:<inst>:<method> 注册断路器规则（幂等）
func ensureBreaker(resource string, cfg config.BreakerConfig) {
    if !cfg.Enable {
        return
    }
    if _, loaded := breakerRules.LoadOrStore(resource, true); loaded {
        return // 已注册
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

// breakerStateListener 推送断路器状态变更到日志 + Prometheus
type breakerStateListener struct{}

// parseResource 从 "logic:logic-1:InsertUserMessage" 解析出 service/instance/method
func parseResource(resource string) (service, instance, method string) {
    parts := strings.SplitN(resource, ":", 3)
    if len(parts) != 3 {
        return "", "", ""
    }
    return parts[0], parts[1], parts[2]
}

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

### 3.3 客户端拦截器 `internal/client/interceptor.go`

```go
package client

import (
    "context"
    "time"

    sentinel "github.com/alibaba/sentinel-golang/api"
    "github.com/klintcheng/kim/internal/config"
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
        // Sentinel 在 entry.Exit() 时自动统计 RTT 和错误，无需手动计时/TraceError
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
            return status.Error(codes.ResourceExhausted, "client rate limited: "+resource)
        }
        defer entry.Exit()
        return invoker(ctx, method, req, reply, cc, opts...)
    }
}

// ensureLimiter 定义在 internal/client/limiter.go（见 3.4 节）
```

### 3.4 客户端限流器 `internal/client/limiter.go`

```go
package client

import (
    "sync"

    "github.com/alibaba/sentinel-golang/core/flow"
    "github.com/klintcheng/kim/internal/logger"
)

// ensureLimiter 为 resource 注册限流规则（幂等）
// 被 limiterInterceptor 调用
var limiterRules sync.Map

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

> **注意**：3.3 节 `interceptor.go` 中的 `limiterInterceptor` 调用的 `ensureLimiter` 定义在本节（`limiter.go`）。

### 3.5 服务端限流拦截器 `internal/server/limiter.go`

```go
package server

import (
    "context"

    sentinel "github.com/alibaba/sentinel-golang/api"
    "github.com/alibaba/sentinel-golang/core/flow"
    "github.com/klintcheng/kim/internal/config"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

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
            return nil, status.Error(codes.ResourceExhausted, "server rate limited: "+resource)
        }
        defer entry.Exit()
        return handler(ctx, req)
    }
}

var serverLimiterRules sync.Map

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
```

### 3.6 ResilientClient `internal/client/resilient.go`

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

// ResilientClient 封装 Pool，提供带重试+fallback 的调用
type ResilientClient struct {
    pool        *Pool
    cfg         config.ResilienceConfig
    serviceName string
}

func NewResilientClient(pool *Pool, serviceName string, cfg config.ResilienceConfig) *ResilientClient {
    return &ResilientClient{pool: pool, cfg: cfg, serviceName: serviceName}
}

// InvokeFunc 由调用方提供，执行实际的 gRPC 调用
type InvokeFunc func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error)

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

### 3.7 Pool 改造 `internal/client/pool.go`

```go
// 新增字段
type Pool struct {
    // ... 原有字段 ...
    cfg          config.ResilienceConfig
    serviceName  string
}

// NewPool 签名变更：传入 cfg
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

// 新增方法：GetAnyExcluding 排除已试过的实例
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

// refresh 改造：grpc.Dial 挂载客户端拦截器链
func (p *Pool) refresh() {
    // ... 原有查找 services 逻辑 ...
    for _, svc := range services {
        id := svc.ServiceID()
        currentIDs[id] = true
        if _, exists := p.conns[id]; !exists {
            addr := fmt.Sprintf("%s:%d", svc.PublicAddress(), svc.PublicPort())
            conn, err := grpc.Dial(addr,
                grpc.WithTransportCredentials(insecure.NewCredentials()),
                grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(10*1024*1024)),
                grpc.WithChainUnaryInterceptor(
                    timeoutInterceptor(p.cfg.Timeout),
                    breakerInterceptor(p.cfg.Breaker, p.serviceName, id),
                    limiterInterceptor(p.cfg.Limiter, p.serviceName),
                ),
            )
            // ...
        }
    }
    // ... 原有清理逻辑 ...
}
```

### 3.8 服务端拦截器链改造 `internal/server/grpc.go`

```go
type options struct {
    serviceName    string
    resilienceCfg  config.ResilienceConfig
}

func WithResilience(cfg config.ResilienceConfig) Option {
    return func(o *options) { o.resilienceCfg = cfg }
}

// NewGRPCServer 改造
func NewGRPCServer(addr string, opts ...Option) (*GRPCServer, error) {
    o := &options{resilienceCfg: config.DefaultResilienceConfig()}
    for _, opt := range opts {
        opt(o)
    }

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
    // ... 原有 health + reflection ...
}
```

### 3.9 调用方改造

**[services/comet/service/logic_client.go](../../services/comet/service/logic_client.go)：**

```go
type LogicClient struct {
    resilient *client.ResilientClient
}

func NewLogicClient(pool *client.Pool, cfg config.ResilienceConfig) *LogicClient {
    return &LogicClient{resilient: client.NewResilientClient(pool, "logic", cfg)}
}

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
// ... 其余 12 个方法同样改造 ...
```

**[services/gateway/forwarder.go](../../services/gateway/forwarder.go)：**

```go
// CometForwarder 新增 cfg 字段
type CometForwarder struct {
    ns        naming.Naming
    pool      *client.Pool
    selector  *serv.RouteSelection
    gatewayID string
    cfg       config.ResilienceConfig // 新增
}

// NewCometForwarder 签名变更：传入 cfg
func NewCometForwarder(ns naming.Naming, selector *serv.RouteSelection,
    gatewayID string, cfg config.ResilienceConfig) *CometForwarder {
    return &CometForwarder{
        ns:        ns,
        pool:      client.NewPool(ns, wire.SNChat, cfg), // Pool 签名也变了
        selector:  selector,
        gatewayID: gatewayID,
        cfg:       cfg,
    }
}

// Forwarder 不用 ResilientClient（保留 selector 语义）
// 只通过 Pool 的客户端拦截器获得熔断+限流+超时
// 失败直接返回错误，让上层（handler）决定是否重试
// 注意：超时由 timeoutInterceptor 自动处理（ctx 无 deadline 时加默认超时）
func (f *CometForwarder) Forward(p *pkt.LogicPkt) error {
    // ... 原有 selector.Lookup 逻辑保留 ...
    conn, err := f.pool.Get(targetID)
    if err != nil {
        return err
    }
    cli := rpc.NewCometServiceClient(conn)
    _, err = cli.Forward(context.Background(), &rpc.ForwardReq{Packet: pkt.Marshal(p)})
    return err
}
```

**[services/comet/service/pusher.go](../../services/comet/service/pusher.go)：**

```go
type Pusher struct {
    resilient *client.ResilientClient
}

func (p *Pusher) Push(channelID string, packet []byte) error {
    _, err := p.resilient.Call(context.Background(), "Push",
        func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
            return rpc.NewGatewayServiceClient(conn).Push(ctx, &rpc.PushReq{
                ChannelIds: channelID,
                Packet:     packet,
            })
        })
    return err
}
```

---

## 4. 数据流

### 4.1 正常调用数据流

以 Comet 调用 `LogicService.InsertUserMessage` 为例：

```
Comet handler
  │
  ▼
LogicClient.InsertUser()
  │
  ▼
ResilientClient.Call(ctx, "InsertUserMessage", invoke)
  │  attempt=1: pool.GetAnyExcluding(∅) → conn[logic-1]
  │   │
  │   ▼ invoke(ctx, conn[logic-1])
  │   │
  │   ▼ gRPC 客户端拦截器链
  │   │  1. timeoutInterceptor: ctx.WithTimeout(3s)
  │   │  2. breakerInterceptor: sentinel.Entry("logic:logic-1:InsertUserMessage") ✓
  │   │  3. limiterInterceptor: sentinel.Entry("client:logic:InsertUserMessage") ✓
  │   │  4. invoker → Logic 服务端
  │   │
  │   ▼ 服务端拦截器链
  │   │  1. RecoveryInterceptor
  │   │  2. LoggingInterceptor
  │   │  3. MetricsInterceptor
  │   │  4. LimiterInterceptor: sentinel.Entry("server:logic:/rpc.LogicService/InsertUserMessage") ✓
  │   │  5. handler → MySQL 写入
  │   │
  │   ◄── resp 返回，entry.Exit()，指标 +1
  │
  ◄── resp 返回给 Comet handler
```

### 4.2 熔断触发数据流

假设 `logic-1` 实例 DB 挂了，所有调用返回 `Unavailable`：

```
attempt=1: conn[logic-1]
  breakerInterceptor: sentinel.Entry ✓（断路器仍 Closed）
  invoker → Unavailable
  entry.Exit() 时 Sentinel 自动计入错误
  return Unavailable

isRetryable(Unavailable) = true
tried = {logic-1: true}
backoff 50ms ± 10%

attempt=2: pool.GetAnyExcluding({logic-1}) → conn[logic-2]
  breakerInterceptor: sentinel.Entry("logic:logic-2:InsertUserMessage") ✓
  invoker → 成功
  return resp ✓

（logic-1 的断路器统计窗口内错误率 > 50%，达到 minRequestAmount=10 后打开）
```

后续对 `logic-1` 的调用：

```
attempt=1: conn[logic-1]  // round-robin 又选到它
  breakerInterceptor: sentinel.Entry("logic:logic-1:InsertUserMessage") ✗
    → 断路器已 Open，返回 Unavailable("circuit breaker open")
  return Unavailable（未真正发起 gRPC 调用）

isRetryable = true
tried = {logic-1: true}

attempt=2: conn[logic-2] → 成功
```

5 秒后（`RetryTimeoutMs`）断路器进入 Half-Open，放一个探测请求：

```
breakerInterceptor: sentinel.Entry ✓（Half-Open，允许 1 个探测）
  invoker → 成功 → 断路器 Closed，恢复正常
  invoker → 失败 → 断路器重新 Open，再等 5s
```

### 4.3 慢调用熔断数据流

`logic-1` DB 变慢，每个请求 800ms（> `SlowCallRTT=500ms`）：

```
invoker → 成功（但耗时 800ms）
breakerInterceptor 的 SlowRequestRatio 统计：
  慢调用数 / 总调用数 > 0.5（达到 minRequestAmount=10）
  → 断路器 Open

后续调用直接返回 Unavailable，避免继续打慢 logic-1
```

### 4.4 限流触发数据流

**客户端限流**（Comet → Logic，Logic QPS 超 100）：

```
limiterInterceptor: sentinel.Entry("client:logic:InsertUserMessage") ✗
  → 返回 ResourceExhausted("client rate limited")

isRetryable(ResourceExhausted) = false  // 限流不重试，立即失败
return ResourceExhausted 给上层
```

**服务端限流**（Logic 收到的 QPS 超 200）：

```
服务端 LimiterInterceptor: sentinel.Entry("server:logic:/rpc.LogicService/InsertUserMessage") ✗
  → 返回 ResourceExhausted("server rate limited")

客户端收到 ResourceExhausted
isRetryable = false  // 不重试
```

---

## 5. 错误处理

### 5.1 错误处理矩阵

| 错误场景 | gRPC Code | 拦截器行为 | ResilientClient 行为 | 上层看到 |
|---|---|---|---|---|
| 断路器打开 | Unavailable | 直接返回，不调用 | 换实例重试 | 重试全部熔断则返回 Unavailable |
| 实例连接失败 | Unavailable | 计入断路器 | 换实例重试 | 同上 |
| 超时 | DeadlineExceeded | 计入断路器 | 换实例重试 | 重试全部超时则返回 DeadlineExceeded |
| 客户端限流 | ResourceExhausted | 直接返回 | **不重试**，立即返回 | ResourceExhausted |
| 服务端限流 | ResourceExhausted | 服务端拦截器返回 | **不重试**，立即返回 | ResourceExhausted |
| 业务错误（InvalidArgument 等） | 各种 | 计入断路器 | **不重试**，立即返回 | 原始业务错误 |
| ctx 被取消 | Canceled | invoker 返回 | **不重试**，立即返回 | Canceled |
| panic | Internal | RecoveryInterceptor 捕获 | 不重试（非 Unavailable） | Internal |

### 5.2 关键不变式

1. **重试总时长受 deadline 约束**：`retryCtx` 的 deadline = min(ctx 原 deadline, 默认超时 × maxAttempts)。任何 attempt 超过 deadline 立即终止。
2. **fallback 不破坏 selector 语义**：Forwarder 不用 ResilientClient，只挂拦截器。LogicClient/Pusher 用 ResilientClient（这些场景本来就是 round-robin）。
3. **熔断器 resource name 唯一性**：`<serviceName>:<serviceID>:<method>`，serviceID 来自 Consul 注册的 ServiceID，全局唯一。
4. **限流不重试**：ResourceExhausted 立即返回，避免限流场景下重试加剧拥塞。
5. **业务错误不重试**：InvalidArgument/NotFound/PermissionDenied 等立即返回，避免无意义重试。

---

## 6. 测试策略

### 6.1 单元测试

**`internal/client/resilient_test.go`：**

| 测试用例 | 验证点 |
|---|---|
| `TestCall_Success` | 首次成功直接返回，不重试 |
| `TestCall_RetryableThenSuccess` | 第 1 次 Unavailable，第 2 次成功，验证重试 |
| `TestCall_AllInstancesFail` | 所有实例都 Unavailable，验证返回 lastErr |
| `TestCall_NonRetryableError` | 返回 InvalidArgument，验证不重试 |
| `TestCall_ContextCanceled` | ctx 取消，验证不重试 |
| `TestCall_RetryBudgetExhausted` | MaxAttempts=3，验证最多调用 3 次 |
| `TestCall_BackoffRespectsDeadline` | 总时长不超过 deadline |
| `TestCall_ExcludeTriedInstances` | 验证 tried map 生效，不重复选同一实例 |
| `TestCall_RetryDisabled` | cfg.Retry.Enable=false，单次调用 |

**`internal/client/breaker_test.go`：**

| 测试用例 | 验证点 |
|---|---|
| `TestBreaker_OpenOnHighErrorRate` | 10 次中 6 次 Unavailable，断路器打开 |
| `TestBreaker_OpenOnSlowCall` | 10 次中 6 次 >500ms，断路器打开 |
| `TestBreaker_HalfOpenRecovery` | Open → 等待 RetryTimeout → Half-Open → 探测成功 → Closed |
| `TestBreaker_HalfOpenFail` | Open → Half-Open → 探测失败 → 重新 Open |
| `TestBreaker_Disabled` | cfg.Enable=false，断路器不生效 |
| `TestBreaker_ResourceIsolation` | logic-1 熔断不影响 logic-2 |

**`internal/client/limiter_test.go`：**

| 测试用例 | 验证点 |
|---|---|
| `TestLimiter_AllowUnderQPS` | QPS 内的请求全部通过 |
| `TestLimiter_RejectOverQPS` | 超过 QPS 的请求返回 ResourceExhausted |
| `TestLimiter_Disabled` | cfg.Enable=false，不限流 |

**`internal/server/limiter_test.go`：**

| 测试用例 | 验证点 |
|---|---|
| `TestServerLimiter_RejectOverQPS` | 服务端超 QPS 返回 ResourceExhausted |

### 6.2 集成测试

**`internal/client/resilient_integration_test.go`：**

用 `google.golang.org/grpc/test/bufconn` 启动内存 gRPC server，模拟故障：

| 测试场景 | 实现 |
|---|---|
| 单实例正常调用 | bufconn server 正常响应 |
| 单实例熔断后恢复 | server 前N次返回 Unavailable，之后正常 |
| 多实例 fallback | 启动 2 个 bufconn server，第 1 个挂掉，验证自动切到第 2 个 |
| 慢调用熔断 | server 故意 sleep 600ms，验证慢调用断路器打开 |

### 6.3 测试辅助

```go
// internal/client/testhelper.go

// FakePool 构造可控的 *grpc.ClientConn 列表
func FakePool(conns map[string]*grpc.ClientConn) *Pool { ... }

// ErrorInvoker 返回指定错误
func ErrorInvoker(err error) InvokeFunc { ... }

// SlowInvoker sleep 指定时长后返回
func SlowInvoker(d time.Duration) InvokeFunc { ... }

// CountingInvoker 计数调用次数
func CountingInvoker(counter *int) InvokeFunc { ... }
```

---

## 7. 可观测性

### 7.1 新增 Prometheus 指标

```go
// internal/metrics/metrics.go 新增
var (
    GRPCCircuitBreakerState = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "grpc_breaker_state",
            Help: "Circuit breaker state: 0=Closed, 1=Open, 2=HalfOpen",
        },
        []string{"service", "instance", "method"},
    )

    GRPCRetryTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "grpc_retry_total",
            Help: "Total retry attempts",
        },
        []string{"service", "method", "reason"},
    )  // reason=unavailable/deadline_exceeded

    GRPCRateLimitRejected = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "grpc_ratelimit_rejected_total",
            Help: "Total rate-limited requests",
        },
        []string{"side", "service", "method"},  // side=client/server
    )
)
```

### 7.2 日志

通过 Sentinel 的 `circuitbreaker.RegisterStateChangeListeners` 把断路器状态变化推到 Zap 日志：

- `Closed → Open`：WARN 级别，含 resource name 和 snapshot
- `Open → HalfOpen`：INFO 级别
- `HalfOpen → Closed`：INFO 级别
- `HalfOpen → Open`：WARN 级别

---

## 8. 实施顺序

建议按依赖关系分阶段实施：

1. **阶段 1：基础设施**
   - `internal/config/resilience.go` + 默认值
   - `internal/metrics/metrics.go` 新增指标
   - `go.mod` 引入 Sentinel-Go

2. **阶段 2：客户端拦截器**
   - `internal/client/breaker.go`
   - `internal/client/limiter.go`
   - `internal/client/interceptor.go`
   - `internal/client/pool.go` 改造（挂拦截器 + GetAnyExcluding）

3. **阶段 3：ResilientClient**
   - `internal/client/resilient.go`
   - `internal/client/testhelper.go`
   - 单元测试

4. **阶段 4：服务端限流**
   - `internal/server/limiter.go`
   - `internal/server/grpc.go` 改造

5. **阶段 5：调用方接入**
   - `services/comet/service/logic_client.go` 改造
   - `services/comet/service/pusher.go` 改造
   - `services/gateway/forwarder.go` 改造（仅挂拦截器）

6. **阶段 6：配置接入**
   - 各服务 `conf.yaml` 增加 `resilience` 段
   - 各服务 `config.go` 加载 ResilienceConfig

7. **阶段 7：集成测试 + 验证**
   - bufconn 集成测试
   - `make test` 全量通过
   - `make run-all` 启动验证

---

## 9. 不在本次范围

以下能力**不在本次设计范围**，避免 YAGNI：

- **链路追踪**（OpenTelemetry）：gRPC 重构设计已声明"无 OTel"
- **热点参数限流**（按 user_id/message_id 限流）：需 Sentinel 参数限流，复杂度高
- **自适应限流**（基于系统负载）：Sentinel 自适应限流，需更多调参
- **熔断后的降级返回值**（如返回缓存/默认值）：IM 场景下业务错误更合适
- **熔断规则动态下发**（如通过配置中心）：当前用代码默认 + YAML 覆盖
- **Forwarder 的 fallback**：保留 selector 语义，失败让上层处理

---

## 10. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| Sentinel 全局状态污染测试 | 测试间相互影响 | 测试用独立 resource name；`IsolationTest` 用子进程 |
| 断路器规则重复注册 | 内存泄漏 | `sync.Map` LoadOrStore 防重复 |
| 重试加剧拥塞 | 故障放大 | 限流不重试；总 deadline 约束；MaxAttempts=3 上限 |
| 慢调用阈值不合理 | 误熔断或漏熔断 | 默认 500ms 可配置；minRequestAmount=10 避免小样本误判 |
| Forwarder 无 fallback | 单实例故障即失败 | selector 已有重试机制（原设计）；本次不破坏 |
| Sentinel 与 gRPC 拦截器兼容性 | 拦截器 panic | RecoveryInterceptor 兜底；集成测试覆盖 |

---

## 附录 A：Sentinel-Go 关键 API 参考

```go
// 初始化
sentinel.InitWithDefaultConfig()

// 断路器规则
circuitbreaker.LoadRules([]*circuitbreaker.Rule{...})

// 限流规则
flow.LoadRules([]*flow.Rule{...})

// Entry/Exit
entry, err := sentinel.Entry(resource)
defer entry.Exit()
sentinel.TraceError(entry, err)

// 状态监听
circuitbreaker.RegisterStateChangeListeners(listener)
```

## 附录 B：resource name 命名规范

| 类型 | 格式 | 示例 |
|---|---|---|
| 客户端断路器 | `<svc>:<serviceID>:<method>` | `logic:logic-1:InsertUserMessage` |
| 客户端限流 | `client:<svc>:<method>` | `client:logic:InsertUserMessage` |
| 服务端限流 | `server:<svc>:<fullMethod>` | `server:logic:/rpc.LogicService/InsertUserMessage` |
