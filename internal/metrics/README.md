# metrics 模块

## 模块概述
Prometheus 指标定义模块，使用 `promauto` 自动注册 gRPC 相关的监控指标，包括请求总数、处理耗时、断路器状态、重试次数、限流拒绝次数。

## 架构设计
模块通过 Go `init()` 机制在包导入时自动注册所有指标到 Prometheus 默认 Registry。所有指标使用 `kim_` 命名空间前缀，通过 `promauto.NewCounterVec`/`NewHistogramVec`/`NewGaugeVec` 创建并自动注册，无需手动调用 `prometheus.MustRegister`。指标在 gRPC 拦截器（server 端和 client 端）、断路器状态监听器、重试逻辑中被采集。

## 关键组件

| 指标名 | 类型 | Labels | 说明 |
|--------|------|--------|------|
| `kim_grpc_server_handled_total` | CounterVec | `service`, `method`, `code` | gRPC server 处理的请求总数 |
| `kim_grpc_server_handling_seconds` | HistogramVec | `service`, `method` | gRPC server 处理耗时（秒） |
| `kim_grpc_breaker_state` | GaugeVec | `service`, `instance`, `method` | 断路器状态：0=Closed, 1=Open, 2=HalfOpen |
| `kim_grpc_retry_total` | CounterVec | `service`, `method`, `reason` | gRPC 重试次数 |
| `kim_grpc_ratelimit_rejected_total` | CounterVec | `side`, `service`, `method` | 限流拒绝次数（side: client/server） |

## 核心接口

本模块无对外暴露的函数接口，仅通过包级变量暴露指标供其他模块使用。

```go
var (
    GRPCServerHandledTotal     *prometheus.CounterVec
    GRPCServerHandlingSeconds  *prometheus.HistogramVec
    GRPCCircuitBreakerState    *prometheus.GaugeVec
    GRPCRetryTotal             *prometheus.CounterVec
    GRPCRateLimitRejected      *prometheus.CounterVec
)
```

## 配置说明

### 指标标签说明

#### `kim_grpc_server_handled_total`
- `service`：服务名（如 "gateway"、"logic"、"comet"）
- `method`：gRPC 方法全路径（如 "/rpc.LogicService/InsertUserMessage"）
- `code`：gRPC 状态码字符串（如 "OK"、"Internal"、"Unauthenticated"）

#### `kim_grpc_server_handling_seconds`
- `service`：服务名
- `method`：gRPC 方法全路径
- 使用默认 Histogram buckets（适合一般 RPC 耗时分布）

#### `kim_grpc_breaker_state`
- `service`：服务名
- `instance`：实例 ID（如 "logic-1"）
- `method`：短方法名（如 "InsertUserMessage"）
- 取值：
  - `0` - Closed（正常，请求通过）
  - `1` - Open（熔断，请求被拒绝）
  - `2` - HalfOpen（半开，试探性放少量请求）

#### `kim_grpc_retry_total`
- `service`：目标服务名
- `method`：短方法名
- `reason`：重试原因（如 "retry"）

#### `kim_grpc_ratelimit_rejected_total`
- `side`：限流侧，"client" 或 "server"
- `service`：服务名
- `method`：短方法名

## 使用示例

### 在 HTTP handler 中暴露 metrics 端点
```go
import (
    "net/http"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    _ "github.com/klintcheng/kim/internal/metrics" // 导入以触发指标注册
)

func main() {
    http.Handle("/metrics", promhttp.Handler())
    http.ListenAndServe(":9090", nil)
}
```

注意：server 模块的 `NewMonitorMux` 已经内置了 `/metrics` 端点处理器，无需额外配置。

### 在自定义代码中使用指标
```go
import "github.com/klintcheng/kim/internal/metrics"

// 计数
metrics.GRPCRetryTotal.WithLabelValues("logic", "InsertUserMessage", "retry").Inc()

// 观察耗时
start := time.Now()
// ... 处理请求 ...
metrics.GRPCServerHandlingSeconds.WithLabelValues(
    "gateway", "/rpc.GatewayService/Push",
).Observe(time.Since(start).Seconds())

// 设置 Gauge
metrics.GRPCCircuitBreakerState.WithLabelValues("logic", "logic-1", "Push").Set(1)
```

## 依赖关系
- `github.com/prometheus/client_golang/prometheus` - Prometheus 客户端库
- `github.com/prometheus/client_golang/prometheus/promauto` - 自动注册工具

### 被依赖关系
- `internal/server` - MetricsInterceptor 采集服务端指标
- `internal/client` - 拦截器和弹性客户端采集重试、限流、断路器指标
