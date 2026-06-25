# trace 模块

## 模块概述
OpenTelemetry 链路追踪初始化模块，封装 TracerProvider 创建、OTLP/stdout/noop exporter 配置、采样率设置，提供统一的初始化入口和 shutdown 函数。

## 架构设计
模块通过 `InitTrace` 函数初始化全局 OpenTelemetry TracerProvider。支持三种 exporter 类型：OTLP gRPC（默认，发送到 Collector）、stdout（调试用，输出到控制台）、noop（空实现，不导出）。初始化时自动合并默认 Resource 与服务名属性，设置 `TraceIDRatioBased` 采样器，并配置 W3C TraceContext + Baggage 传播器。返回的 shutdown 函数在服务优雅关闭时调用，确保剩余 span 批量导出。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `InitTrace` | 函数 | 初始化全局 TracerProvider，返回 shutdown 函数 |
| `noopExporter` | 结构体 | 空 SpanExporter 实现，禁用追踪时使用 |

### Exporter 类型
| 类型 | 说明 |
|------|------|
| `otlp` | OTLP gRPC exporter（默认），发送到 OTLP Collector |
| `stdout` | 控制台 exporter，将 span 以 JSON 输出到 stdout（调试用） |
| `noop` | 空 exporter，丢弃所有 span |

## 核心接口

```go
// InitTrace 初始化全局 TracerProvider
// cfg.Enable=false 时返回 noop shutdown，不做任何操作
func InitTrace(serviceName string, cfg config.TraceConfig) (func(), error)

var ErrTraceDisabled = errors.New("trace disabled")
```

## 配置说明

### TraceConfig 字段
| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| Enable | bool | false | 是否启用链路追踪 |
| Exporter | string | "otlp" | 导出器类型：otlp/stdout/noop |
| Endpoint | string | "127.0.0.1:4317" | OTLP gRPC Collector 地址 |
| SamplingRatio | float64 | 0.1 | 采样率（0.0~1.0），≤0 时视为 1.0（全采样） |
| Insecure | bool | true | OTLP 是否使用明文（不使用 TLS） |

### OTLP gRPC Exporter
- 使用 `otlptracegrpc` 驱动
- 连接超时：5 秒
- Insecure=true 时使用 `insecure.NewCredentials()`
- 默认端点：`127.0.0.1:4317`（标准 OTLP gRPC 端口）

### 传播器配置
自动注册全局 TextMapPropagator，组合：
- `propagation.TraceContext{}` - W3C TraceContext（traceparent/tracestate header）
- `propagation.Baggage{}` - W3C Baggage（跨服务传递业务上下文）

### Shutdown 行为
- 创建 5 秒超时 context
- 调用 `tp.Shutdown(ctx)` 强制批量导出剩余 span
- 失败时记录 Warn 日志，不返回错误

## 使用示例

### 服务启动时初始化追踪
```go
import (
    "github.com/klintcheng/kim/internal/trace"
    "github.com/klintcheng/kim/internal/config"
)

func main() {
    // 配置（通常来自 config.Load）
    traceCfg := config.TraceConfig{
        Enable:        true,
        Exporter:      "otlp",
        Endpoint:      "127.0.0.1:4317",
        SamplingRatio: 0.1,
        Insecure:      true,
    }

    // 初始化链路追踪
    shutdown, err := trace.InitTrace("gateway", traceCfg)
    if err != nil {
        panic(err)
    }
    defer shutdown() // 服务关闭时 flush 剩余 span

    // ... 启动服务 ...
}
```

### 开发环境调试（输出到控制台）
```go
traceCfg := config.TraceConfig{
    Enable:   true,
    Exporter: "stdout", // 直接输出到控制台，无需 Collector
}
shutdown, _ := trace.InitTrace("comet", traceCfg)
defer shutdown()
```

### 禁用追踪（默认行为）
```go
// Enable=false 时 InitTrace 直接返回空 shutdown，无开销
shutdown, _ := trace.InitTrace("logic", config.DefaultTraceConfig())
defer shutdown()
```

### gRPC 自动集成
trace 模块本身只负责 TracerProvider 初始化。gRPC 客户端和服务端的自动埋点由以下拦截器/StatsHandler 完成：
- 客户端：`otelgrpc.NewClientHandler()`（在 `internal/client/pool.go` 中自动配置）
- 服务端：`otelgrpc.UnaryServerInterceptor()` + `otelgrpc.NewServerHandler()`（在 `internal/server/grpc.go` 中自动配置）

无需手动在业务代码中创建 span，gRPC 调用会自动生成 span 并传播 trace context。

## 依赖关系
- `go.opentelemetry.io/otel` - OpenTelemetry API
- `go.opentelemetry.io/otel/sdk/trace` - TracerProvider SDK
- `go.opentelemetry.io/otel/sdk/resource` - Resource 检测
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` - OTLP gRPC exporter
- `go.opentelemetry.io/otel/exporters/stdout/stdouttrace` - stdout exporter
- `go.opentelemetry.io/otel/semconv/v1.26.0` - 语义约定
- `google.golang.org/grpc` - gRPC（用于 OTLP 连接）
- `github.com/klintcheng/kim/internal/config` - TraceConfig 配置结构
- `github.com/klintcheng/kim/internal/logger` - 日志
