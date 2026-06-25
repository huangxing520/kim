# client 模块

## 模块概述
带弹性防护的 gRPC 客户端连接池，支持 Consul 服务自动发现、round-robin 负载均衡、自动重连，内置断路器、限流、超时、重试、链路追踪等拦截器链，以及基于 Sentinel 的弹性防护能力。

## 架构设计
连接池 `Pool` 维护到某个服务所有实例的 gRPC 连接 map，通过订阅 Naming 服务的变更事件（后台 goroutine watch）自动新增/移除连接。负载均衡采用简单的 round-robin 轮询策略。`ResilientClient` 在连接池之上封装重试逻辑，重试时自动切换到不同实例以避免故障节点。拦截器链按顺序为：OpenTelemetry tracing → 超时设置 → 弹性防护（断路器 + 限流一次 Entry 完成）。断路器和限流均基于 Alibaba Sentinel 实现，支持按 `service:instance:method` 粒度配置规则。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `Pool` | 结构体 | gRPC 连接池，管理多个服务实例的连接，支持自动服务发现 |
| `ResilientClient` | 结构体 | 弹性客户端，封装重试逻辑（指数退避+抖动）、fallback 机制 |
| `roundRobin` | 结构体 | round-robin 轮询负载均衡器 |
| `InvokeFunc` | 类型 | RPC 调用函数签名，由弹性客户端注入 conn |

### 拦截器链顺序
```
otelgrpc.UnaryClientInterceptor()  →  trace（最外层，让内部错误也落入 span）
    ↓
timeoutInterceptor()               →  默认超时（若 ctx 未设置 deadline）
    ↓
resilienceInterceptor()            →  断路器 + 限流（一次 Sentinel Entry 同时检查）
```

## 核心接口

```go
// 创建连接池（使用默认配置）
func NewPool(ns naming.Naming, serviceName string) *Pool

// 创建连接池（自定义配置）
func NewPoolWithConfig(ns naming.Naming, serviceName string, cfg config.ResilienceConfig, grpcCfg config.GRPCConfig) *Pool

// 按 serviceID 获取指定连接
func (p *Pool) Get(serviceID string) (*grpc.ClientConn, error)

// 轮询获取任意一个可用连接
func (p *Pool) GetAny() (*grpc.ClientConn, error)

// 轮询获取连接并返回 serviceID
func (p *Pool) GetAnyWithID() (string, *grpc.ClientConn, error)

// 排除指定实例后获取连接（重试时换实例）
func (p *Pool) GetAnyExcluding(excludeID string) (*grpc.ClientConn, error)

// 获取该服务的客户端拦截器链
func (p *Pool) Interceptors(instanceID string) []grpc.UnaryClientInterceptor

// 关闭连接池（取消订阅 + 关闭所有连接）
func (p *Pool) Close()

// 创建弹性客户端
func NewResilientClient(pool *Pool, serviceName string, cfg config.ResilienceConfig) *ResilientClient

// 执行带重试的 RPC 调用
func (c *ResilientClient) Call(ctx context.Context, method string, invoke InvokeFunc) (interface{}, error)

// 执行主调用，失败时执行 fallback
func (c *ResilientClient) CallWithFallback(ctx context.Context, method string, primary, fallback InvokeFunc) (interface{}, error)

// 初始化 Sentinel（进程级一次）
func InitSentinel() error
```

## 配置说明

### 连接池特性
- **自动服务发现**：当 ns 不为 nil 时，启动后台 goroutine 订阅服务变更，自动刷新连接
- **TLS 支持**：通过 GRPCConfig 配置 mTLS（CA 证书 + 客户端证书）
- **消息大小**：最大接收 10MB
- **链路追踪**：自动集成 OpenTelemetry stats handler
- **写缓冲区**：Channel 写缓冲区 32（抗突发流量）

### 重试策略
- 重试时自动通过 `GetAnyExcluding` 切换实例
- 不可重试错误直接返回（context 取消/超时、InvalidArgument、PermissionDenied、NotFound 等）
- 指数退避：`initial * multiplier^(attempt-1)`，上限 maxBackoff
- 抖动：`[0, jitter * backoff]` 随机值添加到退避时间
- 每次重试前检查 context 是否取消

### 断路器策略
- 粒度：`service:instance:method`
- 支持策略：error_rate（错误率）、slow_call（慢调用比例）、both（两者）
- 状态变更自动推送到日志 + Prometheus 指标
- 规则注册幂等，防止重复注册

### 限流策略
- 粒度：`service:instance:method`（客户端）
- 策略：Direct 令牌计算 + Reject 直接拒绝
- 规则注册幂等

## 使用示例

### 创建连接池并使用弹性客户端
```go
import (
    "github.com/klintcheng/kim/internal/client"
    "github.com/klintcheng/kim/internal/config"
    "github.com/klintcheng/kim/internal/naming"
)

// 1. 初始化 Consul 命名服务
ns, _ := naming.NewNaming("127.0.0.1:8500")

// 2. 初始化 Sentinel
_ = client.InitSentinel()

// 3. 创建连接池
pool := client.NewPool(ns, "logic")
defer pool.Close()

// 4. 创建弹性客户端
resilient := client.NewResilientClient(pool, "logic", config.DefaultResilienceConfig())

// 5. 执行 RPC 调用
resp, err := resilient.Call(ctx, "InsertUserMessage", func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
    return rpc.NewLogicServiceClient(conn).InsertUserMessage(ctx, req)
})
```

### 直接使用连接池
```go
pool := client.NewPool(ns, "gateway")

// 轮询获取连接
conn, err := pool.GetAny()
if err != nil {
    return err
}
// 使用 conn 进行 RPC 调用
```

### CallWithFallback 使用
```go
result, err := resilient.CallWithFallback(ctx, "Push",
    // 主调用
    func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
        return client.Push(ctx, conn, req)
    },
    // 降级调用（主调用失败时执行）
    func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
        return client.PushToBackup(ctx, conn, req)
    },
)
```

## 依赖关系
- `google.golang.org/grpc` - gRPC 核心库
- `github.com/alibaba/sentinel-golang` - 断路器/限流
- `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` - gRPC tracing
- `github.com/klintcheng/kim/internal/naming` - 服务发现
- `github.com/klintcheng/kim/internal/config` - 配置结构
- `github.com/klintcheng/kim/internal/logger` - 日志
- `github.com/klintcheng/kim/internal/metrics` - Prometheus 指标
