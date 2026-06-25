# server 模块

## 模块概述
gRPC 服务器封装模块，内置拦截器链（recover、日志、指标、限流、认证）、健康检查、TLS 支持、keepalive、gRPC reflection，以及独立的 HTTP 监控端点（健康检查 + Prometheus metrics）。

## 架构设计
`GRPCServer` 封装了 `*grpc.Server`，通过函数式选项模式（Option）配置服务名、限流、TLS、认证密钥等。服务器启动时自动构建拦截器链：Recovery → OpenTelemetry → Logging → Metrics → Limiter → (可选 Auth)。同时提供独立的 HTTP `ServeMux` 用于暴露健康检查（`/health`、`/health/live`、`/health/ready`）和 Prometheus metrics（`/metrics`）端点。健康状态通过 atomic.Bool 管理，支持就绪探针（`SetReady`/`SetNotReady`）与 Consul 服务注册联动。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `GRPCServer` | 结构体 | gRPC 服务器封装，含健康检查、就绪状态管理 |
| `UnaryInterceptor` | 类型 | 一元拦截器函数类型 |
| `Option` | 类型 | 函数式选项类型 |

### 内置拦截器
| 拦截器 | 作用 |
|--------|------|
| `RecoveryInterceptor` | 捕获 panic，记录调用栈，返回 Internal 错误 |
| `otelgrpc.UnaryServerInterceptor` | OpenTelemetry 链路追踪 |
| `LoggingInterceptor` | 记录请求方法、状态码、耗时 |
| `MetricsInterceptor` | 采集 Prometheus 请求总数和耗时直方图 |
| `LimiterInterceptor` | 服务端 QPS 限流（基于 Sentinel） |
| `AuthInterceptor` | Token 认证（可选，通过 WithAuthSecret 启用） |

### 拦截器顺序
```
RecoveryInterceptor  →  最外层，保证 panic 被捕获
    ↓
otelgrpc             →  trace
    ↓
LoggingInterceptor   →  日志
    ↓
MetricsInterceptor   →  指标
    ↓
LimiterInterceptor   →  限流
    ↓
AuthInterceptor      →  认证（可选）
    ↓
业务 Handler
```

## 核心接口

```go
// 创建 gRPC 服务器
func NewGRPCServer(addr string, opts ...Option) (*GRPCServer, error)

// 选项函数
func WithServiceName(name string) Option
func WithLimiter(cfg config.LimiterConfig) Option
func WithGRPCConfig(cfg config.GRPCConfig) Option
func WithAuthSecret(secret string) Option

// 就绪状态管理
func (s *GRPCServer) SetReady()
func (s *GRPCServer) SetNotReady()
func (s *GRPCServer) IsReady() bool

// 启动服务（阻塞）
func (s *GRPCServer) Start() error

// 拦截器链组合
func UnaryChain(interceptors ...UnaryInterceptor) UnaryInterceptor

// HTTP 监控端点
func NewMonitorMux(grpcSrv *GRPCServer) *http.ServeMux
func StartMonitorHTTP(addr string) error
func StartMonitorHTTPWithReady(addr string, grpcSrv *GRPCServer) error
```

## 配置说明

### gRPC 服务器默认配置
- **Keepalive**：30 秒 ping，10 秒超时
- **最大消息大小**：默认（客户端连接池设为 10MB 接收）
- **Health Server**：自动注册 `grpc.health.v1.Health`

### TLS 配置
通过 `GRPCConfig` 配置：
- 支持单向 TLS（服务端证书）
- 支持 mTLS（配置 CAFile 时启用客户端证书验证）
- 最低 TLS 版本：TLS 1.2

### Auth 认证
- 通过 `WithAuthSecret(secret)` 启用
- 从 metadata `authorization` 头提取 Token
- 支持 `Bearer ` 前缀
- 使用 `wire/token.Parse` 验证
- **白名单路径**（不认证）：
  - `/grpc.health.v1.Health/Check`
  - `/rpc.LogicService/Login`

### 服务端限流
- 粒度：`serviceName:method`
- 默认 QPS：200（可通过 LimiterConfig 配置）
- 策略：Direct + Reject

### 健康检查端点
| 路径 | 说明 |
|------|------|
| `/health` | 始终返回 200 OK（存活探针） |
| `/health/live` | 始终返回 200 OK（存活探针） |
| `/health/ready` | 根据 `IsReady()` 返回 200/503（就绪探针） |
| `/metrics` | Prometheus metrics |

## 使用示例

### 创建并启动 gRPC 服务器
```go
import (
    "github.com/klintcheng/kim/internal/server"
    "github.com/klintcheng/kim/internal/config"
)

grpcSrv, err := server.NewGRPCServer(":9001",
    server.WithServiceName("gateway"),
    server.WithLimiter(config.DefaultResilienceConfig().Limiter),
    server.WithGRPCConfig(config.GRPCConfig{Reflection: true}),
)
if err != nil {
    panic(err)
}

// 注册 gRPC 服务
// rpc.RegisterGatewayServiceServer(grpcSrv.Server, impl)

// 启动监控 HTTP（独立端口）
go func() {
    if err := server.StartMonitorHTTPWithReady(":8000", grpcSrv); err != nil {
        log.Errorf("monitor http: %v", err)
    }
}()

// 标记就绪（在服务完全初始化后）
grpcSrv.SetReady()

// 启动 gRPC 服务（阻塞）
if err := grpcSrv.Start(); err != nil {
    log.Errorf("grpc serve: %v", err)
}
```

### 优雅关闭
```go
// 标记未就绪（从负载均衡摘除）
grpcSrv.SetNotReady()

// 等待一段时间让存量请求处理完成
time.Sleep(5 * time.Second)

// GracefulStop
grpcSrv.GracefulStop()
```

## 依赖关系
- `google.golang.org/grpc` - gRPC 核心库
- `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` - tracing
- `github.com/prometheus/client_golang/prometheus/promhttp` - Prometheus HTTP handler
- `google.golang.org/grpc/health` - gRPC 健康检查协议
- `google.golang.org/grpc/reflection` - gRPC reflection
- `github.com/alibaba/sentinel-golang` - 限流
- `github.com/klintcheng/kim/internal/config` - 配置
- `github.com/klintcheng/kim/internal/logger` - 日志
- `github.com/klintcheng/kim/internal/metrics` - Prometheus 指标
- `github.com/klintcheng/kim/wire/token` - Token 解析
